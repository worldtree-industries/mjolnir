package httpx_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

const (
	problemContentType = "application/problem+json"
	requestID          = "6f1c2b9e-3d4a-4f5b-8c7d-9e0f1a2b3c4d"
)

// syncBuffer is a log destination safe for the server goroutines of a test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// serve runs fn through HandlerWithLogger behind RequestID with a fixed
// inbound request id, and returns the recorded response and the log output.
func serve(
	t *testing.T,
	fn func(http.ResponseWriter, *http.Request) error,
) (*httptest.ResponseRecorder, string) {
	t.Helper()
	logs := &syncBuffer{}
	h := middleware.RequestID(httpx.HandlerWithLogger(fn, log.GetLoggerTo("httpx", logs)))

	req := httptest.NewRequest(http.MethodGet, "/widgets/7", nil)
	req.Header.Set("X-Request-ID", requestID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, logs.String()
}

// decodeProblem parses a problem document into its raw members, so a test can
// see exactly which members were sent.
func decodeProblem(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("body %q is not JSON: %v", body, err)
	}
	return doc
}

func TestHandlerWritesClientProblemWithoutCause(t *testing.T) {
	t.Parallel()
	secret := fmt.Errorf("query users: password authentication failed for user %q", "admin")

	rec, _ := serve(t, func(http.ResponseWriter, *http.Request) error {
		p, err := httpx.NewProblem(http.StatusConflict, "widget_exists", "a widget with that name exists", secret)
		if err != nil {
			t.Fatalf("NewProblem: %v", err)
		}
		return p
	})

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if ct := rec.Header().Get("Content-Type"); ct != problemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problemContentType)
	}
	if body := rec.Body.String(); strings.Contains(body, "password") {
		t.Errorf("4xx body leaks the cause: %s", body)
	}
	want := map[string]any{
		"type":     "about:blank",
		"title":    "Conflict",
		"status":   float64(http.StatusConflict),
		"detail":   "a widget with that name exists",
		"instance": "urn:uuid:" + requestID,
		"code":     "widget_exists",
	}
	got := decodeProblem(t, rec.Body.Bytes())
	if len(got) != len(want) {
		t.Errorf("members = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("member %q = %v, want %v", k, got[k], v)
		}
	}
}

func TestHelperSendsAnEmptyCodeUnchanged(t *testing.T) {
	t.Parallel()
	rec, _ := serve(t, func(http.ResponseWriter, *http.Request) error {
		return httpx.BadRequest("", "x")
	})

	got := decodeProblem(t, rec.Body.Bytes())
	if code, ok := got["code"]; !ok || code != "" {
		t.Errorf("code member = %v (present %v), want \"\"", code, ok)
	}
}

func TestHandlerKeepsServerStatusAndMasksDetail(t *testing.T) {
	t.Parallel()
	cause := fmt.Errorf("dial postgres://admin:hunter2@db: %w", errors.New("connection refused"))

	rec, logs := serve(t, func(http.ResponseWriter, *http.Request) error {
		p, err := httpx.NewProblem(http.StatusServiceUnavailable, "db_unavailable", "primary pool is draining", cause)
		if err != nil {
			t.Fatalf("NewProblem: %v", err)
		}
		return p
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	got := decodeProblem(t, rec.Body.Bytes())
	if got["status"] != float64(http.StatusServiceUnavailable) || got["code"] != "db_unavailable" {
		t.Errorf("status/code members = %v/%v, want 503/db_unavailable", got["status"], got["code"])
	}
	if got["detail"] != "The server could not complete the request." {
		t.Errorf("detail = %q, want the fixed generic detail", got["detail"])
	}
	if body := rec.Body.String(); strings.Contains(body, "hunter2") || strings.Contains(body, "draining") {
		t.Errorf("5xx body leaks server-only text: %s", body)
	}

	for _, want := range []string{
		`"level":"ERROR"`,
		`"method":"GET"`,
		`"path":"/widgets/7"`,
		`"request_id":"` + requestID + `"`,
		`"status":503`,
		`"code":"db_unavailable"`,
		"primary pool is draining",
		"connection refused",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("log is missing %s:\n%s", want, logs)
		}
	}
}

func TestHandlerDoesNotLogClientProblems(t *testing.T) {
	t.Parallel()
	_, logs := serve(t, func(http.ResponseWriter, *http.Request) error {
		return httpx.NotFound("no_widget", "no such widget")
	})
	if logs != "" {
		t.Errorf("a 4xx produced log output:\n%s", logs)
	}
}

func TestProblemOmitsInstanceWithoutRequestID(t *testing.T) {
	t.Parallel()
	h := httpx.HandlerWithLogger(func(http.ResponseWriter, *http.Request) error {
		return httpx.BadRequest("bad", "bad input")
	}, log.GetLoggerTo("httpx", &syncBuffer{}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := decodeProblem(t, rec.Body.Bytes()); got["instance"] != nil {
		t.Errorf("instance = %v, want it absent", got["instance"])
	}
}

func TestHandlerLogsEachRequestOnItsOwn(t *testing.T) {
	t.Parallel()
	logs := &syncBuffer{}
	h := httpx.HandlerWithLogger(func(http.ResponseWriter, *http.Request) error {
		return httpx.Internal(errors.New("boom"))
	}, log.GetLoggerTo("httpx", logs))

	for range 2 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	for line := range strings.Lines(logs.String()) {
		if n := strings.Count(line, `"method":`); n > 1 {
			t.Fatalf("a log line carries %d method fields; request fields leaked across requests:\n%s", n, line)
		}
	}
}
