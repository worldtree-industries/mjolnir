# ADR-0001: Preconfigured zerolog Logging Library

## Status

Accepted

## Context

The framework currently uses `github.com/rs/zerolog` (v1.33.0) for structured logging. We are designing `utils/log` as a preconfigured library that applications importing this framework will use uniformly.

## Decision

We will build `utils/log` around `github.com/rs/zerolog` (not `log/slog`), using a factory pattern: `New(name string)` creates named `*Logger` instances, each with the `"logger"` field set to the injectable name.

This is a hard-to-reverse decision: once applications begin importing this package, switching to `slog` or a different abstraction would require migration across all dependent codebases.

The library must provide:

- Two output streams: JSON for Victorialogs/Grafana observability; human-readable for CLI/kubectl debugging
- Structured JSON format with RFC3339 timestamps
- Logger instances created by name via `New(name string)`, with the `"logger"` field set
- Correlation fields (request_id, trace_id, user_id) extracted from `context.Context`
- Always-on redaction for password, auth headers, emails (PII)
- Pluggable chi-style middleware adapter for context injection

All log levels (DEBUG, INFO, WARN, ERROR) are emitted to both streams. Redaction is enforced unconditionally (not opt-in). Double I/O (both streams per log call) is accepted; benchmark before optimizing.

## Consequences

- Applications using this framework get uniform logging behavior by default.
- The framework is tied to zerolog; migration to `slog` would be costly.
- Direct exposure of `zerolog.Logger` avoids wrapper overhead but couples consumers to zerolog.
