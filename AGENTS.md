## Agent skills

### Issue tracker

GitHub Issues (`gh` CLI). See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical labels (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `CONTEXT.md` + `docs/adr/`. See `docs/agents/domain.md`.

## Tooling

- Lint: `make install-lint && make lint` (golangci-lint; version pinned in the Makefile and `.github/workflows/ci.yml`)
- Fresh clone: `make setup-hooks` (pre-commit runs build, vet, tests, and lint when installed)

## Commits

Commit messages are plain prose (e.g. `Redesign Option[T] around explicit presence`); no type or scope prefixes — not `feat:`, `fix:`, `chore:`, or `optional:`.