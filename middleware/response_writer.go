package middleware

import (
	"bufio"
	"io"
	"net"
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// ResponseWriter is the response writer used by the framework and the HTTP
// error handler. It preserves the optional interfaces of the writer it wraps
// ([http.Flusher], [http.Hijacker], [io.ReaderFrom], [http.Pusher]) and exposes
// the status, byte count, and the original writer through Unwrap.
type ResponseWriter = chimiddleware.WrapResponseWriter

const http2 = 2

// ResponseStarted reports whether w has already sent a status. It recognizes
// the framework's [ResponseWriter] and any other writer exposing Status() int.
func ResponseStarted(w http.ResponseWriter) bool {
	rw, ok := w.(interface{ Status() int })
	return ok && rw.Status() != 0
}

// NewResponseWriter wraps w for the given HTTP protocol major version. It is
// the one wrapper used by the access logger and by httpx.Handler, so the
// response can be observed without stripping capabilities.
//
// chi's constructor preserves ReadFrom only when the writer is also a Flusher
// and a Hijacker, and it drops Pusher when HTTP/2 has no Flusher. The wrappers
// below add the missing optional interface while keeping chi's status, byte
// count, tee, and discard behaviour.
func NewResponseWriter(w http.ResponseWriter, protoMajor int) ResponseWriter {
	inner := chimiddleware.NewWrapResponseWriter(w, protoMajor)

	if protoMajor == http2 {
		return newHTTP2ResponseWriter(w, inner)
	}

	return newHTTP1ResponseWriter(w, inner)
}

func newHTTP1ResponseWriter(w http.ResponseWriter, inner chimiddleware.WrapResponseWriter) ResponseWriter {
	reader, hasReader := newReaderFrom(w, inner)
	if !hasReader {
		return inner
	}

	flusher, hasFlusher := inner.(http.Flusher)
	hijacker, hasHijacker := inner.(http.Hijacker)

	switch {
	case hasFlusher && hasHijacker:
		return &flushHijackReadFromWriter{readFromDelegate: reader, flusher: flusher, hijacker: hijacker}
	case hasFlusher:
		return &flushReadFromWriter{readFromDelegate: reader, flusher: flusher}
	case hasHijacker:
		return &hijackReadFromWriter{readFromDelegate: reader, hijacker: hijacker}
	default:
		return &readFromWriter{readFromDelegate: reader}
	}
}

func newHTTP2ResponseWriter(w http.ResponseWriter, inner chimiddleware.WrapResponseWriter) ResponseWriter {
	pusher, hasPusher := w.(http.Pusher)
	if !hasPusher {
		return newHTTP1ResponseWriter(w, inner)
	}

	flusher, hasFlusher := inner.(http.Flusher)
	reader, hasReader := newReaderFrom(w, inner)

	switch {
	case hasFlusher && hasReader:
		return &flushPusherReadFromWriter{readFromDelegate: reader, flusher: flusher, pusher: pusher}
	case hasFlusher:
		return &flushPusherWriter{WrapResponseWriter: inner, flusher: flusher, pusher: pusher}
	case hasReader:
		return &pusherReadFromWriter{readFromDelegate: reader, pusher: pusher}
	default:
		return &pusherWriter{WrapResponseWriter: inner, pusher: pusher}
	}
}

// newReaderFrom returns a ReadFrom delegate when the underlying writer has
// ReadFrom and chi's inner writer does not already expose it.
func newReaderFrom(w http.ResponseWriter, inner chimiddleware.WrapResponseWriter) (readFromDelegate, bool) {
	if _, ok := w.(io.ReaderFrom); !ok {
		return readFromDelegate{}, false
	}
	if _, ok := inner.(io.ReaderFrom); ok {
		return readFromDelegate{}, false
	}
	return readFromDelegate{WrapResponseWriter: inner}, true
}

// copyThrough routes ReadFrom through chi's Write path so status, byte count,
// tee, and discard stay correct. It is used only when chi's inner writer does
// not itself implement ReadFrom.
func copyThrough(w chimiddleware.WrapResponseWriter, src io.Reader) (int64, error) {
	return io.Copy(w, src)
}

// readFromDelegate carries chi's writer plus the ReadFrom implementation that
// every wrapper below shares. Go requires separate types for the optional
// interface combinations, but the delegation itself lives in one place.
type readFromDelegate struct {
	chimiddleware.WrapResponseWriter
}

func (w *readFromDelegate) ReadFrom(src io.Reader) (int64, error) {
	return copyThrough(w.WrapResponseWriter, src)
}

type readFromWriter struct {
	readFromDelegate
}

type flushReadFromWriter struct {
	readFromDelegate

	flusher http.Flusher
}

func (w *flushReadFromWriter) Flush() {
	w.flusher.Flush()
}

type hijackReadFromWriter struct {
	readFromDelegate

	hijacker http.Hijacker
}

func (w *hijackReadFromWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijacker.Hijack()
}

type flushHijackReadFromWriter struct {
	readFromDelegate

	flusher  http.Flusher
	hijacker http.Hijacker
}

func (w *flushHijackReadFromWriter) Flush() {
	w.flusher.Flush()
}

func (w *flushHijackReadFromWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijacker.Hijack()
}

type pusherWriter struct {
	chimiddleware.WrapResponseWriter

	pusher http.Pusher
}

func (w *pusherWriter) Push(target string, opts *http.PushOptions) error {
	return w.pusher.Push(target, opts)
}

type pusherReadFromWriter struct {
	readFromDelegate

	pusher http.Pusher
}

func (w *pusherReadFromWriter) Push(target string, opts *http.PushOptions) error {
	return w.pusher.Push(target, opts)
}

type flushPusherWriter struct {
	chimiddleware.WrapResponseWriter

	flusher http.Flusher
	pusher  http.Pusher
}

func (w *flushPusherWriter) Flush() {
	w.flusher.Flush()
}

func (w *flushPusherWriter) Push(target string, opts *http.PushOptions) error {
	return w.pusher.Push(target, opts)
}

type flushPusherReadFromWriter struct {
	readFromDelegate

	flusher http.Flusher
	pusher  http.Pusher
}

func (w *flushPusherReadFromWriter) Flush() {
	w.flusher.Flush()
}

func (w *flushPusherReadFromWriter) Push(target string, opts *http.PushOptions) error {
	return w.pusher.Push(target, opts)
}
