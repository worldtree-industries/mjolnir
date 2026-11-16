package middleware

import (
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// ResponseWriter is the response writer that middleware hands to the next
// handler. It records the status and the number of body bytes written, and
// Unwrap returns the writer it wraps.
type ResponseWriter = chimiddleware.WrapResponseWriter

// NewResponseWriter wraps w so the status and body size can be observed.
// protoMajor is the request's r.ProtoMajor; it selects between the HTTP/1 and
// HTTP/2 capability sets.
//
// The wrapper never claims a capability w lacks. It keeps every capability of
// net/http's own writers: [http.Flusher], [http.Hijacker] and [io.ReaderFrom] over
// HTTP/1, and [http.Flusher] and [http.Pusher] over HTTP/2. For a custom w with
// an unusual combination (say, a ReaderFrom that is not also a Flusher and a
// Hijacker) the wrapper may drop one; [http.NewResponseController] still
// reaches it through Unwrap.
func NewResponseWriter(w http.ResponseWriter, protoMajor int) ResponseWriter {
	return chimiddleware.NewWrapResponseWriter(w, protoMajor)
}

// AsResponseWriter returns w itself when it already is a ResponseWriter, so
// every layer of a request observes one status, and otherwise wraps it with
// NewResponseWriter for r's protocol.
func AsResponseWriter(w http.ResponseWriter, r *http.Request) ResponseWriter {
	if ww, ok := w.(ResponseWriter); ok {
		return ww
	}
	return NewResponseWriter(w, r.ProtoMajor)
}

// ResponseStarted reports whether w is a started response: one whose status
// line has been sent, by WriteHeader with a final (non-1xx) status or by a
// body write. It can tell only for a ResponseWriter; any other writer reports
// false, because nothing records what was sent through it.
func ResponseStarted(w http.ResponseWriter) bool {
	ww, ok := w.(ResponseWriter)
	return ok && ww.Status() != 0
}
