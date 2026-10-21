# Named loggers with per-name levels; no global logger

## Status

Accepted.

## Context

The framework reassigned zerolog's package-global `log.Logger` (in `router.New`) and every component logged through that one nameless shared instance — the global-logger antipattern. The replacement design, modelled on slf4j (a name-keyed factory, per-name levels changeable on the fly), hit a Go-specific wall with zerolog: `Logger` is a value type with a private `level` field, so a registry of shared `*Logger` instances that `SetLevel(name)` mutates in place is a **data race** — readers holding the entry call pointer-receiver methods (`Info` → `newEvent` → `should`, which reads `l.level`) while the setter reassigns the entry. Verified with `go test -race` (`WARNING: DATA RACE` at zerolog's `log.go:508`). Tellingly, when zerolog's own authors share level state (`SetGlobalLevel`), they synchronize it with an atomic `int32` — they acknowledge the need and provide it only for the global, never per-logger.

## Decision

`utils/log` follows the slf4j model on `log/slog` (ADR-0003):

- **`GetLogger(name string) *slog.Logger`** returns a ready-to-use stdlib logger: no wrapper type, no shared logger instance. Callers hold and emit through it directly.
- **Per-name level table**: the package owns a `name → slog.Level` map (guarded by an `RWMutex`) plus a default level. Each logger's handlers hold a small `Leveler` view that consults the table **per record** (the name's row if present, else the default); `slog.HandlerOptions.Level` accepts any `Leveler`, and the stdlib handlers call `Level()` for each record. `SetLevel(name, lvl)` and `SetDefaultLevel(lvl)` write the table and take effect on the next emitted record for **every** logger of that name — held or `With()`-derived — because the view re-reads the table each time (verified under `-race`).
- **Sinks**: `slog.NewMultiHandler` — a human-readable text handler to stdout, a JSON handler to a file (default `logs/mjolnir.jsonl`, append-only; degrade gracefully if it cannot be opened). Line attribution is on by default via `AddSource`.
- **No automatic redaction**: the package does not inspect or mask attributes; callers decide what is safe to log.
- **No package-level `Fatal`**: slog has none. Call sites that must terminate log at `Error` and call `os.Exit` themselves.
- **Test seam**: `UseOutputForTests(io.Writer)` redirects both sinks and returns a `restore` function; documented as for tests only.

The package's total state is the level table and the two sink writers. Neither is a logger.

## Considered Options

- **Registry of shared `*Logger` instances (slf4j translated literally)**: a data race in Go (see Context). Rejected.
- **A wrapper type with emit-time level gating**: race-free, but puts a wrapper between the caller and the logger, adds a stack frame that breaks caller attribution, and must re-expose the event API. Rejected in favour of returning the stdlib logger directly.
- **Level as a construction input (fully stateless)**: cleanest shape, but cannot change the level of already-issued loggers. Rejected — on-the-fly per-name levels are a hard requirement.
- **A name→level table consulted by a custom `LevelWriter`** (the zerolog version of the same idea): race-free and verified, but bespoke; unnecessary once slog is the foundation, since `LevelVar` is the documented goroutine-safe primitive for exactly this.

## Consequences

- A level is a **minimum threshold**: setting `Info` emits Info, Warn, Error — not Debug. Default is `Info`.
- Level changes are visible from the next emitted event, for every holder of the name; no re-acquisition or re-derivation required.
- `SetLevel` on a name that no `GetLogger` call has made yet creates the row immediately; a later `GetLogger` for that name binds to it (the row-binding rule above).
- The table is bounded by the number of distinct component names ever passed. Names are component identifiers — a small, stable set — not request-scoped strings.
- `config`'s `log.level` is not yet applied: `config.Load()` itself logs while loading (chicken-and-egg), and config has no reload mechanism. Follow-up: call `SetDefaultLevel` with the parsed value after `Load()`.
