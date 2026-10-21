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

A design review of the initial implementation revised the nil contract: a present
Option should never hold a nil, and the type is intended for value types. The
check runs in `Of` itself, via reflection — the only mechanism that works for a
generic `T` (no `== nil` comparison for unconstrained type parameters, no
standard method on `*T`). A split design (unconditional `Of` plus a checked
`OfPtr[T, P ~*T]`) was rejected: it left `Of[*T](nil)` as an unchecked bypass of
the contract. The reflection check was accepted after measurement: ~4 ns per
call, zero allocations.

## Decision

Presence is explicit at every call site, using Go's comma-ok idiom for access and Go's
error convention for construction:

- `Of[T any](val T) (Option[T], error)` — present, or `ErrNilValue` when value is
  a nil pointer, map, slice, channel, function, or interface: a present Option
  never holds a nil. This is Java's `of()` NPE guard translated to Go's error
  convention. For value types — the intended `T` — the error never occurs; the
  check costs ~4 ns and no allocations (measured).
- A checked two-type-parameter constructor (`OfPtr[T, P ~*T]`, using the
  reflection-free `p == nil`) was the rejected alternative: the split left plain
  `Of[*T](nil)` as an unchecked bypass of the nil contract, and the measured
  cost did not justify a second constructor.
- `Empty[T any]() Option[T]` — absent.
- `Get() (T, bool)` — comma-ok access; replaces the silent zero-value `Get() T`.
- `IsPresent() bool` — replaces `IsEmpty()`.
- The zero value of `Option[T]` is an empty Option, so an uninitialized variable is
  absent rather than present-holding-a-zero.
- Companions: `Map`, `Filter`, `OrElse` (eager default), `OrGet` (lazy default) —
  Java's `map` / `filter` / `orElse` / `orElseGet`. Accepted over a minimal
  comma-ok-only surface: the library ships its convenience surface once. `Map`
  applies `f` to the present value; if `f` returns a nil, the result is an empty
  Option (Java's `Optional.map` null→empty semantics, via `Of`'s check).
- `New` and `Some` removed (redundant constructors); `IsEmpty` removed (replaced by
  `IsPresent`).
- `OfNullable[T, P ~*T]` (nil → empty) was dropped: it would treat a nil pointer
  as a silent success, which the nil contract forbids. The `*T` entry point that
  survived the nil-contract review is `OfPtr` above: same shape, but nil is an
  error, not an empty.
- `FlatMap` and `IfPresent` omitted: redundant with comma-ok at call sites; add when
  a concrete consumer needs them.

## Consequences

- Empty access can no longer yield a silent zero value; absence is explicit at every
  call site, and the test suite is rewritten against the new contract.
- `Of` callers handle a returned error. The nil check runs on every `Of` call
  (~4 ns, zero allocations, measured); the invariant holds on every path — there
  is no unchecked constructor. `Map` yields an empty Option when its function
  returns a nil.
- `Map` carries its own type parameter, making it a generic method, which requires
  the go1.27 language level. The module's `go` directive moves 1.25.0 → 1.27.0 and
  CI's Go version moves 1.23 → 1.27; consumers must build with Go ≥ 1.27.
- The API is Java-`Optional`-shaped; Go reviewers may question the functional
  companions on a value type — this ADR is the standing answer.
- Breaking for any v1.x consumer of `utils/optional`; accepted, since v2.0.0 already
  removed the package and no external consumers were identified.
- The companion methods are additive and stable; removing them later would be
  another breaking change.
