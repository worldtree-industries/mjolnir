# ADR-0011: Database support stays in the root module

## Status

Accepted.

## Context

The review proposed splitting `utils/db` and `utils/tx` into a sibling module so that
`pgx` would not be a root-module dependency. The dependency argument does not hold up:
a fresh consumer importing only `router` and `health` against published `v1.5.2` produced
a `go.mod` with only `mjolnir`, `chi`, and `uuid` — no `pgx` in the build list's selected
packages or the consumer's `go.sum` — because module graph pruning excludes a package the
consumer does not import. Nearly every service built on this library does import the
database packages, so a split would change imports for almost every adopter and add a
second module identity without removing a dependency they actually use.

## Decision

Keep `utils/db` and `utils/tx` in the root module. `pgx` remains a root requirement and
the packages are fixed in place: `tx.New` takes the pool instead of forcing `Pool.Raw()`,
partial pool configuration merges with defaults, and both packages gain tests.

The database packages may still be split later if the package is shared outside mjolnir
and needs an independent release cadence, or if the root module adopts a no-database-driver
policy. That would be a new decision, not a deferred part of this one.

## Consequences

- A consumer that imports only the HTTP packages does not build `pgx` and does not get it
  in its own `go.mod`; the root module still lists it, so root-level vulnerability scans
  and `go list -m all` see it.
- A consumer that uses the database packages gets them from the same version and release
  as the router; there is no second tag to coordinate.
- The root `go.mod` remains a single release unit. A future split is a breaking import
  change, permitted under ADR-0007.
- `go mod tidy` in the root still considers all build tags, so a build-tagged subpackage
  would not hide `pgx`; only a real module boundary would.
