# Context

## Overview

This project is a preconfigured logging library (`utils/log`) built around `github.com/rs/zerolog` (not `log/slog`). It provides a standard, opinionated configuration that applications importing this framework should use uniformly. The library exposes the configured `zerolog.Logger` directly, plus helpers for structured logging, correlation, and observability. Correlation fields are extracted from `context.Context`; middleware integration is pluggable.

- Emits structured JSON logs (Victorialogs/Grafana) + human-readable (CLI/kubectl)
- Two output streams with RFC3339 timestamps
- Configurable log levels (DEBUG, INFO, WARN, ERROR)
- Correlation fields extracted from context.Context (request_id, trace_id, user_id)
- Redaction rules: password, auth headers, emails
- Pluggable middleware support
- Factory creates named loggers: New(name string)

## Key Concepts

### Log Event

A log event represents a single unit of information emitted by the application. It contains:
- **Level** – severity of the event (DEBUG, INFO, WARN, ERROR)
- **Message** – human-readable description of the event
- **Fields** – structured key-value pairs for machine parsing
- **Metadata** – optional context such as trace ID, span ID, user ID, request ID

### Log Handler

A handler is responsible for formatting and delivering log events. Handlers can:
- Serialize events to JSON (or other formats)
- Route events to different destinations (file, stdout, external aggregator)
- Add correlation metadata (trace ID, span ID)
- Perform redaction of sensitive fields (PII, passwords)

### Context

Context is the set of data that travels with a log event to enable correlation across services. Correlation fields (request_id, trace_id, user_id) are extracted from `context.Context` via middleware.

## Glossary

| Term | Definition |
|------|------------|
| **Log Event** | A single structured log entry emitted by the application |
| **Handler** | A component that formats and delivers log events |
| **Context** | Metadata attached to a log event for correlation |
| **Trace ID** | Identifier for a distributed transaction across services |
| **Span ID** | Identifier for a specific operation within a trace |
| **Redaction** | Automatic masking of sensitive data in log messages |

## Design Decisions

- **Two output streams**: JSON stream for Victorialogs/Grafana observability; human-readable stream for CLI/kubectl. Every log call writes to both handlers (double I/O) — benchmark at high throughput.
- **Timestamp format**: RFC3339
- **Redaction rules**: password, auth headers, emails (email is ok but not ideal)
- **Correlation fields**: request_id, trace_id, user_id — extracted from context.Context
- **Middleware**: pluggable (works with any HTTP framework, not just chi)
- **API**: factory creates named loggers: New(name string)
- **Human-readable stream**: all levels (not just WARN+)
- **ADR-0001**: decision to use zerolog (not slog), hard-to-reverse
