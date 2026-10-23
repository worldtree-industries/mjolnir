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

// NewResponseWriter wraps w for the given HTTP protocol major version. It is
// the one wrapper used by the access logger and by httpx.Handler, so the
// response can be observed without stripping capabilities.
//
// chi's constructor preserves ReadFrom only when the writer is also a Flusher
// and a Hijacker. The wrappers below add ReadFrom for the remaining supported
// combinations while keeping chi's status, byte count, tee, and discard
// behaviour.
func NewResponseWriter(w http.ResponseWriter, protoMajor int) ResponseWriter {
	inner := chimiddleware.NewWrapResponseWriter(w, protoMajor)

	if _, ok := w.(io.ReaderFrom); !ok {
		return inner
	}
	if _, already := inner.(io.ReaderFrom); already {
		return inner
	}

	flusher, hasFlusher := inner.(http.Flusher)

	if protoMajor == http2 {
		pusher, hasPusher := w.(http.Pusher)
		switch {
		case hasFlusher && hasPusher:
			return &flushPusherReadFromWriter{
				WrapResponseWriter: inner,
				flusher:            flusher,
				pusher:             pusher,
			}
		case hasFlusher:
			return &flushReadFromWriter{
				WrapResponseWriter: inner,
				flusher:            flusher,
			}
		case hasPusher:
			return &pusherReadFromWriter{
				WrapResponseWriter: inner,
				pusher:             pusher,
			}
		default:
			return &readFromWriter{WrapResponseWriter: inner}
		}
	}

	hijacker, hasHijacker := inner.(http.Hijacker)
	switch {
	case hasFlusher && hasHijacker:
		return &flushHijackReadFromWriter{
			WrapResponseWriter: inner,
			flusher:            flusher,
			hijacker:           hijacker,
		}
	case hasFlusher:
		return &flushReadFromWriter{
			WrapResponseWriter: inner,
			flusher:            flusher,
		}
	case hasHijacker:
		return &hijackReadFromWriter{
			WrapResponseWriter: inner,
			hijacker:           hijacker,
		}
	default:
		return &readFromWriter{WrapResponseWriter: inner}
	}
}

// copyThrough routes ReadFrom through chi's Write path so status, byte count,
// tee, and discard stay correct. The wrappers are used only when chi's inner
// writer does not itself implement ReadFrom.
func copyThrough(w chimiddleware.WrapResponseWriter, src io.Reader) (int64, error) {
	return io.Copy(w, src)
}

// readFromWriter adds ReadFrom to chi's basic writer. It deliberately does not
// expose Flush, Hijack, or Push unless the combination below says so.
type readFromWriter struct {
	chimiddleware.WrapResponseWriter
}

func (w *readFromWriter) ReadFrom(src io.Reader) (int64, error) {
	return copyThrough(w.WrapResponseWriter, src)
}

type flushReadFromWriter struct {
	chimiddleware.WrapResponseWriter

	flusher http.Flusher
}

func (w *flushReadFromWriter) Flush() {
	w.flusher.Flush()
}

func (w *flushReadFromWriter) ReadFrom(src io.Reader) (int64, error) {
	return copyThrough(w.WrapResponseWriter, src)
}

type hijackReadFromWriter struct {
	chimiddleware.WrapResponseWriter

	hijacker http.Hijacker
}

func (w *hijackReadFromWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijacker.Hijack()
}

func (w *hijackReadFromWriter) ReadFrom(src io.Reader) (int64, error) {
	return copyThrough(w.WrapResponseWriter, src)
}

type flushHijackReadFromWriter struct {
	chimiddleware.WrapResponseWriter

	flusher  http.Flusher
	hijacker http.Hijacker
}

func (w *flushHijackReadFromWriter) Flush() {
	w.flusher.Flush()
}

func (w *flushHijackReadFromWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijacker.Hijack()
}

func (w *flushHijackReadFromWriter) ReadFrom(src io.Reader) (int64, error) {
	return copyThrough(w.WrapResponseWriter, src)
}

type pusherReadFromWriter struct {
	chimiddleware.WrapResponseWriter

	pusher http.Pusher
}

func (w *pusherReadFromWriter) Push(target string, opts *http.PushOptions) error {
	return w.pusher.Push(target, opts)
}

func (w *pusherReadFromWriter) ReadFrom(src io.Reader) (int64, error) {
	return copyThrough(w.WrapResponseWriter, src)
}

type flushPusherReadFromWriter struct {
	chimiddleware.WrapResponseWriter

	flusher http.Flusher
	pusher  http.Pusher
}

func (w *flushPusherReadFromWriter) Flush() {
	w.flusher.Flush()
}

func (w *flushPusherReadFromWriter) Push(target string, opts *http.PushOptions) error {
	return w.pusher.Push(target, opts)
}

func (w *flushPusherReadFromWriter) ReadFrom(src io.Reader) (int64, error) {
	return copyThrough(w.WrapResponseWriter, src)
}
