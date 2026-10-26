<p>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/rein-logo-dark.svg">
    <img src="docs/images/rein-logo.svg" alt="Agents Rein" width="240">
  </picture>
</p>

# Agents Rein

*The harness of all harnesses.*

[![check](https://github.com/Danidani23/agents-rein/actions/workflows/check.yml/badge.svg?branch=main)](https://github.com/Danidani23/agents-rein/actions/workflows/check.yml)
[![release](https://img.shields.io/github/v/release/Danidani23/agents-rein)](https://github.com/Danidani23/agents-rein/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/Danidani23/agents-rein)](go.mod)
[![licence](https://img.shields.io/github/license/Danidani23/agents-rein)](LICENSE)

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
alone; the harness is generated into each clone, never committed, and brought up to date at
the start of every chat session with the agent.

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
- A harness that already exists in a repository is distilled into declarations: a distiller cuts
  it into units of one concern and suggests the level each belongs to.
- It is a compiler and a linter, never a service, and never deletes a repository.

## Install

macOS or Linux, with Homebrew:

```sh
brew install --cask Danidani23/tap/rein
```

macOS or Linux, with the script, which verifies the checksum and puts `rein` into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/Danidani23/agents-rein/main/install.sh | sh
```

Windows, with Scoop:

```sh
scoop bucket add danidani23 https://github.com/Danidani23/scoop-bucket
scoop install rein
```

Windows, with the script:

```powershell
irm https://raw.githubusercontent.com/Danidani23/agents-rein/main/install.ps1 | iex
```

Anywhere with Go:

```sh
go install github.com/Danidani23/agents-rein/cmd/rein@latest
```

The `.deb`, `.rpm` and `.apk` packages and every other way are in
[docs/commands.md](docs/commands.md).

## Quick start

Sign in to GitHub through the Agents Rein app:

```sh
rein login
```

Create the organisation's repository, a team, and put a project clone under rein:

```sh
rein org create
rein team create
cd <a project clone> && rein init
```

Deliver the settings, then turn the harness `init` moved out into a proposal, review it,
and open the pull request:

```sh
rein sync
rein distill
rein propose
```

Every command asks what it needs and explains itself with `?`; `rein reset project`, `rein org
destroy` and `rein reset machine` take everything back. The full list is in
[docs/commands.md](docs/commands.md).

## Supported agents

Claude Code today: instructions, rules, skills, tool servers, hooks and permissions. Codex,
Cursor, GitHub Copilot, Kimi Code CLI, Gemini CLI and OpenCode are planned; each reads the same
open formats, so each is another output of the same compiler
([findings, Q4](docs/harness-management.md#harness-portability-across-tools)).

## Which model distils

The distiller runs on your own `claude` login unless the organisation names a model endpoint in
`declarations/providers.yaml` of its repository. Any endpoint that speaks Anthropic's Messages
API serves: Anthropic's own, DeepSeek's ([docs](https://api-docs.deepseek.com/guides/anthropic_api))
or Moonshot's for Kimi. The file names the variable that holds the key, never the key; each
developer exports that variable.

```yaml
distiller:
  protocol: anthropic
  url: https://api.deepseek.com/anthropic
  model: deepseek-v4-pro
  key_variable: DEEPSEEK_API_KEY
```

For Kimi, `url: https://api.moonshot.ai/anthropic` with `auth: bearer`, which so far only
third parties document ([guide](https://www.developersdigest.tech/reference/api-compatibility/moonshot)).
An organisation that declares a provider has everyone distil through it, so the brief goes where
the organisation decided; `rein distill --distiller none` still skips the model. DeepSeek has no
coding agent of its own to support yet; its models distil.

## Read more

- [docs/concepts.md](docs/concepts.md) — the model: the levels, the labels, what lives on your
  machine, the supported agents.
- [docs/commands.md](docs/commands.md) — every command and what it touches.
- [docs/harness-management.md](docs/harness-management.md) — the research this began as: six
  questions about AI-assisted development in a large organisation, answered with sources.
- [docs/decisions/](docs/decisions/) — the Architecture Decision Records, the immutable "why".
- [docs/architecture.md](docs/architecture.md) — how the code is organised and where a new
  piece goes.
- [docs/developing.md](docs/developing.md) — building, testing and releasing rein: the tasks,
  how a change is made, how a version is cut.
- [docs/implementation_plans/](docs/implementation_plans/) — the live boards.
- [docs/future-ideas.md](docs/future-ideas.md) — ideas parked on purpose.

## Licence

Apache-2.0, see [LICENSE](LICENSE). The licence does not cover the names "rein" and "Agents Rein"
(its section 6): a fork may use, change and sell the code, under a name of its own.
