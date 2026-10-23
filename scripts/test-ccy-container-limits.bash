#!/usr/bin/env bash
# Unit-test the session container's limits, lib/container-limits.bash: the pids limit every ccy
# session container is started with, CCY_PIDS_LIMIT.
#
# Sources the library from THIS repo, and checks the launcher calls it and passes the limit on
# the session container's run, so the text under test is the text that ships.
#
# WHY THIS TEST EXISTS (docs/ccy.md, "Container limits"). Each case below is a way the limit could
# again be left to the engine (no value, a value the engine reads as unlimited or rejects only
# after the session is built) or applied to something else. That the settled value reaches the
# session's tmux pane is tested in test-ccy-token-handoff.bash, beside the token hand-off.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
LIB="$CCY_DIR/lib/container-limits.bash"
LAUNCHER="$CCY_DIR/claude-yolo"

if [ ! -f "$LIB" ]; then
    echo "FAIL: library not found at $LIB" >&2
    exit 1
fi
print_error() { printf 'ERROR: %s\n' "$*" >&2; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

# limits [value|-] — ccy_container_limits in a fresh shell with CCY_PIDS_LIMIT set to the value
# ("-" leaves it unset): its status and the limit it settled on, as "<rc> <limit>".
limits() {
    (
        # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
        source "$LIB"
        if [ "$1" = - ]; then unset CCY_PIDS_LIMIT; else CCY_PIDS_LIMIT="$1"; fi
        rc=0
        ccy_container_limits 2>"$work/err" || rc=$?
        printf '%s %s' "$rc" "${CCY_PIDS_LIMIT:-}"
    )
}

echo "=== the default ==="
check "unset: the documented default" "0 4096" "$(limits -)"
check "empty: the default, as for every other CCY_ setting" "0 4096" "$(limits '')"
check "the default is above podman's 2048 and still a bound a fork bomb stops at" "yes" "$(
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
    source "$LIB"
    [ "$CCY_PIDS_LIMIT_DEFAULT" -gt 2048 ] && [ "$CCY_PIDS_LIMIT_DEFAULT" -le "$CCY_PIDS_LIMIT_MAX" ] && echo yes
)"

echo "=== an override ==="
check "a whole number in range is used as given" "0 8192" "$(limits 8192)"
check "the smallest allowed" "0 256" "$(limits 256)"
check "the largest allowed" "0 32768" "$(limits 32768)"

echo "=== refused before anything is built: there is no way to turn the limit off ==="
for bad in 0 -1 max unlimited none 2k 1e4 ' 4096' '4096 ' 04096 255 32769 4194304 99999999 '4096;id'; do
    got=$(limits "$bad")
    check "'$bad' refused" "1" "${got%% *}"
done
limits nope >/dev/null
check "the refusal names the variable and what it takes" "1" "$(grep -c 'CCY_PIDS_LIMIT=nope is not a pids limit: a whole number from 256 to 32768; it cannot be turned off' "$work/err")"

echo "=== the launcher ==="
check "the launcher lists the library in CCY_LIBS" "1" "$(grep -c -E '^CCY_LIBS=\(.* container-limits( |\))' "$LAUNCHER")"
check "the launcher sources it" "1" "$(grep -c -x -F "source \"\$SCRIPT_DIR/lib/container-limits.bash\"" "$LAUNCHER")"
check "the limit is settled with the other options, before anything is built" "yes" "$(awk '/^ccy_lifecycle_validate_options / {v=NR} /^ccy_container_limits [|][|] exit 1$/ {l=NR} /ccy_agent_container run / {r=NR} END {print (v && l == v + 1 && l < r) ? "yes" : "no"}' "$LAUNCHER")"
check "the session container's run passes it" "1" "$(grep -c -F "ccy_agent_container run \$DOCKER_FLAGS --rm --pids-limit \"\$CCY_PIDS_LIMIT\" \\" "$LAUNCHER")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
