## Agent skills

### Issue tracker

GitHub Issues (`gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical labels (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `CONTEXT.md` + `docs/adr/`. See `docs/agents/domain.md`.

## Tooling

- Verify: `make verify` (build, vet, lint, workflow lint, race tests) — the same target pre-commit and CI run
- Lint alone: `make install-lint && make lint` (golangci-lint; the Makefile holds the only version pin, and CI installs from it)
- Workflows alone: `make lint-workflows` (actionlint, same pin rule)
- Fresh clone: `make install-lint && make setup-hooks` (pre-commit runs `make verify`, which needs both linters)
- Releases: merging a PR to `main` tags it; `semver:major` / `semver:minor` labels set the bump, otherwise it is a patch — ADR-0006. The decision is `scripts/release-plan.sh`; `gh workflow run release.yml` prints what a release would do, without publishing.

## Worktrees

Work in a worktree, never in the primary checkout. `main` is checked out there, so it is
stale the moment another change lands: `git diff main` compares against an old revision,
not the trunk. Base work on `origin/main`, and read `fatal: 'main' is already used by
worktree` after a merge as harmless — the local branch deletion failed, nothing else did.

## Commits

Commit messages are plain prose (e.g. `Redesign Option[T] around explicit presence`); no type or scope prefixes — not `feat:`, `fix:`, `chore:`, or `optional:`.
