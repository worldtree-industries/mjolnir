package middleware

import (
	"log/slog"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/dfryer1193/mjolnir/utils/log"
)

// RequestLogger is a middleware that writes one access line per request. It
// obtains its own logger for the "http" component; RequestLoggerWith takes
// one instead.
func RequestLogger(next http.Handler) http.Handler {
	return RequestLoggerWith(next, log.GetLogger("http"))
}

// RequestLoggerWith is RequestLogger with an injected logger, so callers (and
// tests, via [log.GetLoggerTo]) choose where access lines go.
//
// next receives a ResponseWriter from NewResponseWriter, so it keeps the
// capabilities of the server's writer. The line is written in a defer, so a
// panic in next still produces it, carrying the status sent before the
// panic (0 if nothing was sent).
func RequestLoggerWith(next http.Handler, lg *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := NewResponseWriter(w, r.ProtoMajor)
		returned := false

		defer func() {
			status := ww.Status()
			if status == 0 && returned {
				// The handler wrote nothing; net/http sends 200 for it.
				status = http.StatusOK
			}

			// The id is attached to a per-request local: reassigning the
			// captured lg would share state across concurrent requests.
			reqLog := lg
			if id := GetRequestID(r.Context()); id != "" {
				reqLog = reqLog.With(slog.String("request_id", id))
			}

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("latency", time.Since(start)),
				slog.String("remote_addr", r.RemoteAddr),
			}
			// Resolved only when a ClientIPFrom* middleware runs earlier.
			if ip := chimiddleware.GetClientIP(r.Context()); ip != "" {
				attrs = append(attrs, slog.String("client_ip", ip))
			}
			reqLog.LogAttrs(r.Context(), slog.LevelInfo, "request completed", attrs...)
		}()

		next.ServeHTTP(ww, r)
		returned = true
	})
}
