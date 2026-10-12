#!/usr/bin/env bash
# Links the toolkit into the folders Claude Code reads global instructions and skills from.
# OpenCode falls back to the same folders. Safe to run again: a correct link is left alone,
# anything else already at a path is reported and never replaced.
set -euo pipefail

toolkit="$(cd "$(dirname "$0")" && pwd)"
refused=0

link() {
  local target="$1" path="$2"
  if [ -L "$path" ] && [ "$(readlink "$path")" = "$target" ]; then
    echo "ok       $path"
  elif [ -e "$path" ] || [ -L "$path" ]; then
    echo "refused  $path already exists and is not a link to $target" >&2
    refused=1
  else
    mkdir -p "$(dirname "$path")"
    ln -s "$target" "$path"
    echo "linked   $path -> $target"
  fi
}

link "$toolkit/global/AGENTS.md" "$HOME/.claude/CLAUDE.md"
for skill in "$toolkit"/skills/*/; do
  name="$(basename "$skill")"
  link "$toolkit/skills/$name" "$HOME/.claude/skills/$name"
done

exit "$refused"
