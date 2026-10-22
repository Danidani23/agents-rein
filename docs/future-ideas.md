# Future Ideas

Ideas parked on purpose: designed far enough to be worth writing down, consciously not built now.
Each entry says where it came from, why it is parked, and what would make it current. An idea
that gets picked up moves onto a board in `docs/implementation_plans/`; the entry here keeps a
link to it.

## F1 — Label detection for the harness tool

Parked 2026-10-11 during the design of the harness tool's lint and source layout
([ADR](decisions/2026-10-11_harness-tool-compiler-and-linter.md), B1 and B2;
[board](implementation_plans/2026-10-11_harness-distribution-tool.md), P1.1 and P1.2).

**What it is.** A label in the registry can carry a detector: a few file globs, optionally a
string the matched file must contain. The detector is data on the label, not code in the tool,
so an organisation can add one for an internal framework without a release.

```yaml
# declarations/organizations/acme/labels/language.yaml
category: language
labels:
  go:         { detect: { anyOf: ["go.mod"] } }
  dart:       { detect: { anyOf: ["pubspec.yaml"] } }
  typescript: { detect: { anyOf: ["tsconfig.json"] } }
```

- At `init`, the tool evaluates every detector against the repository and pre-selects the
  matching labels in the pick list; the user confirms or edits. A label without a detector is
  picked by hand.
- In `lint` and `sync`, the tool compares the labels declared in the project record with what
  the detectors find and warns both ways: detected but not declared, declared but not detected.
- A monorepo matches several detectors and gets several labels.
- The same data compiles into Claude Code's marketplace `relevance` signals, which make a plugin
  suggested when a project matches
  ([organisation plugins](https://code.claude.com/docs/en/plugins/org#recommend-plugins)), so the
  information is written once.

**Why parked.** The label registry with its pick list solves the problem that a developer does
not know which labels to choose. Detection is a convenience on top of it.

**What would make it current.** More projects to label than one person can keep straight, or
drift between declared labels and repository contents showing up in the inventory report (B5).

## F2 — Chat history kept by rein

Parked 2026-10-15 during the design of `rein export`
([board](implementation_plans/2026-10-11_harness-distribution-tool.md), P2.11).

**What it is.** Claude Code keeps session transcripts under `~/.claude/projects/<path>/`. The
idea: rein keeps them too, so a developer's history follows them across machines and an
organisation keeps a record.

**Why parked, and the constraints already known.** Transcripts hold code, prompts and at times
secrets, so they are never committed to the org repository, which is shared and reviewed. If
kept at all, they go to object storage such as S3, encrypted, with access per user. Retention,
who may read whose history and what the vendor already stores are open.

**What would make it current.** A need to carry a session across machines, or an organisation's
duty to retain agent interactions.

## F3 — Telling an open conversation that the harness changed

Parked 2026-10-22 after the question whether `rein sync` can make Claude Code reload an open
conversation ([board](implementation_plans/2026-10-11_harness-distribution-tool.md), R43).

**What it is.** No outside program can make a running Claude Code session reload. Claude Code
reads the instructions at the start of a session and after a compaction, picks up a new skill
while a session runs, and keeps the hooks it started with. The one channel into a running
conversation is a hook's output, which becomes context for the model. The idea: the
session-start hook records the session's id and time; a hook on each prompt compares that with
the ledger's last write and adds one line to the conversation, once, when the harness changed
since the session started: "the harness changed since this session started; `/clear` loads it".

**Why parked.** It costs a hook on every prompt for a line that matters rarely, and the agent
hears about the change without receiving it; the next session receives it anyway.

**What would make it current.** Organisations whose declarations change several times a day,
or a required rule that must reach a session the moment it is merged.
