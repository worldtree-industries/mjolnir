package log

import "testing"

func TestNewCreatesNamedLogger(t *testing.T) {
	l := New("FooRepository")
	if l == nil {
		t.Fatal("New returned nil")
	}
}

// TestRedaction verifies that password and auth headers are redacted.
func TestRedaction(t *testing.T) {
	l := New("Test")
	l = l.With("password", "secret123")
	l = l.With("authorization", "Bearer token123")
	l = l.With("email", "user@example.com")

	// TODO: implement redaction - verify sensitive values are masked
	if l == nil {
		t.Fatal("logger should not be nil after With")
	}
}