package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/dfryer1193/mjolnir/health"
	enhancedmiddleware "github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// Router is a pre-configured chi router with the probe endpoints mounted.
type Router struct {
	*chi.Mux

	health *health.Health
}

// New creates a pre-configured router.
//
// The startup and readiness functions are required: New returns an error when
// either is missing, so an application cannot start without a health surface.
// The router mounts /livez, /readyz, and /startupz for GET.
//
// Logging is configured by the application (utils/log); the router does not
// set up a logger.
func New(cfg health.Config) (*Router, error) {
	h, err := health.New(cfg)
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()

	// Add default chi middleware
	r.Use(middleware.Recoverer)

	r.Use(enhancedmiddleware.RequestID)
	r.Use(enhancedmiddleware.RequestLogger)

	r.Method(http.MethodGet, "/livez", h.Liveness())
	r.Method(http.MethodGet, "/readyz", h.Readiness())
	r.Method(http.MethodGet, "/startupz", h.Startup())

	return &Router{Mux: r, health: h}, nil
}

// Serve runs srv until ctx is cancelled, then drains and shuts it down.
//
// On cancellation readiness answers 503 so load balancers stop sending
// traffic, srv keeps serving for drain so in-flight requests can finish, and
// Shutdown is then called with the same duration as its deadline. A drain of
// zero closes srv immediately, dropping in-flight requests.
//
// A Router serves one lifetime: once drained, readiness stays 503.
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
// the rest of the drain window.
func (r *Router) serve(
	ctx context.Context,
	srv *http.Server,
	drain time.Duration,
	abort <-chan struct{},
) error {
	serverErr := make(chan error, 1)
	go func() { serverErr <- runServer(srv) }()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	r.health.Drain()
	log.GetLogger("router").InfoContext(ctx, "draining", "drain", drain.String())

	aborted := false

	if drain > 0 {
		select {
		case <-time.After(drain):
		case <-abort:
			aborted = true
		case err := <-serverErr:
			return err
		}
	}

	if drain == 0 || aborted {
		if err := srv.Close(); err != nil {
			return fmt.Errorf("close server: %w", err)
		}
	} else {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), drain)
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

// runServer serves srv, treating the error from Shutdown or Close as a clean
// stop.
func runServer(srv *http.Server) error {
	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}
