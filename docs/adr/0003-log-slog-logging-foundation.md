# Structured logging on log/slog

## Status

Accepted. Supersedes [ADR-0001](0001-preconfigured-zerolog-logging.md).

## Context

ADR-0001 chose `github.com/rs/zerolog` over `log/slog` for `utils/log` and called the choice hard-to-reverse "once applications begin importing this package." The follow-up work — removing the global logger and adding on-the-fly per-name levels — showed that zerolog lacks the seams for that design: a per-logger level is a private field of a value type (a shared-instance registry with `SetLevel` is a data race, proven under `-race`), caller attribution requires a fragile stack-frame count baked into every logger, and dual output needs a console writer that re-parses the JSON bytes.

At the time of that re-evaluation, `utils/log` had **zero adoption outside its own tests** — every call site logged through zerolog's package-global logger, and all of them were being rewritten to remove that global anyway.

## Decision

Build `utils/log` on the standard library's `log/slog` and remove zerolog from `go.mod`. slog provides every required mechanism as documented stdlib behaviour:

- **On-the-fly levels**: `*slog.LevelVar` — documented as "a Level variable, to allow a Handler level to change dynamically… safe for use by multiple goroutines".
- **Line attribution**: `HandlerOptions.AddSource` — the handler resolves the captured program counter to file:line; verified in a probe to point at the call site with no frame-count tuning.
- **Dual output**: `slog.NewMultiHandler` — an independent text handler to stdout and a JSON handler to a file.

The switch happens while adoption is zero — the cheapest moment it will ever be.

## Considered Options

- **Keep zerolog with a custom sink-side gate** (a `LevelWriter` that consults a name→level table at emit time): race-free (verified) but bespoke; caller attribution fragile (a baked-in frame count pinned only by regression tests).
- **The `log/slog` global** (`slog.SetDefault`): the same global-logger antipattern this work set out to remove.

## Consequences

- The framework no longer depends on an external logging library; a `depguard` rule bans zerolog (and its global-logger subpackage) outside `utils/log`.
- The on-the-wire field shape changes (`msg` + `source{function,file,line}` where zerolog emitted `message` + `caller`). No external system consumed the old shape: adoption was zero.
- slog has no `Fatal`; the former `log.Fatal` call sites log at `Error` and exit at the call site (see ADR-0004).
