package middleware

import (
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// ResponseWriter is the response writer used by the framework and the HTTP
// error handler. It preserves the optional interfaces of the writer it wraps
// ([http.Flusher], [http.Hijacker], [io.ReaderFrom], [http.Pusher]) and exposes
// the status, byte count, and the original writer through Unwrap.
type ResponseWriter = chimiddleware.WrapResponseWriter

// NewResponseWriter wraps w for the given HTTP protocol major version. It is
// the one wrapper used by the access logger and by httpx.WriteProblem, so the
// response can be observed without stripping capabilities.
func NewResponseWriter(w http.ResponseWriter, protoMajor int) ResponseWriter {
	return chimiddleware.NewWrapResponseWriter(w, protoMajor)
}
