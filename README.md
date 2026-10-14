# AI-Assisted Development for Large Teams

How does a large engineering organisation manage AI-assisted development across many dev
teams? This repository collects the research and the decisions.

## The questions

- **Q1 — Receiving the harness.** How can dev teams receive a shared harness configuration
  (instructions, rules, skills, hooks, permissions, tool servers) with as little friction as
  possible?
- **Q2 — Extending without breaking.** How can a team extend the harness for its own stack
  without breaking the organisation-wide rules?
- **Q3 — Using services.** How do teams use shared services (tool servers, internal APIs,
  gateways) through the harness?
- **Q4 — Provider flexibility.** How do we stay free to change model provider, or move to
  private inference?
- **Q5 — Mixing providers.** Can cloud and private inference be mixed?
- **Q6 — Adopt or build.** Do solutions for this exist, or should we develop our own?

## Where things are

- [docs/harness-management.md](docs/harness-management.md) — the findings, one section per question.
- [docs/decisions/](docs/decisions/) — Architecture Decision Records (the immutable "why").
- [docs/implementation_plans/](docs/implementation_plans/) — the live boards.
- [docs/future-ideas.md](docs/future-ideas.md) — ideas parked on purpose, with what would make
  them current.
- [cmd/reins/](cmd/reins/) and [internal/](internal/) — reins, the harness distribution tool:
  a compiler and a linter for agent configuration, in Go. `go vet ./... && go test ./...` is
  its gate ([ADR](docs/decisions/2026-10-11_harness-tool-compiler-and-linter.md),
  [board](docs/implementation_plans/2026-10-11_harness-distribution-tool.md)).
- [toolkit/](toolkit/) — the personal agent toolkit: global instructions and skills, and
  `install.sh`, which links them into `~/.claude/` for every repository on the machine
  ([ADR](docs/decisions/2026-10-10_personal-toolkit-open-formats.md),
  [board](docs/implementation_plans/2026-10-10_agent-toolkit.md)).
- [CLAUDE.md](CLAUDE.md) and [.claude/](.claude/) — the harness this repo runs with; it is also
  a worked example of the generic layer of a harness.
