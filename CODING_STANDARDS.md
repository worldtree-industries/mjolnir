# Coding Standards

Rules for the review agent (the implementation agent is steered by the linter config and the ADRs).

## Verify before disputing

Before flagging a technical claim in a spec or ADR — a language-version requirement, an API semantic, tooling behaviour — verify it against primary evidence: compile the code at the stated language level, or read the language spec. Cite that evidence in the finding, and report only what is verified; where evidence is unavailable, say so explicitly.

## Behaviour beyond the spec is a finding

Every behaviour the diff adds traces to a line in its issue or spec: an extra validation, header, default, special case, or rejected input that no line asked for is a surprise for the caller, however small or defensive. Report each one as a finding that must be resolved before merge, in one of two ways: remove it, or amend the issue so the behaviour is specified. When removing it would itself surprise (a panic in place of an error, a protocol violation, divergence from the library's own defaults), name that trade-off in the finding so the author can choose.

## A corrected claim is corrected everywhere

When a change replaces an explanation — a root cause, an API behaviour, the reason a line exists — the old explanation usually survives somewhere else: a comment above the code it explains, an ADR, `AGENTS.md`, a PR body. Grep the repository for a distinctive phrase from the old claim and require the diff to fix every copy. A corrected ADR next to a stale comment is worse than the original mistake, because the next reader trusts whichever they hit first.
