package httpx_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

func TestNewProblemValidation(t *testing.T) {
	t.Parallel()
	cause := errors.New("database password rejected")

	tests := []struct {
		name    string
		status  int
		code    string
		cause   error
		wantErr bool
	}{
		{name: "valid 400", status: http.StatusBadRequest, code: "bad_request"},
		{name: "valid 503", status: http.StatusServiceUnavailable, code: "unavailable", cause: cause},
		{name: "status below 400", status: 399, code: "bad", wantErr: true},
		{name: "status above 599", status: 600, code: "bad", cause: cause, wantErr: true},
		{name: "empty code", status: http.StatusBadRequest, wantErr: true},
		{name: "5xx without cause", status: http.StatusInternalServerError, code: "internal", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := httpx.NewProblem(tt.status, tt.code, "", tt.cause)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", p)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Status != tt.status || p.Code != tt.code {
				t.Errorf("problem = %+v", p)
			}
			if tt.cause != nil && !errors.Is(p, tt.cause) {
				t.Errorf("Unwrap chain does not reach cause")
			}
		})
	}
}

func TestHandlerWritesProblemAndInstance(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()

	handler := middleware.RequestID(httpx.HandlerWithLogger(
		func(http.ResponseWriter, *http.Request) *httpx.Problem {
			return httpx.BadRequest("validation_failed", "name is required")
		},
		log.GetLoggerTo("http", io.Discard),
	))

	req := httptest.NewRequest(http.MethodPost, "/users", nil)
	req.Header.Set("X-Request-ID", id)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q", got)
	}

	var got httpx.Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != http.StatusBadRequest || got.Code != "validation_failed" {
		t.Errorf("problem = %+v", got)
	}
	if got.Detail != "name is required" {
		t.Errorf("detail = %q", got.Detail)
	}
	if got.Instance != "urn:uuid:"+id {
		t.Errorf("instance = %q, want urn:uuid:%s", got.Instance, id)
	}
	if got.Type != "about:blank" {
		t.Errorf("type = %q", got.Type)
	}
}

func TestHandler4xxDoesNotLeakCause(t *testing.T) {
	t.Parallel()
	cause := errors.New(`pq: password authentication failed for user "app"`)

	handler := httpx.HandlerWithLogger(
		func(http.ResponseWriter, *http.Request) *httpx.Problem {
			p, err := httpx.NewProblem(http.StatusBadRequest, "bad_request", "public detail", cause)
			if err != nil {
				t.Fatal(err)
			}
			return p
		},
		log.GetLoggerTo("http", io.Discard),
	)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if strings.Contains(rr.Body.String(), "password") {
		t.Errorf("cause leaked to client: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "public detail") {
		t.Errorf("public detail missing: %s", rr.Body.String())
	}
}

func TestHandler5xxPreservesStatusAndMasksDetail(t *testing.T) {
	t.Parallel()
	cause := errors.New("database password rejected")

	handler := httpx.HandlerWithLogger(
		func(http.ResponseWriter, *http.Request) *httpx.Problem {
			return httpx.ServiceUnavailable(cause)
		},
		log.GetLoggerTo("http", io.Discard),
	)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}

	var got httpx.Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != http.StatusServiceUnavailable {
		t.Errorf("body status = %d, want 503", got.Status)
	}
	if got.Code != "service_unavailable" {
		t.Errorf("code = %q", got.Code)
	}
	if got.Detail != http.StatusText(http.StatusServiceUnavailable) {
		t.Errorf("detail = %q", got.Detail)
	}
	if strings.Contains(rr.Body.String(), "password") {
		t.Errorf("cause leaked to client: %s", rr.Body.String())
	}
}

func TestHandlerDoesNotConcatenateAfterWrite(t *testing.T) {
	t.Parallel()

	inner := httpx.HandlerWithLogger(
		func(w http.ResponseWriter, _ *http.Request) *httpx.Problem {
			_, _ = w.Write([]byte("ok"))
			return httpx.BadRequest("late_error", "too late")
		},
		log.GetLoggerTo("http", io.Discard),
	)

	rr := httptest.NewRecorder()
	inner.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rr.Body.String(), "ok")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestWriteProblemAfterStartReturnsSentinel(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	rw := middleware.NewResponseWriter(rr, 1)
	_, _ = rw.Write([]byte("ok"))

	err := httpx.WriteProblem(rw, httptest.NewRequest(http.MethodGet, "/", nil),
		httpx.BadRequest("late", "late"))
	if !errors.Is(err, httpx.ErrResponseStarted) {
		t.Fatalf("err = %v, want ErrResponseStarted", err)
	}
}

func TestHandlerNilReturn(t *testing.T) {
	t.Parallel()

	handler := httpx.HandlerWithLogger(
		func(http.ResponseWriter, *http.Request) *httpx.Problem { return nil },
		log.GetLoggerTo("http", io.Discard),
	)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rr.Body.String())
	}
}

func TestHandlerInvalidProblemFallsBackTo500(t *testing.T) {
	t.Parallel()

	handler := httpx.HandlerWithLogger(
		func(http.ResponseWriter, *http.Request) *httpx.Problem {
			return &httpx.Problem{Status: http.StatusBadRequest}
		},
		log.GetLoggerTo("http", io.Discard),
	)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}
