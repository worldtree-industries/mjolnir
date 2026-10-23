package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/dfryer1193/mjolnir/health"
	enhancedmiddleware "github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// Config configures the router. Health is required; the other fields are
// optional.
type Config struct {
	// Health is the required startup/readiness configuration.
	Health health.Config

	// TrustedProxies lists the CIDRs whose forwarding headers are trusted.
	// Empty means no forwarding header is trusted; the transport peer is the
	// client address.
	TrustedProxies []netip.Prefix

	// AccessLogger receives the access and panic lines. Nil uses the "http"
	// component logger.
	AccessLogger *slog.Logger
}

// Router is a pre-configured chi router with the probe endpoints mounted.
type Router struct {
	*chi.Mux

	health *health.Health
	served atomic.Bool
}

// ErrAlreadyServed is returned by a second Serve or Run call. A Router serves
// one lifetime; readiness stays 503 after the first drain.
var ErrAlreadyServed = errors.New("router: already served")

// New creates a pre-configured router.
//
// The startup and readiness functions are required: New returns an error when
// either is missing, so an application cannot start without a health surface.
// The router mounts /livez, /readyz, and /startupz for GET and HEAD.
//
// No forwarding header is trusted unless cfg.TrustedProxies is non-empty.
// Logging is configured by the application (utils/log); cfg.AccessLogger only
// overrides the router's own access logger.
func New(cfg Config) (*Router, error) {
	h, err := health.New(cfg.Health)
	if err != nil {
		return nil, err
	}

	lg := cfg.AccessLogger
	if lg == nil {
		lg = log.GetLogger("http")
	}

	r := chi.NewRouter()

	ipMiddleware, err := clientIP(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	r.Use(enhancedmiddleware.RequestID)
	r.Use(ipMiddleware)
	r.Use(func(next http.Handler) http.Handler {
		return enhancedmiddleware.RequestLoggerWith(next, lg)
	})
	r.Use(recoverer(lg))
	r.Use(chimiddleware.GetHead)

	r.Method(http.MethodGet, "/livez", h.Liveness())
	r.Method(http.MethodGet, "/readyz", h.Readiness())
	r.Method(http.MethodGet, "/startupz", h.Startup())

	return &Router{Mux: r, health: h}, nil
}

func clientIP(prefixes []netip.Prefix) (func(http.Handler) http.Handler, error) {
	if len(prefixes) == 0 {
		return chimiddleware.ClientIPFromRemoteAddr, nil
	}

	trusted := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		if !prefix.IsValid() {
			return nil, fmt.Errorf("router: invalid trusted proxy prefix %q", prefix)
		}
		trusted[i] = prefix.String()
	}
	return chimiddleware.ClientIPFromXFF(trusted...), nil
}

// Health returns the router's health handle for an app-owned serve loop.
func (r *Router) Health() *health.Health {
	return r.health
}

// NewServer returns an *[http.Server] with the framework's baseline timeouts.
//
// WriteTimeout is deliberately zero: it would break SSE and WebSocket routes.
// Set it explicitly for services that do not stream.
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 1 << 20
	drainParts        = 2
)

// recoverer logs a panic through slog and writes a 500 problem when the
// response has not started. If it has, the connection is aborted so the client
// does not mistake a truncated body for a complete 200.
func recoverer(lg *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					handlePanic(lg, w, r, recovered)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

func handlePanic(lg *slog.Logger, w http.ResponseWriter, r *http.Request, recovered any) {
	if isAbortPanic(recovered) {
		panic(recovered)
	}

	attrs := panicAttrs(r, recovered)

	if responseStarted(w) {
		attrs = append(attrs, slog.Bool("response_started", true))
		lg.ErrorContext(r.Context(), "panic after response started", attrs...)
		panic(http.ErrAbortHandler)
	}

	attrs = append(attrs, slog.Bool("response_started", false))
	lg.ErrorContext(r.Context(), "panic recovered", attrs...)

	cause, ok := recovered.(error)
	if !ok {
		cause = fmt.Errorf("panic: %v", recovered)
	}
	if err := httpx.WriteProblem(w, r, httpx.Internal(cause)); err != nil {
		lg.ErrorContext(r.Context(), "failed to write panic response",
			slog.Any("error", err),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
	}
}

func isAbortPanic(recovered any) bool {
	err, ok := recovered.(error)
	return ok && errors.Is(err, http.ErrAbortHandler)
}

func panicAttrs(r *http.Request, recovered any) []any {
	attrs := []any{
		slog.Any("panic", recovered),
		slog.String("stack", string(debug.Stack())),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	}
	if id := enhancedmiddleware.GetRequestID(r.Context()); id != "" {
		attrs = append(attrs, slog.String("request_id", id))
	}
	return attrs
}

func responseStarted(w http.ResponseWriter) bool {
	rw, ok := w.(interface{ Status() int })
	return ok && rw.Status() != 0
}

// Serve runs srv until ctx is cancelled, then drains and shuts it down.
//
// On cancellation readiness answers 503 so load balancers stop sending
// traffic. The configured drain is one total budget: half is the readiness
// window, the rest bounds Shutdown. A drain of zero closes srv immediately.
//
// A Router serves one lifetime: a second call returns ErrAlreadyServed.
//
// Serve returns an error if the server fails or the shutdown deadline is
// exceeded. It never calls [os.Exit].
func (r *Router) Serve(ctx context.Context, srv *http.Server, drain time.Duration) error {
	return r.serve(ctx, srv, drain, nil)
}

// signalQueue buffers the first signal and the second one that aborts the
// drain window.
const signalQueue = 2

// Run serves srv like Serve, starting the drain on SIGINT or SIGTERM. A
// second signal skips the rest of the drain window.
func (r *Router) Run(srv *http.Server, drain time.Duration) error {
	signals := make(chan os.Signal, signalQueue)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan struct{})
	defer close(runDone)

	abort := make(chan struct{})

	go func() {
		select {
		case <-signals:
			cancel()
		case <-runDone:
			return
		}

		select {
		case <-signals:
			close(abort)
		case <-runDone:
		}
	}()

	return r.serve(ctx, srv, drain, abort)
}

// serve is the drain sequence behind Serve and Run. abort, when closed, skips
// the rest of the readiness window.
func (r *Router) serve(
	ctx context.Context,
	srv *http.Server,
	drain time.Duration,
	abort <-chan struct{},
) error {
	if !r.served.CompareAndSwap(false, true) {
		return ErrAlreadyServed
	}

	serverErr := make(chan error, 1)
	go func() { serverErr <- runServer(srv) }()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	r.health.Drain()
	log.GetLogger("router").InfoContext(ctx, "draining", "drain", drain.String())

	wait, shutdown := drainBudget(drain)
	aborted := false

	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-abort:
			aborted = true
		case err := <-serverErr:
			return err
		}
	}

	if drain <= 0 || aborted {
		if err := srv.Close(); err != nil {
			return fmt.Errorf("close server: %w", err)
		}
	} else {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdown)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down server: %w", err)
		}
	}

	if err := <-serverErr; err != nil {
		return err
	}

	log.GetLogger("router").InfoContext(ctx, "drained")

	return nil
}

// drainBudget splits one total drain budget: half for the readiness window,
// the remainder for Shutdown. The sum never exceeds total.
func drainBudget(total time.Duration) (time.Duration, time.Duration) {
	if total <= 0 {
		return 0, 0
	}
	wait := total / drainParts
	return wait, total - wait
}

// runServer serves srv, treating the error from Shutdown or Close as a clean
// stop.
func runServer(srv *http.Server) error {
	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}
