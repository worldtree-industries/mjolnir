package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// maxRequestIDLen is the longest spelling [uuid.Parse] accepts:
// "urn:uuid:" followed by the 36-character canonical form.
const maxRequestIDLen = len("urn:uuid:") + 36

// RequestID gives every request a canonical UUID request id. It stores the id
// on the request context (read it with [GetRequestID]) and echoes it in the
// X-Request-ID response header.
//
// An inbound X-Request-ID is accepted only when it parses as a UUID; it is then
// rewritten to the canonical lowercase hyphenated form. Any other value,
// including one longer than any UUID spelling, is replaced by a fresh v4 UUID
// and never echoed. The policy is default-deny (ADR-0007).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := canonicalRequestID(r.Header.Get("X-Request-ID"))

		w.Header().Set("X-Request-ID", reqID)

		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID returns the request id [RequestID] stored on ctx: a canonical
// UUID, or "" when the middleware did not run.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// canonicalRequestID returns the canonical form of inbound when it is a UUID,
// and a fresh v4 UUID otherwise. The length is checked first so an oversized
// value is never handed to the parser.
func canonicalRequestID(inbound string) string {
	if inbound != "" && len(inbound) <= maxRequestIDLen {
		if id, err := uuid.Parse(inbound); err == nil {
			return id.String()
		}
	}
	return uuid.New().String()
}
