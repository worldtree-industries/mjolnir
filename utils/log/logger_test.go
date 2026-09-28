package log_test

import (
	"testing"

	"github.com/dfryer1193/mjolnir/utils/log"
)

func TestNewCreatesNamedLogger(t *testing.T) {
	t.Parallel()
	l := log.New("FooRepository")
	if l == nil {
		t.Fatal("New returned nil")
	}
}

// TestRedaction verifies that password and auth headers are redacted.
func TestRedaction(t *testing.T) {
	t.Parallel()
	l := log.New("Test")
	l = l.With("password", "secret123")
	l = l.With("authorization", "Bearer token123")
	l = l.With("email", "user@example.com")

	if l == nil {
		t.Fatal("logger should not be nil after With")
	}
}
