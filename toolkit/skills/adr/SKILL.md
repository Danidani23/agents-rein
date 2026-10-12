---
name: adr
description: Write an Architecture Decision Record in docs/decisions/ — when one is warranted, the file name, the template, and the supersede-never-delete rules. Use when a significant or hard-to-reverse technical decision has just been made or is being reversed.
---

# Write an ADR

ADRs record the immutable **"why"** behind significant technical decisions; task boards in
`docs/implementation_plans/` are the living "how" (`implementation-plan` skill).

## When

Write one when a decision:

- affects the overall architecture (framework, hosting, data store, the API, authentication,
  deployment and release flow),
- is hard to reverse later,
- required significant research or debate, or
- would make a future team member ask "why did we do it this way?".

Product decisions — what the product offers, to whom, what it says — are not ADRs: they are
decisions on a board in `docs/implementation_plans/`.

## File

`docs/decisions/YYYY-MM-DD_short-description.md` — the date the decision was **made**, lowercase
kebab-case description.

## Template

```markdown
# [Title]

**Date:** YYYY-MM-DD
**Status:** Accepted | Superseded by [link] | Deprecated

## Context
What is the problem or situation that led to this decision?

## Candidates Considered
Brief summary of each option evaluated, with key pros/cons.

## Decision
What was decided and why.

## Consequences
What are the trade-offs? What does this enable or prevent?
```

## Rules

- **Never delete an ADR** — when reversed, mark it `Superseded by [new ADR]` and write the new one.
- **One decision per file** — don't bundle unrelated decisions.
- **Keep it concise** — link to external docs for deep dives.
- Diagrams are mermaid fenced blocks, never ASCII art (`mermaid-diagrams` skill).
