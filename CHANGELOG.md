# Changelog

## [0.3.1] - 2026-10-24

### Bug Fixes

- **rein:** rein update replaces a script-installed rein on windows (64a29c5)
- **rein:** init records removed paths with the platform's separator (ce638f7)
- **rein:** the machine lock sees a live holder on windows (60384db)

## [0.3.0] - 2026-10-23

### Features

- **sync:** warn in red, with the command, when Claude Code has not trusted the folder (52407ca)
- **sync:** say when Claude Code has not trusted the folder yet (6740776)
- **sync:** approve rein's tool server in the settings file it owns (4ebd005)
- **mcp:** rein mcp serves the queries as read-only MCP tools (fc5b3a3)

## [0.2.0] - 2026-10-23

### Features

- **status:** say what became of each project's latest snapshot (a350bce)
- **cli:** every command ends with its next step in bold (7eeb40d)

### Bug Fixes

- **reinctl:** one pinned GoReleaser checks and publishes a release (4865aef)
- **rein:** a build after a tag is newer than the tag; the comparison reads pre-releases and ignores build metadata (c5bb941)
- **reinctl:** the archives of a release are ignored, so the next release finds a clean tree (161a112)

## [0.1.0] - 2026-10-22

### Features

- **rein:** reinctl release cuts and publishes a version from this machine; install scripts, the cask and the bucket, rein update and the daily nudge (4350844)
- **rein:** one organisation per repository, so the org repository is flat and the user level sits under the organisation; team list, team edit, project list (1c4f6a7)
- **rein:** declaration list, edit and remove; what is around a declaration changes by a pull request (7f206f3)
- **rein:** distill and propose end on the one line the person needs next, in bold green (6996d0a)
- **rein:** the proposal is one reviewable file; the judge's conversation is named; long steps spin (ef2659b)
- **rein:** a label category allows one value unless multiple; label set replaces label require (2cc8aed)
- **rein:** required label categories, project edit and the label registry commands; a plain init intro (98ed796)
- **rein:** init offers the repository's name; descriptions have no default (cbd294f)
- **rein:** propose places each candidate where the judge said; the rein skill in every clone (28e3aed)
- **rein:** org destroy disconnects and prints the deletion steps; use, disconnect and destroy ask which organisation (aea9084)
- **rein:** e at the confirmation re-asks with the answers as defaults; step lines in colour with their duration (f8ec8b2)
- **rein:** the pilot path end to end, rein as a GitHub App, visible and interruptible workflows (a8fd36b)
- **rein:** the whole pilot path, org create to sync and back through restore and reset (3a228e9)
- **rein:** the whole pilot path, org create to sync and back through restore and reset (477cba2)
- **rein:** the pilot path from org create to resolve; one concrete orchestrator; hardened state (d802e05)
- **rein:** org, team and project create, init, distill with the judge, propose, restore, reset; hardened state; one concrete orchestrator (a594083)
- **rein:** org, team and project create, init, declaration formats, distill with the judge, propose; one concrete orchestrator; descriptive names (fa45001)
- **rein:** org, team and project create, init, declaration formats, distill, propose; one concrete orchestrator; descriptive names (82f7ab4)
- **rein:** org, team and project create, init, declaration formats, distill; one concrete orchestrator; descriptive names (c0e42ae)
- **rein:** org, team and project create, init, declaration formats; one concrete orchestrator; descriptive names (a70cac2)
- **rein:** org, team and project create, init; one concrete orchestrator; descriptive names (D18, D20, P2.7, P2.16, P2.17) (1842c05)
- **rein:** org create, team create, project create; one concrete orchestrator; descriptive names (D18, D20, P2.7, P2.16) (a2a8915)
- **rein:** org create; one concrete orchestrator; descriptive names; connected_at (D18, D20, P2.7) (82f427b)
- **rein:** team, project, user and label records; the tree lint; host per organisation (2577d9d)
- **rein:** reset explains placements and imports, ? at confirmations (c7fdde8)
- **rein:** no config command; import from a folder; reset; live identity (0f83ee0)
- **rein:** identity from gh; D8 GitHub host, D9 no local config (1f25324)
- **prompt:** note block, dividers, proposal explained (42923d1)
- **prompt:** explanations styled like descriptions, faint italic (ef2c5f0)
- **rein:** questions explain themselves, rein status (643d296)
- **reins:** export and import in the reins exchange format (523af8a)
- **reins:** config, org connect/list/use/disconnect, version (2d5c844)
- **reins:** lint skills, rules and settings (4bd63c9)
- **toolkit:** personal agent toolkit in open formats (da6d5a1)

### Bug Fixes

- **rein:** the level is a selector, the declaration list a table, and an interrupt or a dead holder never leaves the lock behind (4a0603b)
- **rein:** the judge reports claude's own words and runs isolated without --bare; distill shows its steps; records write every field (7ef32ac)
- **rein:** the judge reports claude's own words and runs isolated; distill shows its steps; records write every field (95bc7d4)

