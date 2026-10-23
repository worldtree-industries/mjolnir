# Release on merge, labelled by the pull request

## Status

Accepted.

Amended by ADR-0007: on the v1 line, minor releases are the breaking channel, so a
breaking change is labelled `semver:minor` and never shipped as a patch. `semver:major`
remains available for a future `/v2` rename; it is not used in this round.

## Context

Releases were cut by hand: 14 lightweight tags from `v0.0.1` to `v1.5.0`, each one a decision someone made at a keyboard. `ci.yml` already ran on `push: [main]`, so the state that would be released was verified, but nothing connected "that run was green" to "a tag exists" — the two facts drifted apart and were reconciled by memory.

Automating the number is harder than automating the tag, because the bump cannot come from the commit message. `AGENTS.md` requires plain-prose commits with no type or scope prefixes, which is exactly the signal conventional-commits tooling reads. Three other homes for it were considered:

- **Commit message conventions.** Rejected: it contradicts a rule the repository already keeps, and rewriting history to please a release script is backwards.
- **A bump file committed with each change.** Rejected: consuming it means deleting it in a follow-up commit to `main`, which is itself a push to `main`, which re-runs the workflow.
- **Always patch, majors and minors by hand.** Rejected as a complete answer: it leaves the common "this is a feature" case manual, which is most of the releases.

Major versions also carry a structural requirement that a number alone cannot express. Go modules require the `/vN` suffix on the module path for `N >= 2`; a `v2` tag on a module path without it is inert, because `go list -m -versions` never lists it — the reason ADR-0005 concluded major bumps were off the table. ADR-0005's premise no longer holds as written (no `v2.0.0` tag exists in this repository any more), but the underlying rule does: **a major release is a rename, not a digit.**

## Decision

- **Pull-request labels carry the bump.** `semver:major` and `semver:minor`; anything else is a `patch`. The default is deliberately the smallest step: a forgotten label ships a feature in a patch release, which is conservative and harmless, while the reverse would publish an inflated version that consumers act on.
- **A second workflow releases, and it only ever verifies and tags.** `.github/workflows/release.yml` runs on `push: [main]` with three jobs: `Verify` (the same `make verify` as the PR workflow), `Version guard` (decides whether this push releases at all, and at which version), and `Tag` (`needs:` both, and does nothing but push the tag it is handed). It also runs on `workflow_dispatch`, which takes the same decision but publishes nothing, because `Tag` is push-only. `ci.yml` consequently triggers on `pull_request` only, so nothing runs twice.
- **Only merges release.** `Version guard` ignores a push whose head commit is not a merge commit, and a commit that already carries a tag. The pull request number is read from the merge commit's subject (`Merge pull request #N from …`), so finding it costs no API call. Labels then come from `GET /repos/{owner}/{repo}/issues/{n}/labels` — the issue form of the pull request. There is no `/pulls/{n}/labels` endpoint; asking for one returns `404` on every request, which is what failed this repository's first two release runs before the endpoint was corrected. A merge commit whose subject has been customised fails the job loudly instead of releasing nothing. A non-releasing push is still verified — an unverified `main` is worse than an untidy workflow — but it never moves the version. Labels are the one fact only the API holds, so reading them retries three times, with the API's own message in the log, before the job fails rather than guessing a version. The whole decision is `scripts/release-plan.sh`, so the push workflow, the dry runs, and a laptop run the same code.
- **The base is the highest tag reachable from `HEAD`**, not the highest tag in the repository, so a tag cut on an abandoned branch cannot poison the sequence. `v0` and `v1` take no module-path suffix; `v2` and above do.
- **The version guard is a check, not a convention.** `scripts/check-major-version.sh` asserts that the module path's `/vN` suffix matches the major version the requested bump would publish, in both directions: a `semver:major` PR must rename the module path to `/v2`, and a `/v2` module path must be released as `v2`. It runs as the blocking `Version guard` job on every PR, and again in `release.yml` before anything is tagged, so the tag job cannot publish a version the guard would reject.
- **Tags stay lightweight and the job is idempotent.** Every existing tag is lightweight; there is no annotation consumer to justify changing that. An already-tagged `HEAD` skips *before* the guard is consulted, because a second run would otherwise bump from the tag it just placed and demand the next major. A retry can never mint a second tag, and it does not turn green into red either.
- **`make verify` is the single definition of green.** `build`, `vet`, `lint`, `lint-workflows`, and `test-race`, called by the pre-commit hook, the PR workflow, and the release workflow. The Makefile holds the only pins — `golangci-lint` and `actionlint`; the former `version:` input in `ci.yml` is gone, which removes the drift that a "keep in sync" comment used to police by hand. The Go version is still per-workflow, so the toolchain check now asserts agreement across `go.mod`, every workflow, and `README.md` rather than `ci.yml` alone.

## Considered Options

- **`pull_request: [closed]` with `if: merged`, reading labels from the payload.** Tempting: labels arrive directly and direct pushes are excluded by construction. Rejected because `pull_request`-derived tokens are read-only for fork-originated pull requests, so the tag push would fail exactly when the change came from outside the organisation. `push` always carries the repository's own token.
- **A `workflow_run` chain: `ci.yml` on both triggers, `tag.yml` when it completes.** Avoids running `verify` twice, but couples two files and replaces an explicit `needs:` gate with an implicit one. Rejected.
- **Tagging inside the same job as `verify`.** Would let a tagging failure report itself as a test failure. Rejected; the `Tag` job is separate and runs only after `verify` succeeds.
- **Branch-name-based release channels (`main` → stable, `beta` → prerelease).** No such branches exist and none are planned. Rejected as speculative.

## Consequences

- **Required checks now gate merges.** Branch protection must require `Verify`, `Version guard`, `Release plan (dry run)`, and `Toolchain consistency`. Until it does, "CI is green" is advice rather than a gate; the guard against a wrong major is only as strong as that setting, and a release path that cannot read labels is only caught after a merge.
- **A `semver:major` merge is a rename plus a release in one PR.** The module path, `go.mod`, and every in-repo import change together, then the merge publishes `v2.0.0`. Nothing enforces the import rewrite beyond `go build ./...` inside `verify`, which is sufficient because the imports are all in this module.
- **A mislabelled PR publishes the wrong version, and that is public.** Tags are cheap to create and awkward to withdraw once consumers resolve them. The guard catches structural mismatches, not mistakes of judgement — the label is the human's claim about compatibility.
- **Docs-only merges cut a patch release.** Untidy, and accepted: path filters are the classic way a release silently fails to happen.
- **A customised merge commit message breaks the release.** The pull request number lives in the subject GitHub writes; if it is edited away, the merge verifies but does not release, and the job fails saying so. That is the price of reading the number from the subject instead of asking the API.
- **The release decision is exercised before every merge, not after.** `ci.yml` runs `scripts/release-plan.sh` against each pull request's own labels, and `workflow_dispatch` runs it on `release.yml` without publishing. It used to live inline in the workflow, where only a real merge could run it: an endpoint that has never existed (`/pulls/{n}/labels`) survived review and two green pull requests, and failed two releases. The dry run costs seconds per pull request; `make lint-workflows` keeps the workflow files themselves honest.
- **Tags pushed by `GITHUB_TOKEN` do not trigger other workflows.** Nothing listens on tags today. The first workflow that does — generated release notes, say — needs a PAT instead, and that is the moment this decision gets revisited.
- **`go install` of the linter is on the critical path of both workflows.** Warm `setup-go` caches make it fast; a cold cache adds roughly a minute to each run. The alternative was a second pin, which is the drift this decision removes.
