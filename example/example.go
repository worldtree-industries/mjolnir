package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

const (
	readHeaderTimeout = 5 * time.Second
	drainWindow       = 10 * time.Second
)

func main() {
	lg := log.GetLogger("example")
	codec := httpx.NewCodec(0)

	r, err := router.New(health.Config{
		Started: health.AlwaysStarted,
		Ready:   health.AlwaysReady,
	})
	if err != nil {
		lg.Error("Failed to build router", "error", err)
		os.Exit(1)
	}

	r.Get("/", httpx.Handler(func(w http.ResponseWriter, _ *http.Request) *httpx.Problem {
		if _, writeErr := w.Write([]byte("Hello World!")); writeErr != nil {
			return httpx.Internal(writeErr)
		}
		return nil
	}))

	r.Get("/json", httpx.Handler(func(w http.ResponseWriter, req *http.Request) *httpx.Problem {
		return codec.Respond(w, req, http.StatusOK, map[string]string{"msg": "Hello World!"})
	}))

	r.Post("/json", httpx.Handler(func(w http.ResponseWriter, req *http.Request) *httpx.Problem {
		var name struct {
			Name string `json:"name"`
		}
		if p := codec.Decode(w, req, &name); p != nil {
			return p
		}
		return codec.Respond(w, req, http.StatusOK, map[string]string{"msg": "Hello " + name.Name})
	}))

	r.Get("/panic", func(_ http.ResponseWriter, _ *http.Request) {
		panic("This is a panic")
	})

	r.Get("/error", httpx.Handler(func(_ http.ResponseWriter, _ *http.Request) *httpx.Problem {
		return httpx.ServiceUnavailable(fmt.Errorf("this is an error"))
	}))

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
