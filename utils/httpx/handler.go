package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/log"
)

const (
	// problemContentType is the media type of a problem response (RFC 9457).
	problemContentType = "application/problem+json"
	// serverErrorDetail replaces the detail of every 5xx problem on the wire.
	serverErrorDetail = "The server could not complete the request."
)

// ErrResponseStarted is returned by [WriteProblem] when the status line has
// already been sent, so a problem response can no longer be written.
var ErrResponseStarted = errors.New("httpx: response already started")

// problemDocument is the wire form of a [Problem].
type problemDocument struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
}

// Handler adapts fn to an [http.HandlerFunc] that turns the error fn returns
// into a problem response. It logs through the "httpx" named logger; use
// [HandlerWithLogger] to choose the logger.
//
// A nil error writes nothing. An error that is or wraps a [*Problem] (found
// with [errors.AsType], so wrapping with %w keeps it) sends that problem. Any
// other error is sent as [Internal] with that error as the cause.
//
// A 5xx is logged at Error with the method, path, request id, status, code,
// server-only detail, and cause; the client receives the fixed generic detail.
// A 4xx is not logged. If fn has already started the response (sent a status
// or any body) when it returns a problem, nothing more is written: the
// problem is logged at Warn instead of being appended to the body.
func Handler(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return HandlerWithLogger(fn, log.GetLogger("httpx"))
}

// HandlerWithLogger is [Handler] with the logger supplied by the caller.
//
// To see whether the response has started, the handler hands fn the writer
// from [middleware.AsResponseWriter]: the one it received when that already is
// a [middleware.ResponseWriter] (for example behind [middleware.RequestLogger]),
// or a new wrapper otherwise. Started-detection therefore always works inside
// a Handler.
func HandlerWithLogger(fn func(http.ResponseWriter, *http.Request) error, lg *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rw := middleware.AsResponseWriter(w, r)
		err := fn(rw, r)
		if err == nil {
			return
		}
		p, ok := errors.AsType[*Problem](err)
		if !ok || p == nil {
			p = Internal(err)
		}
		reqLog := requestLogger(lg, r, p)
		if p.Status >= http.StatusInternalServerError {
			reqLog.ErrorContext(r.Context(), "server error",
				slog.String("detail", p.Detail),
				slog.Any("error", p.cause),
			)
		}
		switch writeErr := WriteProblem(rw, r, p); {
		case errors.Is(writeErr, ErrResponseStarted):
			reqLog.WarnContext(r.Context(), "problem not sent: response already started")
		case writeErr != nil:
			reqLog.ErrorContext(r.Context(), "failed to write problem", slog.Any("error", writeErr))
		}
	}
}

// requestLogger returns lg with the fields that place p in its request.
func requestLogger(lg *slog.Logger, r *http.Request, p *Problem) *slog.Logger {
	if id := middleware.GetRequestID(r.Context()); id != "" {
		lg = lg.With(slog.String("request_id", id))
	}
	return lg.With(
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", p.Status),
		slog.String("code", p.Code),
	)
}

// WriteProblem writes p to w as an application/problem+json response. A 5xx
// is sent with a fixed generic detail in place of p.Detail. When p.Instance is
// empty and the request has a request id, instance is "urn:uuid:<request-id>".
//
// Started-detection works only through a [middleware.ResponseWriter]: when w
// is one and [middleware.ResponseStarted] reports true, WriteProblem writes
// nothing and returns [ErrResponseStarted]. [Handler] always passes one. A
// direct caller with any other writer gets the problem written
// unconditionally, even after a status or body has gone out; wrap w with
// [middleware.AsResponseWriter] before writing to it to get the check.
func WriteProblem(w http.ResponseWriter, r *http.Request, p *Problem) error {
	if middleware.ResponseStarted(w) {
		return ErrResponseStarted
	}
	doc := problemDocument{
		Type:     p.Type,
		Title:    p.Title,
		Status:   p.Status,
		Detail:   p.Detail,
		Instance: p.Instance,
		Code:     p.Code,
	}
	if doc.Status >= http.StatusInternalServerError {
		doc.Detail = serverErrorDetail
	}
	if doc.Type == "" {
		doc.Type = "about:blank"
	}
	if doc.Instance == "" {
		if id := middleware.GetRequestID(r.Context()); id != "" {
			doc.Instance = "urn:uuid:" + id
		}
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("httpx: marshal problem: %w", err)
	}
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(doc.Status)
	if _, err = w.Write(body); err != nil {
		return fmt.Errorf("httpx: write problem: %w", err)
	}
	return nil
}
