package router

import (
	"testing"
	"time"
)

func TestDrainBudgetSplitsOneTotal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		total    time.Duration
		wantWait time.Duration
		wantStop time.Duration
	}{
		{total: 0},
		{total: -time.Second},
		{total: time.Nanosecond, wantWait: 0, wantStop: time.Nanosecond},
		{total: 10 * time.Second, wantWait: 5 * time.Second, wantStop: 5 * time.Second},
		{total: 4 * time.Second, wantWait: 2 * time.Second, wantStop: 2 * time.Second},
	}

	for _, tt := range tests {
		wait, shutdown := drainBudget(tt.total)
		if wait != tt.wantWait || shutdown != tt.wantStop {
			t.Errorf("drainBudget(%v) = (%v, %v), want (%v, %v)",
				tt.total, wait, shutdown, tt.wantWait, tt.wantStop)
		}
		if tt.total > 0 && wait+shutdown != tt.total {
			t.Errorf("drainBudget(%v) spends %v, want exactly %v", tt.total, wait+shutdown, tt.total)
		}
	}
}
