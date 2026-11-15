package router

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/httpx"
)

// recoverer recovers a panic in next and logs it to lg with the stack,
// method, path, request id, and whether the response had started.
//
// Before anything is sent it answers with a 500 problem. Once the response
// has started a second body would corrupt it, so it panics with
// [http.ErrAbortHandler] instead: net/http then aborts the connection, and
// the client cannot mistake the truncated body for a complete one. A panic
// that already is [http.ErrAbortHandler] is the handler asking for that
// abort, so it passes through unlogged.
func recoverer(lg *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw, ok := w.(middleware.ResponseWriter)
			if !ok {
				rw = middleware.NewResponseWriter(w, r.ProtoMajor)
			}

			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, isErr := rec.(error); isErr && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}

				// A status, or any body, has been sent.
				started := rw.Status() != 0

				attrs := []slog.Attr{
					slog.String("panic", fmt.Sprint(rec)),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Bool("response_started", started),
					slog.String("stack", string(debug.Stack())),
				}
				if id := middleware.GetRequestID(r.Context()); id != "" {
					attrs = append(attrs, slog.String("request_id", id))
				}
				lg.LogAttrs(r.Context(), slog.LevelError, "panic recovered", attrs...)

				if started {
					panic(http.ErrAbortHandler)
				}

				// The panic is already logged with its stack, so the problem's
				// cause only names it.
				_ = httpx.WriteProblem(rw, r, httpx.Internal(fmt.Errorf("panic: %v", rec)))
			}()

			next.ServeHTTP(rw, r)
		})
	}
}
