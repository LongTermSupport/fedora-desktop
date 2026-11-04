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
# are up on any network); `podman-compose up -d` appends to $UP_LOG and fails (status 3) when
# STUB_UP_RC is not 0 or while it has been called no more than STUB_UP_FAILS times, else brings
# one container up; `podman-compose ps -q` prints STUB_PS_IDS, whose containers inspect as
# running when STUB_PS_RUNNING is true. The session registry lives in $WORK/state, and
# ccy_tmux_current_session prints STUB_SESSION (the CCY session this launch runs in, or none).
run_case() {
    local stdin_kind="$1" answer="$2" settings="$3" call="$4"
    local driver="$WORK/driver.bash"
    cat >"$driver" <<DRIVER
set -uo pipefail
cd "$PROJECT" || exit 99
export CONTAINER_ENGINE=podman CCY_STATE_DIR="$WORK/state"
STUB_UP_RC=0 STUB_UP_FAILS=0 STUB_PS_IDS="" STUB_PS_RUNNING=false STUB_SESSION=""
source "$LIB_DIR/common-pure.bash"
source "$LIB_DIR/network-management.bash"
source "$LIB_DIR/session-registry.bash"
source "$LIB_DIR/ssh-handling.bash"
sleep() { :; }
ccy_tmux_current_session() { printf '%s' "\$STUB_SESSION"; }
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
        [ "\$(grep -c . "$UP_LOG")" -gt "\$STUB_UP_FAILS" ] || return 3
        printf '1\n' >"$RUNNING"
        ;;
    "ps -q") printf '%s\n' "\$STUB_PS_IDS" ;;
    *) return 1 ;;
    esac
}
$settings
( $call; call_rc=\$?; echo; echo "ret=\$call_rc"; echo "outcome=\${CCY_COMPOSE_OUTCOME:-}" )
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

# A person who gave --compose start at a terminal chose it for a launch they are watching: a
# stack that will not start ends that fresh launch, with the reason.
fresh 0 compose.yml
out="$(run_case none "" "COMPOSE_MODE=start STUB_UP_RC=3" '_do_compose_start project-a_default project-a')"
check "not a restore: a stack that will not start ends the launch" "1" "$(field rc "$out")"
check "  it never returned to carry on" "" "$(field ret "$out")"
check "  and the reason is named" "yes" "$(has "$out" "could not be started")"
check "  after one try" "1" "$(ups)"

echo ""
echo "=== --compose start in a restore: a failure never ends the launch (fedora-desktop#87 review B1) ==="
# A restored session's record goes when its launcher returns, so a restore that ended on a
# stack that would not start would never be restored again. Right after a boot `up -d` fails
# for passing reasons, so it is tried again, and then the question is asked in the pane.
fresh 0 compose.yml
out="$(run_case none "" "COMPOSE_MODE=start SESSION_RESTORE=true STUB_UP_FAILS=2" '_do_compose_start project-a_default project-a')"
check "a passing failure: tried again until it starts, nothing asked" "0|3|started" \
    "$(field ret "$out")|$(ups)|$(field outcome "$out")"
check "  and no question was printed" "no" "$(has "$out" "$CCY_PROMPT_COMPOSE_START")"

fresh 0 compose.yml
out="$(run_case terminal "n" "COMPOSE_MODE=start SESSION_RESTORE=true STUB_UP_RC=3" '_do_compose_start project-a_default project-a')"
check "it keeps failing: the launch carries on (the subshell returned)" "0|1" "$(field rc "$out")|$(field ret "$out")"
check "  after every try" "3" "$(ups)"
check "  the question is asked in the pane" "yes" "$(has "$out" "$CCY_PROMPT_COMPOSE_START")"
check "  with why, on a line before it" "yes" \
    "$(printf '%s\n' "$out" | awk -v q="$CCY_PROMPT_COMPOSE_START" 'index($0, "did not start after") { note = NR } index($0, q) == 1 && !ask { ask = NR } END { print (note && ask && note < ask ? "yes" : "no") }')"
check "  and n there is not the record's answer: no outcome is noted" "" "$(field outcome "$out")"

fresh 0 compose.yml
out="$(run_case terminal "y" "COMPOSE_MODE=start SESSION_RESTORE=true STUB_UP_FAILS=3" '_do_compose_start project-a_default project-a')"
check "y there tries again, and a start then is started" "0|4|started" \
    "$(field ret "$out")|$(ups)|$(field outcome "$out")"

# The record itself, end to end: a restored session whose stack will not start keeps its
# record, compose=started included, so the next boot tries again.
rm -rf "$WORK/state"
mkdir -p "$WORK/state/sessions"
printf 'ccy-session-record 1\nname=ccy-project-a\ndir=%s\nlauncher=/launch/ccy\nprefix=ccy\nrestore=yes\ncompose=started\narg=--token\narg=work\n' \
    "$PROJECT" >"$WORK/state/sessions/ccy-project-a"
fresh 0 compose.yml
out="$(run_case terminal "n" "COMPOSE_MODE=start SESSION_RESTORE=true STUB_UP_RC=3 STUB_SESSION=ccy-project-a" \
    '_do_compose_start project-a_default project-a; ccy_compose_record_outcome')"
check "a failed up -d in a restore leaves the launch running" "0" "$(field rc "$out")"
check "  and the record in place, still compose=started" "1" \
    "$(grep -cx 'compose=started' "$WORK/state/sessions/ccy-project-a" 2>&1)"

echo ""
echo "=== the outcome reaches the record (fedora-desktop#87 review B2) ==="
rm -rf "$WORK/state"
mkdir -p "$WORK/state/sessions"
rec_line() { printf 'ccy-session-record 1\nname=ccy-project-a\ndir=%s\nlauncher=/launch/ccy\nprefix=ccy\nrestore=yes\narg=--token\narg=work\n' "$PROJECT"; }
rec_line >"$WORK/state/sessions/ccy-project-a"
out="$(run_case none "" "STUB_SESSION=ccy-project-a CCY_COMPOSE_OUTCOME=running" 'ccy_compose_record_outcome')"
check "an outcome is written into this session's record" "0|1" \
    "$(field ret "$out")|$(grep -cx 'compose=running' "$WORK/state/sessions/ccy-project-a")"
check "  the rest of the record as it was" "2" "$(grep -c '^arg=' "$WORK/state/sessions/ccy-project-a")"
rec_line >"$WORK/state/sessions/ccy-project-a"
out="$(run_case none "" "STUB_SESSION=ccy-project-a" 'ccy_compose_record_outcome')"
check "no outcome: nothing written" "0|0" "$(field ret "$out")|$(grep -c '^compose=' "$WORK/state/sessions/ccy-project-a")"
out="$(run_case none "" "STUB_SESSION= CCY_COMPOSE_OUTCOME=started" 'ccy_compose_record_outcome')"
check "no CCY session: nothing written, not a failure" "0|0" "$(field ret "$out")|$(grep -c '^compose=' "$WORK/state/sessions/ccy-project-a")"
fresh 0 compose.yml
out="$(run_case none "" "COMPOSE_MODE=start STUB_SESSION=ccy-project-a" '_do_compose_start project-a_default project-a; ccy_compose_record_outcome')"
check "a start noted by the compose code lands in the record" "1" \
    "$(grep -cx 'compose=started' "$WORK/state/sessions/ccy-project-a")"
rm -rf "$WORK/state"

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
echo "=== the launcher's own argument loop sets COMPOSE_MODE (review B2) ==="
# The loop is taken from the launcher as it stands, from its variable defaults to its `done`,
# and run on real argv: a replayed `--compose start` must reach COMPOSE_MODE as start.
awk '/^# Check for wrapper flags$/ { on = 1 } on { print } on && /^done$/ { exit }' "$LAUNCHER" >"$WORK/parse.bash"
check "the argument loop was found" "yes" \
    "$(grep -q 'COMPOSE_MODE=ask' "$WORK/parse.bash" && grep -qx 'done' "$WORK/parse.bash" && echo yes || echo no)"
cat >"$WORK/parse-driver.bash" <<'PARSE_DRIVER'
set -uo pipefail
source "$1"
parse="$2"
shift 2
# shellcheck source=/dev/null
source "$parse"
printf 'mode=%s dangling=%s\n' "$COMPOSE_MODE" "$NEXT_IS_COMPOSE"
PARSE_DRIVER
parse_args() { bash "$WORK/parse-driver.bash" "$LIB_DIR/common-pure.bash" "$WORK/parse.bash" "$@" 2>&1; }
check "no flag: ask" "mode=ask dangling=false" "$(parse_args --token work)"
check "--compose start" "mode=start dangling=false" "$(parse_args --token work --compose start)"
check "--compose skip" "mode=skip dangling=false" "$(parse_args --compose skip --no-network)"
check "--compose ask" "mode=ask dangling=false" "$(parse_args --compose ask)"
check "after --, --compose is claude's word" "mode=ask dangling=false" "$(parse_args -- --compose start)"
check "--compose at the end is left wanting a value (the launcher then refuses it)" "mode=ask dangling=true" \
    "$(parse_args --token work --compose)"
check "  and that refusal covers it" "1" "$(grep -c -F "compose:\"\$NEXT_IS_COMPOSE\"" "$LAUNCHER")"
out="$(parse_args --compose sometimes)"
check "a value that is none of the three exits 64, naming it" "64|yes" \
    "$(bash "$WORK/parse-driver.bash" "$LIB_DIR/common-pure.bash" "$WORK/parse.bash" --compose sometimes >"$WORK/parse.out" 2>&1; echo "$?")|$(has "$out" "sometimes")"

echo ""
echo "=== the SSH questions with nobody to answer (review R1) ==="
NOKEY_HOME="$WORK/nokey-home"
mkdir -p "$NOKEY_HOME/.ssh"
out="$(run_case none "" "HOME=$NOKEY_HOME; unset SSH_AUTH_SOCK; CCY_UNATTENDED_LAUNCH=true" 'discover_and_select_ssh_keys ccy')"
check "no key at all, a restore: carries on without one, nothing asked" "0|yes|no" \
    "$(field ret "$out")|$(has "$out" "continuing WITHOUT an SSH key")|$(has "$out" "$CCY_PROMPT_SSH_NO_KEY")"
out="$(run_case terminal "" "HOME=$NOKEY_HOME; unset SSH_AUTH_SOCK" 'discover_and_select_ssh_keys ccy')"
check "no key at all, a person: asked, Enter carries on" "0|yes" \
    "$(field ret "$out")|$(has "$out" "$CCY_PROMPT_SSH_NO_KEY")"
SSH_ADD_LOG="$WORK/ssh-add.log"
rm -f "$SSH_ADD_LOG"
out="$(run_case none "" "CCY_PROBE_AGENT_SOCK=$WORK/agent.sock; ssh-add() { printf 'x\n' >>'$SSH_ADD_LOG'; return 1; }" '_probe_agent_add_key /k/id_one')"
check "a passphrase that did not open the key, no terminal: refused by name after one try" "1|1|yes" \
    "$(field ret "$out")|$(grep -c . "$SSH_ADD_LOG")|$(has "$out" "ssh-passphrase-retry")"
out="$(run_case none "" "CCY_UNATTENDED_LAUNCH=true" 'ccy_github_443_answer')"
check "GitHub over 443, a restore: yes, nothing asked" "0|no" "$(field ret "$out")|$(has "$out" "$CCY_PROMPT_GITHUB_443")"
out="$(run_case none "" "" 'ccy_github_443_answer')"
check "GitHub over 443, no terminal: yes" "0" "$(field ret "$out")"
out="$(run_case terminal "n" "" 'ccy_github_443_answer')"
check "GitHub over 443, a person says n: no" "1|yes" "$(field ret "$out")|$(has "$out" "$CCY_PROMPT_GITHUB_443")"
out="$(run_case terminal "" "" 'ccy_github_443_answer')"
check "GitHub over 443, a person presses Enter: yes" "0" "$(field ret "$out")"
check "the 443 question is asked only through ccy_github_443_answer" "1|1" \
    "$(grep -c -F "read -rp \"\$CCY_PROMPT_GITHUB_443" "$LIB_DIR/ssh-handling.bash")|$(grep -c 'if ccy_github_443_answer; then' "$LIB_DIR/ssh-handling.bash")"

echo ""
echo "=== --headless before the arguments are parsed, and --debug (review R1) ==="
out="$(run_case terminal "" "" 'ccy_args_headless --token t --headless --prompt go')"
check "--headless among the launcher's arguments is seen" "0" "$(field ret "$out")"
out="$(run_case terminal "" "" 'ccy_args_headless --token t -- --headless')"
check "--headless after -- is claude's word" "1" "$(field ret "$out")"
check "the old-session-directories question honours --headless (it runs before the parse)" "1" \
    "$(grep -c -F "if ccy_nobody_to_ask || ccy_args_headless \"\$@\" || [ -n \"\${CCY_SESSION_RESTORE:-}\" ]; then" "$LAUNCHER")"
check "--debug is refused with nobody to answer its chooser, before the chooser" "yes" \
    "$(awk '/SHOW_DEBUG_CHOOSER" = true \] && ccy_nobody_to_ask/ { g = NR } /Enter layer numbers/ && !r { r = NR } END { print (g && r && g < r ? "yes" : "no") }' "$LAUNCHER")"

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
# Every `read -rp "$CCY_PROMPT_...` in the launcher is a launch-time question. Each must have
# its own ccy_nobody_to_ask or ccy_launch_unattended guard between the question before it and
# itself, so one guard cannot stand for two questions. The exceptions are the three asked
# only inside another question's own loop, after a person has answered that one: network-prune
# (engine-conflict), token-replace (token-recovery) and network-pick (network-connect).
# --debug's reads are not $CCY_PROMPT_ ones and run only when a person asked for the chooser.
# guard_gaps <file> — the line numbers of the questions with no guard of their own.
guard_gaps() {
    awk '
        /(^|[[:space:]])(el)?if (ccy_nobody_to_ask|ccy_launch_unattended)/ { guarded = 1 }
        /read -rp? *"\$\{?CCY_PROMPT_(NETWORK_PRUNE|TOKEN_REPLACE|NETWORK_PICK)/ { next }
        /read -rp? *"\$\{?CCY_PROMPT_/ { if (!guarded) printf "%s ", NR; guarded = 0 }
    ' "$1"
}
check "every launcher question has a guard of its own" "" "$(guard_gaps "$LAUNCHER")"
# The check has to see one guard go missing, wherever it is: drop each guard block in turn
# (the line that tests, through the `exit` or `fi` that ends it) from a copy and require the
# question it protects to be named.
guard_lines="$(grep -n 'if ccy_nobody_to_ask; then$\|if ccy_launch_unattended; then$' "$LAUNCHER" | cut -d: -f1)"
missed=""
for g in $guard_lines; do
    awk -v g="$g" 'NR < g || NR > g + 3' "$LAUNCHER" >"$WORK/launcher-without-guard"
    [ -n "$(guard_gaps "$WORK/launcher-without-guard")" ] || missed+="$g "
done
check "  and finds the question when any one guard is removed" "" "$missed"
check "  (guard blocks removed one at a time: at least ten)" "yes" \
    "$([ "$(wc -w <<<"$guard_lines")" -ge 10 ] && echo yes || echo "no: $(wc -w <<<"$guard_lines")")"
check "and the guards are there at all" "yes" \
    "$([ "$(grep -c 'ccy_nobody_to_ask\|ccy_launch_unattended' "$LAUNCHER")" -ge 10 ] && echo yes || echo "no: $(grep -c 'ccy_nobody_to_ask\|ccy_launch_unattended' "$LAUNCHER")")"

echo ""
printf 'passed: %s   failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
