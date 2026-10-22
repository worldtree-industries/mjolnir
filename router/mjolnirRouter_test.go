package router_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/log"
)

var (
	_ http.Handler = (*router.Router)(nil)
	_ chi.Router   = (*router.Router)(nil)
)

func TestNewRequiresStarted(t *testing.T) {
	_, err := router.New(health.Config{Ready: health.AlwaysReady})
	if !errors.Is(err, health.ErrStartedRequired) {
		t.Fatalf("got %v, want ErrStartedRequired", err)
	}
}

func TestNewRequiresReady(t *testing.T) {
	_, err := router.New(health.Config{Started: health.AlwaysStarted})
	if !errors.Is(err, health.ErrReadyRequired) {
		t.Fatalf("got %v, want ErrReadyRequired", err)
	}
}

func TestLivenessDoesNotConsultChecks(t *testing.T) {
	var calls atomic.Int32
	count := func(context.Context) bool {
		calls.Add(1)
		return false
	}

	r := mustNew(t, health.Config{Started: count, Ready: count})

	probe(t, r, http.MethodGet, "/livez", http.StatusOK)

	if calls.Load() != 0 {
		t.Fatalf("liveness consulted a check %d times, want 0", calls.Load())
	}
}

func TestReadinessFollowsReadyFuncOnEveryRequest(t *testing.T) {
	var ready atomic.Bool
	var calls atomic.Int32

	r := mustNew(t, health.Config{
		Started: health.AlwaysStarted,
		Ready: func(context.Context) bool {
			calls.Add(1)
			return ready.Load()
		},
	})

	probe(t, r, http.MethodGet, "/readyz", http.StatusServiceUnavailable)

	ready.Store(true)

	probe(t, r, http.MethodGet, "/readyz", http.StatusOK)

	if got := calls.Load(); got != 2 {
		t.Fatalf("ReadyFunc called %d times, want 2 (once per request)", got)
	}
}

func TestStartupLatches(t *testing.T) {
	var started atomic.Bool

	r := mustNew(t, health.Config{
		Started: func(context.Context) bool { return started.Load() },
		Ready:   health.AlwaysReady,
	})

	probe(t, r, http.MethodGet, "/startupz", http.StatusServiceUnavailable)

	started.Store(true)

	probe(t, r, http.MethodGet, "/startupz", http.StatusOK)

	started.Store(false)

	probe(t, r, http.MethodGet, "/startupz", http.StatusOK)
}

func TestProbeEndpointsRejectOtherMethods(t *testing.T) {
	r := mustNew(t, health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady})

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodHead} {
		for _, path := range []string{"/livez", "/readyz", "/startupz"} {
			probe(t, r, method, path, http.StatusMethodNotAllowed)
		}
	}
}

func TestFailingReadinessProbeAppearsInTheRequestLog(t *testing.T) {
	var buf bytes.Buffer

	restore := log.UseOutputForTests(&buf)
	defer restore()

	r := mustNew(t, health.Config{
		Started: health.AlwaysStarted,
		Ready:   func(context.Context) bool { return false },
	})

	probe(t, r, http.MethodGet, "/readyz", http.StatusServiceUnavailable)

	line := buf.String()
	if !strings.Contains(line, "path=/readyz") || !strings.Contains(line, "status=503") {
		t.Fatalf("request log missing failing readiness probe, got %q", line)
	}
}

func TestUserRoutesStillWork(t *testing.T) {
	r := mustNew(t, health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady})

	r.Get("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hello", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "hello" {
		t.Fatalf("body = %q, want %q", got, "hello")
	}
}

func mustNew(t *testing.T, cfg health.Config) *router.Router {
	t.Helper()

	r, err := router.New(cfg)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	return r
}

// probe performs a request and asserts the current probe contract: the
// expected status, an empty body, and no content type.
func probe(t *testing.T, handler http.Handler, method, path string, wantStatus int) {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))

	if rec.Code != wantStatus {
		t.Fatalf("%s %s: status = %d, want %d", method, path, rec.Code, wantStatus)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("%s %s: body = %q, want empty", method, path, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Fatalf("%s %s: Content-Type = %q, want unset", method, path, ct)
	}
}
