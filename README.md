# Mjolnir

Mjolnir is an opinionated wrapper around [go-chi/chi](https://github.com/go-chi/chi) that provides enhanced middleware and utilities for building HTTP services in Go. It adds structured logging, request ID tracking, health checks, and standardized error handling out of the box.

## Features

- **Request Logging**: Built-in slog-based structured logging middleware that writes one access line per request, capturing:
  - Request ID (when present)
  - HTTP method
  - Path
  - Status code
  - Response body bytes
  - Request latency
  - Remote address (the transport peer)
  - Client IP (only when a client-IP resolver has run)
  - The line is written in a `defer`, so it is emitted even when the handler panics
  - Handlers keep the server writer's `http.Flusher`, `http.Hijacker`, and `io.ReaderFrom`, so streaming and `http.NewResponseController` work; `middleware.AsResponseWriter` reuses or adds the same wrapper for your own middleware, and `middleware.ResponseStarted` reports whether the status line has gone out
  - `RequestLoggerWith` takes an injected `*slog.Logger` instead of the `"http"` component logger

- **Request ID Tracking**: Automatic request ID generation and propagation
  - Generates UUID-based request IDs
  - Accepts an inbound `X-Request-ID` only when it is a UUID, canonicalised to lowercase hyphenated form; any other value is replaced, never echoed (ADR-0007)
  - Adds request ID to response headers
  - Available throughout the request context

- **Health Checks**: Probe endpoints wired by the router itself, with startup and readiness checks required at construction
  - `/livez`, `/readyz`, and `/startupz`
  - Automatic drain on SIGINT or SIGTERM, with readiness as the drain signal

- **Standardized Error Handling**: Comprehensive error management system
  - RFC 9457 `application/problem+json` error responses
  - Automatic internal error logging
  - Panic recovery
  - Helper functions for common HTTP status codes
  - Handlers return an `error` carrying an `*httpx.Problem` instead of writing the error response themselves

## Installation

```bash
go get github.com/dfryer1193/mjolnir
```

## Requirements

- Go 1.27 or later
- github.com/go-chi/chi/v5
- github.com/google/uuid
- github.com/jackc/pgx/v5

## Usage

### Basic Example

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

  r, err := router.New(health.Config{
    Started: health.AlwaysStarted,
    Ready:   health.AlwaysReady,
  })
  if err != nil {
    lg.Error("failed to build router", "error", err)
    os.Exit(1)
  }

  codec := httpx.NewCodec(httpx.DefaultMaxBodyBytes)

  r.Get("/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
    return codec.Respond(w, r, http.StatusOK, map[string]string{"msg": "Hello World!"})
  }))

  r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
    panic("This is a panic")
  })

  srv := &http.Server{
    Addr:              ":8080",
    Handler:           r,
    ReadHeaderTimeout: 5 * time.Second,
  }

  if err := r.Run(srv, 10*time.Second); err != nil {
    lg.Error("server failed", "error", err)
    os.Exit(1)
  }
}
```

### Health Checks

Every router carries three probes. The startup and readiness functions are
required: `router.New` returns an error naming the one that is missing, so an
application cannot start without a health surface.

```go
r, err := router.New(health.Config{
  Started: health.AlwaysStarted,                   // initialization complete
  Ready:   func(ctx context.Context) bool { ... }, // consulted per probe
})
```

| Path | Meaning | Behaviour |
| --- | --- | --- |
| `/livez` | the process is not wedged | always 200; a failure means "restart me" |
| `/readyz` | the process can serve traffic now | 200/503 from `ReadyFunc`, on every request |
| `/startupz` | initialization is complete | 200 once `StartedFunc` first returns true, latched |

Responses are 200/503 with an empty body and no content type; other methods get
405. Probes appear in the request log like any other request.

`StartedFunc` must never check a dependency. A failing startup probe kills the
container, which turns an outage into a restart loop — report dependencies from
`ReadyFunc` instead.

### Graceful shutdown

`Router.Run` starts the drain on SIGINT or SIGTERM; `Router.Serve` does the same
when the context you pass is cancelled.

1. `/readyz` answers 503 without consulting `ReadyFunc`.
2. The server keeps serving for the drain duration, so load balancers stop
   routing to the process.
3. `Shutdown` runs with the same duration as its deadline, letting in-flight
   requests finish.

`/livez` keeps answering 200 throughout: a restart mid-drain would interrupt the
requests being finished. A second signal skips the rest of the window.

The drain duration bounds both the advertise window and the shutdown deadline,
so the worst case is twice that duration plus whatever the process needs to
close its own dependencies. A Kubernetes `terminationGracePeriodSeconds` must be
larger than that: a 10s drain needs at least 20s plus the dependency teardown.
Kubernetes removes endpoints asynchronously after SIGTERM, so the window also
has to be long enough for that removal to propagate. A drain of zero closes the
server immediately instead.

### HTTP Utility Functions

`utils/httpx` is the one place handlers translate to and from HTTP. A
`Codec` decodes JSON request bodies and writes JSON responses; both return an
`error` that is a `*httpx.Problem` on failure and `nil` on success:

```go
import "github.com/dfryer1193/mjolnir/utils/httpx"

codec := httpx.NewCodec(httpx.DefaultMaxBodyBytes) // 1 MiB; httpx.Codec{} is the same

r.Post("/widgets", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
  var in Widget
  if err := codec.Decode(w, r, &in); err != nil {
    return err
  }
  return codec.Respond(w, r, http.StatusCreated, in)
}))
```

`Decode` accepts `application/json` and any `+json` type (such as
`application/vnd.api+json`), whatever its parameters, such as `charset`. It
answers 415 for any other media type, 413 for a value that runs past the limit,
and 400 for a body that is not a JSON value of the expected shape. It reads the
first JSON value only and ignores anything after it, as `json.Decoder` does. `Respond` marshals the payload before
writing anything, so a marshal failure still becomes a clean 500.

### Error Handling Utilities

Handlers wrapped in `httpx.Handler` return an `error`, or `nil` when they
wrote a response themselves. An error that is or wraps (`%w`) a
`*httpx.Problem` sends that problem; any other error is sent as
`httpx.Internal(err)`, a 500 that logs the error as its cause:

```go
r.Get("/widgets/{id}", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
  widget, err := store.Get(r.Context(), chi.URLParam(r, "id"))
  switch {
  case errors.Is(err, store.ErrNotFound):
    return httpx.NotFound("widget_not_found", "No widget has that id.")
  case err != nil:
    return httpx.Internal(err)
  }
  return codec.Respond(w, r, http.StatusOK, widget)
}))
```

The helpers are `BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`,
`Conflict`, and `UnprocessableEntity` (each takes a code and a public detail,
and sends the code as given, even an empty one), and `Internal` and
`ServiceUnavailable` (each takes the cause and sends the code
`internal_server_error` or `service_unavailable`).
`httpx.NewProblem(status, code, detail, cause)` covers any other 4xx or 5xx
status and code; it rejects other statuses, an empty code, and a 5xx without a
cause. `httpx.HandlerWithLogger` takes the logger to use instead of the
`httpx` named logger.

### Error Response Format

Problems are sent as RFC 9457 problem documents with the content type
`application/problem+json` and a `code` extension member (the health probes
are the exception: they answer 200/503 with an empty body):

```json
{
    "type": "about:blank",
    "title": "Not Found",
    "status": 404,
    "detail": "No widget has that id.",
    "instance": "urn:uuid:6f1c2b9e-3d4a-4f5b-8c7d-9e0f1a2b3c4d",
    "code": "widget_not_found"
}
```

`instance` carries the request id when the `RequestID` middleware ran.

#### Error Handling Behavior

- **Client errors (4xx)**: the response carries the detail the handler wrote
  for the client and nothing else. A wrapped cause is never sent, and nothing
  is logged.
- **Server errors (5xx)**: the status and code stay as the handler chose them
  (a 503 is sent as 503), but the detail is replaced by the fixed
  `"The server could not complete the request."`. The handler's detail and the
  cause are logged at Error with the method, path, request id, status, and
  code.
- **A response that already started**: if the handler wrote a status or any
  body before returning a problem, nothing more is written, so bodies are
  never concatenated. The problem is logged at Warn instead.
  `httpx.WriteProblem` reports this case as `httpx.ErrResponseStarted`, but
  only when its writer is a `middleware.ResponseWriter`: `httpx.Handler`
  always provides one, while a direct `WriteProblem` call on any other writer
  writes the problem unconditionally. Wrap with `middleware.AsResponseWriter`
  first to get the check.

This approach ensures that sensitive internal error details are never exposed to clients while still providing meaningful error messages for client-side issues.
