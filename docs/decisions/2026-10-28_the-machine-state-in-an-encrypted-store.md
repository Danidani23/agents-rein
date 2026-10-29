# The machine's state lives in an encrypted store that only its owner opens

**Date:** 2026-10-28
**Status:** Accepted

## Context

Since D19 on the [board](../implementation_plans/2026-10-11_harness-distribution-tool.md#decisions),
the machine's state is `~/.rein/state.yaml`, a YAML file that rein owns. A header says so; the
file carries a version, is validated when read, is written through a temporary file and a rename
under a lock, and keeps the previous copy
([localstate.go](../../internal/adapters/localstate/localstate.go)). Beside it lie the ledger,
rein's record of every file it placed and moved out (`inserts/`, `removals.yaml`), and the
snapshots, as files. D19 named an embedded database as the fallback and left that to an ADR of
its own.

On 2026-10-28 the user asked that the state be protected against agents and people writing into
it (Q28). What can protect a file on one computer:

- **Not a database's users.** An embedded database has none. With SQLite, "the process that
  wants to access the database reads and writes directly from the database files on disk", and
  "Any program that is able to access the disk is able to use an SQLite database"
  ([SQLite](https://www.sqlite.org/serverless.html)).
- **Not the operating system's user account.** The terminal, Claude Code and whatever it runs,
  the CLI and the app all run as the developer, with the same rights to `~/.rein`. The deny list
  that `sync` writes keeps the agent's own tools from the project's files, not from `~/.rein`
  ([settings.local.json](../../.claude/settings.local.json)), and Claude Code's rules do not
  reach "arbitrary subprocesses … like a Python or Node script"
  ([Claude Code](https://code.claude.com/docs/en/permissions)).
- **A key only the owner holds.** Data encrypted so that it fails to open when changed without
  the key. Where the key is kept decides how strong that is:
  - On macOS a signed app keeps such a key in a keychain section of its own: an app "can always
    store and retrieve private keychain items", and sharing them "limits sharing to apps that
    are delivered by a single development team"
    ([Apple](https://developer.apple.com/documentation/security/sharing-access-to-keychain-items-among-a-collection-of-apps)).
  - On Windows the system's protection is per user: "only a user with the same logon credential
    as the user who encrypted the data can decrypt the data"
    ([Microsoft](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata)).
  - On Linux the desktop's keyring answers any program in the session, as it answers rein
    through `secret-tool` today ([keychain.go](../../internal/adapters/keychain/keychain.go)).

The same look found the GitHub token less protected than it seemed. rein stores it through the
`security` command, and "By default, the application which creates an item is trusted to access
its data without warning" (`security add-generic-password -h`, read 2026-10-28). The trusted
application is `security` itself, so any program that asks `security` for the token gets it.

The [app ADR](2026-10-28_an-always-running-app-owns-the-machine.md) makes one process, the owner,
the only one that opens the state. The store has no concurrent writers to serve, so what it
needs is encryption and a place for the key.

## Candidates Considered

1. **YAML as D19 left it, with `~/.rein` added to the deny list.** Readable, no dependency. It
   stops the agent's file tools but not a script, and nothing is encrypted.
2. **As 1, with a checksum in `state.yaml`.** A hand edit is caught and refused. The file is
   still readable by every program.
3. **bbolt with every value encrypted.** "A pure Go key/value store"
   ([bbolt](https://github.com/etcd-io/bbolt), v1.5.0 of 2026-06-21), whose library imports,
   outside itself, only `golang.org/x/sys`, already in rein's module graph
   ([go.mod](../../go.mod)). It "supports fully serializable ACID transactions", and it
   "obtains a file lock on the data file so multiple processes cannot open the same database at
   the same time", so the operating system keeps a second owner out. The encryption is AES-GCM
   from Go's standard library, a cipher that also detects a change.
4. **SQLite translated to pure Go (`modernc.org/sqlite`).** It offers SQL queries, but it is a C
   library translated into Go with its own runtime modules, and the encryption would still be
   rein's own.
5. **One encrypted file, standard library only.** The same protection and no dependency, without
   a database's transactions or file lock.

## Decision

Candidate 3, chosen by the user (Q28 c, Q33 a, Q34 b).

- **What moves in:** the machine state, the ledger (`inserts/`, `removals.yaml`) and the
  snapshots. The automatic-sync setting per project clone (D48) lives there with the rest of the
  state.
- **What stays files:**
  - the organisations' clones under `~/.rein/orgs/`, which git works on and checks against
    GitHub;
  - the proposals, which the developer edits by design: "deleting a candidate's file leaves it
    out" ([commands.md](../commands.md));
  - the files `sync` writes into each project, which the agent reads. They stay guarded by the
    deny list and by the ledger's hashes.
- **The key.**
  - The owner makes the key at its first start and keeps it in the system's keychain.
  - On macOS with the app, the key lives in the app's own keychain section. The GitHub tokens
    and the providers' keys move there from the login keychain, and only the signed app reads
    them.
  - Without the app, and on Windows and Linux, the key lives where rein keeps secrets today,
    readable by any program of the developer's. There the store stops accidents and edits by
    hand, not a program set on reading it.
- **The view.** `rein status` stays the human view. A command shows the store's content, read
  through the owner, for what used to be read in `state.yaml`; it changes nothing.
- **The move.** The first start of an owner of this version moves `state.yaml`, the ledger and
  the snapshots into the store, and reads them back before it removes the files.
- **Dependency.** `go.etcd.io/bbolt` joins the module, recorded here as the dependency rule of
  the [MCP gateway ADR](2026-10-23_mcp-gateway-on-the-official-sdk.md) asks.

## Consequences

- D19 is superseded, including its rule that snapshot contents stay "as files on disk … never
  inline". Of the guards it listed, atomic writes and the lock now come from bbolt's
  transactions and file lock, and validation from a decryption that fails on any change. The
  version for migrations stays.
- The state can no longer be read, compared or mended by hand. The view command and
  `rein reset machine` are the ways in.
- Any program running as the developer can still delete or replace the store. The owner then
  finds it gone, or not matching its key, and says so instead of starting empty.
- The protection is strong on macOS with the signed app, and weaker elsewhere, as the key's
  bullets say.
- `rein reset machine` empties the store and removes its key.
- The board records this as D51 and the item P6.2.
- Amended 2026-10-29 when P6.2 was built, by the agent, as the board's item records:
  - On a machine without a usable keychain, such as a Linux server or CI, the key is a file
    beside the store, `store.key`, readable by the developer alone, so `rein sync` works there.
  - `rein reset machine` erases the store and its key, then keeps the logins, whose tokens stay
    in the keychain, and the placements it could not remove, in a new store with a new key, as
    the reset kept them in `state.yaml` before.
  - The documents' names, such as `snapshots/<project>-<agent>-<time>.json`, are bbolt's keys and
    stay readable in the file; their content is encrypted.
