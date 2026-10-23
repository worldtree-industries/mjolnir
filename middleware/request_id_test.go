package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRequestID(t *testing.T) {
	t.Parallel()

	valid := uuid.NewString()
	urn := "urn:uuid:" + valid
	braced := "{" + valid + "}"
	hex32 := strings.ReplaceAll(valid, "-", "")

	tests := []struct {
		name      string
		inbound   string
		want      string // exact canonical value, or "" for "generated"
		generated bool
	}{
		{name: "no header", inbound: "", generated: true},
		{name: "canonical UUID", inbound: valid, want: valid},
		{name: "uppercase UUID", inbound: strings.ToUpper(valid), want: valid},
		{name: "URN form", inbound: urn, want: valid},
		{name: "braced form", inbound: braced, want: valid},
		{name: "32 hex", inbound: hex32, want: valid},
		{name: "malformed", inbound: "test-request-id-123", generated: true},
		{name: "oversized", inbound: strings.Repeat("a", 4096), generated: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured string
			handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				captured = GetRequestID(r.Context())
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.inbound != "" {
				req.Header.Set("X-Request-ID", tt.inbound)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			respID := rr.Header().Get("X-Request-ID")
			if respID == "" {
				t.Fatal("X-Request-ID header not set in response")
			}
			if captured != respID {
				t.Errorf("context request ID (%s) doesn't match header value (%s)", captured, respID)
			}

			parsed, err := uuid.Parse(respID)
			if err != nil {
				t.Fatalf("response request ID %q is not a UUID: %v", respID, err)
			}
			if parsed.String() != respID {
				t.Errorf("response request ID %q is not canonical (%q)", respID, parsed.String())
			}

			if tt.generated {
				if parsed.Version() != 4 {
					t.Errorf("generated request ID version = %d, want 4", parsed.Version())
				}
				if respID == tt.inbound {
					t.Error("invalid client value was echoed instead of replaced")
				}
				return
			}
			if respID != tt.want {
				t.Errorf("request ID = %q, want %q", respID, tt.want)
			}
		})
	}
}

func TestGetRequestID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "context with request ID",
			ctx:      context.WithValue(context.Background(), requestIDKey, "test-id"),
			expected: "test-id",
		},
		{
			name:     "context without request ID",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "context with wrong type",
			ctx:      context.WithValue(context.Background(), requestIDKey, 123),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetRequestID(tt.ctx)
			if got != tt.expected {
				t.Errorf("GetRequestID() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGenerateRequestID(t *testing.T) {
	t.Parallel()
	ids := make(map[string]bool)
	iterations := 1000

	for range iterations {
		id := generateRequestID()

		parsed, err := uuid.Parse(id)
		if err != nil {
			t.Fatalf("generated ID %q is not a UUID: %v", id, err)
		}
		if parsed.Version() != 4 {
			t.Errorf("generated ID version = %d, want 4", parsed.Version())
		}
		if ids[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

func BenchmarkRequestIDMiddleware(b *testing.B) {
	handler := RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for range b.N {
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkRequestIDMiddlewareWithExisting(b *testing.B) {
	handler := RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", uuid.NewString())
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for range b.N {
		handler.ServeHTTP(rr, req)
	}
}
