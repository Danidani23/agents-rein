# Personal agent toolkit: open formats, linked from one place

**Date:** 2026-10-10
**Status:** Accepted

## Context

Twenty personal repositories under `~/GitHub/dani` each carried their own copies of the same
agent instructions: `communication` in 6, `strict-execution`, `git` and `engineering-principles`
in 5, the `implementation-plan` skill in 10, `commit` in 8, `adr` in 6. Only
`engineering-principles` was still identical everywhere. The rest had drifted for two reasons:
improvements stayed in the newest repository, and project examples were written into generic
rules. The goal: one source, edited once, with no lock-in to one agent vendor's formats.

The findings in [harness-management.md](../harness-management.md) answer the same question for
a large organisation and recommend a plugin marketplace plus managed settings (Q1, Q6). That
answer is shaped for many developers under central policy; this is one developer.

## Candidates Considered

- **Claude Code plugin marketplace.** Versioned, can be enabled per project. Packaging is
  Claude-specific, and plugin skills are namespaced (`/plugin:skill`) —
  [skills docs](https://code.claude.com/docs/en/skills).
- **A generator** ([Ruler](https://github.com/intellectronica/ruler),
  [rulesync](https://github.com/dyoshikawa/rulesync)): one source written out into each tool's
  format. One more tool to run and trust; pays off with three or more agent tools in use.
- **Plain Markdown in open formats, linked into each tool's global folders.** No tooling. Global
  only: nothing is carried inside the repositories.
- **Keep copying.** The status quo, and the drift that came with it.

## Decision

Plain Markdown in open formats, kept in `toolkit/` in this repository:

- `toolkit/global/AGENTS.md` holds the personal policies (communication, engineering principles,
  strict execution, the general half of git, sibling repositories), in the
  [AGENTS.md](https://agents.md) format.
- `toolkit/skills/` holds procedures and stack conventions as
  [Agent Skills](https://agentskills.io/specification), using only the spec's fields. Stack
  rules that used `paths:` (Claude Code only) became skills: portable, but loaded when the model
  judges them relevant rather than on a file match.
- `toolkit/install.sh` links both into `~/.claude/`. Claude Code is the reference tool;
  OpenCode reads `~/.claude/CLAUDE.md` and `~/.claude/skills/` as fallbacks
  ([rules](https://opencode.ai/docs/rules), [skills](https://opencode.ai/docs/skills)); Codex
  would need two more links (`~/.codex/AGENTS.md`, `~/.agents/skills/`).
- The merge base was `szlavik-tomi`, the newest set.

## Consequences

- Edit once; every repository on this machine picks the change up in its next session.
- The content moves to another agent tool without rewriting.
- Nothing global is inside a repository: claude.ai/code cloud sessions, collaborators and
  clients who receive a repository do not see it. Copying it into repositories that leave the
  machine is a later decision.
- In Claude Code a personal skill wins over a project skill of the same name
  ([skills docs](https://code.claude.com/docs/en/skills)). A repository can no longer keep its
  own `commit`, `adr` or `implementation-plan`; what is specific to it moves into its own
  instructions, or its variant takes another name.
- Global and project rules both load and neither overrides the other
  ([memory docs](https://code.claude.com/docs/en/memory#user-level-rules)). A migrated
  repository deletes its copies; a repository that needs a different policy says explicitly
  which section it replaces.
- Stack conventions lose path-based loading.
