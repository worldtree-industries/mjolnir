package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
			// Capture logs
			var buf bytes.Buffer
			log.Logger = zerolog.New(&buf)

			// Create test handler
			handler := RequestLogger(http.HandlerFunc(tt.handlerFunc))

			// Create test request
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			// Execute request
			handler.ServeHTTP(rr, req)

			// Check status code
			if rr.Code != tt.expectedCode {
				t.Errorf("handler returned wrong status code: got %v want %v",
					rr.Code, tt.expectedCode)
			}

			// Check logs
			logStr := buf.String()
			for _, expectedLog := range tt.expectedLogs {
				if !strings.Contains(logStr, expectedLog) {
					t.Errorf("log doesn't contain %q\nLog: %s", expectedLog, logStr)
				}
			}

			// Verify log contains latency
			if !strings.Contains(logStr, `"latency"`) {
				t.Error("log doesn't contain latency information")
			}

			// Verify log contains remote_addr
			if !strings.Contains(logStr, `"remote_addr"`) {
				t.Error("log doesn't contain remote_addr information")
			}
		})
	}
}

func TestResponseWriter(t *testing.T) {
	tests := []struct {
		name         string
		writeHeader  bool
		writeBody    bool
		expectedCode int
	}{
		{
			name:         "explicit header",
			writeHeader:  true,
			writeBody:    true,
			expectedCode: http.StatusCreated,
		},
		{
			name:         "implicit header",
			writeHeader:  false,
			writeBody:    true,
			expectedCode: http.StatusOK,
		},
		{
			name:         "header only",
			writeHeader:  true,
			writeBody:    false,
			expectedCode: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := httptest.NewRecorder()
			rw := &responseWriter{w: base, status: http.StatusOK}

			if tt.writeHeader {
				statusCode := http.StatusCreated
				if !tt.writeBody {
					statusCode = http.StatusNoContent
				}
				rw.WriteHeader(statusCode)

				// Test multiple WriteHeader calls
				rw.WriteHeader(http.StatusOK) // Should be ignored

				if rw.status != statusCode {
					t.Errorf("expected status %d, got %d", statusCode, rw.status)
				}
			}

			if tt.writeBody {
				n, err := rw.Write([]byte("test"))
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if n != 4 {
					t.Errorf("expected to write 4 bytes, wrote %d", n)
				}
			}

			if rw.status != tt.expectedCode {
				t.Errorf("expected status %d, got %d", tt.expectedCode, rw.status)
			}

			// Test Header() method
			rw.Header().Set("X-Test", "test")
			if base.Header().Get("X-Test") != "test" {
				t.Error("Header() method not working correctly")
			}
		})
	}
}

func BenchmarkRequestLogger(b *testing.B) {
	// Disable logging for benchmark
	log.Logger = zerolog.New(nil)

	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/benchmark", nil)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(rr, req)
	}
}
