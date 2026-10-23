package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

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
			name:   "no explicit status code",
			method: http.MethodGet,
			path:   "/implicit",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("test"))
			},
			expectedCode: http.StatusOK,
			expectedLogs: []string{
				"request completed",
				`"status":200`,
				`"bytes":4`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			handler := RequestLoggerWith(http.HandlerFunc(tt.handlerFunc), log.GetLoggerTo("http", &buf))

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedCode {
				t.Errorf("handler returned wrong status code: got %v want %v",
					tt.expectedCode, rr.Code)
			}

			logStr := buf.String()
			for _, expectedLog := range tt.expectedLogs {
				if !strings.Contains(logStr, expectedLog) {
					t.Errorf("log doesn't contain %q\nLog: %s", expectedLog, logStr)
				}
			}

			if !strings.Contains(logStr, "latency") {
				t.Error("log doesn't contain latency information")
			}
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
	id := uuid.NewString()

	handler := RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), log.GetLoggerTo("http", &buf))
	handler = RequestID(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", id)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	logStr := buf.String()
	if !strings.Contains(logStr, `"request_id":"`+id+`"`) {
		t.Errorf("request log line missing correlated request_id:\n%s", logStr)
	}
}

// TestRequestLoggerOmitsAbsentRequestID: without the RequestID middleware the
// field is simply absent, not empty.
func TestRequestLoggerOmitsAbsentRequestID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	handler := RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	inner := RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), log.GetLoggerTo("http", &buf))
	withID := RequestID(inner)

	const requests = 32
	var wg sync.WaitGroup
	for range requests {
		wg.Go(func() {
			id := uuid.NewString()

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

// TestRequestLoggerEmitsOnPanic proves the access line is deferred rather than
// following next.ServeHTTP. The recoverer lives outside this test.
func TestRequestLoggerEmitsOnPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	handler := RequestLoggerWith(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}), log.GetLoggerTo("http", &buf))
	recovered := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = recover() }()
		handler.ServeHTTP(w, r)
	})

	recovered.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))

	if !strings.Contains(buf.String(), "request completed") {
		t.Errorf("access line missing after panic:\n%s", buf.String())
	}
}

// TestResponseWriterPreservesCapabilities uses a real server because a
// ResponseRecorder cannot model Flusher, Hijacker, or ReaderFrom.
func TestResponseWriterPreservesCapabilities(t *testing.T) {
	t.Parallel()

	type capabilities struct {
		Flusher    bool `json:"flusher"`
		Hijacker   bool `json:"hijacker"`
		ReaderFrom bool `json:"reader_from"`
		Unwrap     bool `json:"unwrap"`
		FlushErr   bool `json:"flush_error"`
	}

	handler := RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, flusher := w.(http.Flusher)
		_, hijacker := w.(http.Hijacker)
		_, readerFrom := w.(io.ReaderFrom)
		unwrapper, unwrap := w.(interface{ Unwrap() http.ResponseWriter })

		_ = json.NewEncoder(w).Encode(capabilities{
			Flusher:    flusher,
			Hijacker:   hijacker,
			ReaderFrom: readerFrom,
			Unwrap:     unwrap && unwrapper.Unwrap() != nil,
			FlushErr:   http.NewResponseController(w).Flush() != nil,
		})
	}), log.GetLoggerTo("http", io.Discard))

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var got capabilities
	if decodeErr := json.NewDecoder(resp.Body).Decode(&got); decodeErr != nil {
		t.Fatalf("decode: %v", decodeErr)
	}

	if !got.Flusher || !got.Hijacker || !got.ReaderFrom || !got.Unwrap {
		t.Errorf("capabilities stripped: %+v", got)
	}
	if got.FlushErr {
		t.Error("ResponseController.Flush failed through the wrapper")
	}
}

func BenchmarkRequestLogger(b *testing.B) {
	handler := RequestLoggerWith(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), log.GetLoggerTo("http", io.Discard))

	req := httptest.NewRequest(http.MethodGet, "/benchmark", nil)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for range b.N {
		handler.ServeHTTP(rr, req)
	}
}
