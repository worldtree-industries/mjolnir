package retry

import (
	"context"
	"errors"
	"time"
)

// Config controls the retry policy used by [Do]. Zero fields are replaced
// individually by their [DefaultConfig] values, so a partial Config is valid.
type Config struct {
	// MaxAttempts is the total number of calls to the retried function,
	// including the first one. Values less than one fall back to the default.
	MaxAttempts int
	// InitialDelay is the wait before the second attempt. Values less than or
	// equal to zero fall back to the default, and the delay is capped by
	// MaxDelay.
	InitialDelay time.Duration
	// MaxDelay caps the wait between attempts. Values less than or equal to
	// zero fall back to the default.
	MaxDelay time.Duration
	// ShouldRetry reports whether an attempt that failed with err should be
	// retried. When nil, every error is retried except [context.Canceled] and
	// [context.DeadlineExceeded].
	ShouldRetry func(error) bool
}

// DefaultConfig returns the retry policy used for zero Config fields.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,                      //nolint:mnd // default value
		InitialDelay: 100 * time.Millisecond, //nolint:mnd // default value
		MaxDelay:     5 * time.Second,        //nolint:mnd // default value
	}
}

// Do calls fn until it succeeds, ShouldRetry returns false, MaxAttempts is
// reached, or ctx is done. It passes ctx to fn and returns the last error.
// When ctx is done while waiting between attempts, it returns ctx.Err().
func Do(ctx context.Context, cfg Config, fn func(context.Context) error) error {
	cfg = withDefaults(cfg)

	delay := cfg.InitialDelay

	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}
		if !cfg.shouldRetry(err) || attempt >= cfg.MaxAttempts {
			return err
		}
		if waitErr := wait(ctx, delay); waitErr != nil {
			return waitErr
		}
		delay = nextDelay(delay, cfg.MaxDelay)
	}
}

// withDefaults fills each zero or negative field from DefaultConfig and
// clamps the initial delay to the maximum delay.
func withDefaults(cfg Config) Config {
	defaults := DefaultConfig()

	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaults.MaxAttempts
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = defaults.InitialDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = defaults.MaxDelay
	}
	if cfg.InitialDelay > cfg.MaxDelay {
		cfg.InitialDelay = cfg.MaxDelay
	}

	return cfg
}

func (cfg Config) shouldRetry(err error) bool {
	if cfg.ShouldRetry != nil {
		return cfg.ShouldRetry(err)
	}

	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// nextDelay doubles delay without exceeding maxDelay.
func nextDelay(delay, maxDelay time.Duration) time.Duration {
	if delay >= maxDelay || delay > maxDelay/2 {
		return maxDelay
	}

	return delay * 2 //nolint:mnd // exponential backoff doubles the delay
}

// wait blocks for delay or until ctx is done, whichever comes first.
func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
