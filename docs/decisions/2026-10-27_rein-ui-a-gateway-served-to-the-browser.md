# `rein ui` is a third gateway in the binary, served to the browser

**Date:** 2026-10-27
**Status:** Accepted

## Context

rein is used from the terminal, through the gateway `cli`, and by agents, through the gateway
`mcp`. An organisation with many teams and projects is hard to see from either: which
organisations are connected, which teams and projects each holds, which declarations sit at
which level and which projects each one reaches. Resolving the conflicts of a distillation
means editing `conflict.resolution` in each candidate's YAML file by hand. On 2026-10-27 the
user asked for a page that members open to see one organisation at a time, switch between the
connected ones, and run the commands from, so that a conflict can be resolved by clicking.

Running a command needs what only a member's machine holds: the GitHub login in the keychain,
the organisation's clone, the project clones and the machine lock. A reviewer without rein
reviews the pull request on GitHub, as today ([board, D3](../implementation_plans/2026-10-11_harness-distribution-tool.md#decisions)),
and no page changes that.

Two questions follow: what runs the commands the page asks for, and where the page is shown.

## Candidates Considered

What runs the commands:

1. **A daemon the page calls.** A background service installed on each operating system, kept
   alive and updated, which can lag behind the CLI after `rein update` and holds or competes
   for the machine lock that every writing command waits 5 seconds for. The board's D2 turned
   a daemon down for distribution, and [architecture.md](../architecture.md) holds rein to one
   binary and one process.
2. **The page runs the CLI in the background,** one process per action. It parses output
   written for a person, and a command that asks a question cannot be answered.
3. **A third gateway in the binary.** `rein ui` runs in the foreground while the page is open
   and calls the one `Orchestrator` directly, as `cli` and `mcp` do. The orchestration takes
   and returns plain data and never asks or prints, so the page asks through forms and shows
   what it gets back.

Where the page is shown:

4. **The developer's browser.** `rein ui` serves the page on 127.0.0.1 and opens a tab, as
   `pprof -http` "starts a web server and opens a browser window pointing at that server"
   ([pprof docs](https://github.com/google/pprof/blob/main/doc/README.md)). The standard
   library has the server, the templates and the embedding of files, and the release stays as
   it is: every platform built on one machine with cgo off
   ([.goreleaser.yaml](../../.goreleaser.yaml)). The cost is a port on the machine, which any
   page the browser opens can try to reach.
5. **A native window through Wails.** Its own window and icon, and no port. It needs NPM to
   build, the Xcode command line tools on macOS, gcc with libgtk3 and libwebkit on Linux, and
   the WebView2 runtime on Windows
   ([Wails installation](https://wails.io/docs/gettingstarted/installation)): a build on each
   operating system, a frontend toolchain beside Go, and libraries Linux users must install.

## Decision

Candidates 3 and 4. `rein ui` is a third gateway, `internal/app/0_gateway/web`, started by the
CLI's `ui` command, serving the page to the developer's browser until Ctrl+C.

- **Built from the standard library.** Pages are rendered by `html/template`, with one
  hand-written script and one stylesheet, packed into the binary with `embed`. No module is
  added and nothing is built outside Go, so the dependency rule of the
  [MCP gateway ADR](2026-10-23_mcp-gateway-on-the-official-sdk.md) holds unchanged. The
  progress of a long command reaches the page as server-sent events, a one-way stream over
  plain HTTP ([MDN](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events)).
- **It reads as the MCP gateway reads.** The views call the queries the MCP tools call,
  without the lock. Each query takes the organisation's slug, so switching organisations in the
  page changes what is shown, never the machine's active organisation.
- **It writes as the CLI writes.** Each action takes the machine lock for itself alone, with
  the CLI's wait, never for the page's lifetime, so an open page never makes a session-start
  sync fail. The login gate stands in front of every action that acts for a user. Resolving a
  conflict is a new operation that writes a candidate's `include`, `level` and
  `conflict.resolution`; `propose` then runs as today, and the review stays a pull request.
- **The port belongs to the tab rein opened.**
  - It listens on 127.0.0.1 only, on a free port chosen at start.
  - The URL it opens carries a token made for that run. The first request exchanges it for a
    cookie that scripts cannot read and other sites cannot send (`HttpOnly`,
    `SameSite=Strict`), the way Jupyter's server trades its URL token for a cookie
    ([Jupyter security](https://jupyter-server.readthedocs.io/en/latest/operators/security.html)).
  - A request whose `Host` header is not `127.0.0.1:<port>` is refused. That is the defence
    against DNS rebinding, where a website points its own name at the visitor's machine
    ([Wikipedia](https://en.wikipedia.org/wiki/DNS_rebinding)).
  - A write whose `Origin` is not the page's own is refused, so no other site can make the
    browser act for the member (cross-site request forgery).

## Consequences

- The gateway table of [architecture.md](../architecture.md) and the `allowed` map of the
  architecture test gain a row: the web gateway imports the orchestration, the ports and the
  libraries, and the CLI imports it as it imports `mcp`.
- A member sees and changes the organisation's settings without opening the YAML files; a
  reviewer still reviews on GitHub.
- No window and no icon, only a tab. A later wrapper could put the same pages in a window
  without changing the gateway.
- The page needs rein installed. A hosted, read-only view for people without rein would be a
  separate decision, and none has been taken.
- The gateway is tested with `httptest` over the fakes, including the token, `Host` and
  `Origin` checks; each page is checked by eye in a browser before its commit.
- The board records this as D45 and the phase P4.
