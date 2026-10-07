#!/usr/bin/env bash
# Unit-test ccy's `--teams <seat>@<team>[,...]` and its headless Quick Launch (Plan 00161
# DESIGN.md sections 5.3, 5.5 and 5.6; D36, D38, D44, D48).
#
# WHY THIS TEST EXISTS. A ccy session is in an agent team bus team only when its launch names
# it. The launcher validates the list on the host with `agent-bus seat check` right after its
# own argument parsing (a usage error is 64 before any prompt, SSH or token work), and just
# before the container starts runs `agent-bus seat take`, which creates a missing seat and
# refuses a held one; the container then gets PINGBUS_SEATS and a ccy-seats label, and a
# restart or restore comes back in the same seats. A plain launch calls no agent-bus and
# passes no bus variable. A headless launch never reads the Quick Launch prompt (stdin is the
# session's own input): with no launch-choice flag it takes the saved choices or is refused,
# and a key that needs a passphrase refuses it, since a headless launch never unlocks one.
#
# Three layers: lib/agent-bus-seats.bash's functions against a fake agent-bus; the REAL
# launcher, run in a throwaway repository with stub engine and SSH tools, for every refusal
# that must happen before anything runs; and the launcher's Quick Launch block and headless key
# check, cut out of it and run against a saved configuration with `read -p` replaced by a
# recorder. The wiring the run line needs is read from the launcher's text.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
LAUNCHER="$CCY_DIR/claude-yolo"
LIB_DIR="$CCY_DIR/lib"
SEATS_LIB="$LIB_DIR/agent-bus-seats.bash"
PLAY="$REPO_ROOT/playbooks/imports/play-claude-yolo.yml"

for f in "$LAUNCHER" "$SEATS_LIB" "$LIB_DIR/common-pure.bash" "$LIB_DIR/session-registry.bash" \
    "$LIB_DIR/restart-request.bash" "$LIB_DIR/ssh-handling.bash"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: not found: $f" >&2
        exit 1
    fi
done
unset PINGBUS_SEATS PINGBUS_TEAMS PINGBUS_HOME GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS

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
yes_no() { if "$@"; then echo yes; else echo no; fi; }
has() { yes_no grep -qF -- "$1" "$2"; }

# The fake agent-bus: every call is one line in $FAKE_BUS_LOG. `seat check` drops spaces and
# tabs, refuses an empty item (64) as seat.py does, and prints the list; FAKE_CHECK_RC and
# FAKE_CHECK_OUT override it. `seat take` prints a CHANGED line and exits FAKE_TAKE_RC.
bus_bin="$work/bus-bin"
mkdir -p "$bus_bin"
cat >"$bus_bin/agent-bus" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_BUS_LOG"
if [ "$1 $2" = "seat check" ]; then
    if [ "${FAKE_CHECK_RC:-0}" -ne 0 ]; then
        echo "agent-bus seat check: refused (fake)" >&2
        exit "$FAKE_CHECK_RC"
    fi
    if [ -n "${FAKE_CHECK_OUT+set}" ]; then
        printf '%s' "$FAKE_CHECK_OUT"
        exit 0
    fi
    list="${3//[[:space:]]/}"
    case ",$list," in
    *,,*)
        echo "agent-bus seat check: an empty item in '$3' (write the list without spaces, or quote it)" >&2
        exit 64
        ;;
    esac
    printf '%s\n' "$list"
    exit 0
fi
if [ "$1 $2" = "seat take" ]; then
    echo "CHANGED seat $3 fake-handle"
    exit "${FAKE_TAKE_RC:-0}"
fi
echo "fake agent-bus: unexpected call: $*" >&2
exit 70
FAKE
chmod 755 "$bus_bin/agent-bus"
export FAKE_BUS_LOG="$work/bus.log"
bus_calls() { if [ -s "$FAKE_BUS_LOG" ]; then paste -sd'|' "$FAKE_BUS_LOG"; else echo none; fi; }

echo "=== lib/agent-bus-seats.bash ==="

# lib_run <function> [args...]: call a function with the libraries loaded and the fake on
# PATH; prints its stdout, then "rc=<n>". stderr goes to lib.err. take_and_show runs
# ccy_seats_take, then prints the container arguments it left, each followed by '|'.
cat >"$work/lib-run.bash" <<'RUN'
. "$LIB_DIR/common-pure.bash"
. "$LIB_DIR/agent-bus-seats.bash"
take_and_show() {
    ccy_seats_take "$@"
    local s=$?
    printf '%s|' "${CCY_SEAT_RUN_ARGS[@]}"
    return "$s"
}
"$@"
printf 'rc=%s' "$?"
RUN
lib_run() {
    : >"$FAKE_BUS_LOG"
    LIB_DIR="$LIB_DIR" PATH="$bus_bin:$PATH" bash "$work/lib-run.bash" "$@" 2>"$work/lib.err"
}

check "seat check prints the canonical list" "dev1@team-a,qa2@team-b
rc=0" "$(lib_run ccy_seats_check " dev1@team-a , qa2@team-b")"
check "seat check is given the list as one argument" "seat check  dev1@team-a , qa2@team-b" "$(bus_calls)"
check "seat check's 64 is passed on" "rc=64" "$(FAKE_CHECK_RC=64 lib_run ccy_seats_check x)"
check "seat check's 78 is passed on" "rc=78" "$(FAKE_CHECK_RC=78 lib_run ccy_seats_check x)"
check "a seat check with nothing on stdout: refused, 78" "rc=78" \
    "$(FAKE_CHECK_OUT='' lib_run ccy_seats_check dev1@team-a)"
check "a seat check printing two lines: refused, 78" "rc=78" \
    "$(FAKE_CHECK_OUT=$'a@b\nc@d' lib_run ccy_seats_check dev1@team-a)"
check "agent-bus not installed: refused, 78" "rc=78" \
    "$(LIB_DIR="$LIB_DIR" PATH="/usr/bin:/bin" bash "$work/lib-run.bash" ccy_seats_check dev1@team-a 2>"$work/lib.err")"
check "that refusal names the play that installs it" "yes" "$(has play-agent-bus.yml "$work/lib.err")"

check "take with a list: called with it, the container's arguments filled" \
    "-e|PINGBUS_SEATS=dev1@team-a,qa2@team-b|--label|ccy-seats=dev1@team-a,qa2@team-b|rc=0" \
    "$(lib_run take_and_show dev1@team-a,qa2@team-b false)"
check "take, interactive: no --no-prompt" "seat take dev1@team-a,qa2@team-b" "$(bus_calls)"
check "take's CHANGED lines go to stderr, never stdout" "rc=0" "$(lib_run ccy_seats_take dev1@team-a false)"
check "they are shown" "yes" "$(has "CHANGED seat dev1@team-a" "$work/lib.err")"
lib_run ccy_seats_take dev1@team-a true >/dev/null
check "take, unattended: --no-prompt" "seat take dev1@team-a --no-prompt" "$(bus_calls)"
check "take's 75 (a held seat) is passed on, no arguments for the container" "|rc=75" \
    "$(FAKE_TAKE_RC=75 lib_run take_and_show dev1@team-a false)"
check "take's 78 is passed on" "rc=78" "$(FAKE_TAKE_RC=78 lib_run ccy_seats_take dev1@team-a true)"
check "no list (a plain launch): agent-bus not called, no arguments for the container" "|rc=0" \
    "$(lib_run take_and_show "" false)"
check "no list: the log stays empty" "none" "$(bus_calls)"

# canon <flag-pos> <value-pos> <canonical> <args...>: the rewritten arguments, '|' joined.
canon() {
    bash -c '. "$1/common-pure.bash"; . "$1/agent-bus-seats.bash"; shift
        ccy_teams_canonical_args "$@" | tr "\0" "|"' _ "$LIB_DIR" "$@"
}
check "--teams <list> becomes --teams <canonical>, the rest untouched" \
    "--token|t|--teams|a@x,b@y|--resume|" \
    "$(canon 3 4 a@x,b@y --token t --teams ' a@x , b@y' --resume)"
check "--teams=<list> becomes --teams <canonical>" \
    "--teams|a@x|-c|" "$(canon 1 1 a@x '--teams= a@x' -c)"
check "an argument after -- that reads --teams is left alone" \
    "--teams|a@x|--|--teams|z|" "$(canon 1 2 a@x --teams a@x -- --teams z)"

echo "=== restart and restore keep --teams; a plain launch stays plain ==="

reg() {
    bash -c '. "$1/common-pure.bash"; . "$1/session-registry.bash"; . "$1/ssh-handling.bash"
        . "$1/restart-request.bash"; shift; "$@"' _ "$LIB_DIR" "$@" | tr '\n\0' '||'
}
SID=0b4a8d4e-1c2f-4e5a-9b6c-7d8e9f0a1b2c
check "a restore replays --teams and its list" "--teams|a@x,b@y|--no-ssh|" \
    "$(reg ccy_registry_replay_args ccy --teams a@x,b@y --no-ssh 'first message')"
check "a restore replays --teams=<list>" "--teams=a@x|" "$(reg ccy_registry_replay_args ccy --teams=a@x)"
check "a plain launch's restore has no --teams" "--no-ssh|" "$(reg ccy_registry_replay_args ccy --no-ssh)"
check "a restart keeps --teams and its list" "--teams|a@x|--resume|$SID|" \
    "$(reg ccy_restart_relaunch_args "$SID" --teams a@x --token t)"
check "a plain launch's restart has no --teams" "--resume|$SID|" \
    "$(reg ccy_restart_relaunch_args "$SID" --token t)"

echo "=== the real launcher: refusals before anything runs ==="

repo="$work/repo"
home="$work/home"
stub_bin="$work/stub-bin"
mkdir -p "$repo" "$home" "$stub_bin"
GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 git -C "$repo" init -q
# Seats already in the checkout: a plain launch must still call nothing.
mkdir -p "$repo/.claude/ccy/pingbus/seats/team-a/dev1"
# The engine and the SSH tools record every call and refuse it: none may be reached here.
for tool in podman ssh ssh-add; do
    printf '#!/usr/bin/env bash\nprintf "%s %%s\\n" "$*" >>"%s"\nexit 1\n' "$tool" "$work/tools.log" >"$stub_bin/$tool"
    chmod 755 "$stub_bin/$tool"
done

RC=0
# run_ccy <with-bus|no-bus> <args...>: the launcher in the throwaway repository, stderr in
# ccy.err, its exit code in RC.
run_ccy() {
    local path="$stub_bin:/usr/bin:/bin"
    [ "$1" = with-bus ] && path="$bus_bin:$path"
    shift
    : >"$FAKE_BUS_LOG"
    : >"$work/tools.log"
    RC=0
    (cd "$repo" && env HOME="$home" PATH="$path" CCY_CONTAINER_ENGINE=podman TERM=dumb \
        GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
        bash "$LAUNCHER" "$@" </dev/null >"$work/ccy.out" 2>"$work/ccy.err") || RC=$?
}
nothing_ran() {
    if [ "$(bus_calls)" = none ] && ! grep -q '^podman run\|^ssh' "$work/tools.log"; then echo yes; else echo no; fi
}
no_session_tools() { if grep -q '^podman run\|^ssh' "$work/tools.log"; then echo no; else echo yes; fi; }

run_ccy with-bus --headless --teams
check "--teams with no value: 64" "64" "$RC"
check "  it says --teams needs a list" "yes" "$(has "--teams needs" "$work/ccy.err")"
check "  nothing ran" "yes" "$(nothing_ran)"
run_ccy with-bus --teams --headless
check "--teams followed directly by another option: 64" "64" "$RC"
check "  nothing ran" "yes" "$(nothing_ran)"
run_ccy with-bus --teams= --headless
check "--teams= (empty): 64" "64" "$RC"
run_ccy with-bus --teams dev1@team-a --teams qa2@team-b --headless
check "--teams given twice: 64" "64" "$RC"
check "  it says one flag carries every seat" "yes" "$(has "once" "$work/ccy.err")"
check "  nothing ran" "yes" "$(nothing_ran)"
run_ccy with-bus --teams=dev1@team-a --teams qa2@team-b --headless
check "--teams= then --teams: 64" "64" "$RC"

FAKE_CHECK_RC=64 run_ccy with-bus --teams 'dev1@t,dev2@t' --headless
check "seat check's 64 is the launch's exit code" "64" "$RC"
check "  agent-bus was asked to check the list, nothing else" "seat check dev1@t,dev2@t" "$(bus_calls)"
check "  no SSH, engine or token work" "yes" "$(no_session_tools)"
FAKE_CHECK_RC=78 run_ccy with-bus --teams 'dev1@elsewhere' --headless
check "seat check's 78 (a team not running here) is the launch's exit code" "78" "$RC"
check "  no SSH, engine or token work" "yes" "$(no_session_tools)"

# What the shell hands ccy for an unquoted `--teams dev1@dev-team, qa2@other-team`.
run_ccy with-bus --headless --prompt go --teams dev1@dev-team, qa2@other-team
check "the unquoted-space form: refused, 64" "64" "$RC"
check "  its trailing comma is named" "yes" "$(has "empty item" "$work/ccy.err")"
check "  nothing reached claude or the engine" "yes" "$(no_session_tools)"

run_ccy no-bus --teams dev1@team-a --headless
check "agent-bus not installed: 78" "78" "$RC"
check "  it names play-agent-bus.yml" "yes" "$(has play-agent-bus.yml "$work/ccy.err")"

# --headless with no --prompt is refused just after the parsing, so a launch that gets past
# the seat check stops there: proof it went on, with nothing else run.
run_ccy with-bus --headless --teams ' dev1@team-a , qa2@team-b'
check "an accepted list: the launch goes on to its next check" "1" "$RC"
check "  which is the one after the parsing" "yes" "$(has "--headless flag requires --prompt" "$work/ccy.err")"
check "  seat check got the list as given, and take was not called yet" \
    "seat check  dev1@team-a , qa2@team-b" "$(bus_calls)"

run_ccy with-bus --headless
check "a plain launch in a checkout with seats: goes on as before" "1" "$RC"
check "  no agent-bus call at all" "none" "$(bus_calls)"
run_ccy no-bus --headless
check "a plain launch needs no agent-bus installed" "yes" "$(has "--headless flag requires --prompt" "$work/ccy.err")"

echo "=== headless Quick Launch: the saved choices, never the prompt ==="

# The Quick Launch block and load_launch_config, cut out of the launcher.
awk '/^load_launch_config\(\) \{$/ {p=1} p {print} p && /^}$/ {exit}' "$LAUNCHER" >"$work/quick.bash"
awk '/^# Try to load previous launch configuration/ {p=1} /^# A headless launch never unlocks an SSH key/ {exit} p' \
    "$LAUNCHER" >>"$work/quick.bash"
check "the Quick Launch block was found" "yes" "$(has 'load_launch_config ".claude/ccy"' "$work/quick.bash")"
proj="$work/proj"
mkdir -p "$proj/.claude/ccy"

# quick <headless> <args-as-flags...>: run the block in proj; prints the resulting choices,
# whether a prompt was reached, and the exit code.
quick() {
    local headless="$1"
    shift
    (cd "$proj" && bash -c '
        print_error() { printf "ERROR: %s\n" "$*" >&2; }
        token_expiry_label() { :; }
        read() { if [ "$1" = -rp ]; then echo PROMPT >>"$PROMPT_LOG"; return 1; fi; builtin read "$@"; }
        CONFIG_VERSION=1 CCY_VERSION=9.9.9 CCY_HASH=h TOKEN_DIR=/nonexistent CCY_PROMPT_QUICK_LAUNCH="?"
        HEADLESS_MODE="$1" SESSION_RESTORE=false NO_SSH_MODE=false SSH_KEYS=() SPECIFIED_TOKEN=""
        SPECIFIED_NETWORK="" NO_NETWORK_MODE=false NETWORK_FROM_CONFIG=false
        [ "${2:-}" = --no-ssh ] && NO_SSH_MODE=true
        . "$0" >/dev/null
        printf "token=%s keys=%s no_ssh=%s net=%s no_net=%s" "$SPECIFIED_TOKEN" "${SSH_KEYS[*]}" \
            "$NO_SSH_MODE" "$SPECIFIED_NETWORK" "$NO_NETWORK_MODE"' "$work/quick.bash" "$headless" "$@" \
        2>"$work/quick.err")
    printf ' rc=%s prompt=%s' "$?" "$(if [ -s "$PROMPT_LOG" ]; then echo yes; else echo no; fi)"
}
export PROMPT_LOG="$work/prompt.log"
save() {
    : >"$PROMPT_LOG"
    printf 'SAVED_CONFIG_VERSION=1\nSAVED_CCY_VERSION="9.9.9"\nSAVED_CCY_HASH="h"\nLAST_TOKEN="%s"\nLAST_SSH_KEYS="%s"\nLAST_NETWORK="%s"\n' \
        "$1" "$2" "$3" >"$proj/.claude/ccy/.last-launch.conf"
}

save personal /keys/id_a proj_net
check "headless with saved choices: taken, no prompt" \
    "token=personal keys=/keys/id_a no_ssh=false net=proj_net no_net=false rc=0 prompt=no" "$(quick true)"
save personal /keys/id_a proj_net
check "interactive with saved choices: the prompt is still asked" "yes" \
    "$(quick false | grep -q 'prompt=yes' && echo yes || echo no)"
save personal "" ""
check "headless, saved with no key and no network: those are the choices, not a menu" \
    "token=personal keys= no_ssh=true net= no_net=true rc=0 prompt=no" "$(quick true)"
save "" /keys/id_a ""
check "headless, saved with no token: refused, no prompt" "yes" \
    "$(quick true | grep -q 'rc=1 prompt=no' && echo yes || echo no)"
rm -f "$proj/.claude/ccy/.last-launch.conf"
: >"$PROMPT_LOG"
check "headless with no saved choices: refused, no prompt" "yes" \
    "$(quick true | grep -q 'rc=1 prompt=no' && echo yes || echo no)"
check "  it says how to launch instead" "yes" "$(has "--token" "$work/quick.err")"
: >"$PROMPT_LOG"
check "headless with a launch-choice flag: the block is not entered" \
    "token= keys= no_ssh=true net= no_net=false rc=0 prompt=no" "$(quick true --no-ssh)"

echo "=== headless: a key that needs a passphrase is refused before launch ==="

awk '/^# A headless launch never unlocks an SSH key/ {p=1} p {print} p && /^fi$/ {exit}' "$LAUNCHER" >"$work/keys.bash"
check "the headless key check was found" "yes" "$(has 'HEADLESS_MODE' "$work/keys.bash")"
ssh-keygen -q -t ed25519 -N '' -C t -f "$work/open_key"
ssh-keygen -q -t ed25519 -N 'example-passphrase' -C t -f "$work/locked_key"
# keys <headless> <keys...>: the check's exit code.
keys() {
    bash -c '. "$1/common-pure.bash"; . "$1/ssh-handling.bash"; . "$1/restart-request.bash"
        HEADLESS_MODE="$2"; RESTORE_SSH_PASSPHRASE_FILE=""; shift 3; SSH_KEYS=("$@")
        . "$0"; echo ok' "$work/keys.bash" "$LIB_DIR" "$1" _ "${@:2}" 2>"$work/keys.err"
    printf 'rc=%s' "$?"
}
check "headless, a key with no passphrase: accepted" "ok
rc=0" "$(keys true "$work/open_key")"
check "headless, a key that needs a passphrase: refused" "rc=1" "$(keys true "$work/locked_key")"
check "  it names ssh-add" "yes" "$(has "ssh-add" "$work/keys.err")"
check "interactive, the same key: not this check's business" "ok
rc=0" "$(keys false "$work/locked_key")"

echo "=== the launcher's wiring ==="

check "the library is in CCY_LIBS" "1" "$(grep -c -E '^CCY_LIBS=\(.* agent-bus-seats( |\))' "$LAUNCHER")"
check "the launcher sources it" "1" "$(grep -c -F "source \"\$SCRIPT_DIR/lib/agent-bus-seats.bash\"" "$LAUNCHER")"
check "the play installs it" "1" "$(grep -c -F 'files/var/local/claude-yolo/lib/agent-bus-seats.bash' "$PLAY")"
check "--help documents --teams" "1" "$(grep -c -E '^  --teams ' "$LAUNCHER")"
check "the did-you-mean list knows --teams" "yes" \
    "$(awk '/^            ccy_flags=\(/,/^            \)/' "$LAUNCHER" | grep -q -- '--teams' && echo yes || echo no)"
check "the run line carries the seat arguments" "1" "$(grep -c -F "\"\${CCY_SEAT_RUN_ARGS[@]}\"" "$LAUNCHER")"
take_call="ccy_seats_take \"\$CCY_SEATS\""
take_line=$(grep -n -F "$take_call" "$LAUNCHER" | cut -d: -f1)
run_line=$(grep -n "^container_cmd run \\\$DOCKER_FLAGS --rm" "$LAUNCHER" | cut -d: -f1)
check "seat take is called once" "1" "$(grep -c -F "$take_call" "$LAUNCHER")"
check "seat take runs just before the container starts" "yes" \
    "$([ -n "$take_line" ] && [ -n "$run_line" ] && [ "$take_line" -lt "$run_line" ] \
        && [ "$((run_line - take_line))" -lt 6 ] && echo yes || echo no)"
rewrite_line=$(grep -n -F 'ccy_teams_canonical_args' "$LAUNCHER" | cut -d: -f1)
insulate_line=$(grep -n -F "ccy_tmux_insulate \"\$PROJECT_NAME\"" "$LAUNCHER" | cut -d: -f1)
check "the canonical list replaces the given one before tmux records the session" "yes" \
    "$([ -n "$rewrite_line" ] && [ -n "$insulate_line" ] && [ "$rewrite_line" -lt "$insulate_line" ] \
        && echo yes || echo no)"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
