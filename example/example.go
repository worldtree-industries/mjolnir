package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/config"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

const (
	drainWindow     = 10 * time.Second
	defaultPort     = 8080
	defaultMinConns = 5
	defaultMaxConns = 25
)

func main() {
	lg := log.GetLogger("example")

	cfg, cfgErr := config.Load("config.json", appDefaults())
	if cfgErr != nil {
		lg.Error("Failed to load config", "error", cfgErr)
		os.Exit(1)
	}

	codec := httpx.NewCodec(0)

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

	address := serverAddress(cfg)

	lg.Info("Server starting", "address", address)

	srv := router.NewServer(address, r)

	if runErr := r.Run(srv, drainWindow); runErr != nil {
		lg.Error("Server failed", "error", runErr)
		os.Exit(1)
	}

	lg.Info("Server stopped")
}

// appDefaults is the example application's schema. The config package is a
// temporary generic loader, so the schema lives here rather than in the
// library.
func appDefaults() config.Config {
	return config.Config{
		"server": map[string]any{
			"host":         "localhost",
			"port":         defaultPort,
			"readTimeout":  "30s",
			"writeTimeout": "30s",
		},
		"log": map[string]any{
			"level":       "info",
			"json_output": true,
		},
		"db": map[string]any{
			"pool": map[string]any{
				"min_conns": defaultMinConns,
				"max_conns": defaultMaxConns,
			},
		},
		"migrations": map[string]any{
			"directory": "./migrations",
		},
	}
}

func serverAddress(cfg config.Config) string {
	server, _ := cfg["server"].(map[string]any)
	host, _ := server["host"].(string)
	port := intValue(server["port"])

	if host == "" || port <= 0 {
		return ":" + strconv.Itoa(defaultPort)
	}

	return net.JoinHostPort(host, strconv.Itoa(port))
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return 0
	}
}
