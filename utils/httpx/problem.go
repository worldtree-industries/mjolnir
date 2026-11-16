package httpx

import (
	"errors"
	"fmt"
	"net/http"
)

// maxStatus is the largest status a [Problem] may carry.
const maxStatus = 599

// Problem is an HTTP error a handler returns instead of writing the response
// itself. It is sent as an RFC 9457 problem document (application/problem+json)
// with the members type, title, status, detail, and instance, plus a "code"
// extension member: a stable, machine-readable identifier clients can switch on.
//
// Detail follows one rule. For a 4xx status it is public: it is the only
// explanation the client receives, so write it for the client. The cause is
// never sent. For a 5xx status the detail and the cause are server-only: they
// are logged, and the client receives a fixed generic detail. The status and
// the code are always sent as the caller set them, so a 503 stays a 503.
//
// Build one with [NewProblem] or a helper such as [BadRequest]; the cause is
// unexported and reachable only through [Problem.Unwrap].
type Problem struct { //nolint:errname // the RFC 9457 name; spec #29 fixes it
	// Type is a URI reference identifying the problem type. Empty means
	// "about:blank": the problem is described by its status alone.
	Type string
	// Title is a short summary of the problem type; [NewProblem] sets it to
	// [http.StatusText] of the status.
	Title string
	// Detail explains this occurrence. Public for 4xx, server-only for 5xx.
	Detail string
	// Instance is a URI reference identifying this occurrence. When it is
	// empty and the request has a request id, the response carries
	// "urn:uuid:<request-id>".
	Instance string
	// Code is the machine-readable error code sent in the "code" member.
	Code string
	// Status is the HTTP status, 400 to 599.
	Status int

	cause error
}

// NewProblem builds a [Problem]. status must be a 4xx or 5xx status and code
// must be non-empty. A 5xx requires a non-nil cause, so every server error
// that reaches a log names what went wrong; a 4xx may have none. Title is set
// from [http.StatusText].
func NewProblem(status int, code, detail string, cause error) (*Problem, error) {
	switch {
	case status < http.StatusBadRequest || status > maxStatus:
		return nil, fmt.Errorf("httpx: problem status %d is not a 4xx or 5xx status", status)
	case code == "":
		return nil, errors.New("httpx: problem code is empty")
	case status >= http.StatusInternalServerError && cause == nil:
		return nil, fmt.Errorf("httpx: problem status %d requires a cause", status)
	}
	return &Problem{
		Title:  http.StatusText(status),
		Detail: detail,
		Code:   code,
		Status: status,
		cause:  cause,
	}, nil
}

// Error describes the problem for logs, including the server-only detail and
// the cause. It is never sent to the client.
func (p *Problem) Error() string {
	msg := fmt.Sprintf("%d %s", p.Status, p.Code)
	if p.Detail != "" {
		msg += ": " + p.Detail
	}
	if p.cause != nil {
		msg += ": " + p.cause.Error()
	}
	return msg
}

// Unwrap returns the cause, or nil when there is none.
func (p *Problem) Unwrap() error {
	return p.cause
}
