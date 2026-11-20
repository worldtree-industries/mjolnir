// Package retry calls a function again when it fails, with exponential
// backoff between attempts.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// Clock is the source of the waits between attempts. Its method set matches
// the clock interfaces of programs that inject one, so a fake clock from a
// caller's tests can drive retries without sleeping.
type Clock interface {
	After(d time.Duration) <-chan time.Time
}

// Policy configures [Run].
type Policy struct {
	// MaxAttempts is the number of times fn is called before Run gives up.
	// Zero calls fn once, the same as one.
	MaxAttempts uint
	// InitialDelay is the wait after the first failed attempt; each later
	// wait doubles it, up to MaxDelay.
	InitialDelay time.Duration
	// MaxDelay caps the wait between attempts.
	MaxDelay time.Duration
	// Jitter makes each wait a random duration between zero and the backoff
	// delay ("full jitter"), so clients retrying together do not synchronise.
	Jitter bool
	// Retryable reports whether an attempt's error is worth another attempt;
	// Run returns any other error at once. Nil retries every error except
	// context.Canceled and context.DeadlineExceeded.
	Retryable func(error) bool
	// Clock times the waits between attempts. Nil uses the real clock.
	Clock Clock
}

// DefaultPolicy returns three attempts, starting at 100ms and capped at 5s,
// without jitter, with the default Retryable and the real clock.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts:  3,                      //nolint:mnd // default value
		InitialDelay: 100 * time.Millisecond, //nolint:mnd // default value
		MaxDelay:     5 * time.Second,        //nolint:mnd // default value
	}
}

// Run calls fn with ctx until it succeeds, returns an error Retryable
// rejects, or MaxAttempts calls have failed, waiting between attempts with
// exponential backoff capped at MaxDelay. It returns the last attempt's
// error. If ctx is done during a wait, or when it ends, Run returns at once with an error
// wrapping both ctx.Err() and the last attempt's error.
func Run(ctx context.Context, p Policy, fn func(context.Context) error) error {
	retryable := p.Retryable
	if retryable == nil {
		retryable = notContextError
	}
	clock := p.Clock
	if clock == nil {
		clock = realClock{}
	}

	delay := min(p.InitialDelay, p.MaxDelay)
	for attempt := uint(1); ; attempt++ {
		err := fn(ctx)
		if err == nil || attempt >= p.MaxAttempts || !retryable(err) {
			return err
		}
		select {
		case <-ctx.Done():
		case <-clock.After(wait(delay, p.Jitter)):
		}
		// Checked after either case: the timer and ctx can be ready together,
		// and select picks between them at random.
		if ctx.Err() != nil {
			return fmt.Errorf("retry: %w: %w", ctx.Err(), err)
		}
		delay = min(delay*2, p.MaxDelay) //nolint:mnd // doubling is the backoff
	}
}

func wait(delay time.Duration, jitter bool) time.Duration {
	// rand.N panics on zero, and a zero delay has nothing to spread.
	if !jitter || delay <= 0 {
		return delay
	}

	return rand.N(delay + 1) //nolint:gosec // jitter needs spread, not unpredictability
}

func notContextError(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

type realClock struct{}

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
