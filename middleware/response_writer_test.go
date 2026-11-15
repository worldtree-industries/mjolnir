package middleware_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// capabilities records which optional interfaces a handler's writer offers.
type capabilities struct {
	flusher, hijacker, readerFrom bool
}

func capabilitiesOf(w http.ResponseWriter) capabilities {
	_, f := w.(http.Flusher)
	_, h := w.(http.Hijacker)
	_, rf := w.(io.ReaderFrom)
	return capabilities{flusher: f, hijacker: h, readerFrom: rf}
}

// serve runs one GET against a real HTTP/1.1 server whose handler is h. A
// ResponseRecorder cannot stand in: it is not a Hijacker.
func serve(t *testing.T, h http.Handler) *http.Response {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestRequestLoggerPreservesCapabilities: net/http's HTTP/1 writer is a
// Flusher, Hijacker and ReaderFrom, and a handler behind the access logger
// still sees all three.
func TestRequestLoggerPreservesCapabilities(t *testing.T) {
	t.Parallel()
	seen := make(chan capabilities, 1)
	h := middleware.RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen <- capabilitiesOf(w)
	}), log.GetLoggerTo("http", io.Discard))

	serve(t, h)

	got := <-seen
	want := capabilities{flusher: true, hijacker: true, readerFrom: true}
	if got != want {
		t.Errorf("handler saw %+v, want %+v", got, want)
	}
}

// bareWriter hides every optional interface of the writer it embeds.
type bareWriter struct{ http.ResponseWriter }

// flushOnlyWriter offers [http.Flusher] and nothing else.
type flushOnlyWriter struct{ http.ResponseWriter }

func (f flushOnlyWriter) Flush() { f.ResponseWriter.(http.Flusher).Flush() }

// TestNewResponseWriterInventsNoCapability: wrapping a writer that lacks a
// capability must not make the handler believe it has it.
func TestNewResponseWriterInventsNoCapability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		limit func(http.ResponseWriter) http.ResponseWriter
		want  capabilities
	}{
		{
			name:  "bare writer",
			limit: func(w http.ResponseWriter) http.ResponseWriter { return bareWriter{w} },
			want:  capabilities{},
		},
		{
			name:  "flusher only",
			limit: func(w http.ResponseWriter) http.ResponseWriter { return flushOnlyWriter{w} },
			want:  capabilities{flusher: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seen := make(chan capabilities, 1)
			serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- capabilitiesOf(middleware.NewResponseWriter(tt.limit(w), r.ProtoMajor))
			}))
			if got := <-seen; got != tt.want {
				t.Errorf("wrapped writer offers %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestNewResponseWriterUnwrapsToOriginal: Unwrap returns the writer that was
// wrapped, which is what [http.ResponseController] follows.
func TestNewResponseWriterUnwrapsToOriginal(t *testing.T) {
	t.Parallel()
	same := make(chan bool, 1)
	serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		same <- middleware.NewResponseWriter(w, r.ProtoMajor).Unwrap() == w
	}))
	if !<-same {
		t.Error("Unwrap did not return the original writer")
	}
}

// TestRequestLoggerStreams: a handler behind the access logger can flush
// through [http.ResponseController], and the client receives the first event
// while the handler is still running (an SSE stream does not stall).
func TestRequestLoggerStreams(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	flushErr := make(chan error, 1)
	h := middleware.RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		err := http.NewResponseController(w).Flush()
		flushErr <- err
		if err != nil {
			return // no stream to hold open
		}
		<-release
	}), log.GetLoggerTo("http", io.Discard))

	resp := serve(t, h)
	defer close(release)

	if err := <-flushErr; err != nil {
		t.Fatalf("ResponseController.Flush: %v", err)
	}
	want := "data: first\n\n"
	buf := make([]byte, len(want))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("reading first event: %v", err)
	}
	if string(buf) != want {
		t.Errorf("first event = %q, want %q", buf, want)
	}
}

// countingReaderFrom records whether its ReadFrom was used.
type countingReaderFrom struct {
	http.ResponseWriter

	calls chan<- struct{}
}

func (c countingReaderFrom) Flush() { c.ResponseWriter.(http.Flusher).Flush() }

func (c countingReaderFrom) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return c.ResponseWriter.(http.Hijacker).Hijack()
}

func (c countingReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	c.calls <- struct{}{}
	return c.ResponseWriter.(io.ReaderFrom).ReadFrom(r)
}

// onlyReader hides any WriterTo, so [io.Copy] must choose the destination's
// ReaderFrom if there is one.
type onlyReader struct{ io.Reader }

// TestNewResponseWriterCopyUsesReaderFrom: [io.Copy] into the wrapper reaches
// the underlying writer's ReadFrom, and the bytes are counted.
func TestNewResponseWriterCopyUsesReaderFrom(t *testing.T) {
	t.Parallel()
	calls := make(chan struct{}, 1)
	written := make(chan int, 1)
	resp := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewResponseWriter(countingReaderFrom{w, calls}, r.ProtoMajor)
		_, _ = io.Copy(ww, onlyReader{strings.NewReader("payload")})
		written <- ww.BytesWritten()
	}))

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "payload" {
		t.Errorf("body = %q, want %q", body, "payload")
	}
	select {
	case <-calls:
	default:
		t.Error("io.Copy did not use the underlying ReadFrom")
	}
	if n := <-written; n != len("payload") {
		t.Errorf("BytesWritten = %d, want %d", n, len("payload"))
	}
}

// TestRequestLoggerPreservesHTTP2Capabilities: over HTTP/2 net/http's writer
// is a Flusher and a Pusher but not a Hijacker or ReaderFrom, and the handler
// sees the same.
func TestRequestLoggerPreservesHTTP2Capabilities(t *testing.T) {
	t.Parallel()
	type seen struct {
		proto  int
		caps   capabilities
		pusher bool
	}
	got := make(chan seen, 1)
	h := middleware.RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, p := w.(http.Pusher)
		got <- seen{proto: r.ProtoMajor, caps: capabilitiesOf(w), pusher: p}
	}), log.GetLoggerTo("http", io.Discard))

	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()

	want := seen{proto: 2, caps: capabilities{flusher: true}, pusher: true}
	if g := <-got; g != want {
		t.Errorf("handler saw %+v, want %+v", g, want)
	}
}
