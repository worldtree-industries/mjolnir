package main

import (
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/errorx"
	"github.com/dfryer1193/mjolnir/utils/httpx"
)

func main() {
	r := router.New()

	r.Get("/", errorx.ErrorHandler(
		func(w http.ResponseWriter, _ *http.Request) *errorx.APIError {
			_, err := w.Write([]byte("Hello World!"))
			if err != nil {
				return errorx.InternalServerErr(err)
			}
			return nil
		}),
	)

	r.Get("/json", func(w http.ResponseWriter, req *http.Request) {
		_ = httpx.RespondJSON(w, req, http.StatusOK, map[string]string{"msg": "Hello World!"})
	})

	r.Post("/json", errorx.ErrorHandler(
		func(w http.ResponseWriter, req *http.Request) *errorx.APIError {
			var name struct {
				Name string `json:"name"`
			}

			_, err := httpx.DecodeJSON(req, &name)
			if err != nil {
				return errorx.BadRequestErr(err)
			}

			_ = httpx.RespondJSON(w, req, http.StatusOK, map[string]string{"msg": "Hello " + name.Name})
			return nil
		}),
	)

	r.Get("/panic", func(_ http.ResponseWriter, _ *http.Request) {
		panic("This is a panic")
	})

	r.Get("/error", errorx.ErrorHandler(
		func(_ http.ResponseWriter, _ *http.Request) *errorx.APIError {
			return errorx.NewAPIError(fmt.Errorf("this is an error"), http.StatusServiceUnavailable)
		}),
	)

	log.Info().Msg("Server starting on :8080")
	//nolint:gosec // This is an example server, timeouts configured elsewhere if needed
	err := http.ListenAndServe(":8080", r)
	if err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}
	log.Info().Msg("Server stopped")
}
