# The commands of `rein`

Every command that exists today, with what it does and the files it touches. The model behind
them is in [concepts.md](concepts.md). `rein <command> --help` says the same at the terminal,
with the flags; `?` at any question explains it.

What exists today. There is no setup step beyond `rein login`: your identity is your
GitHub account, and the agents on your machine are detected.

| Command | What it does |
|---|---|
| `rein login`, `rein logout <login>` | Logs a GitHub account in through the Agents Rein app's device flow, a code typed on github.com; the token goes into the operating system's keychain and is renewed by itself. Several accounts live side by side; each organisation remembers the one that connected it. |
| `rein status` | Shows who you are to rein, the GitHub accounts logged in, the supported agents found on this machine, every placed file and whether its target still exists, organisations, projects and imports. |
| `rein org create` | Creates an organisation on the version control host (GitHub today): the repository, or an empty one of that name adopted, a first commit with the organisation record, the seeded label registry and CODEOWNERS, protection of `main` with a code-owner review, and the connection of this machine to it. Asks for the host, the account, the name, the repository, the owner and the visibility. Rein never deletes a repository. |
| `rein org connect <remote>`, `list`, `use [slug]`, `disconnect [slug]` | Clones an org repository and remembers it, snapshotting your agents' homes into `~/.rein/snapshots/` as the record of what was there before rein; lists the connected ones with the active one marked; switches; forgets one and removes its clone without touching the remote. A slug left out is asked from the connected ones. |
| `rein org destroy [slug]` | The way back to nothing for an organisation: disconnects it as above, then prints the steps to delete its repository on the host by hand. Rein never deletes a repository. |
| `rein team create`, `rein project create`, `rein project edit [slug]` | Write a team or a project record into the active organisation's clone and push it to `main`, the owner's way: name, description, and for a project its teams, labels and path, asked or given as flags. The required label categories are asked first, one value or several as the category allows; the optional ones come in one list, and two values of a single-valued category are refused. `project edit` asks the same again with the current values as defaults, or changes only the flags given. |
| `rein label list`, `add [category:value]`, `remove <category:value>`, `set <category>` | The label registry: what a project can be. `add` puts a value into a category or creates the category, asking or taking `--required`, every project carries one of its values, and `--multiple`, a project may carry several; `remove` refuses a value a project carries and the last value of a category; `set` changes `--required` or `--optional`, `--multiple` or `--single`. Each change is pushed to `main` the owner's way. |
| `rein init` | In a project clone: binds it to its project record by the repository's root commits, picking or creating the record when none carries them; moves `CLAUDE.md`, `.claude/` and `.mcp.json` into a snapshot under `~/.rein/snapshots/`, records the removal, adds a marked block to `.gitignore`, registers the clone and commits the move-out. |
| `rein distill` | Turns the snapshot of an initialised project's harness, or of your agent's home with `--home`, into candidate declarations at that level, compares each with what the organisation already declares by content and id, asks the judge, your agent's CLI until the organisation declares a provider, where each belongs, and writes it all as one proposal file: every candidate with its content, whether it goes, to which level and with which labels, and the judge's reason. The file is `.rein/proposals/proposal-<id>.yaml` in the project's folder, ignored by git, or under `~/.rein/proposals/pending/` for your agent's home; the id comes from the snapshot, and a second distillation of the same snapshot needs `--force`. The judge's conversation stays in your agent's history under the id the file records. `--dry-run` shows the brief, `--judge none` skips the model. |
| `rein propose [file, folder or zip]` | The one way declarations enter an org repository. With the proposal file, which you review and edit first, it does what the file says: lints the included candidates, leaves out the ones identical to what the organisation has, writes the rest on a branch of the clone at their levels with their labels, pushes it and opens the pull request with the comparison as its body; then marks the file proposed and moves it to `~/.rein/proposals/<org>/` as the history. Inside a project folder with one pending proposal, no argument is needed. A plain level folder or an export zip goes whole to your user level unless `--level` says otherwise. |
| `rein declaration list`, `edit <address>`, `remove <address>` | What is around a declaration, each change a pull request on the org repository. `list` shows every declaration with its level, labels and enforcement. `edit` moves one to another level, carrying its files and removing the old ones, or changes its `applyIf`, `doNotApplyIf` and `enforcement`, asked with the current values as defaults or given as flags. `remove` deletes one, unless a skip list or an exception still names it. The content of a declaration changes through `propose`. |
| `rein resolve --project <slug>` | Shows which declarations apply to a project, in level order with what each replaced, what the skip lists took out and through which exception, and the problems that would make `sync` refuse. |
| `rein sync` | Writes the harness this clone's project resolves: `CLAUDE.md` with one imported file per level, rules, skills, hook scripts, permissions and hooks in `.claude/settings.local.json` merged with your own allow list, and `.mcp.json`; every file recorded with its hash, a hand edit stopping the next sync unless `--force`. Every clone also gets the skill `rein`, which tells the agent how these files are managed and how a change is proposed, and `.mcp.json` names rein's own tool server, approved in the settings file. When Claude Code has not trusted the folder yet, sync says so: its first start there asks once, and the tool server connects after that. `--all` for every clone; `--hook` is what the session-start hook runs. |
| `rein lint <path>` | Checks skills against the Agent Skills specification, rule files, permission rules, skills that share a name, and in an org repository every record: organisation, teams, projects, users and the label registry, with their references. Needs no identity, so CI runs it on an org repository. |
| `rein restore` | Reverses `init` for one clone: the files it moved out come back from their snapshots, the rein block leaves `.gitignore`, the clone is forgotten on this machine, and the put-back is committed as the move-out was. |
| `rein reset` | Restores every initialised project first, then removes everything rein placed into an agent's home, forgets the organisations and deletes their clones, and removes the ledger, the snapshots and the proposals: the machine as before rein. A restore that fails stops it before anything is wiped. |
| `rein update` | Asks GitHub for the latest release. A rein installed by Homebrew, Scoop or `go install` is updated by that tool, and `update` prints its command; a rein from the install script is replaced by rein itself after the archive passed the release's checksum. `--check` only reports. `sync` and `status` print one line when a newer release exists, at most once a day; an organisation's `minimum_rein_version` makes an older rein refuse to `sync`. |
| `rein version` | Prints the version. |

Planned, in the board's order: `rein import` and `rein export`, which move a level into and
out of an org repository; `rein review`; `rein migrate`.

## The MCP server

`rein mcp` serves rein's queries to an agent as tools of the MCP (the Model Context Protocol)
over standard input and output. `rein sync` writes the entry into the project's `.mcp.json`
beside any declared tool server, with the executable the session-start hook calls, so an
agent that opens the clone finds the server `rein`. sync also approves it in the settings file
it owns, `.claude/settings.local.json`, so Claude Code starts it without its once-per-project
prompt, once you have trusted the folder. Every tool is a query: it takes no machine lock and changes
nothing, says so with the read-only marker, and the agent's own permission rules decide which
run unasked, by the names `mcp__rein__<tool>`.

| Tool | Answers | Takes |
|---|---|---|
| `status` | what `rein status` prints, as data; runs without a login | nothing |
| `resolve` | what `rein resolve` prints: the declarations a project resolves and why | `project`, `organization` |
| `declaration_list` | what `rein declaration list` prints | `organization`, `level` |
| `declaration_describe` | one declaration: level, kind, id, selectors, files | `address`, `organization` |
| `organization_describe` | the organisation as its clone holds it: teams, projects, user level, labels | `organization` |
| `proposals_pending` | the proposal files distill wrote and propose has not sent | `directory` |
| `lint` | what `rein lint` reports for a folder | `path` |

An answer is structured JSON, also rendered as text. A refusal, such as a missing login, is a
tool error the agent can act on. `sync`, `distill`, `propose` and `restore` are not tools: they
change the machine or open pull requests and stay the developer's commands. The server is built
on the official Go SDK; the decision and what it changed about dependencies is the
[ADR](decisions/2026-10-23_mcp-gateway-on-the-official-sdk.md).

## Installing and releasing

A release is cut by hand, with `task rein:release`, which runs `reinctl release`, the operator
CLI of this repository under `cmd/reinctl`. It reads the conventional commits since the last
tag, a breaking change raising the major version, a `feat` the minor, a `fix` or a `perf` the
patch, and nothing else releasing; refuses off `main`, on a dirty tree, when `main` is not what
`origin` has, and when the tag exists; runs the gate before anything changes; then writes the
section into `CHANGELOG.md`, commits `chore(release): vX.Y.Z`, tags, and pushes `main` and the
tag; then GoReleaser builds rein for macOS, Linux and Windows on amd64 and arm64 on the same
machine, and publishes the GitHub Release with the archives and a checksum file, with the token
`gh` holds, handed to the build for that run and never written anywhere. The Homebrew cask goes
into `Danidani23/homebrew-tap` and the Scoop manifest into `Danidani23/scoop-bucket` when both
repositories exist; otherwise they are skipped and said so. Everything happens on the machine
and nothing on the host: no workflow, no secret on GitHub. `task rein:publish` repeats the last
step for a release whose publish failed after the tag was pushed. The doors, all pointing at
the release:

| How | Command |
|---|---|
| the script, macOS and Linux | `curl -fsSL https://raw.githubusercontent.com/Danidani23/agents-rein/main/install.sh \| sh` |
| the script, Windows | `irm https://raw.githubusercontent.com/Danidani23/agents-rein/main/install.ps1 \| iex` |
| Homebrew | `brew install --cask Danidani23/tap/rein` |
| Scoop | `scoop bucket add danidani23 https://github.com/Danidani23/scoop-bucket` then `scoop install rein` |
| Go | `go install github.com/Danidani23/agents-rein/cmd/rein@latest` |

`REIN_VERSION` pins the script to a release, `REIN_INSTALL_DIR` changes where it puts the
binary. The binaries are not signed yet; the cask removes macOS's quarantine flag at install,
and the script's download carries none. `task rein:release-check` validates the GoReleaser
configuration, and runs as part of the gate before every release.

The reasoning is the [ADR](decisions/2026-10-11_harness-tool-compiler-and-linter.md); the
design, the decisions and what is next are on the
[board](implementation_plans/2026-10-11_harness-distribution-tool.md). The code is Go,
with cobra, yaml.v3 and the official MCP SDK as its dependencies, each the subject of an
[ADR](decisions/2026-10-23_mcp-gateway-on-the-official-sdk.md). Every repeatable command is a task in
[Taskfile.yml](../Taskfile.yml): `task rein:install` installs the binary, `task rein:check` is
the gate, format check, vet, tests and lint; `task rein:coverage` tests with statement coverage
and writes the report to `.tmp/coverage.html`; `task --list` shows the rest. How a change is
made and how a release is cut, for a developer, is [developing.md](developing.md).
