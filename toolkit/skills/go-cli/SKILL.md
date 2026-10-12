---
name: go-cli
description: Conventions for a project's Go operator CLI (a cobra tool under cmd/ that releases, provisions, deploys and verifies) — explicit --env, no silent defaults, preflight before change, addresses from the IaC outputs, one wrapper for every shell-out and every print, a --log run log, secrets over stdin, fixed exit codes. Use when writing or changing such a CLI.
---

# Go Operator CLI

An operator CLI (`<project>ctl`) wraps release, infrastructure, deploy and verify for one
project. Its package comment says what it does and what it never does.

## Behaviour

1. **`--env` is explicit** on every command that touches an environment — never a default,
   never inferred from a branch. The environments are defined in one file.
2. **No silent defaults.** A missing value is an error that names it, never an empty string
   passed on.
3. **Preflight first.** A command checks the tools it shells out to and every precondition — the
   release tag, the stack's outputs — before it changes anything.
4. **Addresses come from the stack**, never constants: read `tofu output -json` (not `-raw`,
   which passes a warning off as a value before the first apply).
5. **Every shell-out goes through one run helper**, so a failure names what ran (with the tool's
   own stderr), and the output reaches the run log.
6. **All output through one pair of writers** (out / err) — never `fmt.Print*`, `os.Stdout` or
   `os.Stderr` directly; a guard test fails the build otherwise. That is what puts a `--log`
   run's output — its own and every subprocess's — into `.tmp/<timestamp>_<tool>_<verb>.log`.
   Every command handed to the human carries `--log` (before any `--`); the human reports only
   the exit line and the agent reads the rest from the file.
7. **Secrets travel over stdin**, never in argv. Values read from `.env` that look like
   credentials are redacted from the run log.
8. **Exit codes**: 0 worked, 1 refused (precondition, failed check), 2 crashed, 3 a wrapped tool
   failed.
9. **The human runs what changes the world**: a command that pushes, deploys, applies or changes
   a server says so in its `Short` help, and the agent never runs it.

## Style

The `go-style` skill, plus:

- **No abbreviations** in names; comments say why, not what.
- **Minimum code** — no flag, option or abstraction before a command needs it.

## Tests

- Standard `testing` (no testify — the module's only dependency is cobra), one top-level
  `Test<Thing>` per concern with `t.Run` cases whose names read like sentences.
- Anything that parses git history is tested against a scratch repository built in the test.
- Vet and test the CLI before every handover.
