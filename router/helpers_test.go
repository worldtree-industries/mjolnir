package router_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// logSink collects log lines written from server goroutines.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.Write(p)
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.String()
}

// logger is an isolated "http" logger writing to s.
func (s *logSink) logger() *slog.Logger {
	return log.GetLoggerTo("http", s)
}

// waitFor waits for one line containing every part. Access lines are written
// after the response is sent, so a test must wait for them.
func (s *logSink) waitFor(t *testing.T, parts ...string) string {
	t.Helper()

	deadline := time.Now().Add(probeTimeout)

	for {
		for line := range strings.Lines(s.String()) {
			if containsAll(line, parts) {
				return line
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("no log line contains all of %q; got:\n%s", parts, s.String())
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// field returns the unquoted value of key in a text log line, or "".
func field(line, key string) string {
	for f := range strings.FieldsSeq(line) {
		if v, ok := strings.CutPrefix(f, key+"="); ok {
			return v
		}
	}

	return ""
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}

	return true
}

// alwaysHealthy is a health config with nothing to check.
func alwaysHealthy() health.Config {
	return health.Config{Started: health.AlwaysStarted, Ready: health.AlwaysReady}
}

type response struct {
	status int
	header http.Header
	body   string
}

// do sends a request with method to url and reads the whole response.
func do(t *testing.T, method, url string, header http.Header) response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	maps.Copy(req.Header, header)

	client := &http.Client{Timeout: probeTimeout}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, url, err)
	}

	return response{status: resp.StatusCode, header: resp.Header, body: string(body)}
}

func get(t *testing.T, url string) response {
	t.Helper()

	return do(t, http.MethodGet, url, nil)
}

func mustNew(t *testing.T, cfg health.Config) *router.Router {
	t.Helper()

	return mustNewWith(t, router.Config{Health: cfg})
}

func mustNewWith(t *testing.T, cfg router.Config) *router.Router {
	t.Helper()

	r, err := router.New(cfg)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	return r
}

// probe performs a GET and asserts the probe contract: the
// expected status, an empty body, and no content type.
func probe(t *testing.T, handler http.Handler, path string, wantStatus int) {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	if rec.Code != wantStatus {
		t.Fatalf("GET %s: status = %d, want %d", path, rec.Code, wantStatus)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("GET %s: body = %q, want empty", path, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Fatalf("GET %s: Content-Type = %q, want unset", path, ct)
	}
}

// probeTimeout bounds test requests so a wedged server fails the test instead
// of hanging it.
const probeTimeout = 2 * time.Second
