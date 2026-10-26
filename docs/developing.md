# Developing rein

How a change to rein is made, checked and released. The user-facing side, every command and
what it touches, is [commands.md](commands.md); the code organisation and where a new piece
goes is [architecture.md](architecture.md). This document is the developer's entry point and
repeats neither.

## What you need

| Tool | For | Check |
|---|---|---|
| Go, the version in [go.mod](../go.mod) | building and testing | `go version` |
| [Task](https://taskfile.dev) | every repeatable command in [Taskfile.yml](../Taskfile.yml) | `task --list` |
| git | the clone and the release | `git --version` |
| the GitHub CLI, logged in | the release's token, read with `gh auth token` | `gh auth status` |
| Claude Code, logged in | `rein distill`'s distiller, which runs `claude -p` | `claude --version` |

The code depends on cobra, yaml.v3 and the official MCP SDK, no more: a further module is an
[ADR](decisions/2026-10-23_mcp-gateway-on-the-official-sdk.md) before it is added, and `go.mod`
records each one's exact version. `go mod download` fetches them. GoReleaser is not
installed: the release runs it with `go run` at one pinned version, named in
[release.go](../cmd/reinctl/release.go) and in the `rein:release-check` task, and a test keeps
the two the same. Raising it is a change to both, then `task rein:release-check`, which fails on
a property the new version deprecates.

## The tasks

Every repeatable command is a task; nothing is typed from memory. `task --list` prints them
with their descriptions; the ones a developer reaches for:

| Task | What it does |
|---|---|
| `task rein:build` | builds rein into `.tmp/rein` for a try without installing it |
| `task rein:install` | installs rein into the Go bin folder and checks that the shell finds this copy, not another |
| `task rein:test` | vets and tests every package |
| `task rein:test-keychain` | round-trips a token and a key through this machine's real keychain, writing a test entry and removing it; CI runs it on all three systems |
| `task rein:coverage` | tests with statement coverage, prints the summary and leaves the report in `.tmp/coverage.html` |
| `task rein:lint` | runs `rein lint` on this repository's own harness |
| `task rein:check` | the gate: format check, vet, tests and lint in one go |
| `task rein:release-check` | validates the GoReleaser configuration without releasing |
| `task rein:release` | cuts and publishes the next version from this machine |
| `task rein:publish` | repeats the publish for a release whose publish failed after the tag was pushed |

`task rein:check` runs before every handover and before every release; a red gate releases
nothing.

GitHub runs the checks as well, on every push to `main` and every pull request, on Linux, macOS
and Windows, the three platforms a release builds for. [check.yml](../.github/workflows/check.yml)
says which task runs where; the first badge in the [README](../README.md) shows how `main`
stands. The workflow only checks: it holds no secret and releases nothing. A release needs
its run green on the commit it is cut from, and the release commit, which changes only
`CHANGELOG.md`, is not checked again.

## The harness is not in the repository

This repository is rein's own first pilot. `rein init` moved its agent settings into the
organisation's repository, and `rein sync` writes `CLAUDE.md`, `.claude/` and `.mcp.json` back
on each machine, at session start and on demand. They are gitignored and never edited by hand:
a change to a rule or a skill is a declaration in the organisation's repository, changed with
`rein declaration edit` or proposed with `rein distill` and `rein propose`; `rein declaration
list` shows what this project resolves and from which level. The generated `rein` skill under
`.claude/skills/rein/` explains this to every agent that opens the clone.

## How the code is organised

One modular monolith in one binary: the gateway reads flags and prints, the orchestration runs
workflows and queries, the services own state and side effects behind ports, the adapters
implement the ports, and a test under `internal/architecture` keeps every dependency pointing
inward and every service apart from its siblings. The rules, the numbering, and where a new
command, service, workflow, library, port or adapter goes are in
[architecture.md](architecture.md), which the `architecture` skill loads whenever Go code
changes. The second binary, `cmd/reinctl`, is the operator CLI that releases; it is never
shipped.

## How a change is made

1. **A plan first.** No file changes without an approved plan or diff; the agent proposes, the
   human says go. The rule is in the global instructions, the user-level declaration
   `organizations.dani.users.danidani23.rules.global`, which sync inlines into
   `.claude/rein/user.md` for every session.
2. **A bug gets its test first.** A defect is reproduced in a failing test at the lowest layer
   where it can be seen, then fixed; the test stays. The procedure and its limits are the
   project's `bug-first-test` skill, which sync writes to `.claude/skills/bug-first-test/`.
3. **Fakes, not mocks.** Every outside system has a fake under `internal/adapters/fake` or a
   stub in the end-to-end test of [cmd/rein](../cmd/rein/main_test.go): a GitHub server, a
   keychain file, a `claude` that answers from a script. A test never reaches the network.
4. **The gate before the handover.** `task rein:check` is green, or the work is not done.
5. **Conventional commits, drafted by the agent and run by the human.** One commit per
   concern, every file staged by its path, the message in the form `type(scope): what changed`.
   The type decides the next version: a breaking change raises the major, `feat` the minor,
   `fix` and `perf` the patch, and `docs`, `chore`, `refactor` and `test` release nothing. No
   branches unless asked; commits go onto `main`.
6. **The board carries the work.** A feature or a decision is an item on the
   [board](implementation_plans/2026-10-11_harness-distribution-tool.md), with a line in its
   progress log when it lands; a hard-to-reverse decision is an ADR in [decisions/](decisions/).

## How a release works

A release is cut from this machine, never on the host: no release workflow, no secret on GitHub. It is
one command, run by the human:

```
task rein:release
```

It runs `reinctl release`, which:

1. **Checks the ground.** Refuses off `main`, on a dirty working tree, without a remote named
   `origin`, when `main` is not what `origin/main` has, when GitHub's check has not passed on
   that commit on Linux, macOS and Windows, or is still running, and when the tag it would
   make already exists locally or on `origin`. Versions are immutable.
2. **Plans the version** from the conventional commits since the last tag, and refuses when
   none of them releases. The first release was `v0.1.0`.
3. **Runs the gate**, `task rein:check` and `task rein:release-check`, before anything changes.
4. **Writes the section** into [CHANGELOG.md](../CHANGELOG.md), commits `chore(release): vX.Y.Z`,
   tags it, and pushes `main` and the tag.
5. **Builds and publishes.** GoReleaser builds rein for macOS, Linux and Windows on amd64 and
   arm64, and publishes the GitHub Release with the archives and the checksum file. The token
   is what `gh auth token` returns, handed to that run and written nowhere. The `.deb`, `.rpm`
   and `.apk` are attached to the release; the Homebrew cask, the Scoop manifest, the winget
   manifest, the Nix package and the AUR package each go out when the repository or the key
   they need exists, and are said to be skipped otherwise. Which needs what is in
   [commands.md](commands.md#installing-and-releasing).

When a push fails after the commit and the tag, the message says what to push by hand; when the
publish fails after the push, `task rein:publish` repeats it once the cause is fixed. The
configuration is [.goreleaser.yaml](../.goreleaser.yaml); the doors through which a release is
installed are listed in [commands.md](commands.md#installing-and-releasing).

### What a version string means

A release build reports its tag, `v0.1.0`, set through the linker at build. A build from the
working tree, `task rein:install` or `go install`, reports the module's pseudo-version, such as
`v0.1.1-0.20261022193712-c5bb941216a4`: the next patch after the last tag, `-0.` marking a
pre-release of it, the commit's time in UTC, and the short commit hash. `rein update --check`
reads it with pre-release semantics: newer than `v0.1.0`, older than `v0.1.1` once that is
released. A build without module information reports `unknown`.

## Where the documents live

Everything starts at the root [README.md](../README.md) and goes one level down into `docs/`:
one document per topic, the ADRs in [decisions/](decisions/), the live boards in
[implementation_plans/](implementation_plans/), the ideas parked on purpose in
[future-ideas.md](future-ideas.md). The conventions, including that every external fact
carries its source and that diagrams are mermaid, are the project's `docs` rule, which sync
writes to `.claude/rules/docs.md` and the agent loads whenever a document or a README is touched.
