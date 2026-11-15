package router

import (
	"net/http"
	"time"
)

// The timeouts NewServer sets.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 1 << 20
)

// NewServer returns an [http.Server] serving h on addr with timeouts that
// bound slow clients: 5s to read the request headers, 30s to read the whole
// request, 120s for an idle keep-alive connection, and 1 MiB of headers.
//
// WriteTimeout is left at zero, so streamed responses (SSE, long downloads)
// are not cut off. A handler that needs a write deadline can set one with
// [http.ResponseController.SetWriteDeadline].
func NewServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}
