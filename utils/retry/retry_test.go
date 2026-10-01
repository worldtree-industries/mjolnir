package retry

import (
	"context"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 100*time.Millisecond {
		t.Errorf("expected InitialDelay=100ms, got %v", cfg.InitialDelay)
	}
}

func TestDoSuccess(t *testing.T) {
	err := Do(context.TODO(), DefaultConfig(), func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
}
