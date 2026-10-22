package compile

// ReinSkillID is the id of the skill rein writes into every synced clone, so
// the agent knows how the settings it reads are managed. A declared skill
// with the same id is not written.
const ReinSkillID = "rein"

// ReinSkill is that skill, in the Agent Skills format.
const ReinSkill = `---
name: rein
description: How the agent settings of this project are managed by rein, the generated files under CLAUDE.md, .claude/ and .mcp.json, why they are never edited by hand, and how a setting is changed through rein propose, checked through rein status and rein resolve, or undone through rein restore. Use before editing CLAUDE.md, .claude/rules, .claude/skills, .claude/hooks, .claude/settings.local.json or .mcp.json in a project rein manages, when a REIN_ variable is reported missing, or when asked how settings reach this project.
---

# Working in a project managed by rein

rein keeps the settings of AI coding agents as declarations in one org repository per
organisation, at four levels: organisation, team, project and user. ` + "`rein sync`" + ` resolves
the levels for this project and writes the files the agent reads. A change is not made in
those files; it is proposed to the org repository and comes back at the next sync.

## What is generated here

- ` + "`CLAUDE.md`" + ` imports one file per level under ` + "`.claude/rein/`" + `. Its header names the levels.
  A higher level wins: organisation over team, team over project, project over user. A rule
  marked required is not weakened by anything below it.
- Rules with paths under ` + "`.claude/rules/`" + `, skills under ` + "`.claude/skills/`" + `, hook scripts
  under ` + "`.claude/hooks/`" + ` and the tool servers in ` + "`.mcp.json`" + ` come from declarations.
- In ` + "`.claude/settings.local.json`" + ` rein owns the hooks and the deny and ask lists, and merges
  the developer's own allow list.
- Every generated file is recorded with its hash. An edit by hand stops the next sync with a
  message; ` + "`rein sync --force`" + ` snapshots the edit first.
- ` + "`CLAUDE.local.md`" + ` is the developer's own and never touched.

## Changing a setting

1. Do not edit a generated file. Say which level the change belongs to: the organisation
   for everyone, a team, this project, or the developer's user level.
2. Write the declaration in a folder in the declarations layout: ` + "`rules/<id>.md`" + `,
   ` + "`skills/<id>/SKILL.md`" + `, ` + "`hooks/<id>/hook.yaml`" + ` with its script, ` + "`permissions/<id>.yaml`" + `,
   ` + "`mcpservers/<id>.yaml`" + `. ` + "`rein lint <folder>`" + ` checks it.
3. ` + "`rein propose <folder> --level <address>`" + ` lints it, compares it with what the organisation
   has and opens the pull request. The address is ` + "`organizations.<org>`" + `,
   ` + "`organizations.<org>.teams.<team>`" + `, ` + "`organizations.<org>.projects.<project>`" + ` or
   ` + "`organizations.<org>.users.<user>`" + `.
4. A harness that was in a repository before rein is distilled: ` + "`rein distill`" + ` writes a
   proposal file under ` + "`.rein/proposals/`" + ` with every candidate, its level and the judge's
   reason; a person reviews and edits it; ` + "`rein propose`" + ` opens the pull request from it.
5. A setting for this developer on this machine only goes into ` + "`CLAUDE.local.md`" + `, or into
   the allow list of ` + "`.claude/settings.local.json`" + `, which sync keeps.

## Secrets

A tool server names a secret as a ` + "`${REIN_...}`" + ` variable. The value lives in the developer's
environment, never in a repository or a generated file. ` + "`rein sync`" + ` names the variables the
shell lacks; ask the developer to export them. Never write a secret into a file.

## Checking and undoing

- ` + "`rein status`" + `: what rein knows about this machine.
- ` + "`rein resolve --project <slug>`" + `: which declarations apply to this project and why.
- ` + "`rein sync`" + `: deliver the latest; the session-start hook runs it.
- ` + "`rein restore`" + `: put the files from before ` + "`rein init`" + ` back and forget the clone.

The agent runs the read-only commands itself: status, resolve, lint. sync, propose and
restore change the machine or open pull requests, so the developer runs them, or the agent
asks first.
`
