package router

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
)

// MemListener is an in-memory [net.Listener] for tests. Its connections are
// [net.Pipe]s, so inside a testing/synctest bubble a server on it blocks
// durably and the bubble's fake clock can advance; loopback TCP cannot.
//
// It is defined in an internal test file so that the internal signal tests
// and the external lifecycle tests share it.
type MemListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
	client *http.Client
}

// NewMemListener returns an open MemListener. Create it inside the bubble
// that uses it.
func NewMemListener() *MemListener {
	l := &MemListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	// No keep-alives: every request has its own connection, so none is left
	// idle when the bubble ends.
	l.client = &http.Client{Transport: &http.Transport{DialContext: l.DialContext, DisableKeepAlives: true}}

	return l
}

// errRefused is the dial error once the listener is closed.
var errRefused = errors.New("memnet: connection refused")

// Accept waits for the next dialled connection.
func (l *MemListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

// Close stops Accept and refuses later dials. It is safe to call twice.
func (l *MemListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

// Addr is a placeholder address.
func (l *MemListener) Addr() net.Addr { return memAddr{} }

// Closed is done once the listener is closed.
func (l *MemListener) Closed() <-chan struct{} { return l.closed }

// DialContext connects to the listener, whatever the network and address.
func (l *MemListener) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()

	select {
	case l.conns <- server:
		return client, nil
	case <-l.closed:
	case <-ctx.Done():
	}

	_ = client.Close()
	_ = server.Close()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return nil, errRefused
}

// Status returns the status of a GET to path on the listener, or 0 when it
// cannot be reached.
func (l *MemListener) Status(t testing.TB, path string) int {
	t.Helper()

	resp, err := l.get(path)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode
}

// StartStuck adds a /stuck route to r that only Close can end, starts a
// request to it, and waits until its handler runs. The channel receives the
// request's error. Call it inside the bubble.
func (l *MemListener) StartStuck(t testing.TB, r *Router) <-chan error {
	t.Helper()

	r.Get("/stuck", func(_ http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
	})

	errCh := make(chan error, 1)

	go func() {
		resp, err := l.get("/stuck")
		if err == nil {
			_ = resp.Body.Close()
		}
		errCh <- err
	}()

	synctest.Wait()

	return errCh
}

func (l *MemListener) get(path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://mem"+path, nil)
	if err != nil {
		return nil, err
	}

	return l.client.Do(req)
}

type memAddr struct{}

func (memAddr) Network() string { return "mem" }
func (memAddr) String() string  { return "mem" }
