package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/utils/httpx"
)

func decodeRequest(t *testing.T, codec httpx.Codec, contentType, body string) *httpx.Problem {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return codec.Decode(httptest.NewRecorder(), req, &struct {
		Name string `json:"name"`
	}{})
}

func TestCodecDecodeAcceptsJSONTypes(t *testing.T) {
	t.Parallel()

	tests := []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/vnd.api+json",
		"application/problem+json; charset=UTF-8",
	}
	for _, contentType := range tests {
		t.Run(contentType, func(t *testing.T) {
			t.Parallel()
			if p := decodeRequest(t, httpx.Codec{}, contentType, `{"name":"ok"}`); p != nil {
				t.Errorf("unexpected problem: %+v", p)
			}
		})
	}
}

func TestCodecDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{
			name:       "missing content type",
			body:       `{}`,
			wantStatus: http.StatusUnsupportedMediaType,
			wantCode:   "unsupported_media_type",
		},
		{
			name:        "wrong content type",
			contentType: "text/plain",
			body:        `{}`,
			wantStatus:  http.StatusUnsupportedMediaType,
			wantCode:    "unsupported_media_type",
		},
		{
			name:        "empty body",
			contentType: "application/json",
			body:        "",
			wantStatus:  http.StatusBadRequest,
			wantCode:    "empty_body",
		},
		{
			name:        "malformed JSON",
			contentType: "application/json",
			body:        `{"name":`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_json",
		},
		{
			name:        "trailing JSON value",
			contentType: "application/json",
			body:        `{"name":"a"} {"name":"b"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := decodeRequest(t, httpx.Codec{}, tt.contentType, tt.body)
			if p == nil {
				t.Fatal("expected problem")
			}
			if p.Status != tt.wantStatus || p.Code != tt.wantCode {
				t.Errorf("problem = %+v, want status %d code %q", p, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestCodecDecodeEnforcesLimit(t *testing.T) {
	t.Parallel()

	body := `{"name":"` + strings.Repeat("a", 64) + `"}`
	codec := httpx.NewCodec(int64(len(body) - 1))
	p := decodeRequest(t, codec, "application/json", body)
	if p == nil {
		t.Fatal("expected problem")
	}
	if p.Status != http.StatusRequestEntityTooLarge || p.Code != "request_too_large" {
		t.Errorf("problem = %+v, want 413 request_too_large", p)
	}

	exact := httpx.NewCodec(int64(len(body)))
	if exactProblem := decodeRequest(t, exact, "application/json", body); exactProblem != nil {
		t.Errorf("body at the limit was rejected: %+v", exactProblem)
	}
}

func TestCodecDecodeDefaultLimit(t *testing.T) {
	t.Parallel()

	if httpx.DefaultMaxBodyBytes != 1<<20 {
		t.Fatalf("DefaultMaxBodyBytes = %d", httpx.DefaultMaxBodyBytes)
	}
	if (httpx.Codec{}).MaxBodyBytes != 0 {
		t.Error("zero Codec should rely on the default at use time")
	}
}

func TestCodecRespond(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	codec := httpx.Codec{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if p := codec.Respond(rr, req, http.StatusCreated, map[string]string{"ok": "yes"}); p != nil {
		t.Fatalf("unexpected problem: %+v", p)
	}
	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ok"] != "yes" {
		t.Errorf("body = %+v", body)
	}
}

func TestCodecRespondNoContentHasNoBody(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	if p := (httpx.Codec{}).Respond(rr, req, http.StatusNoContent, nil); p != nil {
		t.Fatalf("unexpected problem: %+v", p)
	}
	if rr.Code != http.StatusNoContent || rr.Body.Len() != 0 {
		t.Errorf("status = %d body = %q", rr.Code, rr.Body.String())
	}
}

func TestCodecRespondMarshalFailure(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	p := (httpx.Codec{}).Respond(rr, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, func() {})
	if p == nil {
		t.Fatal("expected problem")
	}
	if p.Status != http.StatusInternalServerError || p.Code != "encode_failed" {
		t.Errorf("problem = %+v", p)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("partial body written: %q", rr.Body.String())
	}
}

func TestCodecRespondAfterStart(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	rw := middleware.NewResponseWriter(rr, 1)
	_, _ = rw.Write([]byte("ok"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	p := (httpx.Codec{}).Respond(rw, req, http.StatusOK, map[string]string{"late": "yes"})
	if p == nil || p.Code != "response_started" {
		t.Fatalf("problem = %+v, want response_started", p)
	}
	if rr.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rr.Body.String(), "ok")
	}
}
