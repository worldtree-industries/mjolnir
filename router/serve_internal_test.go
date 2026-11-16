package router

import (
	"testing"
	"time"
)

func TestDrainBudgetSplitsTheTotalBetweenWaitAndShutdown(t *testing.T) {
	t.Parallel()

	cases := []struct {
		total, wait, shutdown time.Duration
	}{
		{total: 10 * time.Second, wait: 5 * time.Second, shutdown: 5 * time.Second},
		{total: 300 * time.Millisecond, wait: 150 * time.Millisecond, shutdown: 150 * time.Millisecond},
		// An odd nanosecond goes to Shutdown, so the parts always sum to total.
		{total: 3, wait: 1, shutdown: 2},
		{total: 1, wait: 0, shutdown: 1},
		{total: 0, wait: 0, shutdown: 0},
		{total: -time.Second, wait: 0, shutdown: 0},
	}

	for _, c := range cases {
		wait, shutdown := drainBudget(c.total)
		if wait != c.wait || shutdown != c.shutdown {
			t.Errorf("drainBudget(%v) = (%v, %v), want (%v, %v)", c.total, wait, shutdown, c.wait, c.shutdown)
		}
	}
}
