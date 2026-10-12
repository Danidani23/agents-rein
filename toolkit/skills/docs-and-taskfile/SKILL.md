---
name: docs-and-taskfile
description: Documentation and Taskfile conventions — README as the entry point, ADRs in docs/decisions, boards in docs/implementation_plans, reference instead of duplicate, Mermaid only, every repeatable command as a named Taskfile task, and the post-change checklist. Use when writing or editing README.md, AGENTS.md or CLAUDE.md, a deploy manual, anything under docs/, or Taskfile.yml.
---

# Documentation & Taskfile Conventions

**The Taskfile is the single source of truth for every repeatable command**, and the project's
operator CLI, where it has one, for every release, infrastructure change and deploy.
Documentation names `task` targets and CLI commands and never repeats the raw shell commands
behind them.

## Documentation hierarchy

Everything starts at the root `README.md`; readers navigate top-down.

```
README.md                     ← entry point: what the project is, how it fits, the layout
├── DEPLOY.md                 ← the operator manual, where the project is deployed
├── docs/
│   ├── decisions/            ← ADRs (the immutable "why") — `adr` skill
│   └── implementation_plans/ ← task boards (the living "how") — `implementation-plan` skill
│       └── finished/         ← boards with no live work left
└── AGENTS.md / CLAUDE.md     ← index of the agent instructions
```

## Rules

1. **Never duplicate a Taskfile command in a doc.** Raw commands appear only for one-off
   diagnostics.
2. **Reference, don't duplicate.** Never copy a value into prose — it goes stale. Link the file
   that holds it: a version in `package.json`, a quota on its source's page.
3. **Diagrams are mermaid**, never ASCII art — the `mermaid-diagrams` skill says how.
4. **ADRs** for significant, hard-to-reverse decisions — the `adr` skill. Never delete one;
   supersede it.
5. **Boards before builds.** Approved multi-step work is recorded as a board in
   `docs/implementation_plans/` before it begins; markers are kept current in the same change —
   the `implementation-plan` skill.

## Taskfile rules

1. **Every repeatable command gets a task**, named `<component>:<action>` (`ui:dev`,
   `api:test`, `infra:check`), with a `desc`.
2. **`dir:` instead of `cd`**; `internal: true` for helpers.

## Post-change checklist

1. `Taskfile.yml` — tasks point to the right paths
2. `README.md` — the layout and the commands still match
3. The deploy manual — still the order the user runs things in
4. `AGENTS.md` / `CLAUDE.md` — updated when an instruction, rule or skill was added, renamed or
   removed
5. The active board — markers flipped for the work just done
