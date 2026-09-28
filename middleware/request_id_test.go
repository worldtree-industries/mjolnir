package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestID(t *testing.T) {
	tests := []struct {
		name            string
		existingReqID   string
		wantHeaderMatch bool
	}{
		{
			name:            "no existing request ID",
			existingReqID:   "",
			wantHeaderMatch: true,
		},
		{
			name:            "existing request ID",
			existingReqID:   "test-request-id-123",
			wantHeaderMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock handler to verify the context
			var capturedReqID string
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedReqID = GetRequestID(r.Context())
			})

			// Create the middleware handler
			handler := RequestID(nextHandler)

			// Create test request
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.existingReqID != "" {
				req.Header.Set("X-Request-ID", tt.existingReqID)
			}

			// Create response recorder
			rr := httptest.NewRecorder()

			// Execute request
			handler.ServeHTTP(rr, req)

			// Get response headers
			respID := rr.Header().Get("X-Request-ID")

			// Verify response header exists
			if respID == "" {
				t.Error("X-Request-ID header not set in response")
			}

			// Verify request ID was added to context
			if capturedReqID == "" {
				t.Error("request ID not found in context")
			}

			// Verify header matches context value
			if capturedReqID != respID {
				t.Errorf("context request ID (%s) doesn't match header value (%s)",
					capturedReqID, respID)
			}

			// If we provided a request ID, verify it was preserved
			if tt.existingReqID != "" && respID != tt.existingReqID {
				t.Errorf("expected request ID %s, got %s", tt.existingReqID, respID)
			}

			// Verify UUID format when no request ID was provided
			if tt.existingReqID == "" {
				if len(respID) != 36 { // UUID v4 length
					t.Errorf("generated request ID %s is not a valid UUID", respID)
				}
			}
		})
	}
}

func TestGetRequestID(t *testing.T) {
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
	// Test multiple generations to ensure uniqueness
	ids := make(map[string]bool)
	iterations := 1000

	for i := 0; i < iterations; i++ {
		id := generateRequestID()

		// Verify UUID length
		if len(id) != 36 {
			t.Errorf("generated ID length = %d, want 36", len(id))
		}

		// Verify uniqueness
		if ids[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

func BenchmarkRequestIDMiddleware(b *testing.B) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkRequestIDMiddlewareWithExisting(b *testing.B) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "test-request-id")
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(rr, req)
	}
}
