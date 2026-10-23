# ADR-0010: Log component names are open identifiers

## Status

Accepted.

## Context

An architecture review proposed replacing the `string` component name with a typed
`Component` and a closed set of constants, so that a typo fails at compile time. The
name is not an enumeration, though: it is both the logger's identity and the key for its
level, and consumers legitimately invent names for components this repository has never
heard of. A closed set would make every new consumer component require a change here.

## Decision

Keep `GetLogger(name string)` and `SetLevel(name string, …)`. Do not introduce a
`Component` type, exported name constants, or a registry. The name is an open string
identifier chosen by the caller; callers that want typo safety define their own `const`
for it. `GetLoggerTo(name, w)` remains the isolated seam for tests and embedding.

## Consequences

- A misspelled name is a new logger with its own level, not a compile error. That is
  accepted in exchange for letting consumers name their own components.
- Documentation must say the name is an open identifier, so a future review does not
  mistake the absence of an enum for an oversight.
- No central list of names exists; "which components log?" is answered by reading the
  call sites and the logs, not by importing a type.
