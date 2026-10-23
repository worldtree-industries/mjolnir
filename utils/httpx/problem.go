// Package httpx is the HTTP translation layer: a JSON codec, RFC 9457 problem
// responses, and the handler adapter that turns a returned Problem into a
// response. It replaces the former errorx package.
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

const problemContentType = "application/problem+json"

// ErrResponseStarted is returned by WriteProblem when the response has already
// been committed. The caller must not attempt to write a second body.
var ErrResponseStarted = errors.New("httpx: response already started")

// Problem is an RFC 9457 problem detail with a stable code extension.
//
// The envelope is application/problem+json:
//
//	{
//	  "type": "about:blank",
//	  "title": "Bad Request",
//	  "status": 400,
//	  "detail": "name is required",
//	  "instance": "urn:uuid:...",
//	  "code": "validation_failed"
//	}
//
// Detail is public and request-specific for 4xx responses. For 5xx responses
// it is replaced with the status text: the cause stays server-side and is
// logged by Handler. Type is about:blank because the stable machine identity
// is Code; Instance identifies the occurrence with the request id.
//
//nolint:errname // Problem is the RFC 9457 name.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`

	cause error
}

// NewProblem builds a validated Problem. Status must be 4xx or 5xx, Code must
// be non-empty, and a 5xx must have a cause. Detail is public for 4xx and is
// replaced with a generic message for 5xx.
func NewProblem(status int, code, detail string, cause error) (*Problem, error) {
	p := &Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
		cause:  cause,
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// Error implements error. The cause is not part of the message.
func (p *Problem) Error() string {
	if p == nil {
		return "httpx: nil problem"
	}
	if p.Detail == "" {
		return p.Code
	}
	return p.Code + ": " + p.Detail
}

// Unwrap exposes the internal cause for logging and [errors.Is] / [errors.As].
func (p *Problem) Unwrap() error {
	if p == nil {
		return nil
	}
	return p.cause
}

func (p *Problem) validate() error {
	if p == nil {
		return errors.New("httpx: nil problem")
	}
	if p.Status < http.StatusBadRequest || p.Status > 599 {
		return fmt.Errorf("httpx: problem status %d is not 4xx or 5xx", p.Status)
	}
	if p.Code == "" {
		return errors.New("httpx: problem code is empty")
	}
	if p.Status >= http.StatusInternalServerError && p.cause == nil {
		return fmt.Errorf("httpx: %d problem requires a cause", p.Status)
	}
	return nil
}

// WriteProblem writes p as application/problem+json. It returns
// ErrResponseStarted without writing when the response has already begun.
//
// Instance defaults to urn:uuid:<request-id> when the request carries an id.
func WriteProblem(w http.ResponseWriter, r *http.Request, p *Problem) error {
	if err := p.validate(); err != nil {
		return err
	}
	if responseStarted(w) {
		return ErrResponseStarted
	}

	out := *p
	if out.Type == "" {
		out.Type = "about:blank"
	}
	if out.Title == "" {
		out.Title = http.StatusText(out.Status)
	}
	if out.Instance == "" {
		if id := middleware.GetRequestID(r.Context()); id != "" {
			out.Instance = "urn:uuid:" + id
		}
	}
	if out.Status >= http.StatusInternalServerError {
		out.Detail = http.StatusText(out.Status)
	}

	body, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("httpx: marshal problem: %w", err)
	}

	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(out.Status)
	if _, writeErr := w.Write(body); writeErr != nil {
		return fmt.Errorf("httpx: write problem: %w", writeErr)
	}
	return nil
}

// Handler adapts a handler that returns a Problem into an [http.Handler]. A nil
// return means the handler already wrote a successful response.
func Handler(fn func(http.ResponseWriter, *http.Request) *Problem) http.HandlerFunc {
	return HandlerWithLogger(fn, log.GetLogger("http"))
}

// HandlerWithLogger is Handler with an injected logger, primarily for tests
// and embedding.
func HandlerWithLogger(fn func(http.ResponseWriter, *http.Request) *Problem, lg *slog.Logger) http.HandlerFunc {
	if lg == nil {
		lg = log.GetLogger("http")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := fn(w, r)
		if p == nil {
			return
		}

		if err := p.validate(); err != nil {
			lg.ErrorContext(r.Context(), "invalid problem returned by handler",
				slog.Any("error", err),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			p, _ = NewProblem(http.StatusInternalServerError, "internal_error", "", err)
		}

		if p.Status >= http.StatusInternalServerError {
			logProblem(lg, r, p)
		}

		if err := WriteProblem(w, r, p); err != nil {
			if errors.Is(err, ErrResponseStarted) {
				lg.WarnContext(r.Context(), "problem not written: response already started",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("code", p.Code),
				)
				return
			}
			lg.ErrorContext(r.Context(), "failed to write problem",
				slog.Any("error", err),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
		}
	}
}

func logProblem(lg *slog.Logger, r *http.Request, p *Problem) {
	attrs := []any{
		slog.Any("error", p.cause),
		slog.Int("status", p.Status),
		slog.String("code", p.Code),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	}
	if id := middleware.GetRequestID(r.Context()); id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}
	lg.ErrorContext(r.Context(), "request failed", attrs...)
}

func responseStarted(w http.ResponseWriter) bool {
	rw, ok := w.(interface{ Status() int })
	return ok && rw.Status() != 0
}

func mustProblem(status int, code, detail string, cause error) *Problem {
	p, err := NewProblem(status, code, detail, cause)
	if err != nil {
		panic(err)
	}
	return p
}

// BadRequest returns a validated 400 problem.
func BadRequest(code, detail string) *Problem {
	return mustProblem(http.StatusBadRequest, code, detail, nil)
}

// Unauthorized returns a validated 401 problem.
func Unauthorized(code, detail string) *Problem {
	return mustProblem(http.StatusUnauthorized, code, detail, nil)
}

// Forbidden returns a validated 403 problem.
func Forbidden(code, detail string) *Problem {
	return mustProblem(http.StatusForbidden, code, detail, nil)
}

// NotFound returns a validated 404 problem.
func NotFound(code, detail string) *Problem {
	return mustProblem(http.StatusNotFound, code, detail, nil)
}

// Conflict returns a validated 409 problem.
func Conflict(code, detail string) *Problem {
	return mustProblem(http.StatusConflict, code, detail, nil)
}

// UnprocessableEntity returns a validated 422 problem.
func UnprocessableEntity(code, detail string) *Problem {
	return mustProblem(http.StatusUnprocessableEntity, code, detail, nil)
}

// Internal returns a validated 500 problem. The cause is required and never
// reaches the client.
func Internal(cause error) *Problem {
	return mustProblem(http.StatusInternalServerError, "internal_error", "", cause)
}

// ServiceUnavailable returns a validated 503 problem. The cause is required
// and never reaches the client.
func ServiceUnavailable(cause error) *Problem {
	return mustProblem(http.StatusServiceUnavailable, "service_unavailable", "", cause)
}
