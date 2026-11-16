package httpx

import (
	"errors"
	"net/http"
)

// The client-error helpers take an error code and the public detail, and
// send the code as given, even when it is empty. Use [NewProblem] to attach
// a cause or to have an empty code rejected.

// BadRequest returns a 400 problem.
func BadRequest(code, detail string) *Problem {
	return clientProblem(http.StatusBadRequest, code, detail)
}

// Unauthorized returns a 401 problem.
func Unauthorized(code, detail string) *Problem {
	return clientProblem(http.StatusUnauthorized, code, detail)
}

// Forbidden returns a 403 problem.
func Forbidden(code, detail string) *Problem {
	return clientProblem(http.StatusForbidden, code, detail)
}

// NotFound returns a 404 problem.
func NotFound(code, detail string) *Problem {
	return clientProblem(http.StatusNotFound, code, detail)
}

// Conflict returns a 409 problem.
func Conflict(code, detail string) *Problem {
	return clientProblem(http.StatusConflict, code, detail)
}

// UnprocessableEntity returns a 422 problem.
func UnprocessableEntity(code, detail string) *Problem {
	return clientProblem(http.StatusUnprocessableEntity, code, detail)
}

// Internal returns a 500 problem with the code "internal_server_error". It
// takes no code, so it always sends that one. The cause is logged and never
// sent. Use [NewProblem] for a custom code.
func Internal(cause error) *Problem {
	return serverProblem(http.StatusInternalServerError, "internal_server_error", cause)
}

// ServiceUnavailable returns a 503 problem with the code
// "service_unavailable". It takes no code, so it always sends that one. The
// cause is logged and never sent. Use [NewProblem] for a custom code.
func ServiceUnavailable(cause error) *Problem {
	return serverProblem(http.StatusServiceUnavailable, "service_unavailable", cause)
}

func clientProblem(status int, code, detail string) *Problem {
	return &Problem{Title: http.StatusText(status), Detail: detail, Code: code, Status: status}
}

// serverProblem builds a 5xx problem. A nil cause is replaced so the log
// still says where the problem came from.
func serverProblem(status int, code string, cause error) *Problem {
	if cause == nil {
		cause = errors.New("httpx: server problem built with a nil cause")
	}
	return &Problem{Title: http.StatusText(status), Code: code, Status: status, cause: cause}
}
