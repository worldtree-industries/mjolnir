package main

import (
	"fmt"
	"net/http"

	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/errorx"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/rs/zerolog/log"
)

func main() {
	r := router.New()

	r.Get("/", errorx.ErrorHandler(
		func(w http.ResponseWriter, r *http.Request) *errorx.ApiError {
			_, err := w.Write([]byte("Hello World!"))
			if err != nil {
				return errorx.InternalServerErr(err)
			}
			return nil
		}),
	)

	r.Get("/json", func(w http.ResponseWriter, r *http.Request) {
		httpx.RespondJSON(w, r, 200, map[string]string{"msg": "Hello World!"})
	})

	r.Post("/json", errorx.ErrorHandler(
		func(w http.ResponseWriter, r *http.Request) *errorx.ApiError {
			var name struct {
				Name string `json:"name"`
			}

			_, err := httpx.DecodeJSON(r, &name)
			if err != nil {
				return errorx.BadRequestErr(err)
			}

			httpx.RespondJSON(w, r, 200, map[string]string{"msg": "Hello " + name.Name})
			return nil
		}),
	)

	r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("This is a panic")
	})

	r.Get("/error", errorx.ErrorHandler(
		func(w http.ResponseWriter, r *http.Request) *errorx.ApiError {
			return errorx.NewApiError(fmt.Errorf("This is an error"), http.StatusServiceUnavailable)
		}),
	)

	log.Info().Msg("Server starting on :8080")
	err := http.ListenAndServe(":8080", r)
	if err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}
	log.Info().Msg("Server stopped")
}
