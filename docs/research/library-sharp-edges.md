# Mjolnir HTTP kit: consumer-facing sharp edges

> **Historical snapshot — pre-fix.** The findings below describe `origin/main` before the
> adoption-hardening work in [#29](https://github.com/worldtree-industries/mjolnir/issues/29).
> They are kept as the evidence that drove the changes, not as a description of the current
> API. The resolution table at the end maps each finding to its regression test; read that
> first.

**Scope.** What a service adopts when it depends on this module: request correlation, client
address, streaming, body limits, timeouts, error bodies, probes, logging, panics, and the
module/toolchain contract. **Method.** Primary sources (RFCs, IANA, Go docs/source, Kubernetes
docs, chi source/advisories, CWE/OWASP/AIP) plus a probe against Mjolnir's public API; probe
results are labelled *local probe*. Anything not verified is called out. **Versions checked:**
`go.mod` at `go 1.27.0`, chi `v5.2.1` at the time of review (the branch under test uses
`v5.5.0`), pgx `v5.11.0`; local toolchain `go1.27.1`.

## 1. Request correlation: `X-Request-ID` is not a standard, and it is trusted verbatim

- `X-Request-ID` is absent from the [IANA HTTP Field Name Registry](https://www.iana.org/assignments/http-fields/http-fields.xhtml), and the `X-` convention is deprecated by [RFC 6648 §2](https://www.rfc-editor.org/rfc/rfc6648.html#section-2). The standardized tracing header is W3C Trace Context's `traceparent`, whose [§7](https://www.w3.org/TR/trace-context/#security-considerations) recommends checking header length and content before acting on it.
- Mjolnir reads the inbound header as-is, generates a UUID only when empty, echoes it in the response, and stores it in the context for logging (`middleware/request_id.go:12-19`). There is no length cap, no UUID/format check, and no indication whether the value was client-supplied.
- *Local probe:* a 4,096-byte `X-Request-ID` was echoed in the response header and written to the access log. `net/http` replaces CR/LF in header values with spaces before writing ([`header.go:190`](https://cs.opensource.google/go/go/+/go1.27.3:src/net/http/header.go;l=190)), so response splitting is blocked, but unbounded length and forged correlation remain. OWASP recommends bounding user-controlled values in logs ([Logging Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html), CWE-117).
- **Relevance to mjolnir:** decide whether to accept, truncate, or regenerate client IDs, and whether to emit `traceparent`; also reject IDs that are not in the documented format so logs cannot be flooded or forged.

## 2. Client IP: `RealIP` is pinned to a version with published spoofing advisories

- `router.New` installs `chi/middleware.RealIP` (`router/mjolnirRouter.go:45`). In v5.2.1 it trusts `True-Client-IP`, then `X-Real-IP`, then the leftmost `X-Forwarded-For` with no trusted-proxy check ([`realip.go`](https://github.com/go-chi/chi/blob/v5.2.1/middleware/realip.go)); `RemoteAddr` is overwritten, so anything logging `r.RemoteAddr` records an attacker-chosen address.
- chi published three advisories covering this code (one medium, two high), all fixed in `v5.3.0`: [GHSA-3fxj-6jh8-hvhx](https://github.com/go-chi/chi/security/advisories/GHSA-3fxj-6jh8-hvhx) (affected `>=5.2.1,<5.3.0`), [GHSA-rjr7-jggh-pgcp](https://github.com/go-chi/chi/security/advisories/GHSA-rjr7-jggh-pgcp) and [GHSA-9g5q-2w5x-hmxf](https://github.com/go-chi/chi/security/advisories/GHSA-9g5q-2w5x-hmxf) (`<5.3.0`). `v5.3.0` deprecates `RealIP` in favour of `ClientIPFromXFFTrustedProxies`/`ClientIPFromHeader` ([`realip.go` @ v5.3.0](https://github.com/go-chi/chi/blob/v5.3.0/middleware/realip.go)).
- *Local probe:* sending `X-Forwarded-For: 203.0.113.9` made the access log record `remote_addr=203.0.113.9`.
- **Unverified:** exploitability depends on deployment topology (whether an edge proxy overwrites the headers); the advisories' exact preconditions were not re-tested. **Relevance to mjolnir:** bump chi and replace `RealIP`, or document that the router must only sit behind a proxy that sanitises forwarding headers.

## 3. The logging wrapper silently removes `Flusher`, `Hijacker`, `ReaderFrom`, and `Unwrap`

- `RequestLogger` wraps every writer in its own `responseWriter`, which implements only `Header`, `Write`, and `WriteHeader` (`middleware/request_logger.go:49-75`). It does not forward optional interfaces or implement `Unwrap`.
- `net/http` tells handlers to test for [`http.Flusher`](https://pkg.go.dev/net/http#Flusher)/[`http.Hijacker`](https://pkg.go.dev/net/http#Hijacker) at runtime because wrappers may not support them; [`ResponseController`](https://pkg.go.dev/net/http#ResponseController) needs the original writer or `Unwrap() http.ResponseWriter` and otherwise returns `ErrNotSupported`; [`io.Copy`](https://pkg.go.dev/io#ReaderFrom) uses `ReaderFrom` for sendfile. chi's own `WrapResponseWriter` preserves Flush/Hijack/Push/ReadFrom and implements `Unwrap` ([source](https://github.com/go-chi/chi/blob/v5.2.1/middleware/wrap_writer.go)).
- *Local probe:* inside a Mjolnir handler, `w.(http.Flusher)` was `false`, `w.(http.Hijacker)` was `false`, and `http.NewResponseController(w).Flush()` returned `ErrNotSupported`. Consequences: SSE/streaming stalls, WebSocket upgrades fail, file responses lose sendfile, and per-response deadlines cannot be set.
- **Relevance to mjolnir:** either forward the optional interfaces (and `Unwrap`) or stop wrapping; this is a router-wide regression, not a per-route opt-in.

## 4. `DecodeJSON` reads an unbounded body and rejects valid `Content-Type` values

- `DecodeJSON` calls `io.ReadAll(r.Body)` with no `http.MaxBytesReader` (`utils/httpx/httpx.go:34`). `net/http` sets no body limit by default; [`MaxBytesReader`](https://pkg.go.dev/net/http#MaxBytesReader) is opt-in, returns `*MaxBytesError`, and does not itself write 413.
- `ValidateContentType` compares the whole header to the literal `application/json` (`utils/httpx/httpx.go:47-49`). Media type parameters do not change the type/subtype ([RFC 9110 §8.3](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.3)), so `application/json; charset=utf-8` and vendor `+json` types are wrongly rejected. *Local probe:* `application/json; charset=utf-8` returned 400, while an 8 MiB body was read fully and accepted.
- **Relevance to mjolnir:** move the limit to middleware (`MaxBytesReader`) with an explicit 413 mapping, and parse the media type with `mime.ParseMediaType` instead of string equality.

## 5. Timeouts are entirely the consumer's job, and the example sets only one

- `http.Server` treats zero or negative timeouts as "no timeout". `ReadHeaderTimeout` bounds only headers, `ReadTimeout` covers the whole request including body, `WriteTimeout` covers response writes (and breaks long-lived streaming), and `IdleTimeout` bounds keep-alive ([`Server` docs](https://pkg.go.dev/net/http#Server)). `WriteTimeout` making streaming impossible is [golang/go#16100](https://github.com/golang/go/issues/16100).
- Mjolnir takes an `*http.Server` and sets nothing itself; the example configures `ReadHeaderTimeout: 5s` only (`example/example.go:78`), and the README repeats that fragment. A slow-body or slow-read client is therefore bounded only by the consumer's choices.
- **Relevance to mjolnir:** ship a documented baseline (header/read/idle timeouts, `MaxHeaderBytes`) or a constructor that requires them; note that `WriteTimeout` must be relaxed deliberately for SSE/WebSocket routes.

## 6. Error responses leak 4xx detail, mislabel 5xx codes, and cannot detect a second write

- The 4xx path writes `err.Error()` verbatim (`utils/errorx/errorx.go:95-96`). *Local probe:* a wrapped `pq: password authentication failed for user "app"` was returned to the client. For 5xx the text is masked, but the JSON `code` is hard-coded to 500 while the HTTP status uses `reqErr.code` (`utils/errorx/errorx.go:87-91`), so a 503 produces a body claiming 500.
- The envelope has only `error` and `code`; it lacks RFC 9457's `type`, `title`, `instance` and `application/problem+json` ([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457.html)), and does not separate developer-facing messages from dynamic details as [AIP-193](https://google.aip.dev/193) recommends. Leaking implementation detail is [CWE-209](https://cwe.mitre.org/data/definitions/209.html).
- *Local probes:* `BadRequestErr(nil)` wrote a 400 with an empty body (panic after the header); a handler that wrote a 200 and then returned an `APIError` emitted `200 {"ok":true}{"error":...}`; a panic after a partial 200 left a truncated 200 because chi's `Recoverer` cannot rewrite a sent header ([source](https://github.com/go-chi/chi/blob/v5.2.1/middleware/recoverer.go)).
- **Relevance to mjolnir:** define one error shape with stable machine codes, keep 5xx detail server-side only, and track whether a response has started before attempting to write an error.

## 7. Probes are GET-only; drain can consume twice the configured window

- Probes are registered with `r.Method(http.MethodGet, ...)` (`router/mjolnirRouter.go:51-53`). *Local probe:* `HEAD /livez` returns 405. HEAD must be identical to GET without a body ([RFC 9110 §9.3.2](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.3.2)); Kubernetes HTTP probes always use GET ([probe docs](https://kubernetes.io/docs/concepts/workloads/pods/probes/#types-of-probes)), so K8s works, but HEAD-based load balancers and uptime checks mark the service down. chi offers `middleware.GetHead` ([source](https://github.com/go-chi/chi/blob/v5.2.1/middleware/get_head.go)); Mjolnir does not use it.
- Liveness always returns 200, including while draining (`health/health.go:71-79`); readiness returns 503 once `Drain` is called. Kubernetes requires liveness to signal unrecoverable failure, not dependency trouble ([probe docs](https://kubernetes.io/docs/concepts/workloads/pods/probes/#when-should-you-use-liveness-readiness-and-startup-probes)).
- Drain flips readiness, waits the full `drain`, then calls `Shutdown` with another `drain` deadline (`router/mjolnirRouter.go:127-153`). Worst case is ~2× `drain`, which must fit inside `terminationGracePeriodSeconds`; the README states a 10s drain needs 20s plus teardown. Kubernetes removes the endpoint concurrently with SIGTERM and SIGKILLs at grace expiry ([pod lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination)).
- `Shutdown` does not wait for hijacked connections such as WebSockets ([`Shutdown` docs](https://pkg.go.dev/net/http#Server.Shutdown)). **Relevance to mjolnir:** add HEAD, document the grace-period arithmetic, and decide whether hijacked connections need explicit tracking.

## 8. Dual logging sinks can fail silently, and the schema is fixed

- Loggers are built with `slog.NewMultiHandler` over text and JSON handlers (`utils/log/logger.go:125-134`). `NewMultiHandler` requires **Go 1.26** ([release notes](https://go.dev/doc/go1.26#logslogpkglogslog), log/slog section), not 1.27. `slog.Logger` discards handler errors ([`Handler` docs](https://pkg.go.dev/log/slog#Handler)), so a failing JSON sink cannot fail a request.
- The default JSON sink is a lazy file that, on open failure, warns once on stderr and then discards writes (`utils/log/logger.go:242-272`). *Local probe:* with a read-only working directory, logging returned normally and no file was created; there is no error or health signal.
- `AddSource: true` is always on (`utils/log/logger.go:126-127`), which resolves a frame via `runtime.CallersFrames` per record. **Unverified:** no official benchmark quantifies the overhead. The package explicitly does not redact (`utils/log/logger.go:17`). A `nil` error field renders as JSON `null` / text `<nil>` in a *local probe*; the `slog` docs define no special handling.
- **Relevance to mjolnir:** decide whether a broken audit sink should be observable, whether `AddSource` is worth its cost in hot paths, and whether the fixed schema needs explicit redaction hooks.

## 9. `go 1.27.0` sets a hard toolchain floor, and pgx is a hard module dependency

- Since Go 1.21 the `go` directive is a mandatory minimum; older toolchains either fetch a newer one or refuse ([`go` directive](https://go.dev/ref/mod#go-mod-file-go), [toolchain selection](https://go.dev/doc/toolchain)). Mjolnir's `go 1.27.0` therefore forces every consumer onto 1.27+, even though `NewMultiHandler` itself only needs 1.26.
- pgx is a direct requirement (`go.mod`). Module graph pruning means a consumer that imports only `router` need not build pgx; *local probe:* importing `router` added only chi and uuid as indirect requirements, while `utils/set` added none. But `go mod tidy` considers all build tags ([`go mod tidy`](https://go.dev/ref/mod#go-mod-tidy)), and a *local probe* confirmed a tagged-only pgx import still adds the module; `go list -m all` still lists it. pgx also brings `pgpassfile`, `pgservicefile`, and `puddle`.
- The module is at `v1.5.2`. Under [SemVer §7](https://semver.org/#spec-item-7), a v1 minor release should be backward-compatible; ADR-0007 deliberately treats v1 minors as the breaking channel, so consumers must pin exact versions. The `/vN` path rule only starts at v2 ([module paths](https://go.dev/ref/mod#vN)); a later `/v2` would be a rename.
- **Decision (2026-10-23):** the database packages stay in the root module (ADR-0011). Module pruning means an HTTP-only consumer does not build `pgx`; nearly every service imports the database packages, so splitting would add import and release overhead without removing a used dependency.
- **Decision (2026-10-23):** keep the `go 1.27.0` floor. `optional.Option.Map` is a generic method, which requires the go1.27 language level (ADR-0002), so the floor is real even though `slog.NewMultiHandler` alone needs only 1.26.
- **Relevance to mjolnir:** keep the compatibility promise explicit in the release notes.

## 10. Recovery and middleware order determine what operators can see

- Middleware order is `RealIP`, `Recoverer`, `RequestID`, `RequestLogger` (`router/mjolnirRouter.go:45-49`). The access log is skipped on panic because `reqLog.InfoContext` runs after `next.ServeHTTP`, not in a defer (`middleware/request_logger.go:27-45`). `Recoverer` is outside the logger, so it handles the panic without the request-scoped log context, and a response may already be committed, leaving the client a truncated 2xx rather than a 500.
- chi's `Recoverer` prints the stack to stderr in colour, attempts `WriteHeader(500)` (which `net/http` ignores once headers are sent), and skips it for `Connection: Upgrade` ([source](https://github.com/go-chi/chi/blob/v5.2.1/middleware/recoverer.go)); it does not use the application's structured logger. *Local probe:* the panic trace appeared on stderr, outside the JSON stream.
- **Relevance to mjolnir:** wrap the access log in a defer, log panics through slog with method/path/request ID, and document that a handler which has already written cannot be converted into a clean 500.

## Resolution (2026-10-23)

The probe was an untracked local program, so its removal is not visible in git history; its
findings were promoted into the tests below and the program was deleted from the worktree. This
table maps each finding to the code and test that now covers it; the work is specified in
[#29](https://github.com/worldtree-industries/mjolnir/issues/29).

| # | Resolution | Evidence |
| --- | --- | --- |
| 1 | UUID-only `X-Request-ID`, length-bounded before parse, canonicalised or regenerated as v4 | `middleware/request_id.go`, `middleware/request_id_test.go` |
| 2 | chi v5.5.0; no `RealIP`; forwarding headers trusted only inside configured CIDRs; `RemoteAddr` never mutated | `router/mjolnirRouter.go`, `router/mjolnirRouter_test.go` |
| 3 | chi's capability-preserving `WrapResponseWriter` exported as `middleware.ResponseWriter` | `middleware/response_writer.go`, `middleware/request_logger_test.go` |
| 4 | `httpx.Codec`: 1 MiB default, `MaxBytesReader`, 413/415/400 mapping, `mime.ParseMediaType` | `utils/httpx/codec.go`, `utils/httpx/codec_test.go` |
| 5 | `router.NewServer` baseline: 5s header, 30s read, 120s idle, 1 MiB headers, `WriteTimeout` unset | `router/mjolnirRouter.go`, `router/mjolnirRouter_test.go`, README |
| 6 | RFC 9457 `Problem`: public 4xx detail, fixed 5xx detail, preserved 5xx status/code, cause logged, started-response detection | `utils/httpx/problem.go`, `utils/httpx/problem_test.go` |
| 7 | `GetHead`; one total drain budget split into readiness window and shutdown grace | `router/mjolnirRouter.go`, `router/mjolnirRouter_test.go`, `router/drain_test.go` |
| 8 | `log.SetSinkErrorHandler` for JSON sink failures; default warn-once retained | `utils/log/logger.go`, tests from #37 |
| 9 | Go 1.27 floor kept because `optional.Option.Map` is a generic method | ADR-0002, ADR-0011 |
| 10 | Deferred access line; router-owned recoverer logs through slog with stack and request id; started-response panic aborts the connection | `router/mjolnirRouter.go`, `middleware/request_logger.go`, `router/mjolnirRouter_test.go` |

Known limitations retained: `http.Server.Shutdown` does not wait for hijacked connections, and a
panic after the response has started cannot be turned into a clean 500 — the connection is aborted
instead so the client does not read the truncation as success.
