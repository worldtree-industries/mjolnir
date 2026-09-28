package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRespondJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		status        int
		payload       any
		expectedBody  string
		setup         func() *http.Request
		expectedCode  int
		expectError   bool
		errorContains string
	}{
		{
			name:         "valid JSON response",
			status:       http.StatusOK,
			payload:      map[string]string{"message": "success"},
			expectedBody: `{"message":"success"}`,
			setup: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/", nil)
			},
			expectedCode:  http.StatusOK,
			expectError:   false,
			errorContains: "",
		},
		{
			name:         "invalid payload",
			status:       http.StatusInternalServerError,
			payload:      func() {},
			expectedBody: "",
			setup: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", nil)
			},
			expectedCode:  http.StatusInternalServerError,
			expectError:   true,
			errorContains: "failed to marshal JSON",
		},
		{
			name:         "custom status code",
			status:       http.StatusAccepted,
			payload:      map[string]string{"status": "accepted"},
			expectedBody: `{"status":"accepted"}`,
			setup: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", nil)
			},
			expectedCode:  http.StatusAccepted,
			expectError:   false,
			errorContains: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := test.setup()
			recorder := httptest.NewRecorder()

			err := RespondJSON(recorder, request, test.status, test.payload)

			if test.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				} else if test.errorContains != "" && !contains(err.Error(), test.errorContains) {
					t.Errorf("expected error containing %q, got %q", test.errorContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result := recorder.Result()
			defer result.Body.Close()

			if result.StatusCode != test.expectedCode {
				t.Errorf("expected status %d, got %d", test.expectedCode, result.StatusCode)
			}

			body, err := io.ReadAll(result.Body)
			if err != nil {
				t.Fatalf("unexpected error while reading body: %v", err)
			}

			if !json.Valid([]byte(test.expectedBody)) && string(body) != test.expectedBody {
				t.Errorf("expected body %q, got %q", test.expectedBody, body)
			} else if json.Valid([]byte(test.expectedBody)) {
				expBody := make(map[string]any)
				gotBody := make(map[string]any)
				if err := json.Unmarshal([]byte(test.expectedBody), &expBody); err != nil {
					t.Fatalf("failed to parse expected body: %v", err)
				}
				if err := json.Unmarshal(body, &gotBody); err != nil {
					t.Fatalf("failed to parse body: %v", err)
				}
				if !equals(expBody, gotBody) {
					t.Errorf("expected body %v, got %v", expBody, gotBody)
				}
			}
		})
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name          string
		contentType   string
		body          string
		target        any
		expected      any
		expectedBody  string
		expectError   bool
		errorContains string
	}{
		{
			name:         "valid JSON",
			contentType:  "application/json",
			body:         `{"name":"test","value":123}`,
			target:       &testStruct{},
			expected:     &testStruct{Name: "test", Value: 123},
			expectedBody: `{"name":"test","value":123}`,
			expectError:  false,
		},
		{
			name:          "invalid content type",
			contentType:   "text/plain",
			body:          `{"name":"test","value":123}`,
			target:        &testStruct{},
			expectError:   true,
			errorContains: "is not supported",
		},
		{
			name:          "invalid JSON",
			contentType:   "application/json",
			body:          `{"name":"test",,}`,
			target:        &testStruct{},
			expectError:   true,
			errorContains: "failed to decode JSON",
		},
		{
			name:          "empty body",
			contentType:   "application/json",
			body:          "",
			target:        &testStruct{},
			expectError:   true,
			errorContains: "failed to decode JSON: unexpected end of JSON input",
		},
		{
			name:         "null JSON",
			contentType:  "application/json",
			body:         `null`,
			target:       &testStruct{},
			expected:     &testStruct{},
			expectedBody: `null`,
			expectError:  false,
		},
		{
			name:          "missing content-type header",
			contentType:   "",
			body:          `{"name":"test"}`,
			target:        &testStruct{},
			expectError:   true,
			errorContains: "is not supported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Create request with test body
			req := httptest.NewRequest(
				http.MethodPost,
				"/test",
				bytes.NewBufferString(test.body),
			)
			req.Header.Set("Content-Type", test.contentType)

			// Execute DecodeJSON
			bodyBytes, err := DecodeJSON(req, test.target)

			// Check error conditions
			if test.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				} else if test.errorContains != "" && !strings.Contains(err.Error(), test.errorContains) {
					t.Errorf("expected error containing %q, got %q", test.errorContains, err.Error())
				}
				return
			}

			// Check for unexpected errors
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify returned body bytes match the input
			if test.expectedBody != "" && string(bodyBytes) != test.expectedBody {
				t.Errorf("expected body bytes %q, got %q", test.expectedBody, string(bodyBytes))
			}

			// Compare result with expected value
			if test.expected != nil {
				expected := test.expected.(*testStruct)
				got := test.target.(*testStruct)

				if expected.Name != got.Name || expected.Value != got.Value {
					t.Errorf("expected %+v, got %+v", expected, got)
				}
			}
		})
	}
}

type closeTracker struct {
	io.Reader

	closed bool
}

func (c *closeTracker) Close() error {
	c.closed = true
	return nil
}

func TestDecodeJSONClosesBody(t *testing.T) {
	t.Parallel()
	body := &closeTracker{Reader: strings.NewReader(`{"name":"test"}`)}
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", "application/json")

	var v struct{ Name string }
	_, err := DecodeJSON(req, &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !body.closed {
		t.Error("request body was not closed")
	}
}

func contains(s, substr string) bool {
	return s != "" && substr != "" && s != substr && len(s) > len(substr) && s[0:len(substr)] == substr
}

func equals(expected, actual map[string]any) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	for key := range actual {
		if _, exists := expected[key]; !exists {
			return false
		}
	}
	return true
}
