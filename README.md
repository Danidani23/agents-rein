# Reins

Reins keeps the settings of your AI coding agents in one place and brings them to every project
you work in, whatever agent tool you use and whichever model provider it talks to. Switching
tool or provider then changes nothing about how you work.

Agent settings are the instructions, rules, skills, tool servers, hooks and permissions an
agent reads before it does anything. Today they are scattered: copied into every repository,
drifting apart, written in one vendor's format, and lost when the repository changes hands.

## Who it is for

- **One developer with several clients and kinds of projects.** Your own conventions follow you
  everywhere; a client's conventions apply in that client's repositories only; a static site
  and a Go service get different skills. Labels on projects decide which settings apply where.
- **An organisation with many teams.** The organisation's rules hold everywhere, each team adds
  its own, every change is reviewed as a pull request by the people who own that level, and a
  lint runs before anything is distributed.

## How it works

Settings are declarations in one git repository per organisation, the org repository, at four
levels: organisation, team, project and user. Reins validates them (`reins lint`), resolves
which apply to a project, and writes them into each clone in the form the agent tool reads.

The principle: **agent settings are not repository settings.** A project repository contains
the software alone. What reins writes into a clone is never committed; the only trace is a
marked block in `.gitignore`. A fresh clone gets back on track with one `reins sync`. To hand
your settings to someone, `reins export` makes a zip.

Declarations are written in open formats, AGENTS.md, Agent Skills and MCP, so another agent
tool is another output of the compiler, not a rewrite of your settings. Claude Code is the
first target. Which model provider or gateway a project talks to is one of the settings reins
carries; the switch itself happens at the gateway, which reins never replaces.

## Status

| Command | State |
|---|---|
| `reins lint <path>` | works: skills against the Agent Skills specification, rule files, permission rules, skills sharing a name |
| `reins config`, `reins org connect`, `list`, `use`, `disconnect`, `reins version` | work: machine setup, organisations, the user level into the Claude home |
| `reins init`, `reins sync`, `reins org create`, `reins export`, `reins reset` | next, in the board's order |

The reasoning is the [ADR](docs/decisions/2026-10-11_harness-tool-compiler-and-linter.md); the
design, the decisions and what is next are on the
[board](docs/implementation_plans/2026-10-11_harness-distribution-tool.md). The code is Go,
with cobra and yaml.v3 as the only dependencies:

```bash
go build -o reins ./cmd/reins
go vet ./... && go test ./...
```

## The research behind it

This repository began as research: how does a large engineering organisation manage
AI-assisted development across many teams? Six questions, answered with sources in
[docs/harness-management.md](docs/harness-management.md):

- **Q1 — Receiving the harness.** How dev teams receive a shared harness with as little
  friction as possible.
- **Q2 — Extending without breaking.** How a team extends it for its own stack without breaking
  the organisation-wide rules.
- **Q3 — Using services.** How teams use shared services, tool servers, internal APIs and
  gateways, through the harness.
- **Q4 — Provider flexibility.** How to stay free to change model provider or move to private
  inference.
- **Q5 — Mixing providers.** Whether cloud and private inference can be mixed.
- **Q6 — Adopt or build.** What exists, and what to build. The answer, adopt the vendor layers
  and build only a compiler and a linter, is what Reins is.

## Where things are

- [docs/harness-management.md](docs/harness-management.md) — the findings, one section per
  question.
- [docs/decisions/](docs/decisions/) — Architecture Decision Records, the immutable "why".
- [docs/implementation_plans/](docs/implementation_plans/) — the live boards: one for Reins,
  one for the personal toolkit.
- [docs/future-ideas.md](docs/future-ideas.md) — ideas parked on purpose, with what would make
  them current.
- [cmd/reins/](cmd/reins/) and [internal/](internal/) — the tool.
- [toolkit/](toolkit/) — the personal agent toolkit: global instructions and ten skills in open
  formats, the content of the pilot. Today `reins config` places it into `~/.claude/`; it moves
  into the pilot's org repository when `reins sync` delivers the personal level per project
  ([ADR](docs/decisions/2026-10-10_personal-toolkit-open-formats.md),
  [board](docs/implementation_plans/2026-10-10_agent-toolkit.md)).
- [CLAUDE.md](CLAUDE.md) and [.claude/](.claude/) — the harness this repository runs with: its
  own documentation rule and permissions; the generic layer comes from the toolkit.
