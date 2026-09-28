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
		func(w http.ResponseWriter, r *http.Request) *errorx.APIError {
			_, err := w.Write([]byte("Hello World!"))
			if err != nil {
				return errorx.InternalServerErr(err)
			}
			return nil
		}),
	)

	r.Get("/json", func(w http.ResponseWriter, r *http.Request) {
		_ = httpx.RespondJSON(w, r, 200, map[string]string{"msg": "Hello World!"})
	})

	r.Post("/json", errorx.ErrorHandler(
		func(w http.ResponseWriter, r *http.Request) *errorx.APIError {
			var name struct {
				Name string `json:"name"`
			}

			_, err := httpx.DecodeJSON(r, &name)
			if err != nil {
				return errorx.BadRequestErr(err)
			}

			_ = httpx.RespondJSON(w, r, 200, map[string]string{"msg": "Hello " + name.Name})
			return nil
		}),
	)

	r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("This is a panic")
	})

	r.Get("/error", errorx.ErrorHandler(
		func(w http.ResponseWriter, r *http.Request) *errorx.APIError {
			return errorx.NewAPIError(fmt.Errorf("this is an error"), http.StatusServiceUnavailable)
		}),
	)

	log.Info().Msg("Server starting on :8080")
	err := http.ListenAndServe(":8080", r)
	if err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}
	log.Info().Msg("Server stopped")
}
