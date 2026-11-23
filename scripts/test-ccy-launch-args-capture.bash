#!/usr/bin/env bash
# Test that cc and ccy_tmux_insulate keep every argument their builders print (#322).
#
# WHY THIS EXISTS. Both capture the launch arguments with $(...) rather than reading a process
# substitution whose wait "$!" can fail falsely (#321). $(...) drops trailing newlines, so a
# capture that is not guarded loses an empty LAST argument (cc -p ""), and one that does not
# tell "no output" from "one empty line" turns no arguments into one empty argument. A builder
# that fails must still stop the launch.
#
# HOW. cc's own two capture lines are read out of its source and run against a stub builder,
# since cc itself runs only where it is deployed. ccy_tmux_insulate is the library's real
# function, run in a pseudo-terminal (it insulates only a terminal) by
# scripts/lib/ccy-test-insulate-driver.bash, with the real registry builders and the tmux
# steps stubbed to record the argv they were given.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
DRIVER="$REPO_ROOT/scripts/lib/ccy-test-insulate-driver.bash"
CC_DRIVER="$REPO_ROOT/scripts/lib/ccy-test-cc-capture-driver.bash"

for f in "$REPO_ROOT/files/var/local/claude-code/cc" "$DRIVER" "$CC_DRIVER" "$LIB_DIR/tmux-session.bash" "$LIB_DIR/session-registry.bash" "$LIB_DIR/common-pure.bash"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: $f does not exist" >&2
        exit 1
    fi
done
if ! command -v script >/dev/null; then
    echo "FAIL: script (util-linux) not found; this suite needs it for a pseudo-terminal." >&2
    exit 1
fi

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %q\n        got:  %q\n' "$label" "$want" "$got" >&2
    fi
}

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/launch-args.XXXXXX")"
export GIT_CEILING_DIRECTORIES="$REPO_ROOT/untracked"  # git must not read the checkout's own remote and config
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

# shown <args...> — the count and each argument bracketed, so an empty one is visible.
shown() {
    printf '%d:' "$#"
    printf '[%s]' "$@"
}

echo "── cc ──"
grep -E '^(CC_ARGS_TEXT=|mapfile .* CC_ARGS )' "$REPO_ROOT/files/var/local/claude-code/cc" >"$SCRATCH/cc-capture"
check "cc has its two capture lines" 2 "$(wc -l <"$SCRATCH/cc-capture")"

# The builder cc calls, as a command on PATH: one that prints its argv back, one line each,
# and one that prints a line and fails.
mkdir -p "$SCRATCH/echoing" "$SCRATCH/failing"
printf '#!/bin/sh\n[ "$#" -eq 0 ] || printf "%%s\\n" "$@"\n' >"$SCRATCH/echoing/ccy_registry_launch_args"
printf '#!/bin/sh\necho a\nexit 3\n' >"$SCRATCH/failing/ccy_registry_launch_args"
chmod 755 "$SCRATCH/echoing/ccy_registry_launch_args" "$SCRATCH/failing/ccy_registry_launch_args"

# cc_capture <args...> — cc's capture lines run with "$@" as cc's argv against the echoing
# builder; prints the CC_ARGS they built.
cc_capture() {
    PATH="$SCRATCH/echoing:$PATH" "$BASH" "$CC_DRIVER" "$SCRATCH/cc-capture" "$@"
}
check "cc: no arguments stay none" "$(shown)" "$(cc_capture)"
check "cc: one empty argument is kept" "$(shown "")" "$(cc_capture "")"
check "cc: an empty last argument is kept" "$(shown -p "")" "$(cc_capture -p "")"
check "cc: two empty last arguments are kept" "$(shown a "" "")" "$(cc_capture a "" "")"
check "cc: an empty first argument is kept" "$(shown "" b)" "$(cc_capture "" b)"
check "cc: spaces stay in one argument" "$(shown -p "hi there")" "$(cc_capture -p "hi there")"
failing_out=$(
    PATH="$SCRATCH/failing:$PATH" "$BASH" "$CC_DRIVER" "$SCRATCH/cc-capture" 2>"$SCRATCH/cc-failing-stderr"
    echo " rc=$?"
)
check "cc: a failing builder stops cc" " rc=1" "$failing_out"
check "cc: a failing builder says so" "cc: could not build claude's arguments." "$(cat "$SCRATCH/cc-failing-stderr")"

echo "── ccy_tmux_insulate ──"
mkdir -p "$SCRATCH/bin"
printf '#!/bin/sh\nexit 0\n' >"$SCRATCH/bin/tmux"
printf '#!/bin/sh\nexit 0\n' >"$SCRATCH/bin/systemd-run"
chmod 755 "$SCRATCH/bin/tmux" "$SCRATCH/bin/systemd-run"

# insulated <args...> — for /launcher "$@", run through ccy_tmux_insulate in a pseudo-terminal:
# the replay argv it records, then " | ", then the launch argv it starts the session with. Each
# run has a directory of its own; its terminal is shown when the driver recorded nothing.
runs=0
insulated() {
    runs=$((runs + 1))
    local run="$SCRATCH/run-$runs" cmd
    mkdir -p "$run"
    printf -v cmd '%q ' "$BASH" "$DRIVER" "$run/out" "$LIB_DIR" "$@"
    PATH="$SCRATCH/bin:$PATH" script -qec "$cmd" "$run/terminal" >"$run/script-output" 2>&1 </dev/null
    if [ ! -f "$run/out.launch" ] || [ ! -f "$run/out.replay" ]; then
        echo "the insulate driver recorded nothing; its terminal:" >&2
        cat "$run/terminal" >&2
        return 0
    fi
    printf '%s | %s' "$(cat "$run/out.replay")" "$(cat "$run/out.launch")"
}
# The replay keeps flags and their values and drops bare words before "--" (prefix cc).
check "insulate: no arguments stay none" "$(shown) | $(shown /launcher)" "$(insulated)"
check "insulate: an empty last argument is kept" "$(shown -p "") | $(shown /launcher -p "")" "$(insulated -p "")"
check "insulate: an empty first argument is kept" "$(shown) | $(shown /launcher "" b)" "$(insulated "" b)"
check "insulate: an empty argument after -- is kept" "$(shown -- "") | $(shown /launcher -- "")" "$(insulated -- "")"
check "insulate: a -- of the user's is kept once" "$(shown -m x -- a) | $(shown /launcher -m x -- a)" "$(insulated -m x -- a)"

echo ""
printf 'passed: %d   failed: %d\n' "$passed" "$failed"
if [ "$passed" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "OK"
