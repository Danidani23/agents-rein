---
name: mermaid-diagrams
description: How to draw Mermaid diagrams in Markdown so every doc reads the same way — one diagram kind per purpose, and the syntax gotchas that break rendering (semicolons, colons in labels, colours, labels starting with a slash). Use when adding or editing a mermaid block in any Markdown file.
---

# Mermaid Diagrams

Diagrams in docs are Mermaid, never ASCII art (`docs-and-taskfile` skill); this skill says how.

## 1. One kind per purpose

- `sequenceDiagram` — a process: who calls whom, in what order (a request going from the browser
  through a gateway to the API).
- `stateDiagram-v2` — a lifecycle: the states of one thing and the legal transitions.
- `flowchart TD` / `flowchart LR` — topology or dependencies (how the front end, the gateway and
  the API connect).

One diagram per process, under a heading that names it. External services are named by product.

## 2. Syntax gotchas (each one cost a broken render)

- **No semicolon anywhere in a diagram.** Mermaid reads `;` as a statement separator, so a label
  like `retry; else park` fails the whole diagram.
- **A colon only as Mermaid's own delimiter** — after `participant X as`, never inside the alias
  text; after the arrow's target in a message. Inside a label, write "status open", not
  "status: open".
- **No colours.** `box rgb(…)` and `style … fill:` render unreadably in dark and light themes
  alike.
- Safe in labels: parentheses, braces `{addr}`, `+`, `|`, `→`, commas. `<br/>` breaks a line in
  flowchart nodes. A node label starting with `/` or `\` (`[/api]`) is read as a parallelogram
  shape — quote it: `["/api"]`.
- `Note over A,B: text` is fine; `alt` / `else` / `loop` need a text after the keyword.
- There is no local renderer — a diagram is verified by the human opening the file; say so when
  handing one over.
