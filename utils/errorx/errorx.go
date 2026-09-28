package errorx

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/dfryer1193/mjolnir/middleware"
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

func NewAPIError(err error, code int) *APIError {
	return &APIError{
		err:  err,
		code: code,
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
			log.Error().
				Str("request_id", middleware.GetRequestID(r.Context())).
				Err(reqErr.err).
				Int("status", reqErr.code).
				Str("path", r.URL.Path).
				Str("method", r.Method).
				Msg("internal server error occurred")

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
