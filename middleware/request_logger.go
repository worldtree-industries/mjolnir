package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/dfryer1193/mjolnir/utils/log"
)

// RequestLogger is a middleware that logs HTTP requests. It obtains its own
// logger for the "http" component.
func RequestLogger(next http.Handler) http.Handler {
	return requestLogger(next, log.GetLogger("http"))
}

// requestLogger is the injectable core of RequestLogger. Taking the logger
// explicitly lets tests pass an isolated one (log.GetLoggerTo) instead of
// swapping process-wide sinks, so tests can run in parallel.
func requestLogger(next http.Handler, lg *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a custom response writer to capture the status code
		ww := &responseWriter{w: w, status: http.StatusOK}

		next.ServeHTTP(ww, r)

		// Attach the request id (set by the RequestID middleware) so the
		// request can be correlated. It is omitted when absent. The id is
		// attached to a per-request local: reassigning the captured lg would
		// share state across concurrent requests.
		reqLog := lg
		if id := GetRequestID(r.Context()); id != "" {
			reqLog = reqLog.With(slog.String("request_id", id))
		}

		// Log the request details
		reqLog.InfoContext(r.Context(), "request completed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_addr", r.RemoteAddr),
			slog.Int("status", ww.status),
			slog.Duration("latency", time.Since(start)),
		)
	})
}

// responseWriter is a custom response writer that captures the status code.
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
