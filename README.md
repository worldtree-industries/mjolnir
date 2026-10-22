# Mjolnir

Mjolnir is an opinionated wrapper around [go-chi/chi](https://github.com/go-chi/chi) that provides enhanced middleware and utilities for building HTTP services in Go. It adds structured logging, request ID tracking, and standardized error handling out of the box.

## Features

- **Request Logging**: Built-in zerolog-based structured logging middleware that captures:
  - Request ID
  - HTTP method
  - Path
  - Remote address
  - Status code
  - Request latency

- **Request ID Tracking**: Automatic request ID generation and propagation
  - Generates UUID-based request IDs
  - Respects existing `X-Request-ID` headers
  - Adds request ID to response headers
  - Available throughout the request context

- **Standardized Error Handling**: Comprehensive error management system
  - Consistent JSON error responses
  - Automatic internal error logging
  - Panic recovery
  - Helper functions for common HTTP status codes
  - Context-based error propagation

## Installation

```bash
go get github.com/dfryer1193/mjolnir
```

## Requirements

- Go 1.27 or later
- github.com/go-chi/chi/v5
- github.com/rs/zerolog
- github.com/google/uuid

## Usage

### Basic Example

```go
package main

import (
  "net/http"
  "time"

  "github.com/dfryer1193/mjolnir/health"
  "github.com/dfryer1193/mjolnir/router"
  "github.com/dfryer1193/mjolnir/utils/httpx"
)

func main() {
  r, err := router.New(health.Config{
    Started: health.AlwaysStarted,
    Ready:   health.AlwaysReady,
  })
  if err != nil {
    panic(err)
  }

  r.Get("/", func(w http.ResponseWriter, r *http.Request) {
    httpx.RespondJSON(w, r, http.StatusOK, map[string]string{"msg": "Hello World!"})
  })

  r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
    panic("This is a panic")
  })

  srv := &http.Server{
    Addr:              ":8080",
    Handler:           r,
    ReadHeaderTimeout: 5 * time.Second,
  }

  if err := r.Run(srv, 10*time.Second); err != nil {
    panic(err)
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
```go
import "github.com/dfryer1193/mjolnir/utils/httpx"

// Respond with JSON
httpx.RespondJSON(w, r, http.StatusOK, payload)

// Decode JSON request body
var data MyStruct
if err := httpx.DecodeJSON(r, &data); err != nil {
// Handle error
}

// Validate content type
if !httpx.ValidateContentType(r, "application/json") {
// Handle invalid content type
}
```

### Error Handling Utilities
Mjolnir provides convenient error handling functions:
```go
middleware.SetError(r, status, err)           // Generic error
middleware.SetInternalError(r, err)           // 500 Internal Server Error
middleware.SetBadRequestError(r, err)         // 400 Bad Request
middleware.SetNotFoundError(r, err)           // 404 Not Found
middleware.SetUnauthorizedError(r, err)       // 401 Unauthorized
```

#### General Usage
To handle errors in logic, you can use `SetError` (or any of the more specific error handling functions) at the site of the error to be handled by the error handling middleware.
```go
r.Get("/error", func(w http.ResponseWriter, r *http.Request) {
	middleware.SetError(r, 504, errors.New("this is an error"))
})
```

### Error Response Format

All errors are returned as JSON with the following structure:

```json
{
    "error": "Error message",
    "code": 400
}
```

#### Error Handling Behavior

The error handler distinguishes between internal (500-level) and other errors:

- **Internal Server Errors (500+)**:
  - Logs the full error details including stack trace to the server logs
  - Returns a generic "Internal Server Error" message to the client
  - Includes request ID in logs for correlation
  - Always returns status code 500

Example internal error response:
```json
{
    "error": "Internal Server Error",
    "code": 500
}
```

- **Other Errors (4xx)**:
  - Returns the actual error message to the client
  - Includes the specific status code
  - Does not log detailed error information

Example client error response:
```json
{
    "error": "Resource not found",
    "code": 404
}
```

This approach ensures that sensitive internal error details are never exposed to clients while still providing meaningful error messages for client-side issues.
