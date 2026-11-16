package router

import (
	"context"
	"net/http"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dfryer1193/mjolnir/health"
)

// signalRun is serveSignals running on an in-memory listener inside a
// synctest bubble, with fake signals sent on a channel.
type signalRun struct {
	r       *Router
	ln      *MemListener
	signals chan os.Signal
	cancel  context.CancelFunc
	errCh   chan error
}

func startSignals(t *testing.T, drain time.Duration) *signalRun {
	t.Helper()

	r, err := New(Config{Health: health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ln := NewMemListener()
	srv := &http.Server{Handler: r, ReadHeaderTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())

	s := &signalRun{
		r:       r,
		ln:      ln,
		signals: make(chan os.Signal, signalQueue),
		cancel:  cancel,
		errCh:   make(chan error, 1),
	}

	go func() { s.errCh <- r.serveSignals(ctx, s.signals, ln, srv, drain) }()

	t.Cleanup(func() {
		cancel()
		_ = srv.Close()
	})

	synctest.Wait()

	return s
}

// signal sends a fake signal and lets the server react to it.
func (s *signalRun) signal() {
	s.signals <- os.Interrupt
	synctest.Wait()
}

// returned reports whether serveSignals has returned, and with what.
func (s *signalRun) returned() (error, bool) {
	select {
	case err := <-s.errCh:
		return err, true
	default:
		return nil, false
	}
}

func TestServeSignalsServesUntilTheContextIsDoneWithoutASignal(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := startSignals(t, 0)

		synctest.Sleep(time.Hour)

		if err, done := s.returned(); done {
			t.Fatalf("serveSignals returned %v without a signal or cancellation", err)
		}
		if got := s.ln.Status(t, "/readyz"); got != http.StatusOK {
			t.Fatalf("readyz = %d, want 200", got)
		}

		s.cancel()

		if err := <-s.errCh; err != nil {
			t.Fatalf("serveSignals: %v", err)
		}
	})
}

func TestServeSignalsStartsTheDrainOnTheFirstSignal(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := startSignals(t, 10*time.Second)
		start := time.Now()

		s.signal()

		if got := s.ln.Status(t, "/readyz"); got != http.StatusServiceUnavailable {
			t.Fatalf("readyz after the first signal = %d, want 503", got)
		}
		if got := s.ln.Status(t, "/livez"); got != http.StatusOK {
			t.Fatalf("livez during the wait = %d, want 200", got)
		}

		if err := <-s.errCh; err != nil {
			t.Fatalf("serveSignals: %v", err)
		}
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("serveSignals returned %s after the signal, want the 5s wait", elapsed)
		}
	})
}

func TestServeSignalsClosesOnASecondSignalDuringTheWait(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := startSignals(t, 10*time.Second)
		stuck := s.ln.StartStuck(t, s.r)

		s.signal()
		synctest.Sleep(time.Second)

		start := time.Now()
		s.signal()

		err, done := s.returned()
		if !done || err != nil {
			t.Fatalf("serveSignals after the second signal = (%v, returned %t), want an immediate nil", err, done)
		}
		if reqErr := <-stuck; reqErr == nil {
			t.Fatal("the stuck request completed; want its connection closed")
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("the close took %s", elapsed)
		}
	})
}

func TestServeSignalsClosesOnASecondSignalDuringShutdown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// 5s of wait, then a 5s shutdown deadline the stuck request would
		// miss.
		s := startSignals(t, 10*time.Second)
		stuck := s.ln.StartStuck(t, s.r)

		s.signal()
		synctest.Sleep(6 * time.Second)

		// Shutdown closes the listener first.
		select {
		case <-s.ln.Closed():
		default:
			t.Fatal("Shutdown has not started 6s into the drain")
		}

		s.signal()

		// Nil, not the missed deadline: the signal closed the server first.
		err, done := s.returned()
		if !done || err != nil {
			t.Fatalf("serveSignals after the second signal = (%v, returned %t), want an immediate nil", err, done)
		}
		if reqErr := <-stuck; reqErr == nil {
			t.Fatal("the stuck request completed; want its connection closed")
		}
	})
}

func TestServeSignalsClosesOnASignalAfterTheContextStartedTheDrain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := startSignals(t, 10*time.Second)

		s.cancel()
		synctest.Wait()

		if got := s.ln.Status(t, "/readyz"); got != http.StatusServiceUnavailable {
			t.Fatalf("readyz after cancellation = %d, want 503", got)
		}

		s.signal()

		err, done := s.returned()
		if !done || err != nil {
			t.Fatalf("serveSignals after the signal = (%v, returned %t), want an immediate nil", err, done)
		}
	})
}
