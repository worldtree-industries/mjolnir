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
  - Remote address (the transport peer, never rewritten)
  - Client IP (when the router resolves one; see Router)
  - The line is written in a `defer`, so it is emitted even when the handler panics
  - Handlers keep the server writer's `http.Flusher`, `http.Hijacker`, and `io.ReaderFrom`, so streaming and `http.NewResponseController` work; `middleware.AsResponseWriter` reuses or adds the same wrapper for your own middleware, and `middleware.ResponseStarted` reports whether the status line has gone out
  - `RequestLoggerWith` takes an injected `*slog.Logger` instead of the `"http"` component logger

- **Request ID Tracking**: Automatic request ID generation and propagation
  - Generates UUID-based request IDs
  - Accepts an inbound `X-Request-ID` only when it is a UUID, canonicalised to lowercase hyphenated form; any other value is replaced, never echoed (ADR-0007)
  - Adds request ID to response headers
  - Available throughout the request context

- **Router**: one `router.Config`, and a fixed framework stack the app's own middleware runs inside
  - `r.Use(...)` works after `router.New`, the way chi documents it
  - Forwarding headers are ignored unless the proxies that set them are configured as trusted (ADR-0007)
  - `router.NewServer` builds an `http.Server` with slow-client timeouts

- **Health Checks**: Probe endpoints wired by the router itself, with startup and readiness checks required at construction
  - `/livez`, `/readyz`, and `/startupz`, for GET and HEAD; the paths are reserved for the framework
  - Automatic drain on SIGINT or SIGTERM within one time budget, with readiness as the drain signal

- **Standardized Error Handling**: Comprehensive error management system
  - RFC 9457 `application/problem+json` error responses
  - Automatic internal error logging
  - Panic recovery: logged through slog with the stack, answered with a 500 problem, or the connection aborted if the response had already started
  - Helper functions for common HTTP status codes
  - Handlers return an `error` carrying an `*httpx.Problem` instead of writing the error response themselves

## Installation

```bash
go get github.com/dfryer1193/mjolnir
```

## Requirements

- Go 1.27 or later
- Linux or macOS. Windows is not a supported platform: nothing is built or tested there, and a Windows build failure is not treated as a bug.
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

  codec := httpx.NewCodec(httpx.DefaultMaxBodyBytes)

  r.Get("/", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
    return codec.Respond(w, r, http.StatusOK, map[string]string{"msg": "Hello World!"})
  }))

  r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
    panic("This is a panic")
  })

  if err := r.Run(router.NewServer(":8080", r), 10*time.Second); err != nil {
    lg.Error("server failed", "error", err)
    os.Exit(1)
  }
}
```

### Router

`router.New` takes one `router.Config`:

```go
r, err := router.New(router.Config{
  Health:         health.Config{Started: ..., Ready: ...}, // required, see Health Checks
  TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, // optional
  AccessLogger:   lg,                                                    // optional; nil is the "http" logger
})
```

Every request passes through the framework stack, in this fixed order: request
ID, client IP, access logging, panic recovery, then HEAD-to-GET routing. Your
routes and middleware live on an app mux beneath it, so `Use`, `Get`, `Group`,
`Route`, `With`, `Mount`, `NotFound`, and `MethodNotAllowed` behave as they do
on any chi router. `r.Use(...)` after `router.New` works. App middleware sees
the request ID, and a request it rejects or a panic it raises is still
access-logged.

```go
r.Use(auth)          // wraps every app route, never the probes
r.Get("/widgets", ...)
```

**Client address.** By default the client IP is the transport peer, and
`X-Forwarded-For`, `X-Real-IP`, and `True-Client-IP` are ignored, so a client
cannot choose its logged address. Behind load balancers, list their CIDR
prefixes in `TrustedProxies`. `X-Forwarded-For` is then read right to left past
the trusted hops, and the first untrusted address is the client; a spoofed
leftmost entry has no effect. A peer outside every prefix is itself the client.
The access line always carries `remote_addr` (the peer as the server saw it),
and `client_ip` when one resolves. Resolution fails closed: an unparsable peer,
or a garbled `X-Forwarded-For` entry from a trusted one, resolves none. Read it in
a handler with chi's `middleware.GetClientIP(r.Context())`.

**Panics.** A panic anywhere below the framework stack, including one with
`http.ErrAbortHandler`, is logged through the access logger with its stack,
method, path, request ID, and `response_started`. If nothing has been written, the client gets a 500
problem. If the response had started, the connection is aborted, so a truncated
body cannot pass for a complete 200.

**Server.** `router.NewServer(addr, r)` returns an `http.Server` with
`ReadHeaderTimeout` 5s, `ReadTimeout` 30s, `IdleTimeout` 120s, and
`MaxHeaderBytes` 1 MiB. `WriteTimeout` is left at zero so that streaming
responses (SSE, long downloads) are not cut off. A handler that needs a write
deadline can set its own with `http.ResponseController.SetWriteDeadline`.

A Router serves one lifetime: a second `Serve`, `ServeListener` or `Run`
returns `router.ErrAlreadyServed`. A `Serve` or `Run` whose listen failed does
not use it up, so a retry after "address in use" works.

### Health Checks

Every router carries three probes. The startup and readiness functions are
required: `router.New` returns an error naming the one that is missing, so an
application cannot start without a health surface.

```go
r, err := router.New(router.Config{Health: health.Config{
  Started: health.AlwaysStarted,                   // initialization complete
  Ready:   func(ctx context.Context) bool { ... }, // consulted per probe
}})
```

| Path | Meaning | Behaviour |
| --- | --- | --- |
| `/livez` | the process is not wedged | always 200; a failure means "restart me" |
| `/readyz` | the process can serve traffic now | 200/503 from `ReadyFunc`, on every request |
| `/startupz` | initialization is complete | 200 once `StartedFunc` first returns true, latched |

Responses are 200/503 with an empty body and no content type. `HEAD` gets the
same status as `GET`; any other method gets an empty 405, with no `Allow` header. Probes
appear in the request log like any other request.

The three paths are reserved. App middleware never runs on them, whatever the
method, so auth or rate limiting cannot fail a probe, and an app route on one
of them is never reached.

`r.Health()` returns the probe state, so an app can call `r.Health().Drain()`
to flip readiness to 503 on its own trigger.

`StartedFunc` must never check a dependency. A failing startup probe kills the
container, which turns an outage into a restart loop — report dependencies from
`ReadyFunc` instead.

### Graceful shutdown

`Router.Run` starts the drain on SIGINT or SIGTERM; `Router.Serve` does the same
when the context you pass is cancelled. Both listen on `srv.Addr` (`:http` when
empty), and a listen failure, such as an address in use, is returned before any
drain starts and leaves the Router free to try again.

`Router.ServeListener(ctx, ln, srv, drain)` serves on a listener you already
hold instead (socket activation, a pre-bound port, an in-memory listener in
tests), with the same drain. It owns `ln` from the call on: shutting down
closes it. For TLS, wrap the listener first:

```go
ln, err := net.Listen("tcp", ":8443")
if err != nil { ... }
err = r.ServeListener(ctx, tls.NewListener(ln, tlsConfig), router.NewServer("", r), 10*time.Second)
```

The drain duration is one budget, split in half:

1. `/readyz` answers 503 without consulting `ReadyFunc`.
2. For the first half, the server keeps serving, so load balancers stop
   routing to the process.
3. `Shutdown` runs with the second half as its deadline, letting in-flight
   requests finish. A request still running at the deadline is dropped by
   `Close`, and `Serve` or `ServeListener` returns the deadline error.

`/livez` keeps answering 200 throughout: a restart mid-drain would interrupt the
requests being finished. Under `Run`, a second signal closes the server at any
point, during the serving half or during `Shutdown`. `Serve` and
`ServeListener` drain on context cancellation and have no abort: the drain
always runs to its budget.

The whole drain fits inside its duration, so a Kubernetes
`terminationGracePeriodSeconds` must exceed the drain plus whatever the process
needs to close its own dependencies: a 10s drain needs more than 10s plus the
dependency teardown. Kubernetes removes endpoints asynchronously after SIGTERM,
so the serving half must be long enough for that removal to propagate. A drain
of zero closes the server immediately instead.

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
