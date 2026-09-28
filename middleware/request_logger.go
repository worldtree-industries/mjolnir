package middleware

import (
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// RequestLogger is a middleware that logs HTTP requests using zerolog
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a custom response writer to capture the status code
		ww := &responseWriter{w: w, status: http.StatusOK}

		next.ServeHTTP(ww, r)

		// Log the request details
		log.Info().
			Str("request_id", GetRequestID(r.Context())).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Int("status", ww.status).
			Dur("latency", time.Since(start)).
			Msg("request completed")
	})
}

// responseWriter is a custom response writer that captures the status code
type responseWriter struct {
	w           http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rw *responseWriter) Header() http.Header {
	return rw.w.Header()
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.w.Write(b)
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	if rw.wroteHeader {
		return
	}

	rw.status = statusCode
	rw.w.WriteHeader(statusCode)
	rw.wroteHeader = true
}
