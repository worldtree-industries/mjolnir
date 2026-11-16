package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// TestAsResponseWriterReusesResponseWriter: a writer that already is a
// ResponseWriter comes back as-is, so every layer sees the same status.
func TestAsResponseWriterReusesResponseWriter(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	inner := middleware.NewResponseWriter(httptest.NewRecorder(), r.ProtoMajor)

	got := middleware.AsResponseWriter(inner, r)
	got.WriteHeader(http.StatusTeapot)

	if inner.Status() != http.StatusTeapot {
		t.Errorf("inner status = %d, want %d: the writer was wrapped again", inner.Status(), http.StatusTeapot)
	}
}

// TestAsResponseWriterWrapsOtherWriters: any other writer is wrapped, and the
// wrapper records what passes through it.
func TestAsResponseWriterWrapsOtherWriters(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	ww := middleware.AsResponseWriter(rec, r)
	ww.WriteHeader(http.StatusAccepted)
	_, _ = ww.Write([]byte("ok"))

	if ww.Status() != http.StatusAccepted || ww.BytesWritten() != 2 {
		t.Errorf("status, bytes = %d, %d; want %d, 2", ww.Status(), ww.BytesWritten(), http.StatusAccepted)
	}
	if rec.Code != http.StatusAccepted || rec.Body.String() != "ok" {
		t.Errorf("recorder got %d %q, want %d \"ok\"", rec.Code, rec.Body.String(), http.StatusAccepted)
	}
}

func TestResponseStarted(t *testing.T) {
	t.Parallel()
	newRW := func() middleware.ResponseWriter {
		return middleware.NewResponseWriter(httptest.NewRecorder(), 1)
	}
	tests := []struct {
		name  string
		write func() http.ResponseWriter
		want  bool
	}{
		{"nothing written", func() http.ResponseWriter { return newRW() }, false},
		{"status written", func() http.ResponseWriter {
			w := newRW()
			w.WriteHeader(http.StatusNoContent)
			return w
		}, true},
		{"body written", func() http.ResponseWriter {
			w := newRW()
			_, _ = w.Write([]byte("x"))
			return w
		}, true},
		{"informational status only", func() http.ResponseWriter {
			w := newRW()
			w.WriteHeader(http.StatusEarlyHints)
			return w
		}, false},
		{"other writer, even after a write", func() http.ResponseWriter {
			w := httptest.NewRecorder()
			w.WriteHeader(http.StatusOK)
			return w
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := middleware.ResponseStarted(tt.write()); got != tt.want {
				t.Errorf("ResponseStarted = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRequestLoggerReusesResponseWriter: behind a writer that is already a
// ResponseWriter, the access logger hands that same writer on.
func TestRequestLoggerReusesResponseWriter(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	outer := middleware.NewResponseWriter(httptest.NewRecorder(), r.ProtoMajor)
	var seen http.ResponseWriter
	h := middleware.RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen = w
	}), log.GetLoggerTo("http", io.Discard))

	h.ServeHTTP(outer, r)

	if seen != outer {
		t.Errorf("handler got %T, want the caller's ResponseWriter", seen)
	}
}
