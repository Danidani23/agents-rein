# The commands of `rein`

Every command that exists today, with what it does and the files it touches. The model behind
them is in [concepts.md](concepts.md). `rein <command> --help` says the same at the terminal,
with the flags; `?` at any question explains it.

What exists today. There is no setup step beyond `rein login`: your identity is your
GitHub account, and the agents on your machine are detected.

| Command | What it does |
|---|---|
| `rein key set [NAME]`, `rein key remove [NAME]` | Keeps the key of the organisation's distiller provider in the operating system's keychain, under the variable `providers.yaml` names, or `NAME`; the key comes through standard input, as in `pbpaste \| rein key set`, and typed at a terminal it is refused, since it would show. The distiller reads the keychain first and the environment second, as on CI. The keychain is macOS's login keychain, the desktop's keyring on Linux (GNOME Keyring or KWallet, through `secret-tool`, from `libsecret-tools` on Debian and Ubuntu) or Windows's Credential Manager. |
| `rein login`, `rein logout <login>` | Logs a GitHub account in through the Agents Rein app's device flow, a code typed on github.com; the token goes into the operating system's keychain and is renewed by itself. Several accounts live side by side; each organisation remembers the one that connected it. |
| `rein status` | Starts with the project the folder is in, or `--path`: its address and team, labels, the agent `sync` writes for, its settings counted by kind with the rules split into always on and by path, how many characters load into every session and into the skill listing, what `sync` wrote, and its proposals. Then the machine (the GitHub accounts logged in, the agents found, the files placed into the agent's home), the organisations, the other projects and the imports. |
| `rein org create` | Creates an organisation on the version control host (GitHub today): the repository, or an empty one of that name adopted, a first commit with the organisation record, the seeded label registry and CODEOWNERS, protection of `main` with a code-owner review, and the connection of this machine to it. Asks for the host, the account, the name, the repository, the owner and the visibility. Rein never deletes a repository. |
| `rein org connect <remote>`, `list`, `use [slug]`, `disconnect [slug]` | Clones an org repository and remembers it, snapshotting your agents' homes into `~/.rein/snapshots/` as the record of what was there before rein; lists the connected ones with the active one marked; switches; forgets one and removes its clone without touching the remote. A slug left out is asked from the connected ones. |
| `rein org destroy [slug]` | The way back to nothing for an organisation: disconnects it as above, then prints the steps to delete its repository on the host by hand. Rein never deletes a repository. |
| `rein team create`, `rein project create`, `rein project edit [slug]` | Write a team or a project record into the active organisation's clone and push it to `main`, the owner's way: name, description, and for a project its teams, labels and path, asked or given as flags. The required label categories are asked first, one value or several as the category allows; the optional ones come in one list, and two values of a single-valued category are refused. `project edit` asks the same again with the current values as defaults, or changes only the flags given. |
| `rein label list`, `add [category:value]`, `remove <category:value>`, `set <category>` | The label registry: what a project can be. `add` puts a value into a category or creates the category, asking or taking `--required`, every project carries one of its values, and `--multiple`, a project may carry several; `remove` refuses a value a project carries and the last value of a category; `set` changes `--required` or `--optional`, `--multiple` or `--single`. Each change is pushed to `main` the owner's way. |
| `rein init` | In a project clone: binds it to its project record by the repository's root commits, picking or creating the record when none carries them; moves `CLAUDE.md`, `.claude/` and `.mcp.json` into a snapshot under `~/.rein/snapshots/`, records the removal, adds a marked block to `.gitignore`, registers the clone and commits the move-out. |
| `rein distill` | Turns the snapshot of an initialised project's harness, or of your agent's home with `--home`, into candidate declarations at that level, one rule per `##` section of `CLAUDE.md` and of each rule file, a skill whole, compares each with what the organisation already declares by content and id, asks the distiller, your agent's CLI unless the organisation declares a provider in `declarations/providers.yaml` (any endpoint speaking Anthropic's Messages API, such as DeepSeek's or Moonshot's for Kimi, the key read from the variable it names), where each belongs and whether a rule or skill that mixes concerns is cut into units of one concern, its lines taken verbatim and every line accounted for, and writes it all as one proposal folder: one YAML file per candidate, `<kind>_<id>.yaml`, saying whether it goes, to which level and with which labels, with the distiller's reason and the candidate's text, beside `proposal.yaml` with the proposal's own fields; deleting a candidate's file leaves it out. An answer rein cannot accept goes back once into the same conversation with its problems, for the distiller to mend while it keeps the rest; `proposal.yaml` keeps those problems under `distiller.rejected`, and the distiller line names them. A distiller that fails, out of time or with an answer still wrong after that, writes nothing, so the next distill starts fresh; one that is not there, its CLI missing or logged out, refuses before anything is written and names `--distiller none`. The folder is `.rein/proposals/proposal-<id>/` in the project's folder, ignored by git, or under `~/.rein/proposals/pending/` for your agent's home; the id comes from the snapshot, and a second distillation of the same snapshot needs `--force`, refused before the distiller is asked. Your agent's CLI answers with no tools and without your tool servers, whose definitions would fill most of its prompt. The distiller's conversation stays in your agent's history, and the last line names the command that opens it. A proposal written as one file before folders still reads. For a project, the first part of its README and the names of the files its repository tracks, never their contents, tell the distiller what the project is. A lift to the organisation or a team names the labels of the projects it is for, or the distiller says why it holds for every project; the brief lists the organisation's projects with their labels, and distill and the pull request name the projects each such declaration reaches. The distiller also reviews the labels: a registry label the project plainly has but lacks, never a second value of a category of one value, a new one with its meaning when something it lifts needs it, saying for a new category whether a project may carry several of its values, and one that duplicates another, which is only flagged. Knowledge a human contributor needs too, such as the architecture or a style guide, can become a document for the project's repository, with a one-line rule pointing at it; the distiller picks its path beside the Markdown the repository tracks. A candidate that contradicts an existing declaration is marked as a conflict, left out until you set `conflict.resolution` in its file to `drop`, `override` or `amend`, its note naming the one the distiller's suggestion amounts to, `keep` being `override`; the header of each file says that the distiller's `suggestion`, `keep`, `drop` or `lift`, is advice already applied to `include` and `level`; the files in conflict close distill's output, each as its full path. A skill's frontmatter field the Agent Skills specification lacks, such as Claude Code's `argument-hint`, is removed with a note naming it and its value; then the candidates are linted as propose will lint them, and one that still fails carries the error as a note and is listed after the files in conflict, by its full path. While the distiller works, a line says what it judges against what, and its step shows whether it is thinking or how many judgements its answer holds so far. `--model` names the distiller's model, such as `sonnet` for a large harness; `--dry-run` shows the brief, `--distiller none` skips the model. |
| `rein propose [proposal, level folder or zip]` | The one way declarations enter an org repository. With the proposal, which you review and edit first, it does what it says, and refuses while a conflict is unresolved: `drop` keeps the existing declaration, `override` puts the candidate in with a first line naming what it replaces, refused against a `required` one, `amend` leaves it out for the existing one to change in a proposal of its own. A document goes into the project's clone, never over a file it has, left for you to commit; its pointer rule goes into the pull request. A label added or assigned changes the registry or the project's record on the same branch. Then it lints the included candidates, each error naming the file to mend by its full path, leaves out the ones identical to what the organisation has, writes the rest on a branch of the clone at their levels with their labels, pushes it and opens the pull request with the comparison as its body; then marks the proposal proposed and moves it to `~/.rein/proposals/<org>/` as the history. Inside a project folder with one pending proposal, no argument is needed. A plain level folder or an export zip goes whole to your user level unless `--level` says otherwise. |
| `rein declaration list`, `edit <address>`, `remove <address>` | What is around a declaration, each change a pull request on the org repository. `list` shows every declaration with its level, labels and enforcement. `edit` moves one to another level, carrying its files and removing the old ones, or changes its `applyIf`, `doNotApplyIf` and `enforcement`, asked with the current values as defaults or given as flags. `remove` deletes one, unless a skip list or an exception still names it. The content of a declaration changes through `propose`. |
| `rein resolve --project <slug>` | Shows which declarations apply to a project, in level order with what each replaced, what the skip lists took out and through which exception, and the problems that would make `sync` refuse. |
| `rein sync` | Writes the harness this clone's project resolves: `CLAUDE.md` with one imported file per level, rules, skills, hook scripts, permissions and hooks in `.claude/settings.local.json` merged with your own allow list, and `.mcp.json`; every file recorded with its hash, a hand edit stopping the next sync unless `--force`. A line in `CLAUDE.md` and the deny list keep the agent from editing these files by hand; a file in rein's folders that rein did not write is named, at session start too, and so is a rule pointing at a document the clone lacks, and `--force` keeps it in a snapshot for `rein distill --snapshot`. Every clone also gets the skill `rein`, which tells the agent how these files are managed and how a change is proposed, and `.mcp.json` names rein's own tool server, approved in the settings file. When Claude Code has not trusted the folder yet, sync warns in red and prints the command that makes it ask, `claude` in the folder; the tool server connects after that. `--all` for every clone; `--hook` is what the session-start hook runs. |
| `rein lint <path>` | Checks skills against the Agent Skills specification, rule files, permission rules, skills that share a name, and in an org repository every record: organisation, teams, projects, users and the label registry, with their references. Needs no identity, so CI runs it on an org repository. |
| `rein reset project` | Reverses `init` for one clone: the files it moved out come back from their snapshots, the rein block leaves `.gitignore`, the clone is forgotten on this machine, and the put-back is committed as the move-out was. |
| `rein reset machine` | Puts every initialised project back first, as `rein reset project` does, then removes everything rein placed into an agent's home, forgets the organisations and deletes their clones, and removes the ledger, the snapshots and the proposals: the machine as before rein. A project that fails to go back stops it before anything is wiped. `rein reset` alone changes nothing and names the two scopes, since a destructive command has no silent default. |
| `rein ui` | Serves pages from this machine and opens them in your browser. This machine shows what `rein status` shows: the logins, the agents, the connected organisations, the project clones and the proposals waiting. Projects shows one project of an organisation at a time: its teams and labels, what it resolves as a tree of levels, kinds and settings, each saying why it applies, what a skip list took out, and the files `sync` wrote into its clones here, each as written, edited by hand or missing. The top bar switches the organisation shown and the login whose user level applies, without changing the active organisation or the login that acts. From a clone's section, Sync, Sync every clone and Reset project run as the commands do: a preview says what will change, a confirmation starts it, its steps show as they run with a Cancel that does what Ctrl-C does, and the outcome names the next step; one action runs at a time, under the machine lock. It listens on 127.0.0.1 only, on a free port; the address it prints carries a token made for the run, which the first visit trades for a cookie, and a request without it or naming another host is refused. It runs until Ctrl-C; `--no-browser` prints the address without opening it. The decision is the [ADR](decisions/2026-10-27_rein-ui-a-gateway-served-to-the-browser.md). |
| `rein update` | Asks GitHub for the latest release. A rein installed by Homebrew, Scoop, winget or `go install` is updated by that tool, and `update` prints its command; one from a Linux package, the AUR or Nix gets the way to update it; a rein from the install script is replaced by rein itself after the archive passed the release's checksum. `--check` only reports. `sync` and `status` print one line when a newer release exists, at most once a day; an organisation's `minimum_rein_version` makes an older rein refuse to `sync`. |
| `rein version` | Prints the version. |

Planned, in the board's order: `rein import` and `rein export`, which move a level into and
out of an org repository; `rein review`; `rein migrate`.

## The background process

Every command above except `version`, `lint` and help runs in rein's background process, one
per machine and rein home (D52, D55). The `rein` you start, in a terminal, from the
session-start hook or as the tool server, sends the command line, your folder and your
environment to it and relays what you type, Ctrl-C and what the command prints, as `ssh` does;
the exit code is the command's. The first command that needs it starts it, as `rein owner`,
and it ends after 15 minutes without a command. It listens on `owner.sock` in rein's home, or,
when that path is too long for a socket, on `rein-<hash>.sock` in the system's temporary
folder; either is open to your account alone. What it has to say about itself goes to
`owner.log` in rein's home. A `rein` of another version or another build makes it stop taking
commands and end once its running ones have, and starts one of its own, so `rein update` takes
effect at the next command.

Several commands run at once, and when they collide the background process decides by the
rules of D56. A command that only reads, such as `status`, `resolve` or `org list`, never
waits. Commands that change something, the page's actions among them, take turns in the order
they arrive; one that waits says what for, which command, run from where and since when, and
Ctrl-C, or Cancel on the page, takes it out of the line with nothing done. A command identical
to one in line, the same command line in the same folder, does not run twice: it waits for that
one and ends as it did, with its exit code. The `list` commands of teams, projects, labels and
declarations pull the organisation's repository first, which changes its clone, so they wait
like a change.

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
| `proposals_pending` | the proposals distill wrote and propose has not sent | `directory` |
| `lint` | what `rein lint` reports for a folder | `path` |

An answer is structured JSON, also rendered as text. A refusal, such as a missing login, is a
tool error the agent can act on. `sync`, `distill`, `propose` and `reset` are not tools: they
change the machine or open pull requests and stay the developer's commands. The server is built
on the official Go SDK; the decision and what it changed about dependencies is the
[ADR](decisions/2026-10-23_mcp-gateway-on-the-official-sdk.md).

## Installing and releasing

A release is cut by hand, with `task rein:release`, which runs `reinctl release`, the operator
CLI of this repository under `cmd/reinctl`. It reads the conventional commits since the last
tag, a breaking change raising the major version, a `feat` the minor, a `fix` or a `perf` the
patch, and nothing else releasing; refuses off `main`, on a dirty tree, when `main` is not what
`origin` has, when GitHub's check has not passed on that commit, and when the tag exists; runs
the gate before anything changes; then writes the
section into `CHANGELOG.md`, commits `chore(release): vX.Y.Z`, tags, and pushes `main` and the
tag; then GoReleaser builds rein for macOS, Linux and Windows on amd64 and arm64 on the same
machine, and publishes the GitHub Release with the archives and a checksum file, with the token
`gh` holds, handed to the build for that run and never written anywhere. The `.deb`, `.rpm` and
`.apk` are attached to every release. Each further channel is written when what it needs
exists, and skipped with a line otherwise: the Homebrew cask into `Danidani23/homebrew-tap`, the
Scoop manifest into `Danidani23/scoop-bucket`, the winget manifest as a pull request to
`microsoft/winget-pkgs` from the fork `Danidani23/winget-pkgs`, the Nix package into
`Danidani23/nur-packages`, and the AUR package `rein-bin` with the SSH key at `~/.ssh/aur`.
Everything happens on the machine and nothing on the host: no release workflow, no secret on
GitHub. `task rein:publish` repeats the last step for a release whose publish failed after the
tag was pushed. The doors, all pointing at the release; a channel's row works once its
repository exists, which the
[board](implementation_plans/2026-10-11_harness-distribution-tool.md) tracks (P2.35 to P2.39):

| How | Command |
|---|---|
| the script, macOS and Linux | `curl -fsSL https://raw.githubusercontent.com/Danidani23/agents-rein/main/install.sh \| sh` |
| the script, Windows | `irm https://raw.githubusercontent.com/Danidani23/agents-rein/main/install.ps1 \| iex` |
| Homebrew, macOS and Linux | `brew install --cask Danidani23/tap/rein` |
| Scoop | `scoop bucket add danidani23 https://github.com/Danidani23/scoop-bucket` then `scoop install rein` |
| winget | `winget install Danidani23.rein` |
| a Linux package | the `.deb`, `.rpm` or `.apk` from the [latest release](https://github.com/Danidani23/agents-rein/releases/latest), installed with the system's package manager |
| the AUR | `rein-bin`, with an AUR helper such as `yay -S rein-bin` |
| Nix | `nix profile install github:Danidani23/nur-packages#rein` |
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
