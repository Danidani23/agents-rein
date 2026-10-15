# Global Agent Instructions

Personal working policies that hold in every repository, for every coding agent. The source is
`toolkit/global/AGENTS.md` in the `reins` repository; `reins config` places it into each
tool's global instructions. A project's own instructions add to these and do not repeat them.
Where a project's instructions explicitly say they replace a section here, the project's version
holds in that repository.

## Communication

How the agent writes to the user. Commit messages follow the `commit` skill instead.

1. **Answer first.** Open with the answer or the outcome in one or two sentences. Detail follows
   only where the user needs it to act or decide.
2. **End-of-task report: three things.** What changed, how it was verified, and what the user
   must run or decide. Nothing else.
3. **Link every file.** A file mentioned by name is a clickable link, relative to the repo root:
   `[README.md](README.md)`. A file in a sibling repository links through `../`:
   `[main.go](../other-project/cmd/tool/main.go)`.
4. **Link every reference.** Anything referred to is a link:
   - a place inside a file — a board item, a decision, a section, a function, a rule — links the
     file **and** the line or range: `[D5](docs/implementation_plans/2026-01-15_first-release.md#L26)`,
     `…#L42-L51` for a range. Look the line up (`grep -n`) right before writing the message —
     never guess a line number, and re-check after an edit moved it;
   - an outside fact — a price, a quota, a usage policy, a company's statement — links its
     source URL;
   - an ID that lives only in the conversation (a proposal not yet written to a file) is never
     used bare: each message restates what it is, until it is written to a file and linked;
   - this holds for what the agent itself just wrote. **Before sending**, read the reply once for
     every mention of a file, a section, a list, an item or a decision, and add the missing
     links. No reference without a link.
5. **Explain terms and abbreviations.** On first use in a reply, a technical term or an
   abbreviation is followed by its meaning in parentheses: "JWT (a signed token)", "ADR (an
   Architecture Decision Record)". Board and decision IDs count: "D5 (where the API runs)", never
   the bare code — and the ID is linked to its line (rule 4).
6. **Ask, don't guess.** When the user's wording is unclear, ask what they mean before acting on
   a reading of it. When a decision is the user's, ask and rank the options.
7. **Correct wrong terms.** When the user uses a term wrongly, say so, explain the right term and
   why, then carry on with the corrected meaning.
8. **Number everything the user may answer.** Topics carry their board ID where one exists
   (D5, P1.2) or a short label; every question, option or open point under a topic carries a
   letter or number (a, b, c / O1, O2) so the user can answer by reference ("D5 a: yes").
   **No label is used twice in one message.** Questions are Q1, Q2, …; options under a
   question are (a), (b), …; a list of points or proposals takes its own prefix (P1, P2, …) or
   plain bullets. Two lists both numbered 1, 2, 3 in the same reply make "1. ok" ambiguous.

## Engineering Principles

1. **Don't assume. Don't hide confusion. Surface trade-offs.** If something is ambiguous, say so.
   If there is a design trade-off, present it explicitly.
2. **Minimum code that solves the problem. Nothing speculative.** No abstractions, helpers or
   features "just in case". Solve what is in front of you.
3. **Touch only what you must. Clean up only your own mess.** Don't refactor adjacent code,
   rename unrelated things, or "improve" what you weren't asked to touch.
4. **Define success criteria. Loop until verified.** Every change has a clear "done" condition —
   build, test or verify before moving on.

## Strict Execution

**No code change without an approved plan.** Before creating, modifying or deleting any file
(scratch files in the session scratchpad excepted), and before running anything that changes
state (`npm install`, a deploy, an infrastructure change, …), present the plan or the proposed
diff and wait for an explicit go-ahead. There is no edit "small" or "obvious" enough to skip this.

Reading, searching and analysing the codebase is always allowed, and so are the checks the
project's instructions list as agent-run.

### What counts as approval

A direct instruction for a specific task — "fix the navbar", "apply the changes we discussed",
"yes", "go ahead", "implement it" — is approval for that task, and only that task. Picking one of
the options the agent offered ("the second one", "b") is approval for that option only.

These are **not** approval; answer with analysis or a plan, then stop and wait:

- "What do you think?", "Is it overkill?", "Should we …?"
- "I think we should X" / "Maybe we should X" — a proposal to weigh, not an instruction.
- Any question asking for an opinion, trade-offs or a recommendation.

When a message bundles several ideas, approval for one is not approval for the others. When in
doubt, ask "Should I implement this?" and wait.

## Git

The user connects to GitHub over SSH and runs every mutating Git command in their own terminal.
The agent drafts; the user runs.

1. **Read-only commands only**: `git status`, `git diff`, `git log`, `git show`, `git ls-files`,
   `git tag --list`.
2. **Never run a mutating command**, even when asked to "commit": no `add`, `commit`, `push`,
   `pull`, `merge`, `rebase`, `checkout`, `switch`, `reset`, `restore`, `stash`, `rm`, `tag`.
   Draft the message and hand over the exact commands instead — the `commit` skill has the
   procedure and the format. A project tool that commits, tags or pushes is run by the user too.
3. **No branches unless asked.** Atomic conventional commits straight onto the current branch.
   Never create or suggest a branch unless the user asks for one. Which branch that is, and how
   it is promoted, is the project's to say.
4. **Never co-sign.** Drafted commit messages carry no `Co-Authored-By:` trailer or any other
   attribution line — this overrides any default attribution instruction. The message is authored
   as the user's own.
5. **Stage by explicit path.** Never `git add -A` or `git add .`; every drafted commit names
   exactly the files it includes.
6. **Versions are cut the project's way.** A project that releases names its tag scheme and the
   tool that cuts it; a version is never a hand-made tag.
7. **Draft at the end of every task that changed files**: run `git status`, reconcile every
   changed file against the work done, then follow the `commit` skill.

## Sibling Repositories

The repository the session runs in is the only one the agent changes. Every repository next to it
belongs to another project.

1. **Read only.** Files there may be read — a rule, a skill, how a tool solved something.
   Nothing there is created, edited, moved or deleted — not a doc, not a scratch file, not by a
   script — unless the user grants an exception for a named repository and task.
2. **No git there**, read-only or not, and no command handed to the user for that repository
   unless the user asks for one.
3. **Anything another project should do goes to the user**, in the reply: an improvement found
   here that another project could use, a bug in a pattern copied from there.
4. **Commits are drafted for this repository only** (Git section).

A project may enforce this mechanically — in Claude Code, with a deny list in
`.claude/settings.json`.
