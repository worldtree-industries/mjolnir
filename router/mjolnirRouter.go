package router

import (
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	enhancedmiddleware "github.com/dfryer1193/mjolnir/middleware"
)

// New creates a new pre-configured chi router.
//
//nolint:reassign // initialize logger for router
func New() *chi.Mux {
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339Nano,
	})
	r := chi.NewRouter()

	// Add default chi middleware
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Use(enhancedmiddleware.RequestID)
	r.Use(enhancedmiddleware.RequestLogger)

	return r
}
