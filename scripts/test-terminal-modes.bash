#!/usr/bin/env bash
# Unit-test the prompt hook that switches stray terminal modes off
# (files/home/bashrc-includes/terminal-modes.bash).
#
# WHAT IS AT STAKE. The hook writes escape sequences at every prompt of every terminal. A
# wrong byte clears the screen or scrollback, moves the cursor, or puts garbage into a
# captured stdout; a disturbed exit status makes the prompt report the wrong error. The
# cases drive a real interactive bash on a pseudo-terminal (script(1)), since the hook only
# prints when stdout is a terminal and readline only re-enables bracketed paste on one.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INCLUDE="$REPO_ROOT/files/home/bashrc-includes/terminal-modes.bash"

if [ ! -f "$INCLUDE" ]; then
    echo "FAIL: $INCLUDE does not exist" >&2
    exit 1
fi
if ! command -v script >/dev/null; then
    echo "FAIL: script (util-linux) not found — this gate needs it for a pseudo-terminal." >&2
    exit 1
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# contains <haystack> <needle> — "yes" or "no", byte for byte (no pattern characters).
contains() {
    if [[ "$1" == *"$2"* ]]; then echo yes; else echo no; fi
}

ESC=$'\e'
EXPECTED="${ESC}[?1000l${ESC}[?1002l${ESC}[?1003l${ESC}[?1005l${ESC}[?1006l${ESC}[?1015l"
EXPECTED+="${ESC}[?1004l${ESC}[?2004l${ESC}[>4m${ESC}[<99u${ESC}[?25h"
dollar='$'

# The commands every shell runs. probe_after sits AFTER the hook, as ps1Prompt does on a
# host, and keeps the status it was handed; it prints a marker so a case that expects
# silence can tell a hook that stayed quiet from a prompt that never ran.
session_script="$(cat <<EOF
PROMPT_COMMAND=(); source $INCLUDE; source $INCLUDE; seen=unset; probe_after() { seen=${dollar}?; printf 'PROMPT-RAN\n'; }; PROMPT_COMMAND+=(probe_after)
false
printf 'PROBE-STATUS:%s\n' "${dollar}seen"
printf 'PROBE-PC:%s\n' "${dollar}{PROMPT_COMMAND[*]}"
(exit 3); __terminal_modes_off; printf 'PROBE-DIRECT:%s\n' "${dollar}?"
exit
EOF
)"

# on_pty <TERM> — the session on a pseudo-terminal; the raw bytes it wrote.
on_pty() {
    printf '%s\n' "$session_script" >"$WORK_DIR/input"
    TERM="$1" script -qec "bash --norc --noprofile -i" /dev/null <"$WORK_DIR/input" 2>/dev/null
}

# on_pipe — the same session, interactive but with stdout a pipe.
on_pipe() {
    printf '%s\n' "$session_script" | TERM=xterm-256color bash --norc --noprofile -i 2>/dev/null
}

# probe_value <output> <name> — the value a PROBE-<name> line printed. A pty line is
# "<what readline wrote>\r<what the command printed>\r", so only the text after the last
# carriage return is the command's, less any escape sequence the hook wrote in front of
# it; the echoed command line itself never matches.
probe_value() {
    printf '%s\n' "$1" | awk -v key="PROBE-$2:" '{
        sub(/\r$/, ""); n = split($0, part, "\r"); line = part[n]
        gsub(/\033\[[?<>]?[0-9;]*[A-Za-z]/, "", line)
        if (index(line, key) == 1) print substr(line, length(key) + 1)
    }'
}

echo "== on a terminal"
tty_out="$(on_pty xterm-256color)"
check "the exact sequence is printed at the prompt" "yes" "$(contains "$tty_out" "$EXPECTED")"
check "the prompt hooks really ran" "yes" "$(contains "$tty_out" "PROMPT-RAN")"
check "a later prompt hook still sees the command's own exit status (false -> 1)" "1" \
    "$(probe_value "$tty_out" STATUS)"
# bash restores $? between array elements itself; a scalar PROMPT_COMMAND ("a; b") or a
# direct call relies on the hook returning the status it was given.
check "the hook returns the status it was handed (exit 3 -> 3)" "3" \
    "$(probe_value "$tty_out" DIRECT)"
check "sourcing twice adds the hook once" "__terminal_modes_off probe_after" \
    "$(probe_value "$tty_out" PC)"
for bad in "${ESC}c" "${ESC}[2J" "${ESC}[3J" "?1049"; do
    check "nothing clears or swaps the screen: no $(printf '%q' "$bad")" "no" "$(contains "$tty_out" "$bad")"
done
# The hook turns bracketed paste off; readline turns it back on when it next reads a line,
# after PROMPT_COMMAND. Only the bytes after the hook's last run count.
after_hook="${tty_out##*"$EXPECTED"}"
check "readline turns bracketed paste back on after the hook" "yes|yes" \
    "$(contains "$tty_out" "$EXPECTED")|$(contains "$after_hook" "${ESC}[?2004h")"

echo "== not a terminal"
pipe_out="$(on_pipe)"
check "the prompt hooks really ran" "yes" "$(contains "$pipe_out" "PROMPT-RAN")"
check "nothing is printed when stdout is not a terminal" "no" "$(contains "$pipe_out" "${ESC}[")"

echo "== TERM=dumb"
dumb_out="$(on_pty dumb)"
check "the prompt hooks really ran" "yes" "$(contains "$dumb_out" "PROMPT-RAN")"
check "nothing is printed for TERM=dumb" "no" "$(contains "$dumb_out" "${ESC}[?1000l")"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
