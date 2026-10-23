package middleware

import (
	"log/slog"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/dfryer1193/mjolnir/utils/log"
)

// RequestLogger is a middleware that logs HTTP requests. It obtains its own
// logger for the "http" component.
func RequestLogger(next http.Handler) http.Handler {
	return RequestLoggerWith(next, log.GetLogger("http"))
}

// RequestLoggerWith is RequestLogger with an injected logger. Taking the
// logger explicitly lets tests pass an isolated one (log.GetLoggerTo) instead
// of swapping process-wide sinks, so tests can run in parallel.
//
// The access line is emitted in a defer, so it is written even when the
// handler panics. The recoverer sits inside this middleware and settles the
// status before the deferred line reads it.
func RequestLoggerWith(next http.Handler, lg *slog.Logger) http.Handler {
	if lg == nil {
		lg = log.GetLogger("http")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := NewResponseWriter(w, r.ProtoMajor)

		defer func() {
			reqLog := lg
			if id := GetRequestID(r.Context()); id != "" {
				reqLog = reqLog.With(slog.String("request_id", id))
			}

			status := ww.Status()
			if status == 0 {
				// net/http sends 200 when the handler returns without writing.
				status = http.StatusOK
			}

			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("latency", time.Since(start)),
			}
			if ip := chimiddleware.GetClientIP(r.Context()); ip != "" {
				attrs = append(attrs, slog.String("client_ip", ip))
			}

			reqLog.InfoContext(r.Context(), "request completed", attrs...)
		}()

		next.ServeHTTP(ww, r)
	})
}
