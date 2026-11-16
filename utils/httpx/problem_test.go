package httpx_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/dfryer1193/mjolnir/utils/httpx"
)

func TestNewProblemRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	tests := []struct {
		name   string
		status int
		code   string
		cause  error
	}{
		{name: "2xx status", status: http.StatusOK, code: "ok", cause: nil},
		{name: "3xx status", status: http.StatusFound, code: "moved", cause: nil},
		{name: "status above 5xx", status: 600, code: "odd", cause: cause},
		{name: "zero status", status: 0, code: "zero", cause: cause},
		{name: "empty code", status: http.StatusBadRequest, code: "", cause: nil},
		{name: "5xx without cause", status: http.StatusServiceUnavailable, code: "down", cause: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := httpx.NewProblem(tt.status, tt.code, "detail", tt.cause)
			if err == nil {
				t.Fatalf("NewProblem(%d, %q) = %+v, want an error", tt.status, tt.code, p)
			}
			if p != nil {
				t.Errorf("NewProblem returned a problem alongside the error: %+v", p)
			}
		})
	}
}

func TestNewProblemBuildsProblem(t *testing.T) {
	t.Parallel()
	cause := errors.New("pool exhausted")

	p, err := httpx.NewProblem(http.StatusServiceUnavailable, "db_unavailable", "try later", cause)
	if err != nil {
		t.Fatalf("NewProblem: %v", err)
	}
	if p.Status != http.StatusServiceUnavailable || p.Code != "db_unavailable" || p.Detail != "try later" {
		t.Errorf("got %+v", p)
	}
	if p.Title != "Service Unavailable" {
		t.Errorf("Title = %q, want %q", p.Title, "Service Unavailable")
	}
	if !errors.Is(p, cause) {
		t.Error("errors.Is(problem, cause) = false; Unwrap must expose the cause")
	}

	q, err := httpx.NewProblem(http.StatusNotFound, "no_widget", "no such widget", nil)
	if err != nil {
		t.Fatalf("NewProblem 4xx without cause: %v", err)
	}
	if q.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", q.Unwrap())
	}
}

func TestClientHelpers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		build  func(code, detail string) *httpx.Problem
		status int
	}{
		{name: "BadRequest", build: httpx.BadRequest, status: 400},
		{name: "Unauthorized", build: httpx.Unauthorized, status: 401},
		{name: "Forbidden", build: httpx.Forbidden, status: 403},
		{name: "NotFound", build: httpx.NotFound, status: 404},
		{name: "Conflict", build: httpx.Conflict, status: 409},
		{name: "UnprocessableEntity", build: httpx.UnprocessableEntity, status: 422},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := tt.build("custom", "public detail")
			if p.Status != tt.status || p.Code != "custom" || p.Detail != "public detail" {
				t.Errorf("got %+v", p)
			}
			if p.Title != http.StatusText(tt.status) {
				t.Errorf("Title = %q", p.Title)
			}
			if got := tt.build("", "d").Code; got != "" {
				t.Errorf("empty code became %q, want it passed through", got)
			}
		})
	}
}

func TestServerHelpers(t *testing.T) {
	t.Parallel()
	cause := errors.New("disk full")
	tests := []struct {
		name   string
		build  func(error) *httpx.Problem
		status int
		code   string
	}{
		{name: "Internal", build: httpx.Internal, status: 500, code: "internal_server_error"},
		{name: "ServiceUnavailable", build: httpx.ServiceUnavailable, status: 503, code: "service_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := tt.build(cause)
			if p.Status != tt.status || p.Code != tt.code {
				t.Errorf("got status %d code %q, want %d %q", p.Status, p.Code, tt.status, tt.code)
			}
			if !errors.Is(p, cause) {
				t.Error("cause not reachable through Unwrap")
			}
			if tt.build(nil).Unwrap() == nil {
				t.Error("a nil cause was kept; a 5xx must always carry one")
			}
		})
	}
}
