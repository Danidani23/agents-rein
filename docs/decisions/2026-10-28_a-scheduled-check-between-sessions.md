# A check the operating system schedules tells what changed between sessions

**Date:** 2026-10-28
**Status:** Accepted

## Context

rein brings a project up to date when a session starts: the session-start hook runs
`rein sync --hook`, which pulls the organisation's repository and rewrites what changed
([board, D2](../implementation_plans/2026-10-11_harness-distribution-tool.md#decisions)). The
[compiler ADR](2026-10-11_harness-tool-compiler-and-linter.md) turned down a daemon that pushes
(N1) and left rein with no presence between sessions.

On 2026-10-28 the user named what that leaves out:

- **Long sessions.** A session that stays open for days receives a change of the organisation
  only at `/clear` or at the next session. Claude Code picks up a new skill while a session
  runs, but reads the instructions at its start and keeps the hooks it started with
  ([concepts.md](../concepts.md#when-the-harness-is-brought-up-to-date)).
- **Silence.** The session-start sync writes without telling the developer anything.
- **Checks that wait for a session.** A file in rein's folders that rein did not write, a file
  sync wrote and someone edited by hand, a missing one, a broken link in the agent's home: rein
  knows each, but reports it only when `sync` or `status` runs, and not at all while no session
  is opened.

## Candidates Considered

1. **Nothing new.** The session-start hook stays the only trigger. All three gaps remain.
2. **A daemon that the CLI and the page call.** One process owns the state and the others are
   its clients. It needs a protocol between them, a service kept alive on three operating
   systems, a CLI that fails or starts the daemon when it is down, a daemon still running the
   old version after `rein update`, and the machine lock handed between processes. It breaks
   "one binary, one process" of [architecture.md](../architecture.md) and was turned down for
   `rein ui` too ([its ADR](2026-10-27_rein-ui-a-gateway-served-to-the-browser.md), candidate 1).
3. **GitHub calls the machine.** A webhook delivers an HTTP request to a URL GitHub can reach,
   which a laptop is not. GitHub's own forwarders are for testing: smee.io "should not" forward
   webhooks in production ([GitHub](https://docs.github.com/en/webhooks/using-webhooks/handling-webhook-deliveries)),
   and `gh webhook forward` "is only designed for use during testing and development"
   ([GitHub](https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/using-the-github-cli-to-forward-webhooks-for-testing)).
   A relay server would be the server with registration that N2 turned down.
4. **`rein ui` checks while it is open.** Covers only the hours the page is open.
5. **A resident `rein` kept alive by the operating system's service manager.** Warnings within
   seconds, but it has to be kept alive and runs the old version after `rein update` until it is
   restarted.
6. **The operating system's scheduler starts a short `rein` run every minute**: launchd on
   macOS through `StartInterval`, a systemd user timer on Linux, Task Scheduler on Windows.
   Apple's guide recommends designing a job "to be launched on-demand" rather than kept alive
   ([Apple](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html)).
   Each run is the binary installed last, takes the machine lock like any command, and exits. A
   finding can wait up to a minute.

## Decision

Candidate 6, chosen by the user (Q19 S1). Each scheduled run:

- **Checks the folders.** It compares each project clone's rein folders and the agent's home
  with the ledger, rein's record of every file it wrote and its hash. It compares file times
  and sizes first and hashes only what changed. This is polling with the standard library: Go
  has no folder watching of its own, and a library for it would be a module beyond the ones
  the [MCP gateway ADR](2026-10-23_mcp-gateway-on-the-official-sdk.md) allows.
- **Checks the organisations,** every few minutes, by asking whether each organisation
  repository's main branch moved (polling, not a webhook). GitHub's REST API does not count an
  authorised conditional request answered `304 Not Modified` against the rate limit
  ([GitHub](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)).
- **Syncs only where the developer chose.** Automatic sync is off by default: the check tells,
  and the developer turns automatic sync on for all projects or for the ones they pick (Q20
  M1). The setting is the developer's, changed from the CLI and from the page, and kept in the
  machine's state in rein's home, per project clone (Q22 a).
- **Tells each new finding once,** as a notification of the operating system: what synced and
  that `/clear` loads the rest in an open session, or what changed in a folder. On macOS that is
  `display notification`
  ([Apple](https://developer.apple.com/library/archive/documentation/LanguagesUtilities/Conceptual/MacAutomationScriptingGuide/DisplayNotifications.html));
  the mechanisms for Linux and Windows are chosen, with their sources, when they are built.
- **Is turned on and off by the developer.** A command writes the job into the system's
  scheduler and another removes it; rein never schedules it unasked (Q23 a).
- **Is called by nothing.** The CLI and `rein ui` work the same whether the check is scheduled
  or not; the page shows the same findings.

## Consequences

- D2 is amended: a sync runs at session start, by hand, and, for the projects the developer
  chose, from the scheduled check. The [compiler ADR](2026-10-11_harness-tool-compiler-and-linter.md)'s
  consequence that rein "has no presence in a running session" no longer holds; N1, a daemon
  that pushes, stays unbuilt.
- rein writes into a project between sessions only where the developer turned automatic sync
  on. In an open session such a sync brings new skills at once, and the instructions and hooks
  at `/clear`; the notification says so.
- A file edited by hand still stops a sync; the check tells the developer instead of waiting
  for the next session to print it.
- Three schedulers and three kinds of notification are built and tested, each checked by hand
  on its operating system, since a test does not reach the real scheduler.
- The board records this as D47 to D49 and the phase P5, built after P4.3 (Q21 a).
