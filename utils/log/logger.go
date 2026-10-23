// Package log is the framework's preconfigured logging, built on the
// standard library's [log/slog].
//
// There is no global logger. Callers obtain a ready-to-use [*slog.Logger] for
// their component by name via GetLogger; the name is the logger's identity
// and becomes the "logger" field on every line. SetLevel and SetDefaultLevel
// change levels on the fly — the effective level is re-checked for every
// record, so no logger needs to be re-fetched.
//
// This package is a general-purpose logging foundation: it knows nothing
// about HTTP or request correlation. Correlation fields (request id, etc.)
// are attached by callers that have them, using the usual [*slog.Logger].With.
//
// Output goes to two streams: a human-readable text stream on stdout and a
// JSON stream to a file (default logs/mjolnir.jsonl) for Victorialogs and
// other exporters. Line-of-code attribution is on by default. The package
// does not redact fields; callers decide what is safe to log.
//
// Sink failures never fail the caller. If the JSON stream cannot be opened or
// written, logging continues (at least on the text stream) and the failure is
// reported to the handler installed by SetSinkErrorHandler. With no handler
// installed, the first failure is warned once on stderr. Applications can use
// the hook to record a metric or surface the failure; the package itself does
// not tie logging to readiness.
//
// The shared state (the two sink destinations, the sink-error reporter, and
// the per-name level table) lives in a [core]. The package holds one default
// core; a logger returned by GetLogger is a cheap value handle onto it, so
// callers obtain one per function rather than caching it at package scope.
// Tests build isolated cores, which is what lets them exercise the machinery
// in parallel.
package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	// DefaultJSONPath is where the JSON stream is written unless
	// SetJSONFile changes it.
	DefaultJSONPath = "logs/mjolnir.jsonl"

	// DefaultLevel is the minimum level for names that have no level of
	// their own.
	DefaultLevel = slog.LevelInfo
)

// defaultCore is the process-wide state every GetLogger handle points at: the
// two sink destinations, the sink-error reporter, and the per-name level
// table. It must be process-wide, hence package-level.
//
//nolint:gochecknoglobals // the shared logging state; the only global this package holds, and it is synchronized
var defaultCore = newDefaultCore()

// newDefaultCore returns the process-wide core: text on stdout, JSON to
// DefaultJSONPath (opened lazily, reported through the core's own sink-error
// reporter).
func newDefaultCore() *core {
	c := newCore(os.Stdout, io.Discard)
	c.json.set(newLazyFile(DefaultJSONPath, c.sinkErrors))
	return c
}

// core holds the two sink destinations, the sink-error reporter, and the
// per-name level table. The package keeps one default core; tests construct
// isolated ones, so the stateful machinery can be exercised in parallel
// without touching process-wide state.
type core struct {
	text *swappableWriter
	json *swappableWriter

	// sinkErrors receives JSON sink failures for this core. lazyFile holds the
	// same reporter, so retargeting the handler reaches an already-open sink.
	sinkErrors *sinkErrorReporter

	mu           sync.RWMutex
	nameLevels   map[string]slog.Level
	defaultLevel slog.Level
}

// newCore returns a core writing text and JSON to the given destinations, with
// an empty level table at DefaultLevel and a default sink-error reporter.
func newCore(text, json io.Writer) *core {
	return newCoreWithSinkErrors(text, json, newSinkErrorReporter(nil))
}

// newCoreWithSinkErrors is newCore with an explicit sink-error reporter, so
// tests can capture the default warning instead of writing to stderr.
func newCoreWithSinkErrors(text, json io.Writer, sinkErrors *sinkErrorReporter) *core {
	return &core{
		text:         &swappableWriter{w: text},
		json:         &swappableWriter{w: json},
		sinkErrors:   sinkErrors,
		nameLevels:   make(map[string]slog.Level),
		defaultLevel: DefaultLevel,
	}
}

// GetLogger returns a ready-to-use [*slog.Logger] for the named component.
// Every line it emits carries the "logger" field set to name and the source
// file:line of the call. The name is an open identifier chosen by the caller,
// not a closed set: define a const for it if you want typo safety. It is both
// the "logger" field and the key for SetLevel. The level in effect for name is
// checked for every record, so SetLevel and SetDefaultLevel take effect on the
// next emitted line without re-fetching the logger — including for
// With()-derived loggers.
//
// The logger writes to the process-wide sinks (text on stdout, JSON to the
// configured file), so it is affected by SetJSONFile and UseOutputForTests.
// It is cheap to construct, so obtain it per function rather than caching it
// at package scope.
func GetLogger(name string) *slog.Logger {
	return defaultCore.logger(name)
}

// logger builds the dual-handler logger for name bound to this core.
func (c *core) logger(name string) *slog.Logger {
	return newLogger(name, c.text, c.json, nameLeveler{core: c, name: name})
}

// GetLoggerTo returns a [*slog.Logger] for the named component that writes
// both streams to w. Unlike GetLogger it is isolated from the process-wide
// sinks: it does not read from, write to, or interfere with the default
// destinations, SetJSONFile, or UseOutputForTests. That makes it safe to use
// from many goroutines at once — it is the seam tests should prefer for
// capture — and it is suitable for embedding a logger into a specific
// destination.
//
// The logger still honours the level currently in effect for name.
func GetLoggerTo(name string, w io.Writer) *slog.Logger {
	return defaultCore.loggerTo(name, w)
}

// loggerTo is GetLoggerTo bound to this core, so it honours this core's level
// table rather than the default one.
func (c *core) loggerTo(name string, w io.Writer) *slog.Logger {
	sw := &swappableWriter{w: w}
	return newLogger(name, sw, sw, nameLeveler{core: c, name: name})
}

// newLogger builds the dual-handler (text + JSON) logger for name, writing to
// the given destinations with source attribution enabled. The level is read
// through lvl for every record.
func newLogger(name string, textDest, jsonDest io.Writer, lvl slog.Leveler) *slog.Logger {
	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     lvl,
	}
	return slog.New(slog.NewMultiHandler(
		slog.NewTextHandler(textDest, opts),
		slog.NewJSONHandler(jsonDest, opts),
	)).With("logger", name)
}

// SetLevel sets the minimum level for name. The change takes effect on the
// next record emitted by any logger of name. Setting a level for a name that
// no GetLogger call has made yet records it; loggers later obtained for that
// name emit at that level.
func SetLevel(name string, lvl slog.Level) {
	defaultCore.setLevel(name, lvl)
}

func (c *core) setLevel(name string, lvl slog.Level) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nameLevels[name] = lvl
}

// SetDefaultLevel sets the minimum level used by names that have no level of
// their own. It takes effect on the next record for every such name.
func SetDefaultLevel(lvl slog.Level) {
	defaultCore.setDefaultLevel(lvl)
}

func (c *core) setDefaultLevel(lvl slog.Level) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultLevel = lvl
}

// SetJSONFile changes the destination of the JSON stream to the given path
// (created, and appended to, if needed). It returns an error, leaving the
// previous destination unchanged, if the file cannot be opened.
func SetJSONFile(path string) error {
	return defaultCore.setJSONFile(path)
}

func (c *core) setJSONFile(path string) error {
	f, err := openLogFile(path)
	if err != nil {
		return err
	}
	closeWriter(c.json.swap(&reportedWriter{w: f, errors: c.sinkErrors, path: path}))
	return nil
}

// SetSinkErrorHandler installs fn as the process-wide handler for failures of
// the JSON file sink: the file could not be opened, or a write to it failed.
// The handler runs on the goroutine that hit the failure, so it should be
// cheap and must not log through this package (a write that fails and logs
// could recurse). It is called as errors occur; a slow handler delays those
// log calls.
//
// The handler is a notification, not a failure path: the logging call that
// triggered it still returns normally, and logging continues on the text
// stream. Installing nil restores the default, which warns once on stderr and
// stays quiet afterwards. SetSinkErrorHandler is safe for concurrent use,
// including while loggers are emitting.
func SetSinkErrorHandler(fn func(error)) {
	defaultCore.setSinkErrorHandler(fn)
}

func (c *core) setSinkErrorHandler(fn func(error)) {
	c.sinkErrors.setHandler(fn)
}

// UseOutputForTests redirects both streams to w and returns a func that
// restores the previous destinations. It is for tests only.
//
// One locked wrapper is installed as the destination of both streams, so
// writes from the two handlers are serialized by a single mutex even though
// they go through two separate stream holders.
func UseOutputForTests(w io.Writer) func() {
	return defaultCore.useOutputForTests(w)
}

func (c *core) useOutputForTests(w io.Writer) func() {
	sw := &swappableWriter{w: w}
	prevText := c.text.swap(sw)
	prevJSON := c.json.swap(sw)
	return func() {
		c.text.set(prevText)
		c.json.set(prevJSON)
	}
}

// nameLeveler is the per-name Leveler: the name's own level, or the core's
// default level when none has been set. It is consulted for every record.
type nameLeveler struct {
	core *core
	name string
}

func (n nameLeveler) Level() slog.Level {
	n.core.mu.RLock()
	defer n.core.mu.RUnlock()
	if l, ok := n.core.nameLevels[n.name]; ok {
		return l
	}
	return n.core.defaultLevel
}

// swappableWriter is an [io.Writer] whose destination can be swapped at any
// time. Its mutex serializes writes from the stream's (two) handlers; the
// handlers' own locks only serialize within a single handler.
type swappableWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *swappableWriter) set(w io.Writer) {
	s.mu.Lock()
	s.w = w
	s.mu.Unlock()
}

func (s *swappableWriter) swap(w io.Writer) io.Writer {
	s.mu.Lock()
	old := s.w
	s.w = w
	s.mu.Unlock()
	return old
}

func (s *swappableWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// sinkErrorReporter records JSON sink failures. With a handler installed it
// forwards every error; otherwise it writes the first one to out and stays
// quiet, so a broken sink cannot flood the terminal. A nil out means
// [os.Stderr], resolved when the warning is written so a redirected stderr is
// honoured. The mutex makes SetSinkErrorHandler safe while loggers emit.
type sinkErrorReporter struct {
	mu      sync.Mutex
	handler func(error)
	out     io.Writer
	warned  bool
}

func newSinkErrorReporter(out io.Writer) *sinkErrorReporter {
	return &sinkErrorReporter{out: out}
}

func (r *sinkErrorReporter) setHandler(fn func(error)) {
	r.mu.Lock()
	r.handler = fn
	r.mu.Unlock()
}

// report delivers err to the handler, or warns once when there is none. The
// handler runs without the lock held: it may be slow or change the handler
// without deadlocking this reporter, though logging from it risks recursion.
func (r *sinkErrorReporter) report(err error) {
	r.mu.Lock()
	handler := r.handler
	warn := handler == nil && !r.warned
	if warn {
		r.warned = true
	}
	r.mu.Unlock()

	switch {
	case handler != nil:
		handler(err)
	case warn:
		out := r.out
		if out == nil {
			out = os.Stderr
		}
		fmt.Fprintf(out, "mjolnir/log: %v\n", err)
	}
}

// reportedWriter reports write failures from an already-open JSON sink. The
// standard slog handler discards writer errors, so this wrapper is what makes
// SetJSONFile write failures visible through SetSinkErrorHandler.
type reportedWriter struct {
	w      io.Writer
	errors *sinkErrorReporter
	path   string
}

func (w *reportedWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil {
		w.errors.report(fmt.Errorf("write %q: %w", w.path, err))
	}
	return n, err
}

func (w *reportedWriter) Close() error {
	return closeWriterErr(w.w)
}

// lazyFile opens its path (append-only, creating the directory) on first
// write. If the file cannot be opened it degrades to a discard and reports
// the failure, so a missing log location never breaks the application. Write
// errors are reported as well, and also returned because that write failed.
type lazyFile struct {
	path    string
	errors  *sinkErrorReporter
	mu      sync.Mutex
	file    *os.File
	missing bool
}

func newLazyFile(path string, errors *sinkErrorReporter) *lazyFile {
	return &lazyFile{path: path, errors: errors}
}

func (f *lazyFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	if f.missing {
		f.mu.Unlock()
		return len(p), nil
	}
	if f.file == nil {
		file, err := openLogFile(f.path)
		if err != nil {
			f.missing = true
			f.mu.Unlock()
			f.errors.report(fmt.Errorf("cannot open %q: %w; JSON stream disabled", f.path, err))
			return len(p), nil
		}
		f.file = file
	}
	n, err := f.file.Write(p)
	f.mu.Unlock()
	if err != nil {
		f.errors.report(fmt.Errorf("write %q: %w", f.path, err))
	}
	return n, err
}

// Close closes the underlying file, if it was opened. A later Write reopens it.
func (f *lazyFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}

// openLogFile creates path's directory if needed and opens the file for
// append-only writes.
func openLogFile(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return f, nil
}

// closeWriter releases the resource held by w, if it owns one. Buffers and
// [io.Discard] are left alone, and the standard streams are never closed.
func closeWriter(w io.Writer) {
	_ = closeWriterErr(w)
}

func closeWriterErr(w io.Writer) error {
	if f, ok := w.(*os.File); ok {
		if f == os.Stdout || f == os.Stderr {
			return nil
		}
		return f.Close()
	}
	if c, ok := w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
