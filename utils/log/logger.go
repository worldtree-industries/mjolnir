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
// The shared state (the two sink destinations and the per-name level table)
// lives in a [core]. The package holds one default core; a logger returned by
// GetLogger is a cheap value handle onto it, so callers obtain one per
// function rather than caching it at package scope. Tests build isolated
// cores, which is what lets them exercise the machinery in parallel.
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
// two sink destinations and the per-name level table. It must be process-wide,
// hence package-level.
//
//nolint:gochecknoglobals // the shared logging state; the only global this package holds, and it is synchronized
var defaultCore = newCore(os.Stdout, newLazyFile(DefaultJSONPath))

// warnOnceMu guards the one-time warning emitted when the JSON file cannot be
// opened.
//
//nolint:gochecknoglobals // one-time warning guard
var warnOnceMu sync.Once

// core holds the two sink destinations and the per-name level table. The
// package keeps one default core; tests construct isolated ones, so the
// stateful machinery can be exercised in parallel without touching
// process-wide state.
type core struct {
	text *swappableWriter
	json *swappableWriter

	mu           sync.RWMutex
	nameLevels   map[string]slog.Level
	defaultLevel slog.Level
}

// newCore returns a core writing text and JSON to the given destinations, with
// an empty level table at DefaultLevel.
func newCore(text, json io.Writer) *core {
	return &core{
		text:         &swappableWriter{w: text},
		json:         &swappableWriter{w: json},
		nameLevels:   make(map[string]slog.Level),
		defaultLevel: DefaultLevel,
	}
}

// GetLogger returns a ready-to-use [*slog.Logger] for the named component.
// Every line it emits carries the "logger" field set to name and the source
// file:line of the call. The level in effect for name is checked for every
// record, so SetLevel and SetDefaultLevel take effect on the next emitted
// line without re-fetching the logger — including for With()-derived
// loggers.
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
	closeWriter(c.json.swap(f))
	return nil
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

// lazyFile opens its path (append-only, creating the directory) on first
// write. If the file cannot be opened it degrades to a discard and warns
// once, so a missing log location never breaks the application.
type lazyFile struct {
	path    string
	mu      sync.Mutex
	file    *os.File
	missing bool
}

func newLazyFile(path string) *lazyFile {
	return &lazyFile{path: path}
}

func (f *lazyFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missing {
		return len(p), nil
	}
	if f.file == nil {
		file, err := openLogFile(f.path)
		if err != nil {
			f.missing = true
			warnOncef("cannot open %q: %v; JSON stream disabled", f.path, err)
			return len(p), nil
		}
		f.file = file
	}
	return f.file.Write(p)
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
	if f, ok := w.(*os.File); ok {
		if f != os.Stdout && f != os.Stderr {
			_ = f.Close()
		}
		return
	}
	if c, ok := w.(io.Closer); ok {
		_ = c.Close()
	}
}

// warnOncef writes a one-time warning to stderr. It writes directly (rather
// than through a logger) so a logging failure here cannot recurse, and to
// avoid importing the standard log package in this non-main file.
func warnOncef(format string, args ...any) {
	warnOnceMu.Do(func() {
		fmt.Fprintf(os.Stderr, "mjolnir/log: "+format+"\n", args...)
	})
}
