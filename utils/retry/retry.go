package retry

import (
	"context"
	"time"
)

type Config struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	RetryTypes   []string // error types that trigger retry (e.g. "network", "timeout")
}

func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,                      //nolint:mnd // default value
		InitialDelay: 100 * time.Millisecond, //nolint:mnd // default value
		MaxDelay:     5 * time.Second,        //nolint:mnd // default value
		RetryTypes:   []string{"network", "timeout", "server_error"},
	}
}

func Do(_ context.Context, cfg Config, fn func() error) error {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultConfig()
	}

	delay := cfg.InitialDelay
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !shouldRetry(err, cfg.RetryTypes) {
			return err
		}
		if attempt == cfg.MaxAttempts {
			return err
		}
		time.Sleep(delay)
		delay *= 2
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}
	return nil
}

func shouldRetry(_ error, _ []string) bool {
	return true
}
