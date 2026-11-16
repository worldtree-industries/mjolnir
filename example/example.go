package main

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// drainBudget is the whole shutdown budget: half with readiness at 503, half
// for Shutdown. Keep it under the orchestrator's termination grace period.
const drainBudget = 10 * time.Second

func main() {
	lg := log.GetLogger("example")

	r, err := router.New(router.Config{
		Health: health.Config{
			Started: health.AlwaysStarted,
			Ready:   health.AlwaysReady,
		},
	})
	if err != nil {
		lg.Error("Failed to build router", "error", err)
		os.Exit(1)
	}

	codec := httpx.NewCodec(httpx.DefaultMaxBodyBytes)

	r.Get("/", httpx.Handler(func(w http.ResponseWriter, _ *http.Request) error {
		if _, writeErr := w.Write([]byte("Hello World!")); writeErr != nil {
			return httpx.Internal(writeErr)
		}
		return nil
	}))

	r.Get("/json", httpx.Handler(func(w http.ResponseWriter, req *http.Request) error {
		return codec.Respond(w, req, http.StatusOK, map[string]string{"msg": "Hello World!"})
	}))

	r.Post("/json", httpx.Handler(func(w http.ResponseWriter, req *http.Request) error {
		var name struct {
			Name string `json:"name"`
		}
		if decodeErr := codec.Decode(w, req, &name); decodeErr != nil {
			return decodeErr
		}
		return codec.Respond(w, req, http.StatusOK, map[string]string{"msg": "Hello " + name.Name})
	}))

	r.Get("/panic", func(_ http.ResponseWriter, _ *http.Request) {
		panic("This is a panic")
	})

	r.Get("/error", httpx.Handler(func(_ http.ResponseWriter, _ *http.Request) error {
		return httpx.ServiceUnavailable(errors.New("this is an error"))
	}))

	lg.Info("Server starting on :8080")

	if runErr := r.Run(router.NewServer(":8080", r), drainBudget); runErr != nil {
		lg.Error("Server failed", "error", runErr)
		os.Exit(1)
	}

	lg.Info("Server stopped")
}
