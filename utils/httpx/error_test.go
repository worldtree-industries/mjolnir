package httpx_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/dfryer1193/mjolnir/utils/log"
)

// decodeStep is the shape of a caller's helper: it passes Decode's result on
// as a plain error.
func decodeStep(w http.ResponseWriter, r *http.Request, dst any) error {
	return httpx.Codec{}.Decode(w, r, dst)
}

func TestDecodeSuccessIsNilError(t *testing.T) {
	t.Parallel()
	var got widget
	err := decodeStep(httptest.NewRecorder(), decodeRequest("application/json", `{"name":"gear"}`), &got)
	if err != nil {
		t.Fatalf("a successful Decode seen as error is non-nil: %#v", err)
	}
}

func TestRespondSuccessIsNilError(t *testing.T) {
	t.Parallel()
	step := func(w http.ResponseWriter, r *http.Request) error {
		return httpx.Codec{}.Respond(w, r, http.StatusOK, widget{Name: "gear"})
	}
	if err := step(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); err != nil {
		t.Fatalf("a successful Respond seen as error is non-nil: %#v", err)
	}
}

func TestHandlerReturningSuccessfulDecodeWritesNothing(t *testing.T) {
	t.Parallel()
	logs := &syncBuffer{}
	h := httpx.HandlerWithLogger(func(w http.ResponseWriter, r *http.Request) error {
		var in widget
		return httpx.Codec{}.Decode(w, r, &in)
	}, log.GetLoggerTo("httpx", logs))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, decodeRequest("application/json", `{"name":"gear"}`))

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Errorf("response = %d %q (Content-Type %q), want nothing written",
			rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if out := logs.String(); out != "" {
		t.Errorf("a successful handler produced log output:\n%s", out)
	}
}

func TestHandlerSendsPlainErrorAsInternalProblem(t *testing.T) {
	t.Parallel()
	rec, logs := serve(t, func(http.ResponseWriter, *http.Request) error {
		return errors.New("boom: replica lag 42s")
	})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != problemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problemContentType)
	}
	got := decodeProblem(t, rec.Body.Bytes())
	if got["code"] != "internal_server_error" || got["detail"] != "The server could not complete the request." {
		t.Errorf("code/detail = %v/%v, want internal_server_error and the fixed generic detail",
			got["code"], got["detail"])
	}
	if strings.Contains(rec.Body.String(), "replica lag") {
		t.Errorf("500 body leaks the cause: %s", rec.Body.String())
	}
	for _, want := range []string{`"level":"ERROR"`, `"status":500`, "boom: replica lag 42s"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log is missing %s:\n%s", want, logs)
		}
	}
}

func TestHandlerFindsWrappedProblem(t *testing.T) {
	t.Parallel()
	rec, logs := serve(t, func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("load widget 7: %w", httpx.NotFound("widget_not_found", "No widget has that id."))
	})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	got := decodeProblem(t, rec.Body.Bytes())
	if got["code"] != "widget_not_found" || got["detail"] != "No widget has that id." {
		t.Errorf("code/detail = %v/%v, want widget_not_found and its public detail", got["code"], got["detail"])
	}
	if logs != "" {
		t.Errorf("a wrapped 4xx produced log output:\n%s", logs)
	}
}
