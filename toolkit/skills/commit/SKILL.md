---
name: commit
description: Draft the copy-paste git block for work just done — reconcile git status, split into atomic conventional commits with explicit paths, no attribution trailer. Use at the end of every task that changed files, or when asked to commit.
---

# Draft Commits

The agent never runs a mutating git command (Git section of the global instructions). It produces
one block the user pastes. The project's own instructions may add commit rules — scopes, files
committed after an operation, a release tool — and those hold alongside this procedure.

## Procedure

1. Run `git status`, then `git diff` where needed. Reconcile **every** changed file against the
   work done — never assume the tree only holds what you remember touching. Files that belong to
   no planned commit are called out explicitly, never swept in. A checkout that is not a git
   repository yet is said so, and the block starts with `git init`.
2. Split into atomic commits by concern. Each commit names its files by path — never
   `git add -A` or `git add .`.
3. Write each message in Conventional Commits form:
   - Types: `feat`, `fix`, `perf`, `docs`, `refactor`, `test`, `build`, `chore`. Where the
     project cuts releases from commits, only `feat`, `fix`, `perf` and a breaking change (`!`)
     cut one.
   - Scope in parentheses when it helps: the scopes the project's instructions list, otherwise
     the top-level directory or component the commit touches.
   - The subject is imperative, lowercase-first, no trailing period, 72 characters or fewer.
   - Details go in the body: one point per `- ` line, wrapped at 72 characters.
   - No `Co-Authored-By:` or any other attribution line.
4. Hand over **one fenced `bash` block** containing every `git add <paths>` followed by its
   `git commit -m "<subject>" -m "<body>"`, in order, so the whole sequence pastes at once. No
   comments inside the block; explanation goes outside it.
5. After the block, say whether anything remains uncommitted and remind the user to `git push`.
   Where the project has a release tool, a release is cut by it, never by a hand-made tag.
