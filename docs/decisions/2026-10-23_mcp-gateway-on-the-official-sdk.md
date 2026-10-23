# The MCP gateway is built on the official Go SDK

**Date:** 2026-10-23
**Status:** Accepted

## Context

An agent reaches rein through the shell today: it runs `rein status` or `rein resolve` and reads
text meant for a person. Three things are wrong with that for a tool whose users are agents.
The output is tables and colours, not structure. The commands that ask questions block an
agent unless every answer is a flag. And each agent has its own shell permission model, so
"the agent may run the read-only queries" has to be configured once per agent and per project.
The research behind this repository found MCP (the Model Context Protocol) to be the one
interface for services that every agent speaks
([harness-management, Q4](../harness-management.md#harness-portability-across-tools)), and
Claude Code gates MCP tools by name, `mcp__rein__<tool>`, in its permission rules and runs a
tool marked read-only in parallel and repeats it safely after a dropped connection
([Claude Code MCP docs](https://code.claude.com/docs/en/mcp)). So rein gets a second gateway, `rein mcp`,
offering the queries as tools over stdio, beside the CLI and over the same `Orchestrator`.

The protocol has two eras. Revision 2026-07-28 dropped the `initialize` handshake: every request
carries its version in `_meta` and a server answers `server/discover`
([versioning](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning)).
Everything up to 2025-11-25 opens with `initialize`
([lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle)). The
compatibility matrix says a client on the old handshake fails against a server that speaks only
the new revision, and that handshake is what Claude Code's first runtime and most other agents
still send; Claude Code's second runtime asks for the new revision and falls back. A new server
therefore speaks both, which the specification calls dual-era.

The repository's rule was "Go, cobra and yaml.v3 only". The reason is supply chain: rein writes
the files every developer's agent obeys, so a library inside rein that is hijacked, as the
[xz backdoor of 2024](https://nvd.nist.gov/vuln/detail/CVE-2024-3094) hijacked a library most
Linux systems trusted, would sit inside the tool that distributes agent settings to a whole
organisation. Every library is also upkeep: its own releases, deprecations and breaking changes.

## Candidates Considered

1. **A hand-written dual-era server over `encoding/json`.** No new module. Both eras by hand:
   the handshake, `ping`, `tools/list`, `tools/call`, `server/discover`, the `_meta` version
   check and the version error, about six hundred lines with tests. We would own every
   divergence from what a client expects and chase a specification that changed its shape
   within a year.
2. **The official Go SDK, `github.com/modelcontextprotocol/go-sdk` v1.8.0.** Stable, maintained
   inside the protocol's own GitHub organisation, speaks 2026-07-28 and every version back to
   2024-11-05, infers a tool's input schema from a Go struct and validates calls against it
   ([pkg.go.dev](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp)). Eight direct
   dependencies, among them a JWT library, OAuth2 and a JSON codec, of which this server uses
   none; Go links only what is called, so they enlarge the trust surface, not the binary's
   behaviour.
3. **No MCP, `--json` on the read-only commands.** A few hours, no module, structured output;
   but the per-agent shell permissions and the human-shaped prompts stay.

## Decision

Candidate 2: `rein mcp` is built on the official SDK, and the dependency rule becomes: the
standard library, cobra, yaml.v3 and the MCP SDK; every further module is a decision recorded
in an ADR before it is added. Go pins each module by construction, `go.mod` records the exact
version and `go.sum` its checksum, verified against the public checksum database, so an update
is a deliberate edit that shows in a diff and runs the gate. The protocol is the SDK's to track;
rein's gateway holds the tool table and nothing of the wire format.

The first version offers the queries only: `status`, `resolve`, `declaration_list`,
`declaration_describe`, `organization_describe`, `proposals_pending` and `lint`, each marked
read-only, each behind the login gate where its command has one. `sync`, `distill`, `propose`
and `restore` change the machine or open pull requests and stay the developer's commands, as
the rein skill says. A later version may offer `distill` and `propose` as tools marked
`anthropic/requiresUserInteraction`, which makes Claude Code ask a person on every call, in
every permission mode; the pilot decides.

`rein sync` writes the server into the project's `.mcp.json` the way it writes the session-start
hook, with the absolute path of the executable, because the path is per machine and not a
declaration, and approves it in `.claude/settings.local.json`, which sync owns, so Claude Code
starts it without its once-per-project prompt. That prompt guards against a cloned `.mcp.json`
starting commands on a machine; this entry is written on the machine, by rein, and never cloned.

## Consequences

- Every clone rein manages gets `.mcp.json` with the server `rein`, beside any declared tool
  server, and the rein skill tells the agent the tools exist.
- The second gateway lives in `internal/app/0_gateway/mcp`, holds one concrete `Orchestrator`,
  takes no machine lock and prints nothing; the CLI's `mcp` command starts it over the
  command's standard streams. The architecture test allows the CLI to import it and nothing
  else of the gateway layer.
- The module graph grows beyond cobra and yaml.v3 for the first time. The gate's `go vet` and
  tests cover the SDK's use; an update to the SDK is a reviewed change like any other.
- A second agent in the pilot, Codex, Cursor or Gemini CLI, gets the same tools from the same
  binary through its own `.mcp.json` reader, which is the point of the research question Q4.
