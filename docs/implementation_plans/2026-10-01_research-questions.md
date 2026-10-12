# Research Questions Board

**Started:** 2026-10-01
**Goal:** answer the six questions in [README.md](../../README.md) with sourced findings in
`docs/`, then record the resulting decisions as ADRs.

Status markers and icons follow the `docs` rule.

## Tasks

- ✅ **Q1 — Receiving the harness.** [EXECUTED] — findings in
  [harness-management.md](../harness-management.md).
- ✅ **Q2 — Extending without breaking global rules.** [EXECUTED] — same document.
- ✅ **Q3 — Using services through the harness.** [EXECUTED] — same document.
- ✅ **Q4 — Provider flexibility and private inference.** [EXECUTED] — same document.
- ✅ **Q5 — Mixing cloud and private inference.** [EXECUTED] — same document.
- ✅ **Q6 — Adopt existing solutions or build.** [EXECUTED] — same document.
- ✅ **S1 — Seed the harness.** [EXECUTED] — generic layer copied from `domain_mesh`
  (`strict-execution`, `engineering-principles`, `git`, `communication`, `docs`,
  `mermaid-diagrams`; skills `commit`, `adr`; settings deny list). Stack-specific rules
  (Go, Flutter, Temporal, Terraform, hexagonal, local stack) deliberately left out.

## Open points

- O1 — Q3 "how to use services" was read as *tool servers and shared internal services reached
  through the harness* (MCP servers, gateways). If it meant something else (for example
  Anthropic's managed services), say so and the section is redone.
- O2 — The checkout is not a git repository yet; the first `/commit` block will say so.
- O3 — Further open points (version-gated features, benchmark figures) are
  listed at the end of [harness-management.md](../harness-management.md).
