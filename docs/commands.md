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
| `rein distill` | Turns the snapshot of an initialised project's harness, or of your agent's home with `--home`, into candidate declarations at that level, compares each with what the organisation already declares by content and id, asks the judge, your agent's CLI until the organisation declares a provider, to compare the prose and suggest keep, drop, merge or lift, and writes it all with a report under `~/.rein/distill/` for `rein propose`. `--dry-run` shows the brief, `--judge none` skips the model. |
| `rein propose <folder or zip>` | The one way declarations enter an org repository: lints the candidates, leaves out what the organisation already has, writes the rest on a branch of its clone, pushes it and opens the pull request with the comparison as its body. A distillation knows its level and carries the judge's view, which propose applies: a lifted candidate goes to the organisation, the project's team or your user level with its `applyIf` labels, a dropped one stays out, and the placement is printed before the confirmation; `--as-distilled` keeps everything at the distilled level. Anything else goes to your user level unless `--level` says otherwise. |
| `rein resolve --project <slug>` | Shows which declarations apply to a project, in level order with what each replaced, what the skip lists took out and through which exception, and the problems that would make `sync` refuse. |
| `rein sync` | Writes the harness this clone's project resolves: `CLAUDE.md` with one imported file per level, rules, skills, hook scripts, permissions and hooks in `.claude/settings.local.json` merged with your own allow list, and `.mcp.json`; every file recorded with its hash, a hand edit stopping the next sync unless `--force`. Every clone also gets the skill `rein`, which tells the agent how these files are managed and how a change is proposed. `--all` for every clone; `--hook` is what the session-start hook runs. |
| `rein lint <path>` | Checks skills against the Agent Skills specification, rule files, permission rules, skills that share a name, and in an org repository every record: organisation, teams, projects, users and the label registry, with their references. Needs no identity, so CI runs it on an org repository. |
| `rein restore` | Reverses `init` for one clone: the files it moved out come back from their snapshots, the rein block leaves `.gitignore`, the clone is forgotten on this machine, and the put-back is committed as the move-out was. |
| `rein reset` | Restores every initialised project first, then removes everything rein placed into an agent's home, forgets the organisations and deletes their clones, and removes the ledger, the snapshots and the distillations: the machine as before rein. A restore that fails stops it before anything is wiped. |
| `rein version` | Prints the version. |

Planned, in the board's order: `rein import` and `rein export`, which move a level into and
out of an org repository; `rein review`; `rein migrate`; `rein update`.

The reasoning is the [ADR](docs/decisions/2026-10-11_harness-tool-compiler-and-linter.md); the
design, the decisions and what is next are on the
[board](docs/implementation_plans/2026-10-11_harness-distribution-tool.md). The code is Go,
with cobra and yaml.v3 as the only dependencies. Every repeatable command is a task in
[Taskfile.yml](Taskfile.yml): `task rein:install` installs the binary, `task rein:check` is the
gate, format check, vet, tests and lint; `task --list` shows the rest.
