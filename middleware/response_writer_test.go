package middleware_test

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/dfryer1193/mjolnir/middleware"
)

type baseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBaseWriter() *baseWriter {
	return &baseWriter{header: make(http.Header)}
}

func (w *baseWriter) Header() http.Header {
	return w.header
}

func (w *baseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *baseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

type readerWriter struct {
	*baseWriter
}

func (w *readerWriter) ReadFrom(src io.Reader) (int64, error) {
	return io.Copy(w.baseWriter, src)
}

type flushReaderWriter struct {
	*baseWriter
}

func (w *flushReaderWriter) Flush() {}

func (w *flushReaderWriter) ReadFrom(src io.Reader) (int64, error) {
	return io.Copy(w.baseWriter, src)
}

type hijackReaderWriter struct {
	*baseWriter
}

func (w *hijackReaderWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func (w *hijackReaderWriter) ReadFrom(src io.Reader) (int64, error) {
	return io.Copy(w.baseWriter, src)
}

type flushPusherReaderWriter struct {
	*baseWriter
}

func (w *flushPusherReaderWriter) Flush() {}

func (w *flushPusherReaderWriter) Push(string, *http.PushOptions) error {
	return nil
}

func (w *flushPusherReaderWriter) ReadFrom(src io.Reader) (int64, error) {
	return io.Copy(w.baseWriter, src)
}

type pusherWriter struct {
	*baseWriter
}

func (w *pusherWriter) Push(string, *http.PushOptions) error {
	return nil
}

type flushPusherWriter struct {
	*baseWriter
}

func (w *flushPusherWriter) Flush() {}

func (w *flushPusherWriter) Push(string, *http.PushOptions) error {
	return nil
}

func TestNewResponseWriterPreservesCapabilitiesExactly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		writer         http.ResponseWriter
		protoMajor     int
		wantFlusher    bool
		wantHijacker   bool
		wantReaderFrom bool
		wantPusher     bool
	}{
		{
			name:           "plain",
			writer:         newBaseWriter(),
			protoMajor:     1,
			wantReaderFrom: false,
		},
		{
			name:           "reader from only",
			writer:         &readerWriter{newBaseWriter()},
			protoMajor:     1,
			wantReaderFrom: true,
		},
		{
			name:           "flusher and reader from",
			writer:         &flushReaderWriter{newBaseWriter()},
			protoMajor:     1,
			wantFlusher:    true,
			wantReaderFrom: true,
		},
		{
			name:           "hijacker and reader from",
			writer:         &hijackReaderWriter{newBaseWriter()},
			protoMajor:     1,
			wantHijacker:   true,
			wantReaderFrom: true,
		},
		{
			name:           "http2 flusher, pusher, and reader from",
			writer:         &flushPusherReaderWriter{newBaseWriter()},
			protoMajor:     2,
			wantFlusher:    true,
			wantPusher:     true,
			wantReaderFrom: true,
		},
		{
			name:       "http2 pusher only",
			writer:     &pusherWriter{newBaseWriter()},
			protoMajor: 2,
			wantPusher: true,
		},
		{
			name:        "http2 flusher and pusher",
			writer:      &flushPusherWriter{newBaseWriter()},
			protoMajor:  2,
			wantFlusher: true,
			wantPusher:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wrapped := middleware.NewResponseWriter(tt.writer, tt.protoMajor)

			assertCapability(t, "Flusher", wrapped, tt.wantFlusher)
			assertCapability(t, "Hijacker", wrapped, tt.wantHijacker)
			assertCapability(t, "ReaderFrom", wrapped, tt.wantReaderFrom)
			assertCapability(t, "Pusher", wrapped, tt.wantPusher)

			if !tt.wantReaderFrom {
				return
			}

			readerFrom, ok := wrapped.(io.ReaderFrom)
			if !ok {
				t.Fatal("ReaderFrom assertion failed")
			}

			n, err := readerFrom.ReadFrom(strings.NewReader("hello"))
			if err != nil {
				t.Fatalf("ReadFrom: %v", err)
			}
			if n != int64(len("hello")) {
				t.Errorf("ReadFrom wrote %d bytes, want %d", n, len("hello"))
			}
			if wrapped.Status() != http.StatusOK {
				t.Errorf("status = %d, want 200", wrapped.Status())
			}
			if wrapped.BytesWritten() != len("hello") {
				t.Errorf("bytes = %d, want %d", wrapped.BytesWritten(), len("hello"))
			}
			if wrapped.Unwrap() != tt.writer {
				t.Error("Unwrap did not return the original writer")
			}
		})
	}
}

func assertCapability(t *testing.T, name string, writer http.ResponseWriter, want bool) {
	t.Helper()

	var got bool

	switch name {
	case "Flusher":
		_, got = writer.(http.Flusher)
	case "Hijacker":
		_, got = writer.(http.Hijacker)
	case "ReaderFrom":
		_, got = writer.(io.ReaderFrom)
	case "Pusher":
		_, got = writer.(http.Pusher)
	default:
		t.Fatalf("unknown capability %q", name)
	}

	if got != want {
		t.Errorf("%s present = %v, want %v", name, got, want)
	}
}
