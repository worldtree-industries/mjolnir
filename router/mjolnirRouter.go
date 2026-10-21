package router

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	enhancedmiddleware "github.com/dfryer1193/mjolnir/middleware"
)

// New creates a new pre-configured chi router.
//
// Logging is configured by the application (utils/log); the router does not
// set up a logger.
func New() *chi.Mux {
	r := chi.NewRouter()

	// Add default chi middleware
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Use(enhancedmiddleware.RequestID)
	r.Use(enhancedmiddleware.RequestLogger)

	return r
}
