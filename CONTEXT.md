# Context

## Overview

This project provides a preconfigured logging library (`utils/log`) built around the standard library's `log/slog`. It gives applications importing this framework one way to get a logger: ask for it by component name. There is no global logger — no package- or process-wide shared instance to configure or reassign. Each logger is bound to a per-name level that can be changed on the fly, and every log line carries line-of-code attribution.

- Foundation: `log/slog` (standard library), per ADR-0003 (supersedes ADR-0001)
- Named loggers via `GetLogger(name)`; per-name levels changeable on the fly (ADR-0004)
- Two output streams: human-readable to stdout, JSON to a file for Victorialogs/Grafana
- Line-of-code attribution (source file:line) on every log line, on by default
- Log level is a minimum threshold (e.g. Info emits Info, Warn, Error)
- No automatic redaction: callers decide what is safe to log
- Deliberately correlation-agnostic: it is a general-purpose logging foundation and knows nothing about HTTP. Request-id (and any other correlation field) is attached by callers that have it, via the ordinary `*slog.Logger.With`.

## Key Concepts

### Log Event

A single unit of information emitted by the application. It contains a **Level** (severity), a **Message**, structured **Fields** (key-value pairs), and carries **Source** (the line of code that emitted it) plus the **logger** field (which component emitted it). Callers may attach further fields — for example the HTTP layer attaches the request id.

### Log Handler

A component that formats and delivers log events to a destination. The framework composes two handlers — a human-readable one to stdout and a JSON one to a file — so every event reaches both.

## Glossary

| Term | Definition |
|------|------------|
| **Log Event** | A single structured log entry emitted by the application |
| **Handler** | A component that formats and delivers log events to a destination |
| **Named Logger** | A logger obtained by component name via `GetLogger(name)`; the name is the logger's identity and the key for its level |
| **Level** | The minimum threshold for a name: an event is emitted only at or above it |
| **Default Level** | The level used by a name that has none of its own; `SetDefaultLevel` changes it for all such names |
| **Source** | The line of code (file and line) that emitted a log event |
| **Context** | The Go `context.Context`: a carrier for request-scoped metadata such as the request id |
| **Request ID** | An identifier for one HTTP request, set by the RequestID middleware on the context and echoed in the `X-Request-ID` header; the HTTP layer attaches it to request log lines |
| **JSON Stream** | The machine-readable stream, written to a file for Victorialogs/Grafana |
| **Text Stream** | The human-readable stream, written to stdout for CLI/kubectl |
| **Option** | A container for a value that may be absent |
| **Present** | An Option holding a non-nil value |
| **Empty** | An Option holding no value |
| **Health** | The umbrella HTTP surface reporting whether the process is started, alive, and ready to serve traffic |
| **Startup** | Initialization complete: the one-time milestone after which the startup probe succeeds; never a dependency check |
| **Liveness** | The process is running and can accept connections; failure means restart |
| **Readiness** | The process can serve traffic right now; failure means remove from traffic, never restart |
| **Drain** | The window between shutdown being requested and the server closing: readiness reports 503, the process keeps serving, then in-flight requests finish |
| **Release Tag** | A lightweight `vX.Y.Z` tag on a merge commit to `main`; the release itself, since a library publishes no artifacts (ADR-0006) |
| **Bump** | The requested version step for a merge: `semver:major` or `semver:minor` from the pull request's labels, `patch` otherwise (ADR-0006) |
| **Version Guard** | The check that the `go.mod` module path's `/vN` suffix matches the major version a bump would publish; blocks merging and runs again before tagging |

## Design Decisions

- **Foundation**: `log/slog`, stdlib — ADR-0003 (supersedes ADR-0001)
- **No global logger**: `GetLogger(name)` hands out ready-to-use loggers; the package owns a name→level table and the sink writers, and nothing else — ADR-0004
- **Per-name levels, on the fly**: `SetLevel(name, lvl)` takes effect on the next emitted event for every logger of that name; level is a minimum threshold, default Info
- **Output**: text → stdout, JSON → file (default `logs/mjolnir.jsonl`, append-only); attribution on by default via `AddSource`
- **No automatic redaction**: the package does not inspect or mask fields; callers decide what is safe to log
- **Correlation is the caller's job**: the logging package is correlation-agnostic; the HTTP layer (`middleware`) owns the request id (context key + `GetRequestID`) and attaches it to request log lines via `*slog.Logger.With`
- **No package-level Fatal**: callers that must terminate log at Error and exit themselves
- **Test seam**: `UseOutputForTests` redirects both sinks; documented as for tests only
- **ADR-0002**: Option[T] redesigned around explicit presence (comma-ok `Get()`), module floor go1.27
- **Health surface**: `/livez` (constant 200), `/readyz` (flappable `ReadyFunc`), `/startupz` (latched `StartedFunc`); both functions are required at `router.New` — ADR-0005
- **Releases happen on merge**: the merge commit to `main` is tagged, bumped by the merged pull request's labels (`patch` by default), and only when the version guard's module-path suffix matches — ADR-0006
