package httpx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// RespondJSON sends a JSON response with proper headers.
func RespondJSON(w http.ResponseWriter, _ *http.Request, status int, payload any) error {
	w.Header().Set("Content-Type", "application/json")

	response, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	w.WriteHeader(status)

	if _, err = w.Write(response); err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	return nil
}

// DecodeJSON decodes JSON request body into the provided struct.
func DecodeJSON(r *http.Request, v any) ([]byte, error) {
	if !ValidateContentType(r, "application/json") {
		return nil, fmt.Errorf("Content-Type %s is not supported", r.Header.Get("Content-Type"))
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	defer r.Body.Close()

	if err = json.Unmarshal(bodyBytes, v); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}
	return bodyBytes, nil
}

// ValidateContentType checks if the request has the required content type.
func ValidateContentType(r *http.Request, contentType string) bool {
	return r.Header.Get("Content-Type") == contentType
}
