package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// RequestID sets a canonical UUID request id on the context and response.
//
// A client-supplied X-Request-ID is accepted only when it parses as a UUID and
// is shortened to the canonical form. Empty, malformed, or oversized values are
// replaced with a freshly generated v4 UUID. The value is echoed in
// X-Request-ID either way.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := requestID(r.Header.Get("X-Request-ID"))
		w.Header().Set("X-Request-ID", reqID)

		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestID(inbound string) string {
	parsed, err := uuid.Parse(inbound)
	if err != nil {
		return generateRequestID()
	}
	return parsed.String()
}

// GetRequestID returns the canonical request id stored on ctx, or "".
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

func generateRequestID() string {
	return uuid.NewString()
}
