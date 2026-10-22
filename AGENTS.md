## Agent skills

### Issue tracker

GitHub Issues (`gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical labels (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `CONTEXT.md` + `docs/adr/`. See `docs/agents/domain.md`.

## Tooling

- Verify: `make verify` (build, vet, lint, race tests) — the same target pre-commit and CI run
- Lint alone: `make install-lint && make lint` (golangci-lint; the Makefile holds the only version pin, and CI installs from it)
- Fresh clone: `make install-lint && make setup-hooks` (pre-commit runs `make verify`, which needs the linter)
- Releases: merging a PR to `main` tags it; `semver:major` / `semver:minor` labels set the bump, otherwise it is a patch — ADR-0006

## Branches

Branch names never contain slashes. Use hyphens: `add-health-checks`, not `feature/add-health-checks` or `fix/xyz`. A slash reads as a hierarchy, and nothing here needs one.

## Commits

Commit messages are plain prose (e.g. `Redesign Option[T] around explicit presence`); no type or scope prefixes — not `feat:`, `fix:`, `chore:`, or `optional:`.
