package middleware_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/log"
)

func TestRequestLogger(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		handlerFunc  func(w http.ResponseWriter, r *http.Request)
		expectedCode int
		expectedLogs []string
	}{
		{
			name:   "successful request",
			method: http.MethodGet,
			path:   "/test",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			expectedCode: http.StatusOK,
			expectedLogs: []string{
				"request completed",
				`"method":"GET"`,
				`"path":"/test"`,
				`"status":200`,
			},
		},
		{
			name:   "error request",
			method: http.MethodPost,
			path:   "/error",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			},
			expectedCode: http.StatusBadRequest,
			expectedLogs: []string{
				"request completed",
				`"method":"POST"`,
				`"path":"/error"`,
				`"status":400`,
			},
		},
		{
			name:         "nothing written",
			method:       http.MethodGet,
			path:         "/empty",
			handlerFunc:  func(http.ResponseWriter, *http.Request) {},
			expectedCode: http.StatusOK,
			expectedLogs: []string{
				"request completed",
				`"status":200`, // what net/http sends, not "none yet"
			},
		},
		{
			name:   "no explicit status code",
			method: http.MethodGet,
			path:   "/implicit",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("test"))
			},
			expectedCode: http.StatusOK,
			expectedLogs: []string{
				"request completed",
				`"status":200`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Capture this subtest's output; the injected logger is isolated
			// from the process-wide sinks, so subtests can run concurrently.
			var buf bytes.Buffer
			handler := middleware.RequestLoggerWith(http.HandlerFunc(tt.handlerFunc), log.GetLoggerTo("http", &buf))

			// Create test request
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			// Execute request
			handler.ServeHTTP(rr, req)

			// Check status code
			if rr.Code != tt.expectedCode {
				t.Errorf("handler returned wrong status code: got %v want %v",
					tt.expectedCode, rr.Code)
			}

			// Check logs
			logStr := buf.String()
			for _, expectedLog := range tt.expectedLogs {
				if !strings.Contains(logStr, expectedLog) {
					t.Errorf("log doesn't contain %q\nLog: %s", expectedLog, logStr)
				}
			}

			// Verify log contains latency
			if !strings.Contains(logStr, "latency") {
				t.Error("log doesn't contain latency information")
			}

			// Verify log contains remote_addr
			if !strings.Contains(logStr, "remote_addr") {
				t.Error("log doesn't contain remote_addr information")
			}
		})
	}
}

// TestRequestLoggerCorrelatesRequestID proves the id stamped by the RequestID
// middleware reaches the request log line.
func TestRequestLoggerCorrelatesRequestID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	handler := middleware.RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), log.GetLoggerTo("http", &buf))
	// RequestID wraps RequestLogger so the id is on the context when the
	// request is logged, as it is on the real router.
	handler = middleware.RequestID(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "0f8fad5b-d9cb-469f-a165-70867728950e")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	logStr := buf.String()
	if !strings.Contains(logStr, `"request_id":"0f8fad5b-d9cb-469f-a165-70867728950e"`) {
		t.Errorf("request log line missing correlated request_id:\n%s", logStr)
	}
}

// TestRequestLoggerOmitsAbsentRequestID: without the RequestID middleware the
// field is simply absent, not empty.
func TestRequestLoggerOmitsAbsentRequestID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	handler := middleware.RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), log.GetLoggerTo("http", &buf))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if strings.Contains(buf.String(), "request_id") {
		t.Errorf("request_id should be absent, not empty:\n%s", buf.String())
	}
}

// TestRequestLoggerConcurrentIsolation: the underlying logger is shared across
// requests, but the request id is per-request. Reassigning the captured logger
// would race and let one request's id leak into another's line.
func TestRequestLoggerConcurrentIsolation(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	inner := middleware.RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), log.GetLoggerTo("http", &buf))
	withID := middleware.RequestID(inner)

	const requests = 32
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() {
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)

			req := httptest.NewRequest(http.MethodGet, "/with/"+id, nil)
			req.Header.Set("X-Request-ID", id)
			withID.ServeHTTP(httptest.NewRecorder(), req)

			// No RequestID middleware: this line must not inherit an id.
			plain := httptest.NewRequest(http.MethodGet, "/without/"+id, nil)
			inner.ServeHTTP(httptest.NewRecorder(), plain)
		})
	}
	wg.Wait()

	for line := range strings.SplitSeq(buf.String(), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue // text-stream line
		}
		var entry struct {
			Path      string `json:"path"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("json parse: %v\n%s", err, line)
		}
		switch {
		case strings.HasPrefix(entry.Path, "/with/"):
			if want := strings.TrimPrefix(entry.Path, "/with/"); entry.RequestID != want {
				t.Errorf("path %s: got request_id %q, want %q", entry.Path, entry.RequestID, want)
			}
		case strings.HasPrefix(entry.Path, "/without/"):
			if entry.RequestID != "" {
				t.Errorf("path %s inherited request_id %q", entry.Path, entry.RequestID)
			}
		}
	}
}

// accessLine serves req through RequestLoggerWith(h) and returns the fields of
// the single JSON access line. A panic from h is swallowed after the logger
// has run, as an outer recoverer would.
func accessLine(t *testing.T, h http.Handler, req *http.Request) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logged := middleware.RequestLoggerWith(h, log.GetLoggerTo("http", &buf))
	func() {
		defer func() { _ = recover() }()
		logged.ServeHTTP(httptest.NewRecorder(), req)
	}()

	var lines []map[string]any
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue // text-stream line
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("json parse: %v\n%s", err, line)
		}
		lines = append(lines, entry)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d access lines, want 1:\n%s", len(lines), buf.String())
	}
	return lines[0]
}

// TestRequestLoggerLogsOnPanic: the access line is written even when the
// handler panics, with the status that had been sent when it did.
func TestRequestLoggerLogsOnPanic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		status  float64
		bytes   float64
	}{
		{
			name:    "before any write",
			handler: func(http.ResponseWriter, *http.Request) { panic("boom") },
			status:  0, // nothing was sent
			bytes:   0,
		},
		{
			name: "after a write",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("partial"))
				panic("boom")
			},
			status: http.StatusAccepted,
			bytes:  float64(len("partial")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			line := accessLine(t, tt.handler, httptest.NewRequest(http.MethodGet, "/panic", nil))
			if line["status"] != tt.status {
				t.Errorf("status = %v, want %v", line["status"], tt.status)
			}
			if line["bytes"] != tt.bytes {
				t.Errorf("bytes = %v, want %v", line["bytes"], tt.bytes)
			}
			if line["path"] != "/panic" {
				t.Errorf("path = %v, want /panic", line["path"])
			}
		})
	}
}

// TestRequestLoggerLogsBytes: the line carries the body size the handler
// wrote.
func TestRequestLoggerLogsBytes(t *testing.T) {
	t.Parallel()
	line := accessLine(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello, "))
		_, _ = w.Write([]byte("world"))
	}), httptest.NewRequest(http.MethodGet, "/", nil))
	if line["bytes"] != float64(12) {
		t.Errorf("bytes = %v, want 12", line["bytes"])
	}
}

// TestRequestLoggerClientIP: client_ip is the address a ClientIPFrom*
// middleware resolved, and is absent when none ran.
func TestRequestLoggerClientIP(t *testing.T) {
	t.Parallel()
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	t.Run("resolved", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.7:4711"
		// The resolver sits outside the logger, as it will on the router.
		var buf bytes.Buffer
		h := chimiddleware.ClientIPFromRemoteAddr(middleware.RequestLoggerWith(ok, log.GetLoggerTo("http", &buf)))
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !strings.Contains(buf.String(), `"client_ip":"203.0.113.7"`) {
			t.Errorf("client_ip missing or wrong:\n%s", buf.String())
		}
	})
	t.Run("unresolved", func(t *testing.T) {
		t.Parallel()
		line := accessLine(t, ok, httptest.NewRequest(http.MethodGet, "/", nil))
		if _, present := line["client_ip"]; present {
			t.Errorf("client_ip should be absent, got %v", line["client_ip"])
		}
	})
}

func BenchmarkRequestLogger(b *testing.B) {
	handler := middleware.RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), log.GetLoggerTo("http", io.Discard))

	req := httptest.NewRequest(http.MethodGet, "/benchmark", nil)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for range b.N {
		handler.ServeHTTP(rr, req)
	}
}
