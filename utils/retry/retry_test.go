package retry_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dfryer1193/mjolnir/utils/retry"
)

var errTransient = errors.New("transient")

// instantClock records every wait it is asked for and fires at once, so a
// retry loop runs to completion without sleeping.
type instantClock struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (c *instantClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()

	ch := make(chan time.Time, 1)
	ch <- time.Time{}

	return ch
}

func (c *instantClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.waits)
}

// parkedClock never fires; it signals waiting each time a wait begins, so a
// test can act while the retry loop is parked between attempts.
type parkedClock struct {
	waiting chan struct{}
}

func (c *parkedClock) After(time.Duration) <-chan time.Time {
	c.waiting <- struct{}{}

	return nil
}

// failTimes returns a fn that fails n times with err, then succeeds, and a
// pointer to the number of calls made.
func failTimes(n int, err error) (func(context.Context) error, *int) {
	calls := 0

	return func(context.Context) error {
		calls++
		if calls <= n {
			return err
		}

		return nil
	}, &calls
}

func TestRunRetriesUntilSuccessDoublingTheWaitUpToMaxDelay(t *testing.T) {
	t.Parallel()
	clock := &instantClock{}
	fn, calls := failTimes(4, errTransient)

	err := retry.Run(t.Context(), retry.Policy{
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		Clock:        clock,
	}, fn)
	if err != nil {
		t.Fatalf("Run: got %v, want nil", err)
	}
	if *calls != 5 {
		t.Errorf("calls: got %d, want 5", *calls)
	}
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
	}
	if got := clock.recorded(); !slices.Equal(got, want) {
		t.Errorf("waits: got %v, want %v", got, want)
	}
}

func TestRunCancelledDuringAWaitReturnsTheContextAndLastAttemptErrors(t *testing.T) {
	t.Parallel()
	clock := &parkedClock{waiting: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	fn, calls := failTimes(10, errTransient)

	done := make(chan error)
	go func() {
		done <- retry.Run(ctx, retry.Policy{
			MaxAttempts:  10,
			InitialDelay: time.Hour,
			MaxDelay:     time.Hour,
			Clock:        clock,
		}, fn)
	}()
	<-clock.waiting
	cancel()
	err := <-done

	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled): got false for %v", err)
	}
	if !errors.Is(err, errTransient) {
		t.Errorf("errors.Is(err, errTransient): got false for %v", err)
	}
	if *calls != 1 {
		t.Errorf("calls: got %d, want 1", *calls)
	}
}

type ctxKey struct{}

func TestRunPassesItsContextToFn(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), ctxKey{}, "marker")
	var got any

	err := retry.Run(ctx, retry.Policy{MaxAttempts: 1, Clock: &instantClock{}}, func(ctx context.Context) error {
		got = ctx.Value(ctxKey{})

		return nil
	})
	if err != nil {
		t.Fatalf("Run: got %v, want nil", err)
	}
	if got != "marker" {
		t.Errorf("fn's context value: got %v, want marker", got)
	}
}

func TestRunReturnsTheLastErrorOnceMaxAttemptsAreSpent(t *testing.T) {
	t.Parallel()
	clock := &instantClock{}
	fn, calls := failTimes(10, errTransient)

	err := retry.Run(t.Context(), retry.Policy{
		MaxAttempts:  3,
		InitialDelay: time.Second,
		MaxDelay:     time.Minute,
		Clock:        clock,
	}, fn)
	if !errors.Is(err, errTransient) {
		t.Errorf("err: got %v, want %v", err, errTransient)
	}
	if *calls != 3 {
		t.Errorf("calls: got %d, want 3", *calls)
	}
	if got := len(clock.recorded()); got != 2 {
		t.Errorf("waits: got %d, want 2 (none after the last attempt)", got)
	}
}

func TestRunReturnsANonRetryableErrorWithoutWaiting(t *testing.T) {
	t.Parallel()
	errPermanent := errors.New("permanent")
	clock := &instantClock{}
	fn, calls := failTimes(10, errPermanent)

	err := retry.Run(t.Context(), retry.Policy{
		MaxAttempts:  5,
		InitialDelay: time.Second,
		MaxDelay:     time.Minute,
		Retryable:    func(err error) bool { return !errors.Is(err, errPermanent) },
		Clock:        clock,
	}, fn)
	if !errors.Is(err, errPermanent) {
		t.Errorf("err: got %v, want %v", err, errPermanent)
	}
	if *calls != 1 {
		t.Errorf("calls: got %d, want 1", *calls)
	}
	if got := clock.recorded(); len(got) != 0 {
		t.Errorf("waits: got %v, want none", got)
	}
}

func TestRunWithoutRetryableDoesNotRetryContextErrors(t *testing.T) {
	t.Parallel()
	for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(ctxErr.Error(), func(t *testing.T) {
			t.Parallel()
			fn, calls := failTimes(10, fmt.Errorf("fetch: %w", ctxErr))

			err := retry.Run(t.Context(), retry.Policy{
				MaxAttempts: 5,
				Clock:       &instantClock{},
			}, fn)
			if !errors.Is(err, ctxErr) {
				t.Errorf("err: got %v, want %v", err, ctxErr)
			}
			if *calls != 1 {
				t.Errorf("calls: got %d, want 1", *calls)
			}
		})
	}
}

func TestRunWithoutRetryableRetriesOtherErrors(t *testing.T) {
	t.Parallel()
	fn, calls := failTimes(2, errTransient)

	err := retry.Run(t.Context(), retry.Policy{MaxAttempts: 5, Clock: &instantClock{}}, fn)
	if err != nil {
		t.Fatalf("Run: got %v, want nil", err)
	}
	if *calls != 3 {
		t.Errorf("calls: got %d, want 3", *calls)
	}
}

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()
	p := retry.DefaultPolicy()

	if p.MaxAttempts != 3 {
		t.Errorf("MaxAttempts: got %d, want 3", p.MaxAttempts)
	}
	if p.InitialDelay != 100*time.Millisecond {
		t.Errorf("InitialDelay: got %v, want 100ms", p.InitialDelay)
	}
	if p.MaxDelay != 5*time.Second {
		t.Errorf("MaxDelay: got %v, want 5s", p.MaxDelay)
	}
	if p.Jitter {
		t.Error("Jitter: got true, want false")
	}
	if p.Retryable != nil {
		t.Error("Retryable: got a func, want nil (the context-error default)")
	}
	if p.Clock != nil {
		t.Error("Clock: got a clock, want nil (the real clock)")
	}
}

func TestRunWithNoMaxAttemptsCallsFnOnce(t *testing.T) {
	t.Parallel()
	fn, calls := failTimes(10, errTransient)

	err := retry.Run(t.Context(), retry.Policy{Clock: &instantClock{}}, fn)
	if !errors.Is(err, errTransient) {
		t.Errorf("err: got %v, want %v", err, errTransient)
	}
	if *calls != 1 {
		t.Errorf("calls: got %d, want 1", *calls)
	}
}

func TestRunWithJitterWaitsAtMostTheBackoffDelay(t *testing.T) {
	t.Parallel()
	clock := &instantClock{}
	fn, _ := failTimes(20, errTransient)

	err := retry.Run(t.Context(), retry.Policy{
		MaxAttempts:  21,
		InitialDelay: time.Hour,
		MaxDelay:     8 * time.Hour,
		Jitter:       true,
		Clock:        clock,
	}, fn)
	if err != nil {
		t.Fatalf("Run: got %v, want nil", err)
	}

	waits := clock.recorded()
	if len(waits) != 20 {
		t.Fatalf("waits: got %d, want 20", len(waits))
	}
	ceiling := time.Hour
	shortened := false
	for i, got := range waits {
		if got < 0 || got > ceiling {
			t.Errorf("wait %d: got %v, want within [0, %v]", i, got, ceiling)
		}
		if got < ceiling {
			shortened = true
		}
		ceiling = min(ceiling*2, 8*time.Hour)
	}
	if !shortened {
		t.Errorf("waits: every wait was the full backoff delay, want jitter: %v", waits)
	}
}

// cancellingClock cancels the context and fires in the same call, so the
// wait ends with both the timer and the context ready.
type cancellingClock struct {
	cancel context.CancelFunc
}

func (c cancellingClock) After(time.Duration) <-chan time.Time {
	c.cancel()
	ch := make(chan time.Time, 1)
	ch <- time.Time{}

	return ch
}

func TestRunCancelledAsTheWaitEndsMakesNoFurtherAttempt(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	fn, calls := failTimes(10, errTransient)

	err := retry.Run(ctx, retry.Policy{
		MaxAttempts:  10,
		InitialDelay: time.Second,
		MaxDelay:     time.Second,
		Retryable:    func(error) bool { return true },
		Clock:        cancellingClock{cancel: cancel},
	}, fn)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errTransient) {
		t.Errorf("err: got %v, want one wrapping context.Canceled and errTransient", err)
	}
	if *calls != 1 {
		t.Errorf("calls: got %d, want 1", *calls)
	}
}

func TestRunCapsTheFirstWaitAtMaxDelay(t *testing.T) {
	t.Parallel()
	clock := &instantClock{}
	fn, _ := failTimes(2, errTransient)

	err := retry.Run(t.Context(), retry.Policy{
		MaxAttempts:  3,
		InitialDelay: time.Minute,
		MaxDelay:     time.Second,
		Clock:        clock,
	}, fn)
	if err != nil {
		t.Fatalf("Run: got %v, want nil", err)
	}
	want := []time.Duration{time.Second, time.Second}
	if got := clock.recorded(); !slices.Equal(got, want) {
		t.Errorf("waits: got %v, want %v", got, want)
	}
}
