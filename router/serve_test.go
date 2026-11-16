package router_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
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

func TestServeListenerServesOnTheCallersListenerUntilTheContextIsDone(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	ln := listenLoopback(t)
	srv := &http.Server{Handler: r, ReadHeaderTimeout: time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() { errCh <- r.ServeListener(ctx, ln, srv, 0) }()

	if got := get(t, "http://"+ln.Addr().String()+"/livez").status; got != http.StatusOK {
		t.Fatalf("livez = %d, want 200", got)
	}

	cancel()

	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("ServeListener: %v", err)
	}
}

func TestServeListenerAnswersReadinessWith503ForTheWaitWindow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var readyCalls atomic.Int32

		s := startMem(t, health.Config{
			Started: health.AlwaysStarted,
			Ready: func(context.Context) bool {
				readyCalls.Add(1)
				return true
			},
		}, 10*time.Second)

		if got := s.ln.Status(t, "/readyz"); got != http.StatusOK {
			t.Fatalf("readyz before the drain = %d, want 200", got)
		}

		callsBeforeDrain := readyCalls.Load()
		start := time.Now()

		s.cancel()
		synctest.Wait()

		// The wait is the first half of the budget. Readiness is 503 for all
		// of it, without consulting the check, and the server keeps serving.
		for _, at := range []time.Duration{0, 5*time.Second - time.Nanosecond} {
			synctest.Sleep(at - time.Since(start))

			if got := s.ln.Status(t, "/readyz"); got != http.StatusServiceUnavailable {
				t.Fatalf("readyz %s into the drain = %d, want 503", at, got)
			}
			if got := s.ln.Status(t, "/livez"); got != http.StatusOK {
				t.Fatalf("livez %s into the drain = %d, want 200", at, got)
			}
		}

		if got := readyCalls.Load(); got != callsBeforeDrain {
			t.Fatalf("ReadyFunc called %d times while draining, want 0", got-callsBeforeDrain)
		}

		// With nothing in flight, Shutdown returns as soon as the wait ends.
		if err := <-s.errCh; err != nil {
			t.Fatalf("ServeListener: %v", err)
		}
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("ServeListener returned %s into the drain, want 5s", elapsed)
		}

		// The server owned the listener and closed it.
		select {
		case <-s.ln.Closed():
		default:
			t.Fatal("the listener is still open")
		}
	})
}

func TestServeListenerFinishesInFlightRequests(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := startMem(t, alwaysHealthy(), 10*time.Second)

		s.r.Get("/slow", func(w http.ResponseWriter, _ *http.Request) {
			<-time.After(time.Second)
			w.WriteHeader(http.StatusOK)
		})

		slow := make(chan int, 1)

		go func() { slow <- s.ln.Status(t, "/slow") }()

		synctest.Wait()
		s.cancel()

		if got := <-slow; got != http.StatusOK {
			t.Fatalf("in-flight request = %d, want 200", got)
		}
		if err := <-s.errCh; err != nil {
			t.Fatalf("ServeListener: %v", err)
		}
	})
}

func TestServeListenerClosesStragglersWithinTheDrainBudget(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const drain = 10 * time.Second

		s := startMem(t, alwaysHealthy(), drain)
		stuck := s.ln.StartStuck(t, s.r)

		start := time.Now()
		s.cancel()

		err := <-s.errCh

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ServeListener = %v, want the missed shutdown deadline", err)
		}
		if elapsed := time.Since(start); elapsed != drain {
			t.Fatalf("ServeListener took %s, want exactly the %s budget", elapsed, drain)
		}
		if reqErr := <-stuck; reqErr == nil {
			t.Fatal("the stuck request completed; want its connection closed")
		}
	})
}

func TestServeListenerWithZeroDrainClosesImmediately(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := startMem(t, alwaysHealthy(), 0)
		stuck := s.ln.StartStuck(t, s.r)

		start := time.Now()
		s.cancel()

		// The shutdown deadline is zero: the server closes rather than
		// reporting a deadline it never had time to meet.
		if err := <-s.errCh; err != nil {
			t.Fatalf("ServeListener: %v", err)
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("ServeListener took %s, want no time at all", elapsed)
		}
		if reqErr := <-stuck; reqErr == nil {
			t.Fatal("the in-flight request completed; want its connection closed")
		}
		if got := s.ln.Status(t, "/livez"); got != 0 {
			t.Fatalf("server still answering after the drain with %d", got)
		}
	})
}

func TestServeListenerReturnsServerErrors(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())
	boom := errors.New("accept failed")
	srv := &http.Server{Handler: r, ReadHeaderTimeout: time.Second}

	err := r.ServeListener(context.Background(), failingListener{err: boom}, srv, 0)
	if !errors.Is(err, boom) {
		t.Fatalf("ServeListener = %v, want the accept error", err)
	}
}

func TestEveryWayOfServingReturnsErrAlreadyServedTheSecondTime(t *testing.T) {
	t.Parallel()

	first := map[string]func(*router.Router, context.Context) error{
		"Serve": func(r *router.Router, ctx context.Context) error {
			return r.Serve(ctx, &http.Server{Addr: "127.0.0.1:0", Handler: r, ReadHeaderTimeout: time.Second}, 0)
		},
		"ServeListener": func(r *router.Router, ctx context.Context) error {
			return r.ServeListener(ctx, router.NewMemListener(), &http.Server{Handler: r}, 0)
		},
	}

	for name, serve := range first {
		t.Run("after "+name, func(t *testing.T) {
			t.Parallel()

			r := mustNew(t, alwaysHealthy())

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			if err := serve(r, ctx); err != nil {
				t.Fatalf("first %s: %v", name, err)
			}

			// The address would be in use, so a second Serve or Run that
			// listened would fail with a listen error instead.
			taken := listenLoopback(t)
			t.Cleanup(func() { _ = taken.Close() })

			second := &http.Server{Addr: taken.Addr().String(), Handler: r, ReadHeaderTimeout: time.Second}

			if err := r.Serve(context.Background(), second, 0); !errors.Is(err, router.ErrAlreadyServed) {
				t.Fatalf("Serve = %v, want ErrAlreadyServed", err)
			}
			if err := r.Run(second, 0); !errors.Is(err, router.ErrAlreadyServed) {
				t.Fatalf("Run = %v, want ErrAlreadyServed", err)
			}

			ln := router.NewMemListener()
			if err := r.ServeListener(context.Background(), ln, second, 0); !errors.Is(err, router.ErrAlreadyServed) {
				t.Fatalf("ServeListener = %v, want ErrAlreadyServed", err)
			}

			// ServeListener owns its listener even when it refuses to serve.
			select {
			case <-ln.Closed():
			default:
				t.Fatal("ServeListener left the refused listener open")
			}
		})
	}
}

func TestServeListensOnTheServerAddress(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := make(chan net.Addr, 1)
	srv := &http.Server{
		Addr:              "127.0.0.1:0",
		Handler:           r,
		ReadHeaderTimeout: time.Second,
		// http.Server hands BaseContext the listener Serve opened.
		BaseContext: func(ln net.Listener) context.Context {
			addr <- ln.Addr()
			return context.Background()
		},
	}

	errCh := make(chan error, 1)

	go func() { errCh <- r.Serve(ctx, srv, 0) }()

	var base string
	select {
	case a := <-addr:
		base = "http://" + a.String()
	case err := <-errCh:
		t.Fatalf("Serve returned before listening: %v", err)
	}

	if got := get(t, base+"/livez").status; got != http.StatusOK {
		t.Fatalf("livez = %d, want 200", got)
	}

	cancel()

	if err := waitForServe(t, errCh); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServeReturnsTheListenErrorForAnAddressInUse(t *testing.T) {
	t.Parallel()

	taken := listenLoopback(t)
	t.Cleanup(func() { _ = taken.Close() })

	r := mustNew(t, alwaysHealthy())
	srv := &http.Server{Addr: taken.Addr().String(), Handler: r, ReadHeaderTimeout: time.Second}

	err := r.Serve(context.Background(), srv, 0)

	var opErr *net.OpError
	if !errors.As(err, &opErr) || opErr.Op != "listen" {
		t.Fatalf("Serve = %v, want the listen error", err)
	}
}

func TestServeListenerLogsTheDrain(t *testing.T) {
	// Not parallel: the router logger writes to the process-wide output.
	var buf bytes.Buffer

	restore := log.UseOutputForTests(&buf)
	defer restore()

	synctest.Test(t, func(t *testing.T) {
		s := startMem(t, alwaysHealthy(), 10*time.Second)

		s.cancel()

		if err := <-s.errCh; err != nil {
			t.Fatalf("ServeListener: %v", err)
		}
	})

	out := buf.String()
	if !containsAll(out, []string{"msg=draining", "wait=5s", "msg=drained"}) {
		t.Fatalf("drain logs missing, got %q", out)
	}
}

// memServer is a Router serving on an in-memory listener inside a synctest
// bubble, so its drain runs on the bubble's fake clock.
type memServer struct {
	r      *router.Router
	ln     *router.MemListener
	cancel context.CancelFunc
	errCh  chan error
}

// startMem starts ServeListener with drain and waits for it to settle. Call
// it inside the bubble.
func startMem(t *testing.T, cfg health.Config, drain time.Duration) *memServer {
	t.Helper()

	r := mustNew(t, cfg)
	ln := router.NewMemListener()
	srv := &http.Server{Handler: r, ReadHeaderTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	s := &memServer{r: r, ln: ln, cancel: cancel, errCh: make(chan error, 1)}

	go func() { s.errCh <- r.ServeListener(ctx, ln, srv, drain) }()

	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
	})

	synctest.Wait()

	return s
}

// failingListener fails every Accept with err.
type failingListener struct{ err error }

func (l failingListener) Accept() (net.Conn, error) { return nil, l.err }
func (failingListener) Close() error                { return nil }
func (failingListener) Addr() net.Addr              { return &net.TCPAddr{} }

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

// listenLoopback listens on a free loopback port. The listener is the test's
// to close or to hand to the server.
func listenLoopback(t *testing.T) net.Listener {
	t.Helper()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	return ln
}
