package httpx_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

type widget struct {
	Name string `json:"name"`
}

func decodeRequest(contentType, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/widgets", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func TestDecodeAcceptsJSONMediaTypes(t *testing.T) {
	t.Parallel()
	for _, ct := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"Application/JSON; charset=UTF-8",
		"application/vnd.api+json",
		"application/merge-patch+json",
	} {
		t.Run(ct, func(t *testing.T) {
			t.Parallel()
			var got widget
			p := httpx.NewCodec(0).Decode(httptest.NewRecorder(), decodeRequest(ct, `{"name":"gear"}`), &got)
			if p != nil {
				t.Fatalf("Decode returned %v", p)
			}
			if got.Name != "gear" {
				t.Errorf("decoded %+v", got)
			}
		})
	}
}

func TestDecodeRejectsBadRequests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		code        string
	}{
		{name: "no content type", contentType: "", body: `{}`, status: 415, code: "unsupported_media_type"},
		{name: "text/plain", contentType: "text/plain", body: `{}`, status: 415, code: "unsupported_media_type"},
		{
			name:        "unparsable media type",
			contentType: "application/",
			body:        `{}`,
			status:      415,
			code:        "unsupported_media_type",
		},
		{
			name:        "non-UTF-8 charset",
			contentType: "application/json; charset=latin1",
			body:        `{}`,
			status:      415,
			code:        "unsupported_media_type",
		},
		{
			name:        "malformed JSON",
			contentType: "application/json",
			body:        `{"name":,}`,
			status:      400,
			code:        "malformed_json",
		},
		{name: "empty body", contentType: "application/json", body: "", status: 400, code: "malformed_json"},
		{name: "wrong type", contentType: "application/json", body: `{"name":7}`, status: 400, code: "malformed_json"},
		{
			name:        "trailing data",
			contentType: "application/json",
			body:        `{"name":"a"} {"name":"b"}`,
			status:      400,
			code:        "malformed_json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got widget
			p := httpx.NewCodec(0).Decode(httptest.NewRecorder(), decodeRequest(tt.contentType, tt.body), &got)
			if p == nil {
				t.Fatalf("Decode accepted it; decoded %+v", got)
			}
			if p.Status != tt.status || p.Code != tt.code {
				t.Errorf("got %d %q, want %d %q", p.Status, p.Code, tt.status, tt.code)
			}
		})
	}
}

// jsonBodyOfSize is a valid JSON string literal exactly n bytes long.
func jsonBodyOfSize(n int64) string {
	return `"` + strings.Repeat("a", int(n)-2) + `"`
}

func TestDecodeBodyLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		codec  httpx.Codec
		size   int64
		tooBig bool
	}{
		{name: "default limit, at the limit", codec: httpx.NewCodec(0), size: 1 << 20, tooBig: false},
		{name: "default limit, one byte over", codec: httpx.NewCodec(0), size: 1<<20 + 1, tooBig: true},
		{name: "zero value codec, one byte over", codec: httpx.Codec{}, size: 1<<20 + 1, tooBig: true},
		{name: "custom limit, at the limit", codec: httpx.NewCodec(64), size: 64, tooBig: false},
		{name: "custom limit, one byte over", codec: httpx.NewCodec(64), size: 65, tooBig: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got string
			req := decodeRequest("application/json", jsonBodyOfSize(tt.size))
			p := tt.codec.Decode(httptest.NewRecorder(), req, &got)
			switch {
			case !tt.tooBig && p != nil:
				t.Errorf("a %d-byte body was rejected: %v", tt.size, p)
			case tt.tooBig && p == nil:
				t.Errorf("a %d-byte body was accepted", tt.size)
			case tt.tooBig && (p.Status != http.StatusRequestEntityTooLarge || p.Code != "request_too_large"):
				t.Errorf("got %d %q, want 413 request_too_large", p.Status, p.Code)
			}
		})
	}
}

func TestOversizedBodyIsAProblemResponse(t *testing.T) {
	t.Parallel()
	codec := httpx.NewCodec(64)
	srv := httptest.NewServer(httpx.HandlerWithLogger(func(w http.ResponseWriter, r *http.Request) *httpx.Problem {
		var got string
		return codec.Decode(w, r, &got)
	}, log.GetLoggerTo("httpx", io.Discard)))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Post(srv.URL, "application/json", strings.NewReader(jsonBodyOfSize(65)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != problemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problemContentType)
	}
}

func TestRespondWritesJSON(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	p := httpx.NewCodec(0).
		Respond(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusCreated, widget{Name: "gear"})
	if p != nil {
		t.Fatalf("Respond returned %v", p)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"name":"gear"}` {
		t.Errorf("body = %s", body)
	}
}

func TestRespondMarshalsBeforeWriting(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	p := httpx.NewCodec(0).Respond(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, func() {})
	if p == nil || p.Status != http.StatusInternalServerError || p.Unwrap() == nil {
		t.Fatalf("Respond = %v, want a 500 problem with a cause", p)
	}
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Errorf("Respond wrote a partial response: %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
}
