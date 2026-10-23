# ADR-0009: `utils/set` is retained as exported surface

## Status

Accepted.

## Context

An architecture review proposed deleting `utils/set`: its nine-method interface has one
in-repo implementation, no non-test importer in this repository, and 596 lines of tests.
The deletion test passes inside this module. The maintainer uses the package extensively
in applications built on this library, which are not part of this repository and were not
visible to the review.

## Decision

Keep `utils/set` as it is. The interface stays the public shape; the one in-repo
implementation is not evidence that the seam is hypothetical, because consumers outside
this repository use it.

No change is made to the operations, the interface, or the tests as part of the
pre-adoption review.

## Consequences

- Future architecture reviews must not re-suggest deleting `utils/set` on the basis of
  in-repo import counts alone. Consumption outside the repository is the relevant fact.
- The interface remains surface a consumer must read. That cost is accepted.
- If the package is ever changed, its tests are the contract for external users, so they
  stay.
