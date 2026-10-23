# ADR-0007: Minor releases are the v1 breaking channel

## Status

Accepted. Amends [ADR-0006](0006-release-on-merge.md)'s assumption that a major release
is the normal way to ship an incompatibility, and retires ADR-0005's note that major bumps
are off the table: they remain possible as a `/v2` rename.

## Context

The pre-adoption review changes public shapes across `router`, `middleware`, `health`, and
`utils/*`: the request-identity policy, the access-log and response-writing modules, the
config loader, and the log name API. Under SemVer these are major changes, but ADR-0006
makes a major release a module-path rename to `/v2`, which touches every import in this
repository and every service that adopts it.

Adoption is at its lowest point now. Three strategies were considered: cut `/v2`
immediately, expand–contract on the v1 line with deprecation aliases, or a v1 line whose
minor releases may break. The last is chosen for this round, not forever: a later `/v2`
remains a legitimate option once the accumulated breaks justify the rename.

## Decision

- **Stay on v1 for now.** The module path stays `github.com/dfryer1193/mjolnir`. This is
  not a promise that v2 will never exist; it is a decision not to pay for the rename in
  this round.
- **Minor releases are the breaking channel on v1.** A minor bump may remove or change
  public shapes. Breaking changes carry `semver:minor`; features are minor; fixes are
  patch. No breaking change ships as a patch.
- **No compatibility aliases.** The new shape replaces the old one. Expand–contract is
  not used to keep the incompatibility invisible.
- **Consumers pin exact versions.** `go get …@latest` is not a safe upgrade path; adopters
  read the release notes for the version they move to.
- **The rule is a weak convention.** It is stated in `AGENTS.md` and the pull-request
  template, not enforced by a script. `semver:major` remains structurally available and
  the version guard is unchanged, because a future `/v2` is a rename plus a major label.

## Consequences

- This knowingly departs from SemVer's rule that incompatible changes require a major
  version. Go's module tooling will not warn a consumer, so the release notes and the
  pull-request body must name the break.
- The default release label is still patch (ADR-0006), so a breaking change labelled by
  mistake ships as a patch. The mitigation is the PR checklist and release notes; the
  label itself cannot be verified mechanically.
- A consumer upgrading across a minor may need code changes. That is stated up front
  rather than discovered at compile time after adoption.
- If a later v2 is cut, it supersedes this ADR and follows ADR-0006's rename path. Until
  then, v1 is the only published line.
- The database packages stay in the root module ([ADR-0011](0011-database-stays-in-root.md)),
  so this decision does not carry a module split with it.
