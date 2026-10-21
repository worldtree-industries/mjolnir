# Coding Standards

Rules for the review agent (the implementation agent is steered by the linter config and the ADRs).

## Verify before disputing

Before flagging a technical claim in a spec or ADR — a language-version requirement, an API semantic, tooling behaviour — verify it against primary evidence: compile the code at the stated language level, or read the language spec. Cite that evidence in the finding, and report only what is verified; where evidence is unavailable, say so explicitly.
