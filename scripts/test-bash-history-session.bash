#!/usr/bin/env bash
# Unit-test the bash history split (Plan 00138): with the Ctrl+R search bound, up-arrow
# walks only the commands typed in this terminal, while every command still lands in the
# shared history file that Ctrl+R searches. Without that search (root, or no fzf) the shell
# is stock bash, whose Ctrl+R searches the loaded list, so the list must hold everything.
# The pieces are the history block of files/etc/profile.d/zz_lts-fedora-desktop.bash and
# files/home/bashrc-includes/history-search.bash, sourced in that order as ~/.bashrc does.
#
# WHAT IS AT STAKE. Loading the shared file into every new shell makes up-arrow offer
# another terminal's last command, which is what the owner reported. The opposite mistake
# is worse and silent: a shell that stops loading the file but also stops appending to it,
# or overwrites it, loses history for good. So both halves are checked, through real
# interactive shells, because history loading and PROMPT_COMMAND only behave as on a host
# inside one.
#
# The include skips root by design, so as root (this repo's container) the shells run as
# the unprivileged uid 65534 through setpriv, on copies of the two files under a directory
# that account can read.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PROFILE_SRC="${HISTORY_TEST_PROFILE:-$REPO_ROOT/files/etc/profile.d/zz_lts-fedora-desktop.bash}"
INCLUDE_SRC="${HISTORY_TEST_INCLUDE:-$REPO_ROOT/files/home/bashrc-includes/history-search.bash}"

for f in "$PROFILE_SRC" "$INCLUDE_SRC"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: $f does not exist" >&2
        exit 1
    fi
done

as_user=()
if [ "$(id -u)" -eq 0 ]; then
    if ! command -v setpriv >/dev/null; then
        echo "FAIL: running as root and setpriv is not installed, so no shell can run as a normal user." >&2
        exit 1
    fi
    as_user=(setpriv --reuid=65534 --regid=65534 --clear-groups)
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
chmod 0755 "$WORK_DIR"
cp "$PROFILE_SRC" "$WORK_DIR/profile.bash"
cp "$INCLUDE_SRC" "$WORK_DIR/history-search.bash"
# Two PATHs: one that finds a stub fzf, and one of only the tools these shells run, so a
# real fzf installed on the machine cannot turn the stock-bash case into the split one.
mkdir "$WORK_DIR/with-fzf" "$WORK_DIR/no-fzf"
printf '#!/bin/sh\nexit 130\n' >"$WORK_DIR/with-fzf/fzf"
for tool in bash cut paste; do
    if ! ln -s "$(command -v "$tool")" "$WORK_DIR/no-fzf/$tool"; then
        echo "FAIL: $tool is not installed" >&2
        exit 1
    fi
done
chmod 0755 "$WORK_DIR/with-fzf" "$WORK_DIR/no-fzf" "$WORK_DIR/with-fzf/fzf"
chmod 0644 "$WORK_DIR/profile.bash" "$WORK_DIR/history-search.bash"

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

# new_home <name> — a home whose shared history already holds another terminal's command.
new_home() {
    local home="$WORK_DIR/$1"
    mkdir -p "$home/.local/state/bash"
    printf '%s\n' "echo from-another-terminal" >"$home/.local/state/bash/history"
    if [ "${#as_user[@]}" -gt 0 ]; then
        chown -R 65534:65534 "$home"
    fi
    chmod 0700 "$home/.local/state/bash"
    printf '%s' "$home"
}

# rc_file <name> [extra line] — the rc file a terminal starts with, sourcing the two pieces
# in ~/.bashrc's order. bash loads HISTFILE only AFTER the rc files, which is why they must
# be the rc file: sourcing them from the script would test a shell already loaded.
rc_file() {
    local rc="$WORK_DIR/$1.rc"
    printf 'source %q\nsource %q\n%s\n' "$WORK_DIR/profile.bash" "$WORK_DIR/history-search.bash" "${2-}" >"$rc"
    chmod 0644 "$rc"
    printf '%s' "$rc"
}

# run_shell <home> <rc> <PATH> <script> — an interactive bash reading the script from a
# pipe; its stdout, where the PROBE lines go. The snippet's terminal-only lines (the prompt
# file, stty) fail here and say so on stderr, which is discarded.
run_shell() {
    printf '%s\n' "$4" |
        "${as_user[@]}" env HOME="$1" PATH="$3" bash --rcfile "$2" -i 2>/dev/null
}
with_fzf="$WORK_DIR/with-fzf:$PATH"
no_fzf="$WORK_DIR/no-fzf"

dollar='$'
probe_list="printf 'PROBE-LIST:%s\n' \"${dollar}(HISTTIMEFORMAT='' builtin history | cut -c8- | paste -sd'|')\""
probe() { printf '%s\n' "$1" | awk -v k="PROBE-$2:" 'index($0, k) == 1 { print substr($0, length(k) + 1) }'; }

echo "== with the Ctrl+R search bound: the first terminal"
home="$(new_home split)"
rc="$(rc_file split)"
out="$(run_shell "$home" "$rc" "$with_fzf" "$(cat <<EOF
echo typed-in-the-first
$probe_list
printf 'PROBE-FILE:%s\n' "${dollar}HISTFILE"
EOF
)")"
list="$(probe "$out" LIST)"
check "up-arrow does not offer a command another terminal typed" "0" \
    "$(printf '%s\n' "$list" | grep -c 'from-another-terminal')"
check "up-arrow offers this terminal's own commands, in order" \
    "echo typed-in-the-first|$probe_list" "$list"
check "after the first prompt, HISTFILE names the shared file Ctrl+R reads" \
    "$home/.local/state/bash/history" "$(probe "$out" FILE)"
lines="$(grep -v '^#[0-9]' "$home/.local/state/bash/history")"
check "the shared file keeps what was there before, first" "echo from-another-terminal" \
    "$(printf '%s\n' "$lines" | awk 'NR == 1')"
check "and gains this terminal's commands after it, once each" "1" \
    "$(printf '%s\n' "$lines" | grep -cx 'echo typed-in-the-first')"

echo "== with the Ctrl+R search bound: a second terminal"
out="$(run_shell "$home" "$rc" "$with_fzf" "$probe_list")"
check "up-arrow in a new terminal does not offer the first terminal's commands" "0" \
    "$(probe "$out" LIST | grep -c 'typed-in-the-first')"

echo "== a prompt hook replaced before the first prompt"
# Something later in ~/.bashrc assigning PROMPT_COMMAND outright would stop the first prompt
# from pointing HISTFILE back at the shared file. The EXIT trap must still save the session.
home="$(new_home replaced)"
rc="$(rc_file replaced 'PROMPT_COMMAND=true')"
run_shell "$home" "$rc" "$with_fzf" "echo kept-by-the-exit-trap" >/dev/null
check "the session is still saved to the shared file when it exits" "1" \
    "$(grep -cx 'echo kept-by-the-exit-trap' "$home/.local/state/bash/history")"

echo "== without the Ctrl+R search (no fzf): stock bash"
home="$(new_home stock)"
rc="$(rc_file stock)"
out="$(run_shell "$home" "$rc" "$no_fzf" "$(cat <<EOF
$probe_list
printf 'PROBE-FILE:%s\n' "${dollar}HISTFILE"
EOF
)")"
check "the loaded list holds every terminal's commands, for stock Ctrl+R to search" "1" \
    "$(probe "$out" LIST | grep -c 'from-another-terminal')"
check "HISTFILE is the shared file from the start" "$home/.local/state/bash/history" "$(probe "$out" FILE)"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
