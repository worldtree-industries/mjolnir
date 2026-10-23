package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 100*time.Millisecond {
		t.Errorf("expected InitialDelay=100ms, got %v", cfg.InitialDelay)
	}
	if cfg.MaxDelay != 5*time.Second {
		t.Errorf("expected MaxDelay=5s, got %v", cfg.MaxDelay)
	}
	if cfg.ShouldRetry != nil {
		t.Error("expected ShouldRetry=nil")
	}
}

func TestDoSuccess(t *testing.T) {
	t.Parallel()

	err := Do(context.Background(), DefaultConfig(), func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
}

func TestDoPassesContext(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "value")

	err := Do(ctx, DefaultConfig(), func(got context.Context) error {
		if got != ctx {
			t.Error("expected the caller's context to be passed to fn")
		}

		return nil
	})
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
}

func TestDoStopsWhenPredicateReturnsFalse(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("permanent")
	calls := 0
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		ShouldRetry: func(error) bool {
			return false
		},
	}

	err := Do(context.Background(), cfg, func(context.Context) error {
		calls++

		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 attempt, got %d", calls)
	}
}

func TestDoNilPredicateRetriesNormalError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("transient")
	calls := 0
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
	}

	err := Do(context.Background(), cfg, func(context.Context) error {
		calls++

		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

func TestDoNilPredicateStopsOnContextErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		err  error
	}{
		{name: "canceled", err: context.Canceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			cfg := Config{
				MaxAttempts:  3,
				InitialDelay: time.Millisecond,
				MaxDelay:     time.Millisecond,
			}

			err := Do(context.Background(), cfg, func(context.Context) error {
				calls++

				return testCase.err
			})
			if !errors.Is(err, testCase.err) {
				t.Errorf("expected %v, got %v", testCase.err, err)
			}
			if calls != 1 {
				t.Errorf("expected 1 attempt, got %d", calls)
			}
		})
	}
}

func TestDoReturnsPromptlyWhenCanceledDuringBackoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	err := Do(ctx, Config{
		MaxAttempts:  3,
		InitialDelay: time.Second,
		MaxDelay:     time.Second,
	}, func(context.Context) error {
		cancel()

		return errors.New("transient")
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("expected cancellation to interrupt the backoff, waited %v", elapsed)
	}
}

func TestDoReturnsLastErrorAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	calls := 0
	cfg := Config{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
	}

	err := Do(context.Background(), cfg, func(context.Context) error {
		calls++

		return fmt.Errorf("attempt %d", calls)
	})
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
	if err == nil || err.Error() != "attempt 3" {
		t.Errorf("expected the last error, got %v", err)
	}
}

func TestDoMergesPartialConfig(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("transient")
	calls := 0
	cfg := Config{
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
	}

	err := Do(context.Background(), cfg, func(context.Context) error {
		calls++
		if calls < 3 {
			return sentinel
		}

		return nil
	})
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected the default MaxAttempts=3, got %d attempts", calls)
	}
}

func TestWithDefaultsMergesFieldsIndividually(t *testing.T) {
	t.Parallel()

	defaults := DefaultConfig()
	cfg := withDefaults(Config{
		MaxAttempts:  7,
		InitialDelay: 2 * time.Millisecond,
	})

	if cfg.MaxAttempts != 7 {
		t.Errorf("expected MaxAttempts=7, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 2*time.Millisecond {
		t.Errorf("expected InitialDelay=2ms, got %v", cfg.InitialDelay)
	}
	if cfg.MaxDelay != defaults.MaxDelay {
		t.Errorf("expected MaxDelay=%v, got %v", defaults.MaxDelay, cfg.MaxDelay)
	}
}

func TestWithDefaultsClampsInitialDelayToMaxDelay(t *testing.T) {
	t.Parallel()

	cfg := withDefaults(Config{
		InitialDelay: time.Second,
		MaxDelay:     time.Millisecond,
	})

	if cfg.InitialDelay != time.Millisecond {
		t.Errorf("expected InitialDelay=1ms, got %v", cfg.InitialDelay)
	}
}

func TestNextDelayDoublesAndCaps(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		delay    time.Duration
		maxDelay time.Duration
		want     time.Duration
	}{
		{
			name:     "doubles",
			delay:    time.Millisecond,
			maxDelay: 10 * time.Millisecond,
			want:     2 * time.Millisecond,
		},
		{
			name:     "caps below max",
			delay:    8 * time.Millisecond,
			maxDelay: 10 * time.Millisecond,
			want:     10 * time.Millisecond,
		},
		{
			name:     "already at max",
			delay:    10 * time.Millisecond,
			maxDelay: 10 * time.Millisecond,
			want:     10 * time.Millisecond,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := nextDelay(testCase.delay, testCase.maxDelay)
			if got != testCase.want {
				t.Errorf("nextDelay(%v, %v) = %v, want %v",
					testCase.delay, testCase.maxDelay, got, testCase.want)
			}
		})
	}
}
