# Coding Standards

Rules for the review agent (the implementation agent is steered by the linter config and the ADRs).

## Verify before disputing

Before flagging a technical claim in a spec or ADR — a language-version requirement, an API semantic, tooling behaviour — verify it against primary evidence: compile the code at the stated language level, or read the language spec. Cite that evidence in the finding, and report only what is verified; where evidence is unavailable, say so explicitly.

## A corrected claim is corrected everywhere

When a change replaces an explanation — a root cause, an API behaviour, the reason a line exists — the old explanation usually survives somewhere else: a comment above the code it explains, an ADR, `AGENTS.md`, a PR body. Grep the repository for a distinctive phrase from the old claim and require the diff to fix every copy. A corrected ADR next to a stale comment is worse than the original mistake, because the next reader trusts whichever they hit first.
