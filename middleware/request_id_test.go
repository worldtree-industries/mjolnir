package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dfryer1193/mjolnir/middleware"
)

const canonicalID = "6ba7b810-9dad-41d1-80b4-00c04fd430c8"

// serveRequestID runs one request through RequestID and returns the echoed
// header and the id the next handler saw on its context.
func serveRequestID(t *testing.T, inbound string, set bool) (string, string) {
	t.Helper()
	var fromCtx string
	handler := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		fromCtx = middleware.GetRequestID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if set {
		req.Header.Set("X-Request-ID", inbound)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr.Header().Get("X-Request-ID"), fromCtx
}

// TestRequestIDAcceptsCanonicalUUIDs: every spelling [uuid.Parse] understands
// is accepted and echoed in its canonical lowercase hyphenated form.
func TestRequestIDAcceptsCanonicalUUIDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		inbound string
	}{
		{name: "canonical", inbound: canonicalID},
		{name: "uppercase", inbound: strings.ToUpper(canonicalID)},
		{name: "urn", inbound: "urn:uuid:" + canonicalID},
		{name: "braces", inbound: "{" + canonicalID + "}"},
		{name: "32 hex", inbound: strings.ReplaceAll(canonicalID, "-", "")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			header, fromCtx := serveRequestID(t, tt.inbound, true)
			if header != canonicalID {
				t.Errorf("X-Request-ID = %q, want %q", header, canonicalID)
			}
			if fromCtx != canonicalID {
				t.Errorf("GetRequestID = %q, want %q", fromCtx, canonicalID)
			}
		})
	}
}

// TestRequestIDReplacesInvalidValues: anything that is not a UUID, including
// a value too long to be one, is replaced by a fresh v4 UUID and never echoed.
func TestRequestIDReplacesInvalidValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		inbound string
		set     bool
	}{
		{name: "absent", set: false},
		{name: "empty", inbound: "", set: true},
		{name: "non-UUID", inbound: "req-abc-123", set: true},
		{name: "malformed hex", inbound: "6ba7b810-9dad-41d1-80b4-00c04fd430cz", set: true},
		{name: "truncated", inbound: canonicalID[:35], set: true},
		{name: "4 KB", inbound: strings.Repeat("a", 4096), set: true},
		{name: "4 KB after a valid UUID", inbound: canonicalID + strings.Repeat("0", 4096-len(canonicalID)), set: true},
		{name: "wrong prefix", inbound: "urn:uid:" + canonicalID + "0", set: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			header, fromCtx := serveRequestID(t, tt.inbound, tt.set)
			if header == tt.inbound || header == canonicalID {
				t.Fatalf("X-Request-ID echoed the inbound value %q", header)
			}
			assertGeneratedV4(t, header)
			if fromCtx != header {
				t.Errorf("GetRequestID = %q, want the echoed %q", fromCtx, header)
			}
		})
	}
}

// TestRequestIDGeneratesDistinctIDs: two requests without an id get different ones.
func TestRequestIDGeneratesDistinctIDs(t *testing.T) {
	t.Parallel()
	first, _ := serveRequestID(t, "", false)
	second, _ := serveRequestID(t, "", false)
	if first == second {
		t.Errorf("two requests shared the generated id %q", first)
	}
}

func TestGetRequestIDWithoutMiddleware(t *testing.T) {
	t.Parallel()
	if got := middleware.GetRequestID(context.Background()); got != "" {
		t.Errorf("GetRequestID = %q, want empty", got)
	}
}

func assertGeneratedV4(t *testing.T, id string) {
	t.Helper()
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("generated id %q is not a UUID: %v", id, err)
	}
	if parsed.String() != id {
		t.Errorf("generated id %q is not canonical", id)
	}
	if parsed.Version() != 4 || parsed.Variant() != uuid.RFC4122 {
		t.Errorf("generated id %q is version %d variant %v, want v4 RFC 4122", id, parsed.Version(), parsed.Variant())
	}
}

func BenchmarkRequestIDMiddleware(b *testing.B) {
	handler := middleware.RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for range b.N {
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkRequestIDMiddlewareWithExisting(b *testing.B) {
	handler := middleware.RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", canonicalID)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for range b.N {
		handler.ServeHTTP(rr, req)
	}
}
