package httpx_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// get fetches url from srv and returns the status, content type, and body.
func get(t *testing.T, srv *httptest.Server) (int, string, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

func TestHandlerDoesNotConcatenateBodies(t *testing.T) {
	t.Parallel()
	writeThenFail := func(w http.ResponseWriter, _ *http.Request) *httpx.Problem {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "partial")
		return httpx.Internal(errors.New("stream broke"))
	}
	tests := []struct {
		name string
		wrap func(http.Handler, *syncBuffer) http.Handler
	}{
		{name: "bare", wrap: func(h http.Handler, _ *syncBuffer) http.Handler { return h }},
		{name: "behind the access logger", wrap: func(h http.Handler, logs *syncBuffer) http.Handler {
			return middleware.RequestLoggerWith(h, log.GetLoggerTo("http", logs))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logs := &syncBuffer{}
			h := httpx.HandlerWithLogger(writeThenFail, log.GetLoggerTo("httpx", logs))
			srv := httptest.NewServer(tt.wrap(h, logs))
			t.Cleanup(srv.Close)

			status, ct, body := get(t, srv)
			if status != http.StatusOK || ct != "text/plain" || body != "partial" {
				t.Errorf("got %d %q %q, want 200 text/plain %q", status, ct, body, "partial")
			}
			if !strings.Contains(logs.String(), "stream broke") {
				t.Errorf("the discarded server problem was not logged:\n%s", logs)
			}
		})
	}
}

func TestHandlerTreatsHeaderOnlyResponseAsStarted(t *testing.T) {
	t.Parallel()
	h := httpx.HandlerWithLogger(func(w http.ResponseWriter, _ *http.Request) *httpx.Problem {
		w.WriteHeader(http.StatusAccepted)
		return httpx.BadRequest("late", "too late")
	}, log.GetLoggerTo("httpx", io.Discard))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	status, _, body := get(t, srv)
	if status != http.StatusAccepted || body != "" {
		t.Errorf("got %d %q, want 202 with an empty body", status, body)
	}
}

func TestWriteProblemAfterResponseStarted(t *testing.T) {
	t.Parallel()
	errs := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := middleware.NewResponseWriter(w, r.ProtoMajor)
		_, _ = io.WriteString(rw, "partial")
		errs <- httpx.WriteProblem(rw, r, httpx.Conflict("late", "too late"))
	}))
	t.Cleanup(srv.Close)

	status, _, body := get(t, srv)
	if err := <-errs; !errors.Is(err, httpx.ErrResponseStarted) {
		t.Errorf("WriteProblem error = %v, want ErrResponseStarted", err)
	}
	if status != http.StatusOK || body != "partial" {
		t.Errorf("got %d %q, want 200 %q", status, body, "partial")
	}
}
