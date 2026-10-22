// Package health provides the probe endpoints a mjolnir application exposes to
// its platform: liveness, readiness, and startup.
package health

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
)

// ReadyFunc reports whether the process can serve traffic right now.
//
// The boolean is the whole contract: implementations handle and log their own
// errors. It is consulted on every readiness request, so it should be cheap
// and must not block for long.
type ReadyFunc func(ctx context.Context) bool

// StartedFunc reports whether initialization is complete.
//
// It must never check a dependency. A failing startup probe kills the
// container, so a dependency check here turns an outage into a restart loop —
// the stampede readiness exists to avoid. Report dependencies from ReadyFunc
// instead.
//
// Started is a milestone, not a state: the check is consulted until it first
// returns true, and the answer is latched from then on.
type StartedFunc func(ctx context.Context) bool

// AlwaysReady is a ReadyFunc for processes with nothing to check.
func AlwaysReady(context.Context) bool { return true }

// AlwaysStarted is a StartedFunc for processes with no initialization to wait
// for.
func AlwaysStarted(context.Context) bool { return true }

// Config carries the checks the probe endpoints consult. Both are required;
// New rejects a nil one.
type Config struct {
	Started StartedFunc
	Ready   ReadyFunc
}

var (
	// ErrStartedRequired reports a Config with no StartedFunc.
	ErrStartedRequired = errors.New("health: StartedFunc is required")
	// ErrReadyRequired reports a Config with no ReadyFunc.
	ErrReadyRequired = errors.New("health: ReadyFunc is required")
)

// Health owns the probe handlers and the state behind them.
type Health struct {
	ready   ReadyFunc
	started StartedFunc
	latched atomic.Bool
}

// New builds a Health from cfg, rejecting a missing check.
func New(cfg Config) (*Health, error) {
	if cfg.Started == nil {
		return nil, ErrStartedRequired
	}
	if cfg.Ready == nil {
		return nil, ErrReadyRequired
	}

	return &Health{ready: cfg.Ready, started: cfg.Started}, nil
}

// Liveness reports that the process is running and not wedged.
//
// It answers 200 without consulting anything: it must not fail because a
// dependency is unavailable.
func (h *Health) Liveness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeStatus(w, true)
	})
}

// Readiness reports whether the process can serve traffic now.
func (h *Health) Readiness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStatus(w, h.ready(r.Context()))
	})
}

// Startup reports whether initialization is complete, latching the first
// success.
func (h *Health) Startup() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.latched.Load() && h.started(r.Context()) {
			h.latched.Store(true)
		}

		writeStatus(w, h.latched.Load())
	})
}

// writeStatus answers 200 when healthy and 503 otherwise, with an empty body
// and no content type.
func writeStatus(w http.ResponseWriter, healthy bool) {
	if healthy {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusServiceUnavailable)
}
