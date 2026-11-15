package router_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/log"
)

func TestHealthLetsTheAppDrainOnItsOwnTrigger(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if got := get(t, srv.URL+"/readyz").status; got != http.StatusOK {
		t.Fatalf("readyz before Drain = %d, want 200", got)
	}

	r.Health().Drain()

	if got := get(t, srv.URL+"/readyz").status; got != http.StatusServiceUnavailable {
		t.Fatalf("readyz after Drain = %d, want 503", got)
	}
}

func TestNewServerSetsSafeTimeoutsAndAllowsStreaming(t *testing.T) {
	t.Parallel()

	h := mustNew(t, alwaysHealthy())
	srv := router.NewServer(":8080", h)

	if srv.Addr != ":8080" || srv.Handler != h {
		t.Fatalf("NewServer: Addr %q Handler %v, want :8080 and the router", srv.Addr, srv.Handler)
	}

	checks := []struct {
		name      string
		got, want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, 5 * time.Second},
		{"ReadTimeout", srv.ReadTimeout, 30 * time.Second},
		{"IdleTimeout", srv.IdleTimeout, 120 * time.Second},
		{"WriteTimeout", srv.WriteTimeout, 0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Fatalf("%s = %s, want %s", c.name, c.got, c.want)
		}
	}
	if srv.MaxHeaderBytes != 1<<20 {
		t.Fatalf("MaxHeaderBytes = %d, want 1 MiB", srv.MaxHeaderBytes)
	}
}

func TestASecondServeReturnsErrAlreadyServed(t *testing.T) {
	t.Parallel()

	r, srv, base := startServer(t, alwaysHealthy())

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- r.Serve(ctx, srv, 0) }()

	waitForStatus(t, base+"/livez", http.StatusOK)
	cancel()

	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("first Serve: %v", err)
	}

	second := &http.Server{Addr: freeAddr(t), Handler: r, ReadHeaderTimeout: time.Second}
	t.Cleanup(func() { _ = second.Close() })

	if err := r.Serve(context.Background(), second, 0); !errors.Is(err, router.ErrAlreadyServed) {
		t.Fatalf("second Serve = %v, want ErrAlreadyServed", err)
	}
	if err := r.Run(second, 0); !errors.Is(err, router.ErrAlreadyServed) {
		t.Fatalf("Run after Serve = %v, want ErrAlreadyServed", err)
	}
	if code := statusOf(t, "http://"+second.Addr+"/livez"); code != 0 {
		t.Fatalf("the second server was started and answered %d", code)
	}
}

func TestServeServesUntilContextIsCancelled(t *testing.T) {
	r, srv, base := startServer(t, health.Config{
		Started: health.AlwaysStarted,
		Ready:   health.AlwaysReady,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- r.Serve(ctx, srv, 50*time.Millisecond) }()

	waitForStatus(t, base+"/livez", http.StatusOK)
	waitForStatus(t, base+"/readyz", http.StatusOK)

	cancel()

	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServeDrainsBeforeShuttingDown(t *testing.T) {
	var calls atomic.Int32

	r, srv, base := startServer(t, health.Config{
		Started: health.AlwaysStarted,
		Ready: func(context.Context) bool {
			calls.Add(1)
			return true
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- r.Serve(ctx, srv, 300*time.Millisecond) }()

	waitForStatus(t, base+"/livez", http.StatusOK)
	waitForStatus(t, base+"/readyz", http.StatusOK)

	callsBeforeDrain := calls.Load()

	cancel()

	// Readiness flips as soon as the drain starts, without consulting the
	// check: its dependencies may already be closing.
	waitForStatus(t, base+"/readyz", http.StatusServiceUnavailable)

	// Liveness keeps answering while the process drains.
	if code := statusOf(t, base+"/livez"); code != http.StatusOK {
		t.Fatalf("liveness during drain = %d, want %d", code, http.StatusOK)
	}

	if got := calls.Load(); got != callsBeforeDrain {
		t.Fatalf("ReadyFunc called %d times while draining, want 0", got-callsBeforeDrain)
	}

	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServeFinishesWithinTheDrainBudgetAndClosesStragglers(t *testing.T) {
	t.Parallel()

	r, srv, base := startServer(t, alwaysHealthy())

	stuckStarted := make(chan struct{})

	// A request that never finishes on its own: only Close ends it.
	r.Get("/stuck", func(_ http.ResponseWriter, req *http.Request) {
		close(stuckStarted)
		<-req.Context().Done()
	})

	const drain = 400 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- r.Serve(ctx, srv, drain) }()

	waitForStatus(t, base+"/livez", http.StatusOK)

	stuckErr := make(chan error, 1)

	go func() {
		resp, err := http.Get(base + "/stuck")
		if err == nil {
			_ = resp.Body.Close()
		}
		stuckErr <- err
	}()

	<-stuckStarted

	start := time.Now()
	cancel()

	// The server keeps serving for the first part of the budget, with
	// readiness at 503.
	waitForStatus(t, base+"/readyz", http.StatusServiceUnavailable)

	err := waitForServe(t, errCh)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Serve = %v, want the missed shutdown deadline", err)
	}
	if elapsed > drain+drainSlack {
		t.Fatalf("Serve took %s, over the %s budget", elapsed, drain)
	}
	if reqErr := <-stuckErr; reqErr == nil {
		t.Fatal("the stuck request completed; want its connection closed")
	}
}

// drainSlack is how far past its budget a drain may run in a test before it
// counts as an overshoot.
const drainSlack = 150 * time.Millisecond

func TestRunClosesOnASecondSignalDuringTheWait(t *testing.T) {
	r, srv, base := startServer(t, alwaysHealthy())

	errCh := make(chan error, 1)

	go func() { errCh <- r.Run(srv, 10*time.Second) }()

	waitForStatus(t, base+"/livez", http.StatusOK)
	signalSelf(t)
	waitForStatus(t, base+"/readyz", http.StatusServiceUnavailable)

	start := time.Now()
	signalSelf(t)

	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > drainSlack {
		t.Fatalf("Run took %s after the second signal, want well under the 5s wait", elapsed)
	}
}

func TestRunClosesOnASecondSignalDuringShutdown(t *testing.T) {
	r, srv, base := startServer(t, alwaysHealthy())

	stuckStarted := make(chan struct{})

	r.Get("/stuck", func(_ http.ResponseWriter, req *http.Request) {
		close(stuckStarted)
		<-req.Context().Done()
	})

	errCh := make(chan error, 1)

	// 1s of wait, then a 1s shutdown deadline the stuck request would miss.
	go func() { errCh <- r.Run(srv, 2*time.Second) }()

	waitForStatus(t, base+"/livez", http.StatusOK)

	stuckErr := make(chan error, 1)

	go func() {
		resp, err := http.Get(base + "/stuck")
		if err == nil {
			_ = resp.Body.Close()
		}
		stuckErr <- err
	}()

	<-stuckStarted
	signalSelf(t)

	// Shutdown closes the listener first, so a refused dial means the
	// shutdown phase has begun.
	waitForRefused(t, srv.Addr)

	start := time.Now()
	signalSelf(t)

	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("Run = %v, want nil: the second signal closes the server before the deadline", err)
	}
	if elapsed := time.Since(start); elapsed > drainSlack {
		t.Fatalf("Run took %s after the second signal, want well under the 1s deadline", elapsed)
	}
	if err := <-stuckErr; err == nil {
		t.Fatal("the stuck request completed; want its connection closed")
	}
}

// signalSelf sends SIGINT to the test process, for Run to catch.
//
// The Run tests signal the test process itself, so they must not run in
// parallel with each other (Run is the only signal listener). Each waits for
// Run's listener to be up before the first signal and for the drain to start
// before the second, so neither signal is lost or coalesced.
func signalSelf(t *testing.T) {
	t.Helper()

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("signal: %v", err)
	}
}

func waitForRefused(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(probeTimeout)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, probeTimeout)
		if err != nil {
			return
		}
		_ = conn.Close()

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("%s still accepts connections", addr)
}

func TestServeLogsTheDrain(t *testing.T) {
	var buf bytes.Buffer

	restore := log.UseOutputForTests(&buf)
	defer restore()

	r, srv, base := startServer(t, health.Config{
		Started: health.AlwaysStarted,
		Ready:   health.AlwaysReady,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- r.Serve(ctx, srv, 20*time.Millisecond) }()

	waitForStatus(t, base+"/livez", http.StatusOK)

	cancel()

	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	line := buf.String()
	if !strings.Contains(line, "draining") || !strings.Contains(line, "drained") {
		t.Fatalf("drain logs missing, got %q", line)
	}
}

func TestServeFinishesInFlightRequests(t *testing.T) {
	r, srv, base := startServer(t, health.Config{
		Started: health.AlwaysStarted,
		Ready:   health.AlwaysReady,
	})

	requestStarted := make(chan struct{})

	r.Get("/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- r.Serve(ctx, srv, 300*time.Millisecond) }()

	waitForStatus(t, base+"/livez", http.StatusOK)

	slowStatus := make(chan int, 1)

	go func() { slowStatus <- statusOf(t, base+"/slow") }()

	<-requestStarted

	cancel()

	if code := <-slowStatus; code != http.StatusOK {
		t.Fatalf("in-flight request = %d, want %d", code, http.StatusOK)
	}
	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServeWithZeroDrainShutsDownImmediately(t *testing.T) {
	r, srv, base := startServer(t, health.Config{
		Started: health.AlwaysStarted,
		Ready:   health.AlwaysReady,
	})

	requestStarted := make(chan struct{})

	r.Get("/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- r.Serve(ctx, srv, 0) }()

	waitForStatus(t, base+"/livez", http.StatusOK)

	slowStatus := make(chan int, 1)

	go func() { slowStatus <- statusOf(t, base+"/slow") }()

	<-requestStarted

	cancel()

	// A request is still in flight and the shutdown deadline is zero: the
	// server closes rather than reporting a deadline it never had time to meet.
	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	if code := statusOf(t, base+"/livez"); code != 0 {
		t.Fatalf("server still answering after shutdown with status %d", code)
	}
}

func TestServeReturnsServerErrors(t *testing.T) {
	r := mustNew(t, health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady})

	addr := freeAddr(t)

	listener, listenErr := net.Listen("tcp", addr)
	if listenErr != nil {
		t.Fatalf("listen: %v", listenErr)
	}
	defer listener.Close()

	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: time.Second}

	if err := r.Serve(context.Background(), srv, 0); err == nil {
		t.Fatal("Serve returned nil for an address already in use")
	}
}

// startServer builds a router and a server on a free loopback port. Serve is
// left to the caller so each test controls the lifetime.
func startServer(t *testing.T, cfg health.Config) (*router.Router, *http.Server, string) {
	t.Helper()

	r := mustNew(t, cfg)
	addr := freeAddr(t)
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: time.Second}

	t.Cleanup(func() { _ = srv.Close() })

	return r, srv, "http://" + addr
}

func freeAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	return listener.Addr().String()
}

func waitForServe(t *testing.T, errCh <-chan error) error {
	t.Helper()

	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
		return nil
	}
}

func waitForStatus(t *testing.T, url string, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if statusOf(t, url) == want {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("%s never returned %d", url, want)
}

// statusOf returns the response status, or 0 when the server cannot be
// reached.
func statusOf(t *testing.T, url string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	client := &http.Client{Timeout: probeTimeout}

	resp, doErr := client.Do(req)
	if doErr != nil {
		return 0
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode
}
