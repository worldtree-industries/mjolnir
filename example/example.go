package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/errorx"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

const (
	readHeaderTimeout = 5 * time.Second
	drainWindow       = 10 * time.Second
)

func main() {
	lg := log.GetLogger("example")

	r, err := router.New(health.Config{
		Started: health.AlwaysStarted,
		Ready:   health.AlwaysReady,
	})
	if err != nil {
		lg.Error("Failed to build router", "error", err)
		os.Exit(1)
	}

	r.Get("/", errorx.ErrorHandler(
		func(w http.ResponseWriter, _ *http.Request) *errorx.APIError {
			_, writeErr := w.Write([]byte("Hello World!"))
			if writeErr != nil {
				return errorx.InternalServerErr(writeErr)
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

			_, decodeErr := httpx.DecodeJSON(req, &name)
			if decodeErr != nil {
				return errorx.BadRequestErr(decodeErr)
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

	lg.Info("Server starting on :8080")

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           r,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	if runErr := r.Run(srv, drainWindow); runErr != nil {
		lg.Error("Server failed", "error", runErr)
		os.Exit(1)
	}

	lg.Info("Server stopped")
}
