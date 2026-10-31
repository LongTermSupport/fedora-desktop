#!/usr/bin/env bash
# Unit-test ccy's two container menus (lib/docker-health.bash) and how the launcher calls
# them (Plan 00161, CCY 3.88.1), and the Docker side of the cross-engine network cleanup
# (lib/docker-network-cleanup.bash, CCY 3.104.11).
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
# CCY_TEST_LIB_DIR: another lib/ to judge (an older revision, to see this suite fail on it).
LIB_DIR="${CCY_TEST_LIB_DIR:-$REPO_ROOT/files/var/local/claude-yolo/lib}"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

for lib in common-pure.bash docker-health.bash docker-network-cleanup.bash; do
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
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/docker-network-cleanup.bash
source "$LIB_DIR/docker-network-cleanup.bash"
# common.bash's wrapper, which needs a live engine to source.
container_cmd() { "$CONTAINER_ENGINE" "$@"; }

if [ -z "$(command -v go)" ]; then
    echo "FAIL: go is needed by this test" >&2
    exit 1
fi
mkdir -p "$REPO_ROOT/untracked/scratch"
work=$(mktemp -d "$REPO_ROOT/untracked/scratch/ccy-container-menus-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
MENU_INPUT="$work/input"

# The listings and stops are asked of the real Go front (ccylaunch F20, F21), built here, through
# a fake podman: `ps` answers FAKE_PS_* (FAKE_PS_RELIST, when set, for a listing by one
# container's own name), `stop` FAKE_STOP_*, `container exists` FAKE_EXISTS_EXIT, `inspect`
# FAKE_INSPECT_*. The cleanup's fake docker answers FAKE_D*, logging each call.
build_out="$(cd "$REPO_ROOT/src-go" && go build -trimpath -buildvcs=false -o "$work/claude-yolo" ./cmd/claude-yolo 2>&1)" || {
    echo "FAIL: the Go front did not build: $build_out" >&2
    exit 1
}
mkdir -p "$work/bin"
cat >"$work/bin/podman" <<'EOF'
#!/usr/bin/env bash
case "$1" in
ps) out="${FAKE_PS_OUT-proj_yolo
}"
    [[ " $* " == *" name=_"* || -z "${FAKE_PS_RELIST+set}" ]] || out="$FAKE_PS_RELIST"
    printf '%s' "$out"; printf '%s' "${FAKE_PS_ERR:-}" >&2; exit "${FAKE_PS_EXIT:-0}" ;;
inspect) printf '%s' "${FAKE_INSPECT_OUT-true
}"; printf '%s' "${FAKE_INSPECT_ERR:-}" >&2; exit "${FAKE_INSPECT_EXIT:-0}" ;;
stop) printf '%s' "${FAKE_STOP_ERR:-}" >&2; exit "${FAKE_STOP_EXIT:-0}" ;;
container) exit "${FAKE_EXISTS_EXIT:-0}" ;;
esac
echo "fake podman: unscripted: $*" >&2
exit 99
EOF
cat >"$work/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_DOCKER_LOG"
case "$1 ${2:-}" in
"network rm") printf '%s' "${FAKE_DNETRM_ERR:-}" >&2; exit "${FAKE_DNETRM_EXIT:-0}" ;;
"network ls") printf '%s' "${FAKE_DNETLS_OUT-}"; exit 0 ;;
"network prune") printf '%s' "${FAKE_DPRUNE_ERR:-}" >&2; exit "${FAKE_DPRUNE_EXIT:-0}" ;;
esac
case "$1" in
ps)
    if [[ " $* " == *" -a "* || " $* " == *" --all "* ]]; then printf '%s' "${FAKE_DSTORE_OUT-}"; exit 0; fi
    if [[ " $* " == *" network="* ]]; then printf '%s' "${FAKE_DNETPS_OUT-web
db
}"; else printf '%s' "${FAKE_DPS_OUT-web
db
other
}"; fi
    printf '%s' "${FAKE_DPS_ERR:-}" >&2; exit "${FAKE_DPS_EXIT:-0}" ;;
stop) printf '%s' "${FAKE_DSTOP_ERR:-}" >&2; exit "${FAKE_DSTOP_EXIT:-0}" ;;
esac
echo "fake docker: unscripted: $*" >&2
exit 99
EOF
chmod +x "$work/bin/podman" "$work/bin/docker"
export FAKE_DOCKER_LOG="$work/docker.log"
export PATH="$work/bin:$PATH"
SCRIPT_DIR="$work"
CONTAINER_ENGINE=podman

# The real zombie scan, judged before it is stubbed: a listing that failed is a failure.
zombie_out=$(FAKE_PS_EXIT=125 FAKE_PS_ERR="Error: database is locked" find_zombie_containers yolo 2>&1)
zombie_rc=$?
# An inspect that failed: the container still listed is the engine failing; gone from the
# listing is the race of a container that exited, and is skipped.
inspect_out=$(FAKE_INSPECT_EXIT=125 FAKE_INSPECT_ERR="Error: database is locked" find_zombie_containers yolo 2>&1)
inspect_rc=$?
raced_out=$(FAKE_INSPECT_EXIT=125 FAKE_INSPECT_ERR="Error: no such container" FAKE_PS_RELIST="" find_zombie_containers yolo 2>&1)
raced_rc=$?
tty_out=$(find_zombie_containers yolo 2>&1)

# The zombie scan and the per-row stats, stubbed after the library so these definitions win.
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

echo "=== listings and stops say what failed ==="
: >"$MENU_INPUT"
FAKE_PS_EXIT=125 FAKE_PS_ERR="Error: database is locked" menu check_project_containers_startup proj yolo false
check "a listing that failed fails the launch (2), never 'no containers'" "2" "$rc"
check "  with the engine's cause" "1" "$(grep -c 'could not be listed: .*database is locked' <<<"$out")"
check "  and no menu" "0" "$(grep -c 'Options:' <<<"$out")"
printf 's\n' >"$MENU_INPUT"
menu check_project_containers_startup proj yolo false
check "s: a stopped container continues (0)" "0" "$rc"
check "  ticked" "1" "$(grep -c 'Stopping proj_yolo... ✓' <<<"$out")"
FAKE_STOP_EXIT=125 FAKE_EXISTS_EXIT=1 menu check_project_containers_startup proj yolo false
check "s: a container the store no longer has is already gone, and continues (0)" "0" "$rc"
check "  said so" "1" "$(grep -c 'Stopping proj_yolo... already gone' <<<"$out")"
FAKE_STOP_EXIT=125 FAKE_STOP_ERR="Error: permission denied" menu check_project_containers_startup proj yolo false
check "s: a stop that failed fails the launch (2)" "2" "$rc"
check "  with the engine's cause, never 'may already be stopped'" "1 0" \
    "$(grep -c 'Stopping proj_yolo... ✗ .*permission denied' <<<"$out") $(grep -c 'may already' <<<"$out")"
printf 'a\n' >"$MENU_INPUT"
FAKE_STOP_EXIT=125 FAKE_STOP_ERR="Error: permission denied" menu check_zombie_containers_startup yolo false
check "orphans, a: a stop that failed fails the launch (2)" "2" "$rc"
check "  and does not say they all stopped" "0" "$(grep -c 'All orphaned containers stopped' <<<"$out")"
check "the zombie scan whose listing failed is a failure (1)" "1" "$zombie_rc"
check "  with the engine's cause" "1" "$(grep -c 'database is locked' <<<"$zombie_out")"
check "the zombie scan whose inspect failed on a listed container is a failure (1)" "1" "$inspect_rc"
check "  with the engine's words, never 'not a zombie'" "1" "$(grep -c 'proj_yolo could not be inspected: .*database is locked' <<<"$inspect_out")"
check "the zombie scan skips a container that exited before its inspect (0)" "0 " "$raced_rc $raced_out"
check "the zombie scan finds a TTY container with no run process" "proj_yolo" "$tty_out"
FAKE_PS_EXIT=125 FAKE_PS_ERR="Error: database is locked" menu clean_stale_containers_startup yolo
check "the stale cleanup whose listing failed is a failure (1)" "1" "$rc"
check "  with the engine's cause" "1" "$(grep -c 'database is locked' <<<"$out")"
FAKE_PS_EXIT=125 FAKE_PS_ERR="Error: database is locked" menu show_container_top yolo
check "ccy --top whose listing failed is a failure (1)" "1" "$rc"
check "  never 'No CCY containers currently running'" "0" "$(grep -c 'No CCY containers' <<<"$out")"

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

echo "=== the cross-engine cleanup's Docker side ==="
# cleanup <args...> — run ccy_docker_network_cleanup with a fresh docker log; sets rc, out, log.
cleanup() {
    : >"$FAKE_DOCKER_LOG"
    out=$(ccy_docker_network_cleanup "$@" 2>&1)
    rc=$?
    log=$(tr '\n' ';' <"$FAKE_DOCKER_LOG")
}
cleanup project proj_net
check "a: stops the network's containers, then removes it (0)" "0" "$rc"
check "  through the front" "ps --filter network=proj_net --format {{.Names}};stop web;stop db;network rm proj_net;" "$log"
FAKE_DNETRM_EXIT=1 FAKE_DNETRM_ERR="Error: network proj_net not found" cleanup project proj_net other_net
check "a: a network already gone (the front's 5) goes on to the next (0)" "0" "$rc"
check "  and removes the next" "2" "$(grep -o 'network rm' <<<"$log" | grep -c .)"
FAKE_DNETRM_EXIT=1 FAKE_DNETRM_ERR="Error: network has active endpoints" FAKE_DNETLS_OUT=$'proj_net\n' cleanup project proj_net other_net
check "a: a removal that failed stops the cleanup (1)" "1" "$rc"
check "  with the engine's words, before the next network" "1 1" \
    "$(grep -c 'could not be removed: .*active endpoints' <<<"$out") $(grep -o 'network rm' <<<"$log" | grep -c .)"
FAKE_DSTOP_EXIT=1 FAKE_DSTOP_ERR="Error: permission denied" FAKE_DSTORE_OUT=$'web\ndb\n' cleanup project proj_net
check "a: a stop that failed stops the cleanup (1), the network left" "1 0" "$rc $(grep -c 'network rm' <<<"$log")"
check "  with the engine's words" "1" "$(grep -c 'Stopping web... ✗ .*permission denied' <<<"$out")"
FAKE_DPS_EXIT=1 FAKE_DPS_ERR="Cannot connect to the Docker daemon" cleanup project proj_net
check "a: a listing that failed stops the cleanup (1), nothing stopped" "1 0" "$rc $(grep -c 'stop\|network rm' <<<"$log")"
check "  with the engine's words" "1" "$(grep -c 'could not be listed: .*Cannot connect' <<<"$out")"
FAKE_DPS_ERR=$'WARNING: a warning\n' cleanup project proj_net
check "a: an engine warning is never read as a container name" "0 0" "$rc $(grep -c 'stop WARNING' <<<"$log")"
cleanup all
check "b: stops every running container, then prunes (0)" "0" "$rc"
check "  through the front" "ps --format {{.Names}};stop web;stop db;stop other;network prune -f;" "$log"
FAKE_DPS_OUT="" cleanup all
check "b: no container running: prunes (0)" "0 ps --format {{.Names}};network prune -f;" "$rc $log"
FAKE_DSTOP_EXIT=1 FAKE_DSTOP_ERR="Error: permission denied" FAKE_DSTORE_OUT=$'web\n' cleanup all
check "b: a stop that failed stops the cleanup (1), nothing pruned" "1 0" "$rc $(grep -c 'prune' <<<"$log")"
check "  with the engine's words, never '(continuing)'" "1 0" \
    "$(grep -c 'Stopping web... ✗ .*permission denied' <<<"$out") $(grep -c 'continuing' <<<"$out")"
FAKE_DPS_EXIT=1 FAKE_DPS_ERR="Cannot connect to the Docker daemon" cleanup all
check "b: a listing that failed stops the cleanup (1), nothing pruned" "1 0" "$rc $(grep -c 'stop\|prune' <<<"$log")"
FAKE_DPRUNE_EXIT=1 FAKE_DPRUNE_ERR="Error: prune refused" cleanup all
check "b: a prune that failed is a failure (1), never 'pruned'" "1 0" "$rc $(grep -c 'networks pruned' <<<"$out")"
check "  with the engine's words" "1" "$(grep -c 'prune refused' <<<"$out")"
cleanup sideways
check "an unknown mode is refused (64)" "64" "$rc"

echo "=== the launcher ==="
check "the cross-engine menu's a and b run the cleanup and stop on its failure" "1 1" \
    "$(grep -c -F 'ccy_docker_network_cleanup project "${DOCKER_NETWORKS[@]}" || exit 1' "$LAUNCHER") $(grep -c -F 'ccy_docker_network_cleanup all || exit 1' "$LAUNCHER")"
check "  and nothing in it goes on past a stop" "0" "$(grep -c 'could not be stopped (continuing)' "$LAUNCHER")"
check "a headless launch takes the unattended answers" "1" \
    "$(grep -c -F "if [ \"\$HEADLESS_MODE\" = true ]; then" <(awk '/^CCY_NO_CONTAINER_PROMPTS=/,/^fi$/' "$LAUNCHER"))"
check "both menus are called with CCY_NO_CONTAINER_PROMPTS" "2" \
    "$(grep -c -F "\"\$CCY_NO_CONTAINER_PROMPTS\" || container_menu_rc=\$?" "$LAUNCHER")"
check "a menu with no answer (2) fails the launch, q (1) exits 0" "1" \
    "$(awk '/^ccy_container_menu_exit\(\)/,/^}/' "$LAUNCHER" | grep -c -F "[ \"\$rc\" -eq 1 ] && exit 0")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
