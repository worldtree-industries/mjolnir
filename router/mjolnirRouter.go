package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// Router is a pre-configured chi router with the probe endpoints mounted.
//
// The embedded *chi.Mux is the app mux: Use, Get, Group, Route, With, Mount,
// NotFound, MethodNotAllowed and the other chi methods act on it, and Use
// works after New. The app mux is mounted beneath a framework-owned outer mux
// that holds the framework middleware and the probes; ServeHTTP enters
// through the outer mux.
type Router struct {
	*chi.Mux

	outer  *chi.Mux
	health *health.Health
	served atomic.Bool // claimed by the first serving call; a failed listen releases it
}

// ErrAlreadyServed is returned by a second Serve, ServeListener or Run, in
// any combination: a Router serves one lifetime. A Serve or Run whose listen
// failed does not count.
var ErrAlreadyServed = errors.New("router: already served")

// Config configures a Router.
type Config struct {
	// Health carries the checks behind the probes. Both are required.
	Health health.Config
	// TrustedProxies are the CIDR prefixes of the reverse proxies in front of
	// the server. Empty (the default) trusts no forwarding header: the client
	// IP is the transport peer. Otherwise X-Forwarded-For is walked right to
	// left past trusted hops, and the first untrusted address is the client.
	// A peer outside every prefix is the client itself. RemoteAddr is never
	// rewritten, and an unparsable peer resolves no client IP.
	TrustedProxies []netip.Prefix
	// AccessLogger receives the access line for every request, probes
	// included, and the report of every recovered panic. Nil means the
	// "http" named logger.
	AccessLogger *slog.Logger
}

// New creates a pre-configured router.
//
// The startup and readiness functions in cfg.Health are required: New returns
// an error when either is missing, so an application cannot start without a
// health surface. It also rejects an invalid trusted proxy prefix.
//
// Every request passes through a fixed framework stack before it reaches the
// app: request ID, client IP, access logging, panic recovery, then HEAD
// routing to GET handlers. App middleware added with Use runs inside that
// stack, so it sees the request ID, and its panics and rejections are logged.
//
// The router answers /livez, /readyz, and /startupz itself, for GET and HEAD,
// and with 405 for any other method. These paths are reserved: app
// middleware never runs on them, and an app route on one is never reached.
//
// Logging is configured by the application (utils/log); the router does not
// set up a logger.
func New(cfg Config) (*Router, error) {
	h, err := health.New(cfg.Health)
	if err != nil {
		return nil, err
	}

	clientIP, err := clientIPMiddleware(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	accessLog := cfg.AccessLogger
	if accessLog == nil {
		accessLog = log.GetLogger("http")
	}

	outer := chi.NewRouter()

	// The framework stack, in a fixed order. The access logger wraps the
	// recoverer, so a panicking request is logged with the 500 the recoverer
	// wrote; the app mux, and so all app middleware, sits inside all of it.
	outer.Use(
		middleware.RequestID,
		clientIP,
		func(next http.Handler) http.Handler { return middleware.RequestLoggerWith(next, accessLog) },
		recoverer(accessLog),
		chimiddleware.GetHead,
	)

	// Each probe takes every method: one registered for GET alone lets the
	// other methods fall through to the app mux mounted at "/".
	outer.Handle("/livez", probe(h.Liveness()))
	outer.Handle("/readyz", probe(h.Readiness()))
	outer.Handle("/startupz", probe(h.Startup()))

	app := chi.NewRouter()
	outer.Mount("/", app)

	return &Router{Mux: app, outer: outer, health: h}, nil
}

// clientIPMiddleware resolves the client IP from the transport peer, or from
// X-Forwarded-For through the trusted prefixes.
func clientIPMiddleware(trusted []netip.Prefix) (func(http.Handler) http.Handler, error) {
	if len(trusted) == 0 {
		return chimiddleware.ClientIPFromRemoteAddr, nil
	}

	prefixes := make([]string, len(trusted))
	for i, p := range trusted {
		if !p.IsValid() {
			return nil, fmt.Errorf("router: trusted proxy %d is not a valid prefix", i)
		}
		prefixes[i] = p.String()
	}

	return chimiddleware.ClientIPFromXFF(prefixes...), nil
}

// probe serves GET and HEAD with h and answers every other method with an
// empty 405.
func probe(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		h.ServeHTTP(w, r)
	})
}

// Health returns the probe state behind the router, so an application can
// start draining on its own trigger with [health.Health.Drain].
func (r *Router) Health() *health.Health {
	return r.health
}

// ServeHTTP serves req through the framework middleware and the probes, then
// the app mux.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.outer.ServeHTTP(w, req)
}

// Serve listens on srv.Addr (":http" when empty) and serves srv there like
// [Router.ServeListener]. A listen failure, such as an address in use, is
// returned before any drain starts, and does not use up the Router's
// lifetime: a later Serve, ServeListener or Run may try again.
func (r *Router) Serve(ctx context.Context, srv *http.Server, drain time.Duration) error {
	if !r.served.CompareAndSwap(false, true) {
		return ErrAlreadyServed
	}

	ln, err := listen(ctx, srv)
	if err != nil {
		r.served.Store(false)
		return err
	}

	return r.serve(ctx, ln, srv, drain, nil)
}

// ServeListener serves srv on ln until ctx is cancelled, then drains and
// shuts it down. It owns ln from the call onwards: shutting srv down closes
// it, and so does an [ErrAlreadyServed] return. srv.Addr is not used. For TLS,
// wrap the listener with [crypto/tls.NewListener] first.
//
// The drain fits inside the drain budget, split by halves. On cancellation
// readiness answers 503 so load balancers stop sending traffic, and srv keeps
// serving for the first half so in-flight requests can finish. Shutdown is
// then called with the second half as its deadline; a request still running
// when it passes is dropped by Close. A drain of zero closes srv immediately,
// dropping in-flight requests.
//
// A Router serves one lifetime: once drained, readiness stays 503, and a
// second Serve, ServeListener or Run returns [ErrAlreadyServed] without
// touching srv. Only a Serve or Run whose listen failed gives the lifetime
// back. ServeListener has no second-signal abort; cancel ctx to drain.
//
// ServeListener returns an error if the server fails or the shutdown deadline
// is exceeded. It never calls [os.Exit].
func (r *Router) ServeListener(ctx context.Context, ln net.Listener, srv *http.Server, drain time.Duration) error {
	if !r.served.CompareAndSwap(false, true) {
		_ = ln.Close()
		return ErrAlreadyServed
	}

	return r.serve(ctx, ln, srv, drain, nil)
}

// signalQueue buffers the first signal and the second one that aborts the
// drain.
const signalQueue = 2

// Run listens and serves srv like Serve, starting the drain on SIGINT or
// SIGTERM. A second signal closes the server at any point in the drain,
// whether it is still serving or already in Shutdown; only Run has this
// abort. As with Serve, a listen failure leaves the Router servable.
func (r *Router) Run(srv *http.Server, drain time.Duration) error {
	if !r.served.CompareAndSwap(false, true) {
		return ErrAlreadyServed
	}

	signals := make(chan os.Signal, signalQueue)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	ctx := context.Background()

	ln, err := listen(ctx, srv)
	if err != nil {
		r.served.Store(false)
		return err
	}

	return r.serveSignals(ctx, signals, ln, srv, drain)
}

// serveSignals serves srv on ln like ServeListener, driven by signals as well
// as ctx: the first signal starts the drain, and the next one, or the first
// after ctx is done, closes the server at any point in it.
func (r *Router) serveSignals(
	ctx context.Context,
	signals <-chan os.Signal,
	ln net.Listener,
	srv *http.Server,
	drain time.Duration,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{})
	defer close(done)

	abort := make(chan struct{})

	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		case <-done:
			return
		}

		select {
		case <-signals:
			close(abort)
		case <-done:
		}
	}()

	return r.serve(ctx, ln, srv, drain, abort)
}

// listen listens on srv.Addr, defaulting it as ListenAndServe does.
func listen(ctx context.Context, srv *http.Server) (net.Listener, error) {
	addr := srv.Addr
	if addr == "" {
		addr = ":http"
	}

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	return ln, nil
}

// serve is the lifecycle behind every way of serving, once the Router is
// claimed. abort, when closed, closes the server at any point in the drain.
func (r *Router) serve(
	ctx context.Context,
	ln net.Listener,
	srv *http.Server,
	drain time.Duration,
	abort <-chan struct{},
) error {
	serverErr := make(chan error, 1)
	go func() { serverErr <- runServer(srv, ln) }()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	lg := log.GetLogger("router")
	wait, grace := drainBudget(drain)

	r.health.Drain()
	lg.InfoContext(ctx, "draining", "drain", drain.String(), "wait", wait.String(), "shutdown", grace.String())

	if err := stop(ctx, lg, srv, wait, grace, abort, serverErr); err != nil {
		return err
	}

	lg.InfoContext(ctx, "drained")

	return nil
}

// stop runs the two phases of a drain: srv keeps serving for wait, then
// Shutdown gets grace as its deadline. abort closes srv during either phase,
// and so does a missed deadline, before its error is returned. serverErr
// carries the result of the serve call and is read exactly once. An abort is
// logged to lg.
func stop(
	ctx context.Context,
	lg *slog.Logger,
	srv *http.Server,
	wait, grace time.Duration,
	abort <-chan struct{},
	serverErr <-chan error,
) error {
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-abort:
			lg.WarnContext(ctx, "drain aborted", "phase", "wait")
			return closeServer(srv, serverErr)
		case err := <-serverErr:
			return err
		}
	}

	if grace <= 0 {
		return closeServer(srv, serverErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- srv.Shutdown(shutdownCtx) }()

	select {
	case err := <-shutdownErr:
		if err != nil {
			// The deadline passed with requests still running: drop them.
			return errors.Join(fmt.Errorf("shut down server: %w", err), closeServer(srv, serverErr))
		}

		return <-serverErr
	case <-abort:
		lg.WarnContext(ctx, "drain aborted", "phase", "shutdown")

		closeErr := closeServer(srv, serverErr)
		// Close has dropped every connection; cancelling stops Shutdown
		// waiting for its next poll to notice.
		cancel()
		<-shutdownErr

		return closeErr
	}
}

// closeServer closes srv and returns the result of the serve call.
func closeServer(srv *http.Server, serverErr <-chan error) error {
	if err := srv.Close(); err != nil {
		return fmt.Errorf("close server: %w", err)
	}

	return <-serverErr
}

// drainBudget splits a drain of total into the time the server keeps serving
// with readiness at 503 (wait) and the deadline then given to Shutdown, so
// that the two never sum to more than total. A non-positive total is (0, 0).
func drainBudget(total time.Duration) (time.Duration, time.Duration) {
	if total <= 0 {
		return 0, 0
	}

	wait := total / 2 //nolint:mnd // half the budget each way

	return wait, total - wait
}

// runServer serves srv on ln, treating the error from Shutdown or Close as a
// clean stop.
func runServer(srv *http.Server, ln net.Listener) error {
	err := srv.Serve(ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}
