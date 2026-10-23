# Mjolnir

Mjolnir is an opinionated wrapper around [go-chi/chi](https://github.com/go-chi/chi) that provides enhanced middleware and utilities for building HTTP services in Go. It adds structured logging, UUID request IDs, health checks, RFC 9457 problem responses, JSON codecs, and database primitives.

## Features

- **Request logging**: slog-based access lines with method, path, status, bytes, latency, `remote_addr`, `client_ip`, and `request_id`. The line is deferred, so a panic still produces one.
- **Request IDs**: a canonical UUID in `X-Request-ID`, the request context, log lines, and problem `instance`. A client value is accepted only when it parses as a UUID; otherwise a v4 UUID is generated.
- **Client IP trust**: forwarding headers are ignored unless `TrustedProxies` lists the peer CIDRs. The transport peer remains in `remote_addr`; the resolved address is `client_ip`.
- **Health checks**: `/livez`, `/readyz`, and `/startupz` answer GET and HEAD. Startup and readiness functions are required at construction.
- **RFC 9457 problems**: `application/problem+json` with `type`, `title`, `status`, `detail`, `instance`, and a stable `code`. 4xx details are public; 5xx details are fixed and the cause is logged.
- **JSON codec**: `application/json` and `+json` media types, parameters accepted, 1 MiB default request limit, 413 on overflow, and 415 on an unsupported type.
- **Panic recovery**: a panic before any write becomes a 500 problem; a panic after a write is logged and the connection is aborted instead of returning a truncated 200.
- **Graceful shutdown**: readiness flips to 503, then one total drain budget is split between the readiness window and `Shutdown`.
- **Database and retry helpers**: pgx pool configuration, transactions, and context-aware retry.

## Installation

```bash
go get github.com/dfryer1193/mjolnir
```

## Requirements

- Go 1.27 or later (`utils/optional` uses a generic method)
- `github.com/go-chi/chi/v5` v5.5.0 or later
- `github.com/google/uuid`
- `github.com/jackc/pgx/v5`

## Usage

### Basic example

```go
package main

import (
	"net/http"
	"os"
	"time"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

func main() {
	lg := log.GetLogger("app")
	codec := httpx.NewCodec(0) // 0 means the 1 MiB default

	r, err := router.New(router.Config{
		Health: health.Config{
			Started: health.AlwaysStarted,
			Ready:   health.AlwaysReady,
		},
	})
	if err != nil {
		lg.Error("failed to build router", "error", err)
		os.Exit(1)
	}

	r.Get("/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) *httpx.Problem {
		return codec.Respond(w, r, http.StatusOK, map[string]string{"msg": "Hello World!"})
	}))

	r.Post("/echo", httpx.Handler(func(w http.ResponseWriter, r *http.Request) *httpx.Problem {
		var in struct {
			Name string `json:"name"`
		}
		if p := codec.Decode(w, r, &in); p != nil {
			return p
		}
		return codec.Respond(w, r, http.StatusOK, map[string]string{"msg": "Hello " + in.Name})
	}))

	r.Get("/panic", func(http.ResponseWriter, *http.Request) {
		panic("This is a panic")
	})

	srv := router.NewServer(":8080", r)

	if err := r.Run(srv, 10*time.Second); err != nil {
		lg.Error("server failed", "error", err)
		os.Exit(1)
	}
}
```

### Health checks

Every router carries three probes. The startup and readiness functions are required: `router.New` returns an error naming the one that is missing, so an application cannot start without a health surface.

```go
r, err := router.New(router.Config{
	Health: health.Config{
		Started: health.AlwaysStarted,                   // initialization complete
		Ready:   func(ctx context.Context) bool { ... }, // consulted per probe
	},
})
```

| Path | Meaning | Behaviour |
| --- | --- | --- |
| `/livez` | the process is not wedged | always 200; a failure means "restart me" |
| `/readyz` | the process can serve traffic now | 200/503 from `ReadyFunc`, on every request |
| `/startupz` | initialization is complete | 200 once `StartedFunc` first returns true, latched |

Probes answer GET and HEAD with 200/503, an empty body, and no content type; other methods get 405. `Router.Health()` exposes the handle for an application-owned serve loop.

`StartedFunc` must never check a dependency. A failing startup probe kills the container, which turns an outage into a restart loop: report dependencies from `ReadyFunc` instead.

### Request IDs and client IP

Every request gets a canonical UUID in `X-Request-ID` and in the context (`middleware.GetRequestID`). An inbound value is accepted only if it parses as a UUID (canonical, braced, 32-hex, or URN form); a malformed, empty, or oversized value is replaced with a generated v4 UUID.

No forwarding header is trusted by default. Configure the proxies whose headers you trust:

```go
r, err := router.New(router.Config{
	Health: health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady},
	TrustedProxies: []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.0.2.0/24"),
	},
})
```

With no prefixes, `X-Forwarded-For`, `X-Real-IP`, and `True-Client-IP` do not affect the resolved address. With prefixes, chi walks the forwarding chain from the right and stops at the first untrusted hop, so a client-added leftmost value cannot spoof the result. `remote_addr` always remains the transport peer.

### JSON codec and body limits

```go
codec := httpx.NewCodec(0)        // 1 MiB
upload := httpx.NewCodec(8 << 20) // per-route override: 8 MiB
```

`Codec.Decode` accepts `application/json` and structured-syntax `+json` media types, with parameters such as `charset=utf-8`. It reads at most the codec's limit. An oversized body returns 413, malformed JSON 400, an empty body 400, and an unsupported media type 415. `Codec.Respond` marshals before writing, so an encoding failure cannot leave a partial response.

### Error responses

Handlers return a `*httpx.Problem`; `httpx.Handler` writes it. A `nil` return means the handler already wrote a successful response.

```go
r.Get("/thing", httpx.Handler(func(w http.ResponseWriter, r *http.Request) *httpx.Problem {
	if err := doThing(); err != nil {
		return httpx.Internal(err) // 500, cause logged, generic detail
	}
	return httpx.NotFound("thing_not_found", "no thing with that id")
}))
```

Problems use `application/problem+json`:

```json
{
  "type": "about:blank",
  "title": "Not Found",
  "status": 404,
  "detail": "no thing with that id",
  "instance": "urn:uuid:9f1c0f2e-...",
  "code": "thing_not_found"
}
```

`code` is the stable machine-readable identity. `instance` identifies the occurrence with the request ID.

Rules enforced by the package:

- 4xx: `detail` is the explicit public message. The wrapped cause is never written to the response.
- 5xx: the caller's status and code are preserved (a 503 stays 503); `detail` is replaced with the status text and the cause is logged at Error with method, path, and request ID.
- `NewProblem` rejects a status outside 4xx/5xx, an empty code, and a 5xx without a cause.
- A problem returned after the response started is not written; `WriteProblem` returns `httpx.ErrResponseStarted`.
- A panic before any write becomes a 500 problem. A panic after a write is logged with a stack and the connection is aborted, so the client does not receive a second body or a clean truncated 200.

Helpers: `BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `UnprocessableEntity`, `Internal`, and `ServiceUnavailable`.

### Migrating from `errorx`

`utils/errorx` is removed. The shapes map directly:

- `errorx.ErrorHandler(fn)` → `httpx.Handler(fn)`
- `*errorx.APIError` → `*httpx.Problem`
- `errorx.BadRequestErr(err)` → `httpx.BadRequest("bad_request", "public detail")`
- `errorx.InternalServerErr(err)` → `httpx.Internal(err)`
- `errorx.NewAPIError(err, status)` → `httpx.NewProblem(status, "code", "public detail", err)`

`router.New` now takes `router.Config`; wrap the old health config as
`router.New(router.Config{Health: health.Config{...}})`.

`retry.Do` now takes a context and a `Config` with a `ShouldRetry` predicate:
`retry.Do(ctx, cfg, fn)` with `fn func(context.Context) error`. `RetryTypes` is
removed; express the same policy through `ShouldRetry`.

### Server timeouts

`router.NewServer` provides a baseline:

| Field | Value |
| --- | --- |
| `ReadHeaderTimeout` | 5s |
| `ReadTimeout` | 30s |
| `IdleTimeout` | 120s |
| `MaxHeaderBytes` | 1 MiB |
| `WriteTimeout` | unset |

`WriteTimeout` is deliberately zero: a non-zero value breaks SSE and WebSocket routes. Set it explicitly for services that do not stream.

### Graceful shutdown

`Router.Run` starts the drain on SIGINT or SIGTERM; `Router.Serve` does the same when the context is cancelled.

1. `/readyz` answers 503 without consulting `ReadyFunc`.
2. The first half of the drain budget is the readiness window, so load balancers stop routing to the process.
3. `Shutdown` runs with the remainder, letting in-flight requests finish.
4. A second signal skips the rest of the window and closes the server. A drain of zero closes immediately.

The total time is bounded by the drain value, not twice it. A Kubernetes `terminationGracePeriodSeconds` still needs to exceed the drain plus dependency teardown and endpoint-removal propagation.

### Logging

`utils/log` writes human-readable lines to stdout and JSON to a file. `log.SetSinkErrorHandler(func(error))` receives JSON sink failures; the default is a one-time stderr warning. A failing log sink never fails the request.

### Other utilities

- `utils/db`: pgx pool configuration with per-field default merging, `Pool.Begin`, and masked DSN logging.
- `utils/tx`: `tx.New(Beginner)` accepts `*db.Pool` or `*pgxpool.Pool`; `InTx` commits, rolls back on error, and rolls back and re-panics on panic.
- `utils/retry`: context-aware retry with `ShouldRetry`, partial-config merging, exponential backoff, and a delay cap.
- `utils/config`: a temporary generic JSON loader (`config.Load(path, defaults)`); expect it to be replaced.

## Release policy

This is the v1 module path. A breaking change ships as a **minor** release, never a patch; label the pull request `semver:minor`. A later `/v2` rename remains possible and would use `semver:major`. The full policy is in ADR-0007.
