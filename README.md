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

- Go 1.25 or later
- github.com/go-chi/chi/v5
- github.com/rs/zerolog
- github.com/google/uuid

## Usage

### Basic Example

```go
package main

import (
  "github.com/dfryer1193/mjolnir/router"
  "github.com/dfryer1193/mjolnir/utils/httpx"
  "github.com/rs/zerolog/log"
  "net/http"
)

func main() {
  r := router.New()

  r.Get("/", func(w http.ResponseWriter, r *http.Request) {
    httpx.RespondJSON(w, r, 200, map[string]string{"msg": "Hello World!"})
  })

  r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
    panic("This is a panic")
  })

  log.Info().Msg("Server starting on :8080")
  http.ListenAndServe(":8080", r)
}
```

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
