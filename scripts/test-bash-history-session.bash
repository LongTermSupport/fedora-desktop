#!/usr/bin/env bash
# Unit-test the bash history split (Plan 00138): up-arrow walks only the commands typed in
# this terminal, while every command still lands in the shared history file that Ctrl+R
# searches. The history block lives in files/etc/profile.d/zz_lts-fedora-desktop.bash.
#
# WHAT IS AT STAKE. Loading the shared file into every new shell makes up-arrow offer
# another terminal's last command, which is what the owner reported. The opposite mistake
# is worse and silent: a shell that stops loading the file but also stops appending to it,
# or overwrites it, loses history for good. So both halves are checked, through real
# interactive shells, because history loading and PROMPT_COMMAND only behave as on a host
# inside one.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PROFILE="$REPO_ROOT/files/etc/profile.d/zz_lts-fedora-desktop.bash"

if [ ! -f "$PROFILE" ]; then
    echo "FAIL: $PROFILE does not exist" >&2
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
        echo "  ok: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label" >&2
        echo "        want: $want" >&2
        echo "        got:  $got" >&2
    fi
}

home="$WORK_DIR/home"
state="$home/.local/state/bash"
mkdir -p "$state"
chmod 0700 "$state"
printf '%s\n' "echo from-another-terminal" >"$state/history"
chmod 0600 "$state/history"

# run_shell <script> — an interactive bash reading the script from a pipe. Its stdout, where
# the PROBE lines go. The snippet is its rc file, as ~/.bashrc sources it on a host: bash
# loads HISTFILE only AFTER the rc files, so sourcing the snippet from the script instead
# would test a shell whose history was already loaded. The snippet's terminal-only lines
# (the prompt file, stty) fail here and say so on stderr, which is discarded.
rcfile="$WORK_DIR/bashrc"
printf 'source %q\n' "$PROFILE" >"$rcfile"
run_shell() {
    printf '%s\n' "$1" |
        HOME="$home" bash --rcfile "$rcfile" -i 2>/dev/null
}

dollar='$'
probe_list="printf 'PROBE-LIST:%s\n' \"${dollar}(HISTTIMEFORMAT='' builtin history | cut -c8- | paste -sd'|')\""
first_script="$(cat <<EOF
echo typed-in-the-first
$probe_list
printf 'PROBE-FILE:%s\n' "${dollar}HISTFILE"
EOF
)"
first_out="$(run_shell "$first_script")"

echo "== the first terminal"
first_list="$(printf '%s\n' "$first_out" | awk '/^PROBE-LIST:/ { sub(/^PROBE-LIST:/, ""); print }')"
check "up-arrow does not offer a command another terminal typed" "0" \
    "$(printf '%s\n' "$first_list" | grep -c 'from-another-terminal')"
check "up-arrow offers this terminal's own commands, in order" \
    "echo typed-in-the-first|$probe_list" "$first_list"
check "after the first prompt, HISTFILE names the shared file Ctrl+R reads" \
    "$state/history" "$(printf '%s\n' "$first_out" | awk '/^PROBE-FILE:/ { sub(/^PROBE-FILE:/, ""); print }')"

echo "== the shared file"
lines="$(grep -v '^#[0-9]' "$state/history")"
check "the shared file keeps what was there before, first" "echo from-another-terminal" \
    "$(printf '%s\n' "$lines" | awk 'NR == 1')"
check "and gains this terminal's commands after it, once each" "1" \
    "$(printf '%s\n' "$lines" | grep -cx 'echo typed-in-the-first')"

echo "== a second terminal, started after the first"
second_out="$(run_shell "$(cat <<EOF
$probe_list
EOF
)")"
second_list="$(printf '%s\n' "$second_out" | awk '/^PROBE-LIST:/ { sub(/^PROBE-LIST:/, ""); print }')"
check "up-arrow in a new terminal does not offer the first terminal's commands" "0" \
    "$(printf '%s\n' "$second_list" | grep -c 'typed-in-the-first')"
check "the first terminal's commands are still in the shared file for Ctrl+R" "1" \
    "$(grep -cx 'echo typed-in-the-first' "$state/history")"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
