#!/usr/bin/env bash
# Unit-test ccy's two container menus (lib/docker-health.bash) and how the launcher calls
# them (Plan 00161, CCY 3.88.1).
#
# WHY THIS TEST EXISTS. A headless `ccy --teams` launch in a checkout with another ccy
# container running reached the "Existing Containers Detected" menu. Its stdin was the
# session's own input fifo, so `read` kept returning and every answer was invalid: the
# menu printed "Invalid choice" 2.4 million times and the session never started. A launch
# nobody can answer now takes the menus' one safe answer, and a menu given no answer, or
# three wrong ones, gives up (return 2) instead of looping.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

for lib in common-pure.bash docker-health.bash; do
    if [ ! -f "$LIB_DIR/$lib" ]; then
        echo "FAIL: library not found at $LIB_DIR/$lib" >&2
        exit 1
    fi
done
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$LIB_DIR/common-pure.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/docker-health.bash
source "$LIB_DIR/docker-health.bash"

# The engine and the zombie scan, stubbed after the library so these definitions win.
container_cmd() {
    case "$1" in
        ps) printf '%s\n' "proj_yolo" ;;
        *) return 0 ;;
    esac
}
find_zombie_containers() { printf '%s\n' "other_yolo"; }
get_container_stats() { printf '1%%|1MiB\n'; }
get_container_uptime() { printf 'a minute\n'; }

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

# menu <function> <args...> — run it with stdin from $MENU_INPUT; sets rc and out.
menu() {
    out=$("$@" <"$MENU_INPUT" 2>&1)
    rc=$?
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
MENU_INPUT="$work/input"

echo "=== the existing-containers menu ==="
: >"$MENU_INPUT"
menu check_project_containers_startup proj yolo true
check "unattended: continues alongside without asking" "0" "$rc"
check "  and says so" "1" "$(grep -c 'Unattended launch: starting alongside 1 running container' <<<"$out")"
check "  and shows no menu" "0" "$(grep -c 'Options:' <<<"$out")"

menu check_project_containers_startup proj yolo false
check "attended, input closed: gives up with 2" "2" "$rc"
check "  saying no answer came" "1" "$(grep -c 'No answer: the input closed' <<<"$out")"

printf 'x\ny\nz\nc\n' >"$MENU_INPUT"
menu check_project_containers_startup proj yolo false
check "three wrong answers: gives up with 2" "2" "$rc"
check "  after two re-prompts" "2" "$(grep -c 'Invalid choice' <<<"$out")"

printf 'x\nc\n' >"$MENU_INPUT"
menu check_project_containers_startup proj yolo false
check "a wrong answer, then c: continues" "0" "$rc"
printf 'q\n' >"$MENU_INPUT"
menu check_project_containers_startup proj yolo false
check "q: the person quits (1)" "1" "$rc"

echo "=== the orphaned-containers menu ==="
: >"$MENU_INPUT"
menu check_zombie_containers_startup yolo true
check "unattended: leaves them running without asking" "0" "$rc"
check "  and says so" "1" "$(grep -c 'Unattended launch: leaving 1 container' <<<"$out")"
menu check_zombie_containers_startup yolo false
check "attended, input closed: gives up with 2" "2" "$rc"
printf 'x\ny\nz\n' >"$MENU_INPUT"
menu check_zombie_containers_startup yolo false
check "three wrong answers: gives up with 2" "2" "$rc"
printf 'i\n' >"$MENU_INPUT"
menu check_zombie_containers_startup yolo false
check "i: continues (0)" "0" "$rc"

echo "=== the launcher ==="
check "a headless launch takes the unattended answers" "1" \
    "$(grep -c -F "if [ \"\$HEADLESS_MODE\" = true ]; then" <(awk '/^CCY_NO_CONTAINER_PROMPTS=/,/^fi$/' "$LAUNCHER"))"
check "both menus are called with CCY_NO_CONTAINER_PROMPTS" "2" \
    "$(grep -c -F "\"\$CCY_NO_CONTAINER_PROMPTS\" || container_menu_rc=\$?" "$LAUNCHER")"
check "a menu with no answer (2) fails the launch, q (1) exits 0" "1" \
    "$(awk '/^ccy_container_menu_exit\(\)/,/^}/' "$LAUNCHER" | grep -c -F "[ \"\$rc\" -eq 1 ] && exit 0")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
