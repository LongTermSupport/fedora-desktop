#!/usr/bin/env bash
# Unit-test how ccy answers the questions that would start a project's compose services, and
# the launch prompts nobody can answer (files/var/local/claude-yolo/lib/network-management.bash,
# lib/common-pure.bash, and the launcher's own prompt sites).
#
# WHY THIS EXISTS (fedora-desktop#87). A session restored after a reboot found its project's
# compose network with the containers stopped, and stopped at "Start services with
# podman-compose up -d? [Y/n]" with nobody there. The session record now says how the services
# stood, the restore replays it as --compose start|skip, and a question with nobody to answer
# it (a --headless launch, or stdin that is not a terminal) is refused by name instead of
# reading the session's input or hanging. These are the decisions; each is driven here in a
# fresh shell, on a pseudo-terminal when a person is supposed to be there, against a stubbed
# engine and compose command.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

for f in "$LIB_DIR/common-pure.bash" "$LIB_DIR/network-management.bash" "$LAUNCHER"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: $f not found" >&2
        exit 1
    fi
done
if [ -z "$(command -v script)" ]; then
    echo "FAIL: script (util-linux) is needed to give a case a terminal, and is not installed" >&2
    exit 1
fi
# The CCY_PROMPT_* texts the cases look for.
# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"

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
has() { if [[ "$1" == *"$2"* ]]; then echo yes; else echo no; fi; }

mkdir -p "$REPO_ROOT/untracked/scratch"
WORK="$(mktemp -d "$REPO_ROOT/untracked/scratch/compose-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
PROJECT="$WORK/project-a"
mkdir -p "$PROJECT"
UP_LOG="$WORK/up.log"
RUNNING="$WORK/running"

# run_case <terminal|none> <answer> <settings> <call> — run <call> in a fresh shell with the
# libraries sourced, the engine and podman-compose stubbed, and <settings> (assignments) in
# force. With "terminal" stdin is a pseudo-terminal fed <answer>; with "none" it is /dev/null.
# The call runs in a subshell, so a refusal's `exit` ends only it. Prints everything it said,
# then "ret=<call's status>" when it returned, "outcome=<CCY_COMPOSE_OUTCOME>", and
# "rc=<subshell status>" last.
#
# The stubs: `network inspect --format {{len .Containers}}` reads $RUNNING (how many containers
# are up on any network); `podman-compose up -d` appends to $UP_LOG, exits STUB_UP_RC and, on
# success, brings one container up; `podman-compose ps -q` prints STUB_PS_IDS, whose
# containers inspect as running when STUB_PS_RUNNING is true.
run_case() {
    local stdin_kind="$1" answer="$2" settings="$3" call="$4"
    local driver="$WORK/driver.bash"
    cat >"$driver" <<DRIVER
set -uo pipefail
cd "$PROJECT" || exit 99
export CONTAINER_ENGINE=podman
STUB_UP_RC=0 STUB_PS_IDS="" STUB_PS_RUNNING=false
source "$LIB_DIR/common-pure.bash"
source "$LIB_DIR/network-management.bash"
sleep() { :; }
container_cmd() {
    case "\$1 \${2:-}" in
    "network inspect") cat "$RUNNING" ;;
    "network ls") printf '%s\n' project-a_default ;;
    "inspect "*) printf '%s\n' "\$STUB_PS_RUNNING" ;;
    *) return 1 ;;
    esac
}
podman-compose() {
    case "\$*" in
    "up -d")
        printf 'up -d\n' >>"$UP_LOG"
        [ "\$STUB_UP_RC" -eq 0 ] || return "\$STUB_UP_RC"
        printf '1\n' >"$RUNNING"
        ;;
    "ps -q") printf '%s\n' "\$STUB_PS_IDS" ;;
    *) return 1 ;;
    esac
}
$settings
( $call; echo "ret=\$?"; echo "outcome=\${CCY_COMPOSE_OUTCOME:-}" )
echo "rc=\$?"
DRIVER
    if [ "$stdin_kind" = terminal ]; then
        script -qec "bash $(printf '%q' "$driver")" /dev/null <<<"$answer" 2>&1 | tr -d '\r'
    else
        bash "$driver" </dev/null 2>&1
    fi
}
field() { printf '%s\n' "$2" | awk -v k="$1=" 'index($0, k) == 1 { v = substr($0, length(k) + 1) } END { print v }'; }
ups() { if [ -f "$UP_LOG" ]; then grep -c . "$UP_LOG"; else echo 0; fi; }
# fresh <containers-running> [compose-file] — reset the stubs' state for a case.
fresh() {
    rm -f "$UP_LOG" "$PROJECT"/*.yml
    printf '%s\n' "$1" >"$RUNNING"
    if [ -n "${2:-}" ]; then : >"$PROJECT/$2"; fi
}

echo ""
echo "=== who can answer a launch prompt ==="
check "stdin that is not a terminal: nobody" "nobody" \
    "$(field ret "$(run_case none "" "" 'ccy_nobody_to_ask')" | awk '{ print ($1 == 0 ? "nobody" : "someone") }')"
check "a terminal: someone" "someone" \
    "$(field ret "$(run_case terminal "" "" 'ccy_nobody_to_ask')" | awk '{ print ($1 == 0 ? "nobody" : "someone") }')"
check "a --headless launch on a terminal: nobody (its input is the session's)" "nobody" \
    "$(field ret "$(run_case terminal "" "HEADLESS_MODE=true" 'ccy_nobody_to_ask')" | awk '{ print ($1 == 0 ? "nobody" : "someone") }')"
check "a restore on a terminal: someone may be, but the launch is unattended" "unattended" \
    "$(field ret "$(run_case terminal "" "CCY_UNATTENDED_LAUNCH=true" 'ccy_launch_unattended')" | awk '{ print ($1 == 0 ? "unattended" : "attended") }')"
check "a launch from a terminal by a person is attended" "attended" \
    "$(field ret "$(run_case terminal "" "" 'ccy_launch_unattended')" | awk '{ print ($1 == 0 ? "unattended" : "attended") }')"
out="$(run_case none "" "" 'ccy_prompt_refuse token-select "Launch with --token NAME."')"
check "a refusal names the prompt" "yes" "$(has "$out" "token-select")"
check "  and what to launch with instead" "yes" "$(has "$out" "Launch with --token NAME.")"

echo ""
echo "=== --compose start: a restore whose stack was started or running before ==="
fresh 0 compose.yml
out="$(run_case none "" "COMPOSE_MODE=start" '_do_compose_start project-a_default project-a')"
check "no terminal is needed: the call returns" "0" "$(field ret "$out")"
check "  up -d ran once" "1" "$(ups)"
check "  the outcome is started" "started" "$(field outcome "$out")"
check "  and it says why it did not ask" "yes" "$(has "$out" "--compose start")"
check "  and no question was printed" "no" "$(has "$out" "$CCY_PROMPT_COMPOSE_START")"

fresh 0 compose.yml
out="$(run_case none "" "COMPOSE_MODE=start STUB_UP_RC=3" '_do_compose_start project-a_default project-a')"
check "a stack that will not start is a failure of the launch, not a session without it" "1" "$(field rc "$out")"
check "  it never returned to carry on" "" "$(field ret "$out")"
check "  and the reason is named" "yes" "$(has "$out" "could not be started")"

echo ""
echo "=== --compose skip: a restore whose stack was declined ==="
fresh 0 compose.yml
out="$(run_case none "" "COMPOSE_MODE=skip" '_do_compose_start project-a_default project-a')"
check "it returns as a no does (1: no services)" "1" "$(field ret "$out")"
check "  up -d never ran" "0" "$(ups)"
check "  the outcome is declined" "declined" "$(field outcome "$out")"

echo ""
echo "=== ask, with nobody to answer: refused by name, never read ==="
fresh 0 compose.yml
out="$(run_case none "" "" '_do_compose_start project-a_default project-a')"
check "no terminal: the launch stops" "1" "$(field rc "$out")"
check "  without returning to carry on" "" "$(field ret "$out")"
check "  naming the prompt" "yes" "$(has "$out" "compose-start")"
check "  and the flags that answer it" "yes" "$(has "$out" "--compose start")"
check "  up -d never ran" "0" "$(ups)"
fresh 0 compose.yml
out="$(run_case terminal "y" "HEADLESS_MODE=true" '_do_compose_start project-a_default project-a')"
check "--headless on a terminal: refused too, its input is not an answer" "1|0" "$(field rc "$out")|$(ups)"

echo ""
echo "=== ask, with a person at the terminal ==="
fresh 0 compose.yml
out="$(run_case terminal "y" "" '_do_compose_start project-a_default project-a')"
check "y starts the services" "0|1|started" "$(field ret "$out")|$(ups)|$(field outcome "$out")"
check "  after asking" "yes" "$(has "$out" "$CCY_PROMPT_COMPOSE_START")"
fresh 0 compose.yml
out="$(run_case terminal "" "" '_do_compose_start project-a_default project-a')"
check "Enter takes the default, yes" "0|1|started" "$(field ret "$out")|$(ups)|$(field outcome "$out")"
fresh 0 compose.yml
out="$(run_case terminal "n" "" '_do_compose_start project-a_default project-a')"
check "n declines" "1|0|declined" "$(field ret "$out")|$(ups)|$(field outcome "$out")"
fresh 0 compose.yml
out="$(run_case terminal "maybe
n" "" '_do_compose_start project-a_default project-a')"
check "a typo is asked again, not taken" "1|0|declined" "$(field ret "$out")|$(ups)|$(field outcome "$out")"

# A record written before compose= existed has no answer. A restore of it still asks, in the
# pane, and says why first: verify-restore reads the last line, which must be the prompt.
fresh 0 compose.yml
out="$(run_case terminal "y" "SESSION_RESTORE=true" '_do_compose_start project-a_default project-a')"
check "a restore with no recorded answer asks" "0|started" "$(field ret "$out")|$(field outcome "$out")"
check "  and says why, before the question" "yes" \
    "$(printf '%s\n' "$out" | awk -v q="$CCY_PROMPT_COMPOSE_START" 'index($0, "holds no compose answer") { note = NR } index($0, q) == 1 && !ask { ask = NR } END { print (note && ask && note < ask ? "yes" : "no") }')"

fresh 0 compose.yml
out="$(run_case none "" "COMPOSE_MODE=sometimes" '_do_compose_start project-a_default project-a')"
check "an unknown --compose answer is refused" "1|0" "$(field rc "$out")|$(ups)"

echo ""
echo "=== how the services stood: recorded even when nothing was asked ==="
fresh 1 compose.yml
out="$(run_case none "" "" 'check_and_start_compose_services project-a_default project-a')"
check "containers up and a compose file: running, nothing asked" "0|running|0" \
    "$(field ret "$out")|$(field outcome "$out")|$(ups)"
fresh 1
out="$(run_case none "" "" 'check_and_start_compose_services project-a_default project-a')"
check "containers up and no compose file: no compose stack to speak of" "0|" \
    "$(field ret "$out")|$(field outcome "$out")"
fresh 0 compose.yml
out="$(run_case none "" "COMPOSE_MODE=start" 'check_and_start_compose_services project-a_default project-a')"
check "containers down, --compose start: started without a question" "0|started|1" \
    "$(field ret "$out")|$(field outcome "$out")|$(ups)"
fresh 0 compose.yml
out="$(run_case none "" "STUB_PS_IDS=abc123 STUB_PS_RUNNING=true" 'offer_compose_start project-a_default project-a')"
check "the saved network gone but the services up: running, nothing asked" "0|running|0" \
    "$(field ret "$out")|$(field outcome "$out")|$(ups)"

echo ""
echo "=== the launcher: every compose question goes through ccy_compose_answer ==="
check "no compose question is read directly any more" "0" \
    "$(cat "$LAUNCHER" "$LIB_DIR"/*.bash | grep -c -F "read -rp \"\$CCY_PROMPT_COMPOSE_START")"
check "the launcher's own two compose questions use it" "2" "$(grep -c 'ccy_compose_answer ' "$LAUNCHER")"
check "  and note started and declined for the record, each of them" "2|2" \
    "$(grep -c 'ccy_compose_outcome_set started' "$LAUNCHER")|$(grep -c 'ccy_compose_outcome_set declined' "$LAUNCHER")"
check "--compose is parsed" "1" "$(grep -c -F "\"\$arg\" = \"--compose\"" "$LAUNCHER")"
check "--compose is in the help" "1" "$(grep -c '^  --compose start|skip|ask' "$LAUNCHER")"

echo ""
echo "=== the launcher: no question is read where nobody can answer it ==="
# Every `read -rp "$CCY_PROMPT_...` in the launcher is a launch-time question. Each must be
# guarded by ccy_nobody_to_ask or ccy_launch_unattended (directly, or by the question it is
# nested in) within the lines before it, or be the end-of-session compose-stop. --debug's
# reads are not $CCY_PROMPT_ ones and run only when a person asked for the debug chooser.
unguarded="$(awk '
    /ccy_nobody_to_ask|ccy_launch_unattended/ { guard = NR }
    /read -rp? *"\$\{?CCY_PROMPT_/ && (guard == 0 || NR - guard > 40) { printf "%s ", NR }
' "$LAUNCHER")"
check "every launcher question has a guard within 40 lines before it" "" "$unguarded"
check "and the guards are there at all" "yes" \
    "$([ "$(grep -c 'ccy_nobody_to_ask\|ccy_launch_unattended' "$LAUNCHER")" -ge 10 ] && echo yes || echo "no: $(grep -c 'ccy_nobody_to_ask\|ccy_launch_unattended' "$LAUNCHER")")"

echo ""
printf 'passed: %s   failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
