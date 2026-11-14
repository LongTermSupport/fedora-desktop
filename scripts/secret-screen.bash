#!/usr/bin/env bash
# secret-screen.bash — show a secret read on stdin so that the terminal keeps no copy of it.
#
# A terminal keeps what its normal screen showed in the scrollback, and tmux in a pane's
# history: a password or token printed there stays readable long after (rule
# secret-on-normal-screen, CLAUDE/QA.md). On a terminal this shows the text on the alternate
# screen, which keeps no scrollback, and leaves it once Enter is pressed (read from /dev/tty, so
# the text can come down a pipe); the screen is left on Ctrl-C too. When stdout is not a
# terminal (a capture, a pipe, a file) the text is the payload: it is written out byte for byte.
#
#   ./vault.bash get name            (vault.bash pipes into this)
#   gh auth token | scripts/secret-screen.bash
#
# The Go twin is term.SecretScreen (src-go/internal/term); ccy, deployed without this checkout,
# uses `claude-yolo --secret-screen`, and Python the show_secret of helpers/alt_screen.py.

set -euo pipefail

if [ ! -t 1 ]; then
    exec cat
fi

# The text, trailing newlines kept.
text="$(cat && printf x)"
text="${text%x}"

trap 'printf "\e[?1049l"' EXIT
trap 'exit 130' INT TERM HUP
printf '\e[?1049h\e[H\e[2J%s\nCopy it, then press Enter to clear it from the screen.\n' "$text"
read -r _ </dev/tty
