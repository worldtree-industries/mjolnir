package errorx

import (
	"encoding/json"
	"net/http"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/rs/zerolog/log"
)

type ErrorReturningHandler func(w http.ResponseWriter, r *http.Request) *ApiError

type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}
type ApiError struct {
	err  error
	code int
}

func (e *ApiError) Error() string {
	return e.err.Error()
}

func (e *ApiError) asErrorResponse() ErrorResponse {
	return ErrorResponse{
		Error: e.err.Error(),
		Code:  e.code,
	}
}

func InternalServerErr(err error) *ApiError {
	return &ApiError{
		err:  err,
		code: http.StatusInternalServerError,
	}
}

func BadRequestErr(err error) *ApiError {
	return &ApiError{
		err:  err,
		code: http.StatusBadRequest,
	}
}

func UnauthorizedErr(err error) *ApiError {
	return &ApiError{
		err:  err,
		code: http.StatusUnauthorized,
	}
}

func NewApiError(err error, code int) *ApiError {
	return &ApiError{
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

func handleError(w http.ResponseWriter, r *http.Request, reqErr *ApiError) {
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
			encoder.Encode(ErrorResponse{
				Error: "Internal Server Error",
				Code:  http.StatusInternalServerError,
			})
			return
		}

		w.WriteHeader(reqErr.code)
		encoder.Encode(reqErr.asErrorResponse())
	}
}
