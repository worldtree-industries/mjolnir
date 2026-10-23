package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// DefaultMaxBodyBytes is the request-body limit used by a zero Codec and by
// NewCodec(0).
const DefaultMaxBodyBytes int64 = 1 << 20

// Codec decodes JSON requests and writes JSON responses. The zero value is
// ready to use with DefaultMaxBodyBytes.
type Codec struct {
	MaxBodyBytes int64
}

// NewCodec returns a Codec with the given request-body limit. A limit <= 0
// uses DefaultMaxBodyBytes.
func NewCodec(maxBodyBytes int64) Codec {
	return Codec{MaxBodyBytes: maxBodyBytes}
}

func (c Codec) limit() int64 {
	if c.MaxBodyBytes <= 0 {
		return DefaultMaxBodyBytes
	}
	return c.MaxBodyBytes
}

// Decode reads a single JSON value from r.Body into dst. It accepts
// application/json and structured-syntax +json media types, with parameters.
// It never reads more than the codec's limit.
//
// A non-nil return is a ready-to-send Problem; the caller returns it from an
// httpx.Handler. Decode writes nothing itself.
func (c Codec) Decode(w http.ResponseWriter, r *http.Request, dst any) *Problem {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !isJSONMediaType(mediaType) {
		return mustProblem(http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/json or a +json media type", nil)
	}

	limit := c.limit()
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)

	if decodeErr := dec.Decode(dst); decodeErr != nil {
		return decodeProblem(decodeErr, limit)
	}
	if trailerErr := dec.Decode(&struct{}{}); !errors.Is(trailerErr, io.EOF) {
		if trailerErr == nil {
			return mustProblem(http.StatusBadRequest, "invalid_json",
				"request body must contain a single JSON value", nil)
		}
		return decodeProblem(trailerErr, limit)
	}
	return nil
}

func decodeProblem(err error, limit int64) *Problem {
	var maxErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxErr):
		return mustProblem(http.StatusRequestEntityTooLarge, "request_too_large",
			fmt.Sprintf("request body exceeds %d bytes", limit), nil)
	case errors.Is(err, io.EOF):
		return mustProblem(http.StatusBadRequest, "empty_body", "request body is empty", nil)
	default:
		return mustProblem(http.StatusBadRequest, "invalid_json", "request body is not valid JSON", nil)
	}
}

func isJSONMediaType(mediaType string) bool {
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// Respond marshals payload and writes it with the given status. It marshals
// before writing, so an encoding failure cannot leave a partial response.
//
// A non-nil return is a ready-to-send Problem. A response that has already
// started is left untouched.
func (c Codec) Respond(w http.ResponseWriter, _ *http.Request, status int, payload any) *Problem {
	if status < 100 || status > 599 {
		return mustProblem(http.StatusInternalServerError, "invalid_status",
			"", fmt.Errorf("httpx: invalid response status %d", status))
	}
	if responseStarted(w) {
		return mustProblem(http.StatusInternalServerError, "response_started",
			"", errors.New("httpx: response already started"))
	}

	if status == http.StatusNoContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return mustProblem(http.StatusInternalServerError, "encode_failed",
			"", fmt.Errorf("httpx: marshal response: %w", err))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, writeErr := w.Write(body); writeErr != nil {
		return mustProblem(http.StatusInternalServerError, "write_failed",
			"", fmt.Errorf("httpx: write response: %w", writeErr))
	}
	return nil
}
