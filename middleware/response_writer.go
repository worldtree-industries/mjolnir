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
