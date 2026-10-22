<p>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/rein-logo-dark.svg">
    <img src="docs/images/rein-logo.svg" alt="Agents Rein" width="240">
  </picture>
</p>

# Agents Rein

Agents Rein keeps the settings of your AI coding agents in one place and brings them to every
project you work in, whatever agent tool you use and whichever model provider it talks to.
Switching tool or provider then changes nothing about how you work.

Agent settings are the instructions, rules, skills, tool servers, hooks and permissions an
agent reads before it does anything. Today they are scattered: copied into every repository,
drifting apart, written in one vendor's format, and lost when the repository changes hands.

## The thesis

**Code and agent settings are two different things, and they do not belong in the same
repository.** The code is what a project is; the settings are how a person, a team and an
organisation want their agents to work, this month, with this tool. Settings change on
their own rhythm, are owned by other people than the code, and are the same across many
repositories. Committed into each repository they drift apart, carry one vendor's format into
the history, and travel with a fork or a hand-over that never asked for them.

So Agents Rein keeps the harness in one central repository per organisation, and configures
it for every single project from four sources: what the organisation requires, what the team
adds, what the project itself needs, and what you prefer. A project repository holds the code
alone; the harness is generated into each clone, never committed, and regenerated at every
session start.

## Who it is for

- **One developer with several clients and kinds of projects.** Your own conventions follow you
  everywhere; a client's conventions apply in that client's repositories only; a static site
  and a Go service get different skills.
- **An organisation with many teams.** The organisation's rules hold everywhere, each team adds
  its own, every change is a reviewed pull request, and a lint runs before anything is
  distributed.

## How it works

- Settings are declarations in one git repository per organisation, the org repository, at
  four levels: organisation, team, project and user. Every change is a pull request there.
- Labels on a project, such as `language:go` or `kind:service`, decide which declarations
  apply to it.
- `rein` lints the declarations, resolves the levels for a project and writes the result into
  each clone in the form the agent reads. Nothing it writes is committed: agent settings are
  not repository settings.
- A harness that already exists in a repository is distilled into declarations, and a judge
  suggests the level each one belongs to.
- It is a compiler and a linter, never a service, and never deletes a repository.

## Quick start

Install the binary, then sign in to GitHub through the Agents Rein app:

```sh
task rein:install
rein login
```

Create the organisation's repository, a team, and put a project clone under rein:

```sh
rein org create
rein team create
cd <a project clone> && rein init
```

Deliver the settings, then turn the harness `init` moved out into a proposal file, review it,
and open the pull request:

```sh
rein sync
rein distill
rein propose
```

Every command asks what it needs and explains itself with `?`; `rein restore`, `rein org
destroy` and `rein reset` take everything back. The full list is in
[docs/commands.md](docs/commands.md).

## Supported agents

Claude Code today: instructions, rules, skills, tool servers, hooks and permissions. Codex,
Cursor, GitHub Copilot, Gemini CLI and OpenCode are planned; each reads the same open formats,
so each is another output of the same compiler ([findings, Q4](docs/harness-management.md#harness-portability-across-tools)).

## Read more

- [docs/concepts.md](docs/concepts.md) — the model: the levels, the labels, what lives on your
  machine, the supported agents.
- [docs/commands.md](docs/commands.md) — every command and what it touches.
- [docs/harness-management.md](docs/harness-management.md) — the research this began as: six
  questions about AI-assisted development in a large organisation, answered with sources.
- [docs/decisions/](docs/decisions/) — the Architecture Decision Records, the immutable "why".
- [docs/architecture.md](docs/architecture.md) — how the code is organised and where a new
  piece goes.
- [docs/implementation_plans/](docs/implementation_plans/) — the live boards.
- [docs/future-ideas.md](docs/future-ideas.md) — ideas parked on purpose.

## Licence

Apache-2.0, see [LICENSE](LICENSE).
