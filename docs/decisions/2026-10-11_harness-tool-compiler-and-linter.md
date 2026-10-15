# Harness distribution tool: a compiler and a linter, not a runtime

**Date:** 2026-10-11
**Status:** Accepted

## Context

The proposal of 2026-10-11 was a Go service on each developer machine: four levels of agent
configuration (organisation, team, project, personal) held on a server, merged into one file,
pushed into every registered repository, validated by a central workflow, with every generated
agent file gitignored. The discussion and the board that records it:
[2026-10-11_harness-distribution-tool.md](../implementation_plans/2026-10-11_harness-distribution-tool.md).

What the vendor already does, verified against the Claude Code docs on 2026-10-11 and summarised
on that board:

- **Enforcement.** Managed settings sit above every other level and are delivered three ways:
  the claude.ai admin console, an MDM (mobile device management) profile or registry key, or a
  root-owned `managed-settings.json` with a `managed-settings.d/` drop-in folder. A
  `policyHelper` executable named by the policy can compute the settings at startup. A
  user-space process cannot write any of these
  ([managed settings](https://code.claude.com/docs/en/managed-settings)).
- **Distribution.** A plugin marketplace is a git repository with a catalogue file; managed
  settings register it, force-enable plugins, pin versions and turn auto-update on; seeds serve
  CI (continuous integration) and containers
  ([organisation plugins](https://code.claude.com/docs/en/plugins/org)).
- **Tool servers.** Fixed sets, provided servers and allow or deny lists are settings keys;
  Claude Code has no built-in registry ([managed MCP](https://code.claude.com/docs/en/managed-mcp)).
- **Model routing.** A gateway at `ANTHROPIC_BASE_URL`, LiteLLM, Bifrost or Anthropic's own
  Claude apps gateway ([Q4](../harness-management.md#q4--staying-flexible-on-model-provider-and-private-inference)).
- **Reach.** A cloud session, a CI agent or another machine sees only what is in the git clone
  ([cloud environments](https://code.claude.com/docs/en/cloud-environments#what-carries-over-from-your-setup)).

What nobody does: turn one reviewed, vendor-neutral source into validated per-vendor artefacts,
check that a team's layer does not contradict the organisation's, and report drift across a
fleet ([Q2](../harness-management.md#q2--extending-the-harness-without-breaking-global-rules),
[Q6](../harness-management.md#q6--adopt-existing-solutions-or-build)).

## Candidates Considered

- **The service as proposed.** Daemon, registration server, push, tool-native approvals,
  gitignored files, one merged file. Re-implements delivery, review and layering that the vendor
  and git already provide, cannot write the enforcement layer, and gitignored files never reach a
  cloud session or a clone.
- **Nothing new.** The vendor layers for an organisation; symlinks for one developer
  ([toolkit ADR](2026-10-10_personal-toolkit-open-formats.md)). Leaves the validation gap, the
  drift gap and the copies for repositories that leave the machine unsolved.
- **Adopt a generator** (Ruler, rulesync). Transpiles content into many tools' formats. No
  levels, no validation, no policy compilation, no inventory.
- **A compiler and a linter.** A CLI (command-line interface) and a CI job over a reviewed git
  source. It validates, merges the levels and emits each vendor's own artefacts. The vendor
  delivers and enforces them.

## Decision

The tool is a compiler and a linter. Its value is at build and validate time, never at session
time. It is a small CLI plus a CI job, not a service. Targets: an organisation with many teams,
and the personal toolkit in this repository as the pilot.

Built, in this order:

- **B1 — The harness lint.** Frontmatter per the Agent Skills spec, skill name equals folder,
  description length, unique rule and skill names, no `allow` matching an organisation `deny`,
  MCP (Model Context Protocol) server URLs on the allowlist, no `CLAUDE.md` inside a plugin.
- **B2 — The source layout and the merge.** One git repository for the organisation and team
  levels, a manifest per project repository naming its team and pinned versions, and the merge
  rules that say what a lower level may add and never override.
- **B3 — The compiler, one vendor at a time, Claude Code first.** From the merged source: a
  plugin and marketplace repository for the team level, a `managed-settings.d/` drop-in for
  policy, `.mcp.json` and `managedMcpServers` for servers, `CLAUDE.local.md` for the personal
  overlay. A second vendor's emitter is written when that vendor is in real use.
- **B4 — The bake.** Copy the overlays into the committed files of a repository that leaves the
  machine (client repositories, cloud sessions). Resolves D6 of the
  [toolkit board](../implementation_plans/2026-10-10_agent-toolkit.md).
- **B5 — The drift and inventory report.** Which repository is on which version of which level,
  and whose committed files differ from the generated ones.

Not built:

- **N1 — A daemon that pushes.** Vendors pull; the root instruction file is read at launch; a
  process writing into every working tree is a security and support burden.
- **N2 — A server with registration.** The org repository is a git repository; registering is
  cloning.
- **N3 — An approval workflow.** A pull request with CODEOWNERS on the org repository.
- **N4 — Enforcement.** The tool generates the managed settings file; it never pretends to
  enforce. Policy stays in the vendor's lock.
- **N5 — A marketplace server, an MCP registry, an LLM gateway or an agent runtime.**
- **N6 — A general-purpose vendor compiler now.** Guidance is already portable by standard
  (AGENTS.md, Agent Skills, MCP). The lock is in permissions, hooks and plugin packaging, a small
  set. Compiling it across vendors with different semantics is justified only with two vendors in
  concurrent use. Until then, vendor neutrality is a constraint on the source, not code.

## Consequences

- Delivery latency and freshness are the vendor's: a change lands at the next session start or
  the vendor's next poll. The tool has no presence in a running session.
- Amended 2026-10-11 by D4 on the board: every generated file is gitignored in the project
  repository and regenerated by `sync`; `init` commits only the manifest, the `.gitignore`
  entries and the settings file carrying the `SessionStart` hook. The bake (B4) is the one
  exception, for repositories that leave the machine. Cloud sessions and CI therefore need the
  tool and access to the org repository. This bullet first read: "The project level is committed
  in the project repository, so it reaches cloud sessions, CI and every clone. Organisation, team
  and personal overlays are generated into the vendor's local-only slots or baked in (B4)."
- Amended 2026-10-15 by D4 on the board, again: a project repository carries no rein file;
  agent settings are not repository settings. The only trace is a marked block in `.gitignore`;
  the hook and the project settings live in the uncommitted `.claude/settings.local.json`; the
  manifest is replaced by the fingerprint. B4, the bake, is deferred in favour of `rein export`.
- Review, history and blame come from git. Ownership of a level is a CODEOWNERS entry.
- The source must stay vendor-neutral even while only Claude Code is emitted, or N6 cannot be
  revisited cheaply. Policy compiled for a second vendor will be lossy where rule semantics
  differ.
- On this machine the personal toolkit keeps its symlink delivery
  ([toolkit ADR](2026-10-10_personal-toolkit-open-formats.md)); this decision adds the lint
  and the bake to it rather than replacing it.
- The board's decisions D1 to D5 and phases P1 to P3 are to be aligned with this record; the
  lint's addressing scheme and the metadata-driven selection of documents are open design items
  for B1 and B2, not decided here.
