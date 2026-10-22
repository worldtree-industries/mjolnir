# Mandatory startup, liveness, and readiness endpoints

## Status

Accepted.

Amended by ADR-0006: the `v2.0.0` tag referenced below no longer exists in this repository, and a major release is now possible once the module path is renamed to `/v2`.

## Context

Apps importing mjolnir had no health surface, so every consumer would hand-roll probes — and the failure modes are asymmetric. A liveness probe that checks a dependency turns a database outage into a restart loop. A readiness probe that never fails leaves a broken pod in rotation. A startup probe that checks a dependency kills the container before it has ever finished starting. The probe *contracts* differ; only their endpoint shape looks similar.

The router is already opinionated about wiring: `router.New` installs `RequestID` and `RequestLogger` unconditionally, and apps mount their own routes on the returned mux. Health follows that precedent rather than handing back optional pieces.

## Decision

- **`health` package.** `ReadyFunc` and `StartedFunc` are both `func(context.Context) bool`; the caller handles and logs its own errors. `AlwaysReady` and `AlwaysStarted` exist for apps with nothing to check. The `bool` is deliberate: it keeps the library out of the caller's error vocabulary and keeps `errorx` (issue #20) out of the probe path.
- **`router.New(health.Config{Started, Ready}) (*Router, error)`** returns a `Router` that embeds `*chi.Mux` — still an `http.Handler` with chi's route methods — and mounts `/livez`, `/readyz`, and `/startupz`. Both functions are mandatory; a nil one is an error naming which is missing, and `main` exits non-zero. ADR-0004 stands: the library never calls `os.Exit`.
- **`/livez` is a constant 200.** No callbacks, no configuration. It answers "is this process wedged", not "is everything fine", so it cannot be dragged down by a dependency.
- **`/startupz` latches.** `StartedFunc` is called with the request context until it first returns true; afterwards the handler answers 200 without calling it again. Started is a one-time milestone, not a state.
- **`/readyz` calls `ReadyFunc` on every request.** Readiness is a state and is expected to flap.
- **Wire contract:** 200 when healthy, 503 when not, empty body, GET only (chi answers 405 on other methods). The handlers write the status themselves until #20 lets `errorx` carry a 503 with a public message at a non-Error level.
- **Drain is automatic.** `Router.Serve(ctx, srv, drain)` and `Router.Run(srv, drain)` flip readiness to 503 without consulting `ReadyFunc`, keep serving for `drain`, then call `Shutdown` bounded by the same duration. A second signal skips the window and closes the server; `drain = 0` closes immediately. `Serve` returns errors and never calls `os.Exit`.

## Considered Options

- **Handlers returned for the app to mount.** Preserves path freedom, but makes health optional and the "cannot start without one" rule unenforceable. Rejected — the library's value is the wiring.
- **Functional options or a builder.** No options mechanism exists in this repo, and both functions are mandatory, so either would defer a missing-required-field error to runtime. Rejected; a config struct enforces the field names at the call site.
- **`func(context.Context) error` for readiness.** Pushes error types and a logging policy into the library, and tempts it into reporting dependency detail it does not own. Rejected.
- **Non-latching startup.** A flapping started check would restart the container under any supervisor that reads the endpoint directly. Accepted in Kubernetes only because the kubelet stops evaluating startup after the first success; not a property worth relying on. Rejected.
- **Liveness that pings a dependency.** The restart stampede. Rejected.

## Consequences

- `router.New` changes signature for every caller. Released as a **minor** (`v1.5.0`): this module stays on the 1.x line, because a `v2` tag without a `/v2` module path is not a valid module version — `go list -m -versions` does not list `v2.0.0` at all, which is why the repository's stray `v2.0.0` tag is inert. Major bumps are therefore off the table unless the module path is renamed.
- **Liveness cannot detect a deadlock while the accept loop still answers.** A wedged Go process that still accepts connections returns 200 forever; `/livez` detects only a process that cannot accept at all. This is a stated limitation, not a bug to "fix" by adding dependency checks.
- **Liveness stays 200 through shutdown.** A liveness failure means "restart", and a restart mid-drain is the one thing shutdown must not cause. Readiness is the drain signal instead. Note that `http.Server.Shutdown` closes the listener immediately, so a 503 from `/readyz` is only observable in the window *before* `Shutdown` is called. Go has no shutdown hook, so keeping that window honest means owning the serve call — which is why the lifecycle lives in `Router.Serve`/`Router.Run` rather than in the app.
- **`StartedFunc` must never check a dependency.** A failing startup probe kills the container, which reproduces the liveness stampede through a different door. This belongs in the type's doc comment, not only here.
- **With liveness constant, the startup probe mainly gates the other two.** In Kubernetes a configured startup probe delays both liveness and readiness until it succeeds, and its own failure kills the container; because liveness cannot fail, that gating is the endpoint's practical effect. It is kept because initialization is a fact an app should be able to declare, and because supervisors outside Kubernetes consult the endpoint directly.
- **Failing readiness probes appear in the request log.** `RequestLogger` stays on health routes; that line is the only signal available, since `ReadyFunc` swallows error detail.
- **`errorx` migration is follow-up #20**, not a blocker: it changes how the 503 is produced, not when it is returned.
