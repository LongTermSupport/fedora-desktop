#!/usr/bin/env bash
# Print the staged files, among the given files the ccy image copies, whose change needs a
# container version bump: a changed line that is not blank. A `#` line is skipped as a comment
# only in code (.py, .sh, .bash, or no extension) and never as a shebang: in a SKILL.md or a .txt
# guide it is a heading. A deletion is all `-` lines, so it counts too. With no HEAD (a first
# commit) every file counts. Called by lib/ccy-image-staged.bash.
# Usage: ccy-image-copied-changed.bash FILE...   (run from the repository root)
set -euo pipefail

if ! git rev-parse -q --verify HEAD >/dev/null; then
    printf '%s\n' "$@"
    exit 0
fi
# Captured first, not piped, so a large diff cannot be cut short.
diff=$(git diff --cached HEAD -- "$@")
awk '
    /^diff --git / { file = $NF; sub(/^b\//, "", file); prose = (file ~ /\.[^\/]+$/ && file !~ /\.(py|sh|bash)$/); next }
    /^(---|\+\+\+) (a\/|b\/|\/dev\/null)/ { next }
    /^[+-]/ && !(file in seen) && (prose ? $0 !~ /^[+-][[:space:]]*$/ : $0 !~ /^[+-][[:space:]]*(#([^!]|$)|$)/) { seen[file] = 1; print file }' <<<"$diff"
