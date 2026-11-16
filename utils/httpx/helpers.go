package httpx

import (
	"errors"
	"net/http"
	"strings"
)

// The client-error helpers take an error code and the public detail. An empty
// code defaults to the status text in snake case. [NewProblem] rejects an
// empty code instead, but a helper has no error to return it through, so it
// fills in the default. Use [NewProblem] to attach a cause.

// BadRequest returns a 400 problem. An empty code defaults to "bad_request":
// a helper cannot return an error, so unlike [NewProblem] it does not reject one.
func BadRequest(code, detail string) *Problem {
	return clientProblem(http.StatusBadRequest, code, detail)
}

// Unauthorized returns a 401 problem. An empty code defaults to "unauthorized":
// a helper cannot return an error, so unlike [NewProblem] it does not reject one.
func Unauthorized(code, detail string) *Problem {
	return clientProblem(http.StatusUnauthorized, code, detail)
}

// Forbidden returns a 403 problem. An empty code defaults to "forbidden":
// a helper cannot return an error, so unlike [NewProblem] it does not reject one.
func Forbidden(code, detail string) *Problem {
	return clientProblem(http.StatusForbidden, code, detail)
}

// NotFound returns a 404 problem. An empty code defaults to "not_found":
// a helper cannot return an error, so unlike [NewProblem] it does not reject one.
func NotFound(code, detail string) *Problem {
	return clientProblem(http.StatusNotFound, code, detail)
}

// Conflict returns a 409 problem. An empty code defaults to "conflict":
// a helper cannot return an error, so unlike [NewProblem] it does not reject one.
func Conflict(code, detail string) *Problem {
	return clientProblem(http.StatusConflict, code, detail)
}

// UnprocessableEntity returns a 422 problem. An empty code defaults to "unprocessable_entity":
// a helper cannot return an error, so unlike [NewProblem] it does not reject one.
func UnprocessableEntity(code, detail string) *Problem {
	return clientProblem(http.StatusUnprocessableEntity, code, detail)
}

// Internal returns a 500 problem with the code "internal_server_error". The
// cause is logged and never sent. Use [NewProblem] for a custom code.
func Internal(cause error) *Problem {
	return serverProblem(http.StatusInternalServerError, cause)
}

// ServiceUnavailable returns a 503 problem with the code
// "service_unavailable". The cause is logged and never sent. Use [NewProblem]
// for a custom code.
func ServiceUnavailable(cause error) *Problem {
	return serverProblem(http.StatusServiceUnavailable, cause)
}

func clientProblem(status int, code, detail string) *Problem {
	if code == "" {
		code = defaultCode(status)
	}
	return &Problem{Title: http.StatusText(status), Detail: detail, Code: code, Status: status}
}

// serverProblem builds a 5xx problem with the default code. A nil cause is
// replaced so the log still says where the problem came from.
func serverProblem(status int, cause error) *Problem {
	if cause == nil {
		cause = errors.New("httpx: server problem built with a nil cause")
	}
	return &Problem{Title: http.StatusText(status), Code: defaultCode(status), Status: status, cause: cause}
}

// defaultCode is the status text in snake case, e.g. "not_found".
func defaultCode(status int) string {
	return strings.ReplaceAll(strings.ToLower(http.StatusText(status)), " ", "_")
}
