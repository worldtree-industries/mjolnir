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

// DefaultMaxBodyBytes is the request body limit a [Codec] applies when none
// is configured: 1 MiB.
const DefaultMaxBodyBytes int64 = 1 << 20

// Codec decodes JSON request bodies and writes JSON responses. The zero value
// is ready to use and applies [DefaultMaxBodyBytes].
type Codec struct {
	// MaxBodyBytes bounds the request body Decode reads. Zero or negative
	// means DefaultMaxBodyBytes.
	MaxBodyBytes int64
}

// NewCodec returns a [Codec] with the given body limit. Zero or negative
// means [DefaultMaxBodyBytes].
func NewCodec(maxBodyBytes int64) Codec {
	return Codec{MaxBodyBytes: maxBodyBytes}
}

const (
	unsupportedMediaTypeDetail = "The request body must be JSON (application/json or a +json type) encoded as UTF-8."
	tooLargeDetail             = "The request body is larger than the server accepts."
	malformedJSONDetail        = "The request body is not a single valid JSON value of the expected shape."
)

// Decode reads one JSON value from the request body into dst.
//
// The Content-Type must be application/json or a +json type (such as
// application/vnd.api+json); a charset parameter, if any, must be UTF-8.
// Otherwise the problem is a 415 with the code "unsupported_media_type". A
// body that is empty, is not valid JSON, does not fit dst, or holds more than
// one value is a 400 with the code "malformed_json". A body longer than the
// codec's limit is a 413 with the code "request_too_large"; the body is read
// through [http.MaxBytesReader], so the server closes the connection after
// the response. A dst that is not a non-nil pointer is a server bug and
// returns a 500. The underlying error is kept as the cause and never sent.
func (c Codec) Decode(w http.ResponseWriter, r *http.Request, dst any) *Problem {
	if !isJSONMediaType(r.Header.Get("Content-Type")) {
		return clientProblem(http.StatusUnsupportedMediaType, "unsupported_media_type", unsupportedMediaTypeDetail)
	}
	if r.Body == nil {
		return malformed(io.EOF)
	}
	limit := c.MaxBodyBytes
	if limit <= 0 {
		limit = DefaultMaxBodyBytes
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := dec.Decode(dst); err != nil {
		if isMaxBytesError(err) {
			return tooLarge(err)
		}
		if _, ok := errors.AsType[*json.InvalidUnmarshalError](err); ok {
			return Internal(fmt.Errorf("httpx: decode target: %w", err))
		}
		return malformed(err)
	}
	// Read to the end so trailing data is rejected and counts against the limit.
	switch _, err := dec.Token(); {
	case errors.Is(err, io.EOF):
	case isMaxBytesError(err):
		return tooLarge(err)
	default:
		return malformed(errors.New("httpx: request body holds more than one JSON value"))
	}
	return nil
}

func isMaxBytesError(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)
	return ok
}

func tooLarge(cause error) *Problem {
	p := clientProblem(http.StatusRequestEntityTooLarge, "request_too_large", tooLargeDetail)
	p.cause = cause
	return p
}

func malformed(cause error) *Problem {
	p := clientProblem(http.StatusBadRequest, "malformed_json", malformedJSONDetail)
	p.cause = cause
	return p
}

// isJSONMediaType reports whether contentType is application/json or a
// type with the +json structured syntax suffix, with no charset other than
// UTF-8.
func isJSONMediaType(contentType string) bool {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	if cs, ok := params["charset"]; ok && !strings.EqualFold(cs, "utf-8") {
		return false
	}
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// Respond writes payload as a JSON response with the given status. The payload
// is marshalled before anything is written, so a marshal failure returns a 500
// problem and leaves the response untouched for [Handler] to write.
//
// A failed write (typically a client that went away) also returns a 500
// problem; by then the response has started, so [Handler] logs it rather than
// sending it.
func (Codec) Respond(w http.ResponseWriter, _ *http.Request, status int, payload any) *Problem {
	body, err := json.Marshal(payload)
	if err != nil {
		return Internal(fmt.Errorf("httpx: marshal response: %w", err))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err = w.Write(body); err != nil {
		return Internal(fmt.Errorf("httpx: write response: %w", err))
	}
	return nil
}
