package health_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dfryer1193/mjolnir/health"
)

func TestNewRequiresStarted(t *testing.T) {
	_, err := health.New(health.Config{Ready: health.AlwaysReady})
	if !errors.Is(err, health.ErrStartedRequired) {
		t.Fatalf("got %v, want ErrStartedRequired", err)
	}
}

func TestNewRequiresReady(t *testing.T) {
	_, err := health.New(health.Config{Started: health.AlwaysStarted})
	if !errors.Is(err, health.ErrReadyRequired) {
		t.Fatalf("got %v, want ErrReadyRequired", err)
	}
}

func TestAlwaysReady(t *testing.T) {
	if !health.AlwaysReady(context.Background()) {
		t.Fatal("AlwaysReady returned false")
	}
}

func TestAlwaysStarted(t *testing.T) {
	if !health.AlwaysStarted(context.Background()) {
		t.Fatal("AlwaysStarted returned false")
	}
}

func TestLivenessAnswersOKWithoutConsultingChecks(t *testing.T) {
	var calls atomic.Int32
	count := func(context.Context) bool {
		calls.Add(1)
		return false
	}

	h := mustNew(t, health.Config{Started: count, Ready: count})

	probe(t, h.Liveness(), http.StatusOK)

	if calls.Load() != 0 {
		t.Fatalf("liveness consulted a check %d times, want 0", calls.Load())
	}
}

func TestReadinessFollowsReadyFuncOnEveryRequest(t *testing.T) {
	var ready atomic.Bool
	var calls atomic.Int32

	h := mustNew(t, health.Config{
		Started: health.AlwaysStarted,
		Ready: func(context.Context) bool {
			calls.Add(1)
			return ready.Load()
		},
	})

	probe(t, h.Readiness(), http.StatusServiceUnavailable)

	ready.Store(true)

	probe(t, h.Readiness(), http.StatusOK)

	if got := calls.Load(); got != 2 {
		t.Fatalf("ReadyFunc called %d times, want 2 (once per request)", got)
	}
}

func TestStartupLatchesOnFirstSuccess(t *testing.T) {
	var started atomic.Bool
	var calls atomic.Int32

	h := mustNew(t, health.Config{
		Started: func(context.Context) bool {
			calls.Add(1)
			return started.Load()
		},
		Ready: health.AlwaysReady,
	})

	probe(t, h.Startup(), http.StatusServiceUnavailable)

	started.Store(true)

	probe(t, h.Startup(), http.StatusOK)

	callsAfterSuccess := calls.Load()
	started.Store(false)

	probe(t, h.Startup(), http.StatusOK)

	if got := calls.Load(); got != callsAfterSuccess {
		t.Fatalf("StartedFunc called again after latching: %d -> %d", callsAfterSuccess, got)
	}
}

func TestDrainMakesReadinessUnreadyWithoutConsultingCheck(t *testing.T) {
	var calls atomic.Int32

	h := mustNew(t, health.Config{
		Started: health.AlwaysStarted,
		Ready: func(context.Context) bool {
			calls.Add(1)
			return true
		},
	})

	probe(t, h.Readiness(), http.StatusOK)

	h.Drain()

	probe(t, h.Readiness(), http.StatusServiceUnavailable)
	probe(t, h.Readiness(), http.StatusServiceUnavailable)

	if got := calls.Load(); got != 1 {
		t.Fatalf("ReadyFunc called %d times, want 1 (not called while draining)", got)
	}
}

func TestDrainLeavesLivenessAndStartupAlone(t *testing.T) {
	h := mustNew(t, health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady})

	h.Drain()

	probe(t, h.Liveness(), http.StatusOK)
	probe(t, h.Startup(), http.StatusOK)
}

func mustNew(t *testing.T, cfg health.Config) *health.Health {
	t.Helper()
	h, err := health.New(cfg)
	if err != nil {
		t.Fatalf("health.New: %v", err)
	}
	return h
}

// probe performs a GET and asserts the shared probe contract: the expected
// status, an empty body, and no content type.
func probe(t *testing.T, handler http.Handler, wantStatus int) {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d", rec.Code, wantStatus)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Fatalf("Content-Type = %q, want unset", ct)
	}
}
