package router_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/middleware"
	"github.com/dfryer1193/mjolnir/router"
)

var (
	_ http.Handler = (*router.Router)(nil)
	_ chi.Router   = (*router.Router)(nil)
)

func TestNewRequiresStarted(t *testing.T) {
	_, err := router.New(router.Config{Health: health.Config{Ready: health.AlwaysReady}})
	if !errors.Is(err, health.ErrStartedRequired) {
		t.Fatalf("got %v, want ErrStartedRequired", err)
	}
}

func TestNewRequiresReady(t *testing.T) {
	_, err := router.New(router.Config{Health: health.Config{Started: health.AlwaysStarted}})
	if !errors.Is(err, health.ErrReadyRequired) {
		t.Fatalf("got %v, want ErrReadyRequired", err)
	}
}

func TestLivenessDoesNotConsultChecks(t *testing.T) {
	var calls atomic.Int32
	count := func(context.Context) bool {
		calls.Add(1)
		return false
	}

	r := mustNew(t, health.Config{Started: count, Ready: count})

	probe(t, r, "/livez", http.StatusOK)

	if calls.Load() != 0 {
		t.Fatalf("liveness consulted a check %d times, want 0", calls.Load())
	}
}

func TestReadinessFollowsReadyFuncOnEveryRequest(t *testing.T) {
	var ready atomic.Bool
	var calls atomic.Int32

	r := mustNew(t, health.Config{
		Started: health.AlwaysStarted,
		Ready: func(context.Context) bool {
			calls.Add(1)
			return ready.Load()
		},
	})

	probe(t, r, "/readyz", http.StatusServiceUnavailable)

	ready.Store(true)

	probe(t, r, "/readyz", http.StatusOK)

	if got := calls.Load(); got != 2 {
		t.Fatalf("ReadyFunc called %d times, want 2 (once per request)", got)
	}
}

func TestStartupLatches(t *testing.T) {
	var started atomic.Bool

	r := mustNew(t, health.Config{
		Started: func(context.Context) bool { return started.Load() },
		Ready:   health.AlwaysReady,
	})

	probe(t, r, "/startupz", http.StatusServiceUnavailable)

	started.Store(true)

	probe(t, r, "/startupz", http.StatusOK)

	started.Store(false)

	probe(t, r, "/startupz", http.StatusOK)
}

// probePaths are the paths the framework reserves.
func probePaths() []string { return []string{"/livez", "/readyz", "/startupz"} }

func TestProbesRejectMethodsOtherThanGetAndHead(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(mustNew(t, alwaysHealthy()))
	t.Cleanup(srv.Close)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, path := range probePaths() {
			resp := do(t, method, srv.URL+path, nil)
			if resp.status != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s = %d, want 405", method, path, resp.status)
			}
			if got := resp.header.Get("Allow"); got != "GET, HEAD" {
				t.Fatalf("%s %s: Allow = %q, want \"GET, HEAD\"", method, path, got)
			}
		}
	}
}

func TestHeadOnProbesMatchesGetWithoutABody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(mustNew(t, health.Config{
		Started: health.AlwaysStarted,
		Ready:   func(context.Context) bool { return false },
	}))
	t.Cleanup(srv.Close)

	want := map[string]int{
		"/livez":    http.StatusOK,
		"/readyz":   http.StatusServiceUnavailable,
		"/startupz": http.StatusOK,
	}

	for _, path := range probePaths() {
		getResp := get(t, srv.URL+path)
		headResp := do(t, http.MethodHead, srv.URL+path, nil)

		if getResp.status != want[path] {
			t.Fatalf("GET %s = %d, want %d", path, getResp.status, want[path])
		}
		if headResp.status != getResp.status {
			t.Fatalf("HEAD %s = %d, want the GET status %d", path, headResp.status, getResp.status)
		}
		if headResp.body != "" {
			t.Fatalf("HEAD %s body = %q, want empty", path, headResp.body)
		}
	}
}

func TestAppMiddlewareNeverRunsOnProbePaths(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	var calls atomic.Int32

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			calls.Add(1)
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/hello", func(http.ResponseWriter, *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		for _, path := range probePaths() {
			do(t, method, srv.URL+path, nil)
		}
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("app middleware ran %d times on probe paths, want 0", got)
	}

	get(t, srv.URL+"/hello")

	if got := calls.Load(); got != 1 {
		t.Fatalf("app middleware ran %d times on /hello, want 1", got)
	}
}

func TestAppRouteOnAProbePathIsUnreachable(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	r.Get("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	r.Post("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if got := get(t, srv.URL+"/livez").status; got != http.StatusOK {
		t.Fatalf("GET /livez = %d, want the framework's 200", got)
	}
	if got := do(t, http.MethodPost, srv.URL+"/livez", nil).status; got != http.StatusMethodNotAllowed {
		t.Fatalf("POST /livez = %d, want the framework's 405", got)
	}
}

func TestFailingReadinessProbeAppearsInTheAccessLog(t *testing.T) {
	t.Parallel()

	logs := newLogSink()
	r := mustNewWith(t, router.Config{
		Health:       health.Config{Started: health.AlwaysStarted, Ready: func(context.Context) bool { return false }},
		AccessLogger: logs.logger(),
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	get(t, srv.URL+"/readyz")

	logs.waitFor(t, "path=/readyz", "status=503")
}

func TestAppMiddlewareSeesTheRequestID(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	seen := make(chan string, 1)

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen <- middleware.GetRequestID(req.Context())
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/hello", func(http.ResponseWriter, *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := get(t, srv.URL+"/hello")

	id := <-seen
	if id == "" {
		t.Fatal("app middleware saw no request ID")
	}
	if echoed := resp.header.Get("X-Request-ID"); echoed != id {
		t.Fatalf("app middleware saw %q, response echoed %q", id, echoed)
	}
}

func TestRequestRejectedByAppMiddlewareIsAccessLogged(t *testing.T) {
	t.Parallel()

	logs := newLogSink()
	r := mustNewWith(t, router.Config{Health: alwaysHealthy(), AccessLogger: logs.logger()})

	r.Use(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	})
	r.Get("/secret", func(http.ResponseWriter, *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if got := get(t, srv.URL+"/secret").status; got != http.StatusUnauthorized {
		t.Fatalf("GET /secret = %d, want 401", got)
	}

	logs.waitFor(t, `msg="request completed"`, "path=/secret", "status=401")
}

func TestSpoofedForwardingHeadersAreIgnoredByDefault(t *testing.T) {
	t.Parallel()

	logs := newLogSink()
	r := mustNewWith(t, router.Config{Health: alwaysHealthy(), AccessLogger: logs.logger()})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	do(t, http.MethodGet, srv.URL+"/livez", http.Header{
		"X-Forwarded-For": {"203.0.113.7"},
		"X-Real-Ip":       {"203.0.113.8"},
		"True-Client-Ip":  {"203.0.113.9"},
	})

	line := logs.waitFor(t, "path=/livez")
	if field(line, "client_ip") != "127.0.0.1" || !strings.HasPrefix(field(line, "remote_addr"), "127.0.0.1:") {
		t.Fatalf("want the transport peer as client_ip and remote_addr, got %q", line)
	}
	if strings.Contains(line, "203.0.113.") {
		t.Fatalf("a spoofed header reached the access line: %q", line)
	}
}

func TestTrustedProxiesResolveTheClientFromXForwardedFor(t *testing.T) {
	t.Parallel()

	logs := newLogSink()
	r := mustNewWith(t, router.Config{
		Health:         alwaysHealthy(),
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		AccessLogger:   logs.logger(),
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	// The leftmost entry is whatever the client sent; the proxy appended the
	// address it saw.
	do(t, http.MethodGet, srv.URL+"/livez", http.Header{"X-Forwarded-For": {"198.51.100.66, 203.0.113.7"}})

	line := logs.waitFor(t, "path=/livez")
	if got := field(line, "client_ip"); got != "203.0.113.7" {
		t.Fatalf("want client_ip=203.0.113.7, got %q", line)
	}
	if !strings.HasPrefix(field(line, "remote_addr"), "127.0.0.1:") {
		t.Fatalf("remote_addr is not the transport peer: %q", line)
	}
}

func TestUnparsablePeerResolvesNoClientIP(t *testing.T) {
	t.Parallel()

	configs := map[string][]netip.Prefix{
		"default":         nil,
		"trusted proxies": {netip.MustParsePrefix("127.0.0.0/8")},
	}

	for name, prefixes := range configs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			logs := newLogSink()
			r := mustNewWith(t, router.Config{
				Health:         alwaysHealthy(),
				TrustedProxies: prefixes,
				AccessLogger:   logs.logger(),
			})

			req := httptest.NewRequest(http.MethodGet, "/livez", nil)
			req.RemoteAddr = "not-an-address"
			req.Header.Set("X-Forwarded-For", "203.0.113.7")
			r.ServeHTTP(httptest.NewRecorder(), req)

			line := logs.waitFor(t, "path=/livez")
			if strings.Contains(line, "client_ip=") {
				t.Fatalf("an unparsable peer resolved a client IP: %q", line)
			}
		})
	}
}

func TestNewRejectsAnInvalidTrustedProxy(t *testing.T) {
	t.Parallel()

	_, err := router.New(router.Config{Health: alwaysHealthy(), TrustedProxies: []netip.Prefix{{}}})
	if err == nil {
		t.Fatal("New accepted the zero netip.Prefix")
	}
}

func TestPanicBeforeAWriteIsA500Problem(t *testing.T) {
	t.Parallel()

	logs := newLogSink()
	r := mustNewWith(t, router.Config{Health: alwaysHealthy(), AccessLogger: logs.logger()})

	r.Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := get(t, srv.URL+"/boom")
	if resp.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.status)
	}
	if ct := resp.header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	if !strings.Contains(resp.body, `"code":"internal_server_error"`) || strings.Contains(resp.body, "boom") {
		t.Fatalf("body = %s, want a generic internal_server_error problem", resp.body)
	}

	id := resp.header.Get("X-Request-ID")
	panicLine := logs.waitFor(t, `msg="panic recovered"`)

	for key, want := range map[string]string{
		"method":           "GET",
		"path":             "/boom",
		"request_id":       id,
		"response_started": "false",
		"panic":            "boom",
	} {
		if got := field(panicLine, key); got != want {
			t.Fatalf("panic line %s = %q, want %q; line %q", key, got, want, panicLine)
		}
	}
	if !strings.Contains(panicLine, "stack=") || !strings.Contains(panicLine, "mjolnirRouter_test.go") {
		t.Fatalf("panic line has no stack through the handler: %q", panicLine)
	}

	logs.waitFor(t, `msg="request completed"`, "path=/boom", "status=500")
}

func TestPanicInAppMiddlewareIsRecoveredAndAccessLogged(t *testing.T) {
	t.Parallel()

	logs := newLogSink()
	r := mustNewWith(t, router.Config{Health: alwaysHealthy(), AccessLogger: logs.logger()})

	r.Use(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("middleware boom")
		})
	})
	r.Get("/hello", func(http.ResponseWriter, *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if got := get(t, srv.URL+"/hello").status; got != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", got)
	}

	logs.waitFor(t, `msg="panic recovered"`, "path=/hello")
	logs.waitFor(t, `msg="request completed"`, "path=/hello", "status=500")
}

func TestPanicAfterAWriteAbortsTheConnection(t *testing.T) {
	t.Parallel()

	logs := newLogSink()
	r := mustNewWith(t, router.Config{Health: alwaysHealthy(), AccessLogger: logs.logger()})

	r.Get("/partial", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		_ = http.NewResponseController(w).Flush()
		panic("late boom")
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/partial", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := (&http.Client{Timeout: probeTimeout}).Do(req)
	if err != nil {
		t.Fatalf("GET /partial: %v", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr == nil {
		t.Fatalf("body read cleanly as %q; want the connection aborted", body)
	}
	if strings.Contains(string(body), "problem") {
		t.Fatalf("a second body was appended: %q", body)
	}

	panicLine := logs.waitFor(t, `msg="panic recovered"`, "path=/partial")
	if got := field(panicLine, "response_started"); got != "true" {
		t.Fatalf("response_started = %q, want true; line %q", got, panicLine)
	}

	logs.waitFor(t, `msg="request completed"`, "path=/partial", "status=200")
}

func TestAppRoutingKeepsWorkingUnderTheFramework(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	tag := func(value string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Add("X-Tag", value)
				next.ServeHTTP(w, req)
			})
		}
	}
	echoParam := func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(chi.URLParam(req, "id")))
	}

	r.Route("/users", func(sub chi.Router) {
		sub.Get("/{id}", echoParam)
	})
	r.Group(func(g chi.Router) {
		g.Use(tag("group"))
		g.Get("/grouped/{id}", echoParam)
	})
	r.With(tag("with")).Get("/with/{id}", echoParam)

	sub := chi.NewRouter()
	sub.Get("/{id}", echoParam)
	r.Mount("/mounted", sub)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	cases := []struct{ path, tag string }{
		{"/users/7", ""},
		{"/grouped/8", "group"},
		{"/with/9", "with"},
		{"/mounted/10", ""},
	}
	for _, tc := range cases {
		resp := get(t, srv.URL+tc.path)
		wantBody := tc.path[strings.LastIndex(tc.path, "/")+1:]

		if resp.status != http.StatusOK || resp.body != wantBody {
			t.Fatalf("GET %s = %d %q, want 200 %q", tc.path, resp.status, resp.body, wantBody)
		}
		if got := resp.header.Get("X-Tag"); got != tc.tag {
			t.Fatalf("GET %s: X-Tag = %q, want %q", tc.path, got, tc.tag)
		}
	}
}

func TestHeadOnAnAppGetRouteMatchesGetWithoutABody(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	r.Get("/hello", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-App", "yes")
		_, _ = w.Write([]byte("hello"))
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := do(t, http.MethodHead, srv.URL+"/hello", nil)
	if resp.status != http.StatusOK || resp.body != "" || resp.header.Get("X-App") != "yes" {
		t.Fatalf("HEAD /hello = %d %q X-App=%q, want 200, no body, X-App=yes",
			resp.status, resp.body, resp.header.Get("X-App"))
	}
}

func TestAppNotFoundAndMethodNotAllowedAnswerAppPaths(t *testing.T) {
	t.Parallel()

	r := mustNew(t, alwaysHealthy())

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("app not found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte("app method not allowed"))
	})
	r.Get("/hello", func(http.ResponseWriter, *http.Request) {})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if resp := get(t, srv.URL+"/nowhere"); resp.status != http.StatusNotFound || resp.body != "app not found" {
		t.Fatalf("GET /nowhere = %d %q, want the app's 404", resp.status, resp.body)
	}

	resp := do(t, http.MethodPost, srv.URL+"/hello", nil)
	if resp.status != http.StatusMethodNotAllowed || resp.body != "app method not allowed" {
		t.Fatalf("POST /hello = %d %q, want the app's 405", resp.status, resp.body)
	}
}
