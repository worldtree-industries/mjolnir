package router_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/log"
)

var (
	_ http.Handler = (*router.Router)(nil)
	_ chi.Router   = (*router.Router)(nil)
)

func TestNewRequiresStarted(t *testing.T) {
	_, err := router.New(router.Config{Health: health.Config{Ready: health.AlwaysReady}})
	if !errors.Is(err, health.ErrStartedRequired) {
		t.Fatalf("got %v, want ErrStartedRequired", err)
	}
}

func TestNewRequiresReady(t *testing.T) {
	_, err := router.New(router.Config{Health: health.Config{Started: health.AlwaysStarted}})
	if !errors.Is(err, health.ErrReadyRequired) {
		t.Fatalf("got %v, want ErrReadyRequired", err)
	}
}

func TestNewRejectsInvalidTrustedPrefix(t *testing.T) {
	_, err := router.New(router.Config{
		Health:         health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady},
		TrustedProxies: []netip.Prefix{{}},
	})
	if err == nil {
		t.Fatal("expected invalid trusted prefix to be rejected")
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

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		for _, path := range []string{"/livez", "/readyz", "/startupz"} {
			probe(t, r, method, path, http.StatusMethodNotAllowed)
		}
	}
}

// TestHeadProbesMatchGet proves GetHead is installed before the route table.
// A ResponseRecorder cannot model HEAD (the server suppresses the body), so
// this uses a real server.
func TestHeadProbesMatchGet(t *testing.T) {
	r := mustNew(t, health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	for _, path := range []string{"/livez", "/readyz", "/startupz"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				t.Fatalf("%s %s: read body: %v", method, path, readErr)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s %s: status = %d, want 200", method, path, resp.StatusCode)
			}
			if len(body) != 0 {
				t.Errorf("%s %s: body = %q, want empty", method, path, body)
			}
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

// TestClientIPDefaultDeny proves forwarding headers are ignored unless the
// peer is a configured trusted proxy.
func TestClientIPDefaultDeny(t *testing.T) {
	r := mustNew(t, health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady})
	r.Get("/who", whoHandler)

	req := httptest.NewRequest(http.MethodGet, "/who", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Real-IP", "203.0.113.8")
	req.Header.Set("True-Client-Ip", "203.0.113.7")

	if got := serveWho(t, r, req); got != "192.0.2.10" {
		t.Fatalf("client IP = %q, want the transport peer", got)
	}
}

func TestClientIPTrustedProxyUsesForwardedAddress(t *testing.T) {
	r := mustNewConfig(t, router.Config{
		Health:         health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady},
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
	})
	r.Get("/who", whoHandler)

	req := httptest.NewRequest(http.MethodGet, "/who", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	if got := serveWho(t, r, req); got != "203.0.113.9" {
		t.Fatalf("client IP = %q, want forwarded address", got)
	}
}

// TestClientIPIgnoresSpoofedLeftmost: a client can add its own leftmost XFF
// entry, but the trusted-proxy walk must stop at the first untrusted hop from
// the right.
func TestClientIPIgnoresSpoofedLeftmost(t *testing.T) {
	r := mustNewConfig(t, router.Config{
		Health:         health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady},
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
	})
	r.Get("/who", whoHandler)

	req := httptest.NewRequest(http.MethodGet, "/who", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.9")

	if got := serveWho(t, r, req); got != "203.0.113.9" {
		t.Fatalf("client IP = %q, want rightmost untrusted hop", got)
	}
}

func TestClientIPFailsClosedOnUnparsablePeer(t *testing.T) {
	r := mustNew(t, health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady})
	r.Get("/who", whoHandler)

	req := httptest.NewRequest(http.MethodGet, "/who", nil)
	req.RemoteAddr = "not-an-ip"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	if got := serveWho(t, r, req); got != "" {
		t.Fatalf("client IP = %q, want empty (fail closed)", got)
	}
}

func whoHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = io.WriteString(w, chimiddleware.GetClientIP(r.Context()))
}

func serveWho(t *testing.T, h http.Handler, req *http.Request) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestPanicBeforeWriteReturnsProblem(t *testing.T) {
	var buf bytes.Buffer
	r := mustNewConfig(t, router.Config{
		Health:       health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady},
		AccessLogger: log.GetLoggerTo("http", &buf),
	})
	r.Get("/panic", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/panic")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read body: %v", readErr)
	}

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q", got)
	}
	if strings.Contains(string(body), "boom") {
		t.Errorf("panic value leaked to client: %s", body)
	}

	logged := buf.String()
	if !strings.Contains(logged, "panic recovered") || !strings.Contains(logged, "stack=") {
		t.Errorf("panic not logged with stack:\n%s", logged)
	}
	if !strings.Contains(logged, `response_started=false`) {
		t.Errorf("panic log missing response_started=false:\n%s", logged)
	}
}

func TestPanicAfterWriteAbortsConnection(t *testing.T) {
	var buf bytes.Buffer
	r := mustNewConfig(t, router.Config{
		Health:       health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady},
		AccessLogger: log.GetLoggerTo("http", &buf),
	})
	r.Get("/panic", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/panic")
	if err != nil {
		// A client may see the abort before headers arrive; that is acceptable.
		return
	}
	_, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr == nil {
		t.Error("expected the aborted connection to produce a read error")
	}

	logged := buf.String()
	if !strings.Contains(logged, "panic after response started") {
		t.Errorf("panic after write not logged:\n%s", logged)
	}
	if !strings.Contains(logged, `response_started=true`) {
		t.Errorf("panic log missing response_started=true:\n%s", logged)
	}
	if strings.Contains(logged, "panic recovered") {
		t.Errorf("started response should not use the normal recovered path:\n%s", logged)
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

	go func() { errCh <- r.Serve(ctx, srv, 500*time.Millisecond) }()

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

	go func() { errCh <- r.Serve(ctx, srv, 500*time.Millisecond) }()

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

func TestSecondServeReturnsErrAlreadyServed(t *testing.T) {
	r, srv, base := startServer(t, health.Config{
		Started: health.AlwaysStarted,
		Ready:   health.AlwaysReady,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- r.Serve(ctx, srv, 20*time.Millisecond) }()

	waitForStatus(t, base+"/livez", http.StatusOK)

	err := r.Serve(context.Background(), router.NewServer(freeAddr(t), r), 0)
	if !errors.Is(err, router.ErrAlreadyServed) {
		t.Fatalf("second Serve = %v, want ErrAlreadyServed", err)
	}

	cancel()
	if serveErr := waitForServe(t, errCh); serveErr != nil {
		t.Fatalf("first Serve: %v", serveErr)
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

	srv := router.NewServer(addr, r)

	if err := r.Serve(context.Background(), srv, 0); err == nil {
		t.Fatal("Serve returned nil for an address already in use")
	}
}

func TestNewServerTimeouts(t *testing.T) {
	srv := router.NewServer(":0", http.NotFoundHandler())

	if srv.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v", srv.ReadTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d", srv.MaxHeaderBytes)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want zero for streaming", srv.WriteTimeout)
	}
}

func TestHealthHandleIsExposed(t *testing.T) {
	r := mustNew(t, health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady})
	if r.Health() == nil {
		t.Fatal("Health() returned nil")
	}
}

func mustNew(t *testing.T, cfg health.Config) *router.Router {
	t.Helper()
	return mustNewConfig(t, router.Config{Health: cfg})
}

func mustNewConfig(t *testing.T, cfg router.Config) *router.Router {
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

// probeTimeout bounds test requests so a wedged server fails the test instead
// of hanging it.
const probeTimeout = 2 * time.Second

// startServer builds a router and a server on a free loopback port. Serve is
// left to the caller so each test controls the lifetime.
func startServer(t *testing.T, cfg health.Config) (*router.Router, *http.Server, string) {
	t.Helper()

	r := mustNew(t, cfg)
	addr := freeAddr(t)
	srv := router.NewServer(addr, r)

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
