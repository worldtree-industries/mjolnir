package errorx

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/log"
)

type ErrorReturningHandler func(w http.ResponseWriter, r *http.Request) *APIError

type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}
type APIError struct {
	err  error
	code int
}

func NewAPIError(err error, code int) *APIError {
	return &APIError{
		err:  err,
		code: code,
	}
}

func (e *APIError) Error() string {
	return e.err.Error()
}

func (e *APIError) asErrorResponse() ErrorResponse {
	return ErrorResponse{
		Error: e.err.Error(),
		Code:  e.code,
	}
}

func InternalServerErr(err error) *APIError {
	return &APIError{
		err:  err,
		code: http.StatusInternalServerError,
	}
}

func BadRequestErr(err error) *APIError {
	return &APIError{
		err:  err,
		code: http.StatusBadRequest,
	}
}

func UnauthorizedErr(err error) *APIError {
	return &APIError{
		err:  err,
		code: http.StatusUnauthorized,
	}
}

func ErrorHandler(h ErrorReturningHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			handleError(w, r, err)
		}
	}
}

func handleError(w http.ResponseWriter, r *http.Request, reqErr *APIError) {
	if reqErr != nil {
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		if reqErr.code >= http.StatusInternalServerError {
			var args []any
			if id := middleware.GetRequestID(r.Context()); id != "" {
				args = append(args, slog.String("request_id", id))
			}
			args = append(args,
				slog.Any("error", reqErr.err),
				slog.Int("status", reqErr.code),
				slog.String("path", r.URL.Path),
				slog.String("method", r.Method),
			)
			log.GetLogger("errorx").ErrorContext(r.Context(), "internal server error occurred", args...)

			w.WriteHeader(reqErr.code)
			_ = encoder.Encode(ErrorResponse{
				Error: "Internal Server Error",
				Code:  http.StatusInternalServerError,
			})
			return
		}

		w.WriteHeader(reqErr.code)
		_ = encoder.Encode(reqErr.asErrorResponse())
	}
}
