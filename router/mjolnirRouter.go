package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/dfryer1193/mjolnir/health"
	enhancedmiddleware "github.com/dfryer1193/mjolnir/middleware"
)

// Router is a pre-configured chi router with the probe endpoints mounted.
type Router struct {
	*chi.Mux
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
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Use(enhancedmiddleware.RequestID)
	r.Use(enhancedmiddleware.RequestLogger)

	r.Method(http.MethodGet, "/livez", h.Liveness())
	r.Method(http.MethodGet, "/readyz", h.Readiness())
	r.Method(http.MethodGet, "/startupz", h.Startup())

	return &Router{Mux: r}, nil
}
