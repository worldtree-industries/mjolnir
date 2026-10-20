# ADR-0002: Option[T] With Explicit Presence

## Status

Accepted

## Context

`utils/optional.Option[T]` shipped in v1.x with `Get() T`, which silently returned the
zero value for an empty option — absence was indistinguishable from a zero value, and
the package's test suite encoded that behaviour as correct. The package was removed in
v2.0.0 and re-added afterwards byte-identical to the v1.x version; no release has
contained it since, and no in-repo code calls it, so the interface can be redesigned
as a clean break (v2.0.0 is precedent for breaking the package; no external consumers
were identified).

The redesign answered two pressures: the silent-zero-value bug, and interface
redundancy (`New` and `Some` were identical constructors). The redesign was evaluated
against the Java `Optional` API (`of` / `ofNullable` / `orElse` / ...), which the
maintainers initially proposed porting.

## Decision

Presence is explicit at every call site, using Go's comma-ok idiom rather than
Java's exception-based strictness:

- `Of[T any](val T) Option[T]` — unconditional; always present. A present option may
  legally hold a nil pointer, and value types have no nil, so Java's `of()` NPE guard
  has no invariant to defend here and was dropped (no error return, no panic).
- `Empty[T any]() Option[T]` — absent.
- `Get() (T, bool)` — comma-ok access; replaces the silent zero-value `Get() T`.
- `IsPresent() bool` — replaces `IsEmpty()`.
- Companions: `Map`, `Filter`, `OrElse` (eager default), `OrGet` (lazy default) —
  Java's `map` / `filter` / `orElse` / `orElseGet`. Accepted over a minimal
  comma-ok-only surface: the library ships its convenience surface once.
- `New` and `Some` removed (redundant constructors); `IsEmpty` removed (replaced by
  `IsPresent`).
- `OfNullable[T, P ~*T]` was considered and dropped: no pointer-means-unset flow
  exists in this framework yet — producers decide presence. It remains an additive
  candidate for when such a flow appears.
- `FlatMap` and `IfPresent` omitted: redundant with comma-ok at call sites; add when
  a concrete consumer needs them.

## Consequences

- Empty access can no longer yield a silent zero value; absence is explicit at every
  call site, and the test suite is rewritten against the new contract.
- The API is Java-`Optional`-shaped; Go reviewers may question the functional
  companions on a value type — this ADR is the standing answer.
- Breaking for any v1.x consumer of `utils/optional`; accepted, since v2.0.0 already
  removed the package and no external consumers were identified.
- The companion methods are additive and stable; removing them later would be
  another breaking change.
