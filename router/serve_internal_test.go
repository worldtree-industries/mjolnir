package router

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/dfryer1193/mjolnir/health"
)

// Run closes the abort channel on a second signal. Serve cannot reach that
// path, so the drain sequence is exercised directly.
func TestServeAbortsTheDrainWindow(t *testing.T) {
	r, err := New(Config{Health: health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	addr := freeLoopbackAddr(t)
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	abort := make(chan struct{})
	errCh := make(chan error, 1)

	go func() { errCh <- r.serve(ctx, srv, 10*time.Second, abort) }()

	waitForDial(t, addr)

	start := time.Now()
	cancel()
	close(abort)

	select {
	case serveErr := <-errCh:
		if serveErr != nil {
			t.Fatalf("serve: %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return after the drain was aborted")
	}

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("serve took %s after abort, want well under the 10s drain", elapsed)
	}
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	return listener.Addr().String()
}

func waitForDial(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			_ = conn.Close()
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("server never started on %s", addr)
}
