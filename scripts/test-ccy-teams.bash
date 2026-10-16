#!/usr/bin/env bash
# Unit-test ccy's `--teams <seat>@<team>[,...]` and its headless Quick Launch (Plan 00161
# DESIGN.md sections 5.3, 5.5 and 5.6; D36, D38, D44, D48).
#
# WHY THIS TEST EXISTS. A ccy session is in an agent team bus team only when its launch names
# it. The launcher validates the list on the host with `agent-bus seat check` right after its
# own argument parsing (a usage error is 64 before any prompt, SSH or token work), and just
# before the container starts runs `agent-bus seat take`, which creates a missing seat and
# refuses a held one, and a restart or restore comes back in the same seats. Either way the
# container is told about the bus (Plan 00203 H1, H2): a session in ccy's tmux gets the
# ccy-session label and, unless --no-bus, its desired membership (written by ccy-teams internal
# membership-init) mounted read-only with the SELinux relabel, and PINGBUS_MEMBERSHIP; a plain
# launch calls no agent-bus. A headless launch never reads the Quick Launch prompt (stdin is the
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
MEMBERSHIP_LIB="$LIB_DIR/agent-bus-membership.bash"
PLAY="$REPO_ROOT/playbooks/imports/play-claude-yolo.yml"

for f in "$LAUNCHER" "$SEATS_LIB" "$MEMBERSHIP_LIB" "$LIB_DIR/common-pure.bash" "$LIB_DIR/session-registry.bash" \
    "$LIB_DIR/restart-request.bash" "$LIB_DIR/ssh-handling.bash"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: not found: $f" >&2
        exit 1
    fi
done
unset PINGBUS_SEATS PINGBUS_TEAMS PINGBUS_HOME PINGBUS_MEMBERSHIP GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS

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
    if [ "${4:-}" = --resolved ] && [[ ",$list," =~ ,[a-z][a-z0-9-]*, ]]; then
        echo "agent-bus seat check: a team alone is answered on a terminal (fake)" >&2
        exit 64
    fi
    printf '%s\n' "$list"
    exit 0
fi
# `seat choose <team>`: FAKE_CHOOSE_OUT, else seat 1 held, 2 free, 3 new; FAKE_CHOOSE_RC refuses.
if [ "$1 $2" = "seat choose" ]; then
    if [ "${FAKE_CHOOSE_RC:-0}" -ne 0 ]; then
        echo "agent-bus seat choose: refused (fake)" >&2
        exit "$FAKE_CHOOSE_RC"
    fi
    if [ -n "${FAKE_CHOOSE_OUT+set}" ]; then
        printf '%s' "$FAKE_CHOOSE_OUT"
        exit 0
    fi
    printf 'CHOICE\t%s\t%s\t%s\tccy (podman)\n' 1 held myrepo.1+local.podman 2 free myrepo.2+local.podman \
        3 new myrepo.3+local.podman
    printf 'DEFAULT\t2\n'
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

# The fake ccy-teams: each call is one line in $FAKE_BUS_LOG (after agent-bus's own calls, in
# order). `choose-teams --type=podman [LIST]` is the team and seat chooser, Go's (its list, scope
# question and seat chooser are tested there): it prints FAKE_CHOOSE_TEAMS (default 2@team-a;
# empty is the owner leaving the chooser) and exits FAKE_CHOOSE_TEAMS_RC.
cat >"$bus_bin/ccy-teams" <<'FAKE'
#!/usr/bin/env bash
printf 'ccy-teams %s\n' "$*" >>"$FAKE_BUS_LOG"
# `internal membership-init <session> <checkout> <seats>`: prints the directory, or fails.
if [ "$1 $2" = "internal membership-init" ]; then
    [ "${FAKE_INIT_RC:-0}" -eq 0 ] || { echo "ccy-teams: membership-init refused (fake)" >&2; exit "$FAKE_INIT_RC"; }
    printf '/run/user/1000/ccy/membership/%s\n' "$3"
    exit 0
fi
if [ "$1 $2" = "describe-seats --block" ]; then
    printf 'This session is on teams (fake block): %s\n' "$3"
    exit "${FAKE_BLOCK_RC:-0}"
fi
[ "${FAKE_CHOOSE_TEAMS_RC:-0}" -eq 0 ] || exit "$FAKE_CHOOSE_TEAMS_RC"
[ -z "${FAKE_CHOOSE_TEAMS-x}" ] || printf '%s\n' "${FAKE_CHOOSE_TEAMS-2@team-a}"
FAKE
chmod 755 "$bus_bin/ccy-teams"

echo "=== lib/agent-bus-seats.bash ==="

# lib_run <function> [args...]: call a function with the libraries loaded and the fake on
# PATH; prints its stdout, then "rc=<n>". stderr goes to lib.err. take_and_show runs
# ccy_seats_take, then prints the container arguments it left, each followed by '|'. The
# session this runs in is FAKE_SESSION (empty: outside ccy's tmux); the SELinux relabel is
# CCY_MOUNT_RELABEL, as the launcher sets it.
cat >"$work/lib-run.bash" <<'RUN'
. "$LIB_DIR/common-pure.bash"
. "$LIB_DIR/agent-bus-membership.bash"
. "$LIB_DIR/agent-bus-seats.bash"
ccy_tmux_current_session() { [ "${FAKE_SESSION_RC:-0}" -eq 0 ] || return 1; printf '%s' "${FAKE_SESSION-ccy-proj-1}"; }
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
    (cd "$work" && LIB_DIR="$LIB_DIR" PATH="$bus_bin:$PATH" CCY_MOUNT_RELABEL="${CCY_MOUNT_RELABEL-z}" \
        bash "$work/lib-run.bash" "$@" 2>"$work/lib.err")
}
INIT="ccy-teams internal membership-init"
MOUNT="-v|/run/user/1000/ccy/membership/ccy-proj-1:/run/ccy/membership:ro,Z|-e|PINGBUS_MEMBERSHIP=/run/ccy/membership"

check "seat check prints the canonical list" "dev1@team-a,qa2@team-b
rc=0" "$(lib_run ccy_seats_check " dev1@team-a , qa2@team-b" true)"
check "seat check is given the list as one argument" "seat check  dev1@team-a , qa2@team-b" "$(bus_calls)"
check "seat check's 64 is passed on" "rc=64" "$(FAKE_CHECK_RC=64 lib_run ccy_seats_check x true)"
check "seat check's 78 is passed on" "rc=78" "$(FAKE_CHECK_RC=78 lib_run ccy_seats_check x true)"
check "a seat check with nothing on stdout: refused, 78" "rc=78" \
    "$(FAKE_CHECK_OUT='' lib_run ccy_seats_check dev1@team-a true)"
check "a seat check printing two lines: refused, 78" "rc=78" \
    "$(FAKE_CHECK_OUT=$'a@b\nc@d' lib_run ccy_seats_check dev1@team-a true)"
check "agent-bus not installed: refused, 78" "rc=78" \
    "$(LIB_DIR="$LIB_DIR" PATH="/usr/bin:/bin" bash "$work/lib-run.bash" ccy_seats_check dev1@team-a true 2>"$work/lib.err")"
check "that refusal names the play that installs it" "yes" "$(has play-agent-bus.yml "$work/lib.err")"
check "attended, a team alone is kept for the chooser" "team-a,qa@team-b
rc=0" "$(lib_run ccy_seats_check team-a,qa@team-b true)"
check "unattended, the check asks for every seat named" "rc=64" "$(lib_run ccy_seats_check team-a false)"
check "  with --resolved" "seat check team-a --resolved" "$(bus_calls)"

check "take with a list in ccy's tmux: the label, the read-only relabelled mount and the marker" \
    "--label|ccy-session=ccy-proj-1|$MOUNT|rc=0" \
    "$(lib_run take_and_show dev1@team-a,qa2@team-b false false yolo-1)"
check "take, interactive: the membership first, no --no-prompt, then the launch block for the list" \
    "$INIT ccy-proj-1 $work dev1@team-a,qa2@team-b|seat take dev1@team-a,qa2@team-b|ccy-teams describe-seats --block dev1@team-a,qa2@team-b" "$(bus_calls)"
check "the PINGBUS_SEATS variable and the ccy-seats label are retired (H2)" "no" \
    "$(lib_run take_and_show dev1@team-a false false yolo-1 | grep -q 'PINGBUS_SEATS\|ccy-seats' && echo yes || echo no)"
check "take's CHANGED lines go to stderr, never stdout" "rc=0" "$(lib_run ccy_seats_take dev1@team-a false false yolo-1)"
check "they are shown" "yes" "$(has "CHANGED seat dev1@team-a" "$work/lib.err")"
check "the launch block is shown on stderr" "yes" "$(has "This session is on teams (fake block): dev1@team-a" "$work/lib.err")"
lib_run ccy_seats_take dev1@team-a true false yolo-1 >/dev/null
check "take, unattended: --no-prompt" "$INIT ccy-proj-1 $work dev1@team-a|seat take dev1@team-a --no-prompt|ccy-teams describe-seats --block dev1@team-a" "$(bus_calls)"
check "take's 75 (a held seat) is passed on" "rc=75" \
    "$(FAKE_TAKE_RC=75 lib_run ccy_seats_take dev1@team-a false false yolo-1)"
check "  and no block is shown for seats that were not taken" "$INIT ccy-proj-1 $work dev1@team-a|seat take dev1@team-a" "$(bus_calls)"
check "a launch block that cannot be made stops the launch with its code" "rc=70" \
    "$(FAKE_BLOCK_RC=70 lib_run ccy_seats_take dev1@team-a false false yolo-1)"
check "  and says so, with the code" "yes" "$(has "Not starting: ccy-teams describe-seats --block failed (exit 70" "$work/lib.err")"
check "take's 78 is passed on" "rc=78" "$(FAKE_TAKE_RC=78 lib_run ccy_seats_take dev1@team-a true false yolo-1)"
check "no list, in ccy's tmux (a plain session): the kit, idle, with an empty membership" \
    "--label|ccy-session=ccy-proj-1|$MOUNT|rc=0" "$(lib_run take_and_show "" false false yolo-1)"
check "  agent-bus is not called; the membership is written with no seat" "$INIT ccy-proj-1 $work " "$(bus_calls)"
check "no list, outside ccy's tmux: no label, no kit" "-e|PINGBUS_MEMBERSHIP=off|rc=0" \
    "$(FAKE_SESSION='' lib_run take_and_show "" false false yolo-1)"
check "  nothing is called" "none" "$(bus_calls)"
check "a list outside ccy's tmux: the membership is named after the container, no label" \
    "-v|/run/user/1000/ccy/membership/yolo-1:/run/ccy/membership:ro,Z|-e|PINGBUS_MEMBERSHIP=/run/ccy/membership|rc=0" \
    "$(FAKE_SESSION='' lib_run take_and_show dev1@team-a false false yolo-1)"
check "no SELinux relabel (Permissive off, or a forwarded agent): the mount is plain :ro" \
    "--label|ccy-session=ccy-proj-1|-v|/run/user/1000/ccy/membership/ccy-proj-1:/run/ccy/membership:ro|-e|PINGBUS_MEMBERSHIP=/run/ccy/membership|rc=0" \
    "$(CCY_MOUNT_RELABEL='' lib_run take_and_show "" false false yolo-1)"
check "--no-bus in ccy's tmux: the label, PINGBUS_MEMBERSHIP=off, no mount (H33b)" \
    "--label|ccy-session=ccy-proj-1|-e|PINGBUS_MEMBERSHIP=off|rc=0" "$(lib_run take_and_show "" false true yolo-1)"
check "  nothing is called" "none" "$(bus_calls)"
check "--no-bus with seats (a Quick Launch team): refused 64 before any seat is taken" "rc=64" \
    "$(lib_run ccy_seats_take dev1@team-a false true yolo-1)"
check "  nothing is called" "none" "$(bus_calls)"
check "  and it says why" "yes" "$(has "--no-bus leaves the agent team bus out" "$work/lib.err")"
check "a membership that cannot be written stops the launch with its code, before any seat" "rc=3" \
    "$(FAKE_INIT_RC=3 lib_run ccy_seats_take dev1@team-a false false yolo-1)"
check "  only membership-init ran" "$INIT ccy-proj-1 $work dev1@team-a" "$(bus_calls)"
check "a session that cannot be named stops the launch" "rc=1" \
    "$(FAKE_SESSION_RC=1 lib_run ccy_seats_take "" false false yolo-1)"

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
check "a restart keeps two teams together" "--teams|a@x,b@y|--resume|$SID|" \
    "$(reg ccy_restart_relaunch_args "$SID" --teams a@x,b@y --token t)"
check "a plain launch's restart has no --teams" "--resume|$SID|" \
    "$(reg ccy_restart_relaunch_args "$SID" --token t)"

echo "=== the re-attach offer keeps --teams (Task 4.6) ==="

# offer <want> [pick]: ccy_tmux_offer for project "app" with the tmux listing and the picker
# replaced. The sessions: ccy-app (seat 1@team-a), ccy-app-2 (2@team-a,qa@team-b),
# ccy-app-3 (no --teams), ccy-app-4 (no record), all detached in $PWD, and ccy-app-5 (1@team-b)
# open in another terminal. The registry records are real, written by ccy_registry_write.
# The picker prints its rows to picker.rows and answers [pick] (a session name), or Esc.
# Prints the decision and the status; stderr in offer.err.
offer_dir="$work/offer"
mkdir -p "$offer_dir/state"
cat >"$work/offer.bash" <<'RUN'
. "$LIB_DIR/common-pure.bash"
. "$LIB_DIR/session-registry.bash"
. "$LIB_DIR/tmux-session.bash"
export CCY_STATE_DIR="$OFFER_DIR/state"
ccy_registry_write ccy-app "$PWD" /l ccy yes --teams 1@team-a
ccy_registry_write ccy-app-2 "$PWD" /l ccy yes --no-ssh --teams 2@team-a,qa@team-b
ccy_registry_write ccy-app-3 "$PWD" /l ccy yes --no-ssh
ccy_registry_write ccy-app-5 "$PWD" /l ccy yes --teams=1@team-b
ccy_tmux_project_sessions() {
    printf '%s 0 %s\n' ccy-app "$PWD" ccy-app-2 "$PWD" ccy-app-3 "$PWD" ccy-app-4 "$PWD"
    printf '%s 1 %s\n' ccy-app-5 "$PWD"
}
ccy_tmux_pick() {
    cat >"$OFFER_DIR/picker.rows"
    printf '%s\n' "$2" >"$OFFER_DIR/picker.header"
    [ -n "$PICK" ] || return 1
    printf '\n%s  detached\n' "$PICK"
}
ccy_tmux_offer app "$1"
printf 'rc=%s' "$?"
RUN
offer() {
    rm -f "$offer_dir/picker.rows" "$offer_dir/picker.header" "$offer_dir/state/sessions/"*
    (cd "$offer_dir" && LIB_DIR="$LIB_DIR" OFFER_DIR="$offer_dir" PICK="${2:-}" \
        bash "$work/offer.bash" "$1" 2>"$work/offer.err")
}
offered() {
    if [ -f "$offer_dir/picker.rows" ]; then awk '{print $1}' "$offer_dir/picker.rows" | paste -sd' '; else echo none; fi
}

check "no --teams: every detached session is offered, as before" "attach ccy-app-3
rc=0" "$(offer "" ccy-app-3)"
check "  all four" "ccy-app ccy-app-2 ccy-app-3 ccy-app-4" "$(offered)"
check "  no seats column" "no" "$(has "seats " "$offer_dir/picker.rows")"
check "--teams 2@team-a: only the session holding it" "attach ccy-app-2
rc=0" "$(offer 2@team-a ccy-app-2)"
check "  it alone is offered" "ccy-app-2" "$(offered)"
check "  with the seats it holds" "yes" "$(has "seats 2@team-a,qa@team-b" "$offer_dir/picker.rows")"
check "  the header says why these" "yes" "$(has "that hold a seat of 2@team-a" "$offer_dir/picker.header")"
check "a list sharing one seat with each of two sessions: both" "ccy-app ccy-app-2" \
    "$(offer 1@team-a,qa@team-b >/dev/null; offered)"
check "Esc in the filtered offer: quit" "quit
rc=0" "$(offer 1@team-a)"
check "seats no detached session holds: no offer, a new session" "create
rc=0" "$(offer 9@team-a)"
check "  the picker never opened" "none" "$(offered)"
check "  one line says why" "1" "$(grep -c "No detached session here holds 9@team-a" "$work/offer.err")"
check "a seat only a session open elsewhere holds: no offer, it is named" "create
rc=0" "$(offer 1@team-b)"
check "  as open in another terminal" "yes" "$(has "Open in other terminals: ccy-app-5" "$work/offer.err")"

teams_arg() { bash -c '. "$1/tmux-session.bash"; shift; ccy_tmux_teams_arg "$@"' _ "$LIB_DIR" "$@"; }
check "the launch's --teams is read from its arguments" "1@a" "$(teams_arg --no-ssh --teams 1@a -c)"
check "  and from --teams=" "1@a,2@b" "$(teams_arg --teams=1@a,2@b)"
check "  never after --" "" "$(teams_arg -- --teams 1@a)"
check "  none: nothing" "" "$(teams_arg --no-ssh)"

# The picker rows ccy_tmux_row prints: the state words are what the pickers test for, and
# the extra column is absent, not blank, when it is not given.
tmux_row() { HOME=/home/u bash -c '. "$1/tmux-session.bash"; shift; ccy_tmux_row "$@"' _ "$LIB_DIR" "$@"; }
squeeze() { tr -s ' ' <<<"$1"; }
check "a row is name, state, directory" "ccy-app detached ~/code/p" "$(squeeze "$(tmux_row ccy-app 0 /home/u/code/p)")"
check "  an extra column sits before the directory" "ccy-app detached seats 1@a ~/code/p" \
    "$(squeeze "$(tmux_row ccy-app 0 /home/u/code/p 'seats 1@a')")"
check "  an attached session says open elsewhere" "ccy-app open elsewhere ~/code/p" \
    "$(squeeze "$(tmux_row ccy-app 1 /home/u/code/p)")"

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

# A bare --teams is answered by ccy-teams choose-teams, on a terminal. With nobody to ask (here:
# headless, no terminal) the empty list is refused by agent-bus seat check (64), and ccy-teams
# is not asked.
for bare in "--headless --teams" "--teams --headless" "--teams= --headless" "--headless --teams --prompt go"; do
    # shellcheck disable=SC2086  # the words are the case
    run_ccy with-bus $bare
    check "bare '$bare', nobody to choose: 64" "64" "$RC"
    check "  it says why: choosing a team needs a terminal, name the seat" "yes" "$(has 'needs a terminal: name the seat instead, --teams <seat>@<team>' "$work/ccy.err")"
    check "  only the check ran, on the empty list, and no chooser" "seat check  --resolved" "$(bus_calls)"
done
FAKE_CHOOSE_TEAMS_RC=1 run_ccy with-bus --teams dev1@team-a --headless
check "an explicit --teams never asks ccy-teams" "no" "$(has 'choose-teams' "$FAKE_BUS_LOG")"
run_ccy with-bus --teams dev1@team-a --teams qa2@team-b --headless
check "--teams given twice: 64" "64" "$RC"
check "  it says one flag carries every seat" "yes" "$(has "once" "$work/ccy.err")"
check "  nothing ran" "yes" "$(nothing_ran)"
run_ccy with-bus --teams=dev1@team-a --teams qa2@team-b --headless
check "--teams= then --teams: 64" "64" "$RC"

FAKE_CHECK_RC=64 run_ccy with-bus --teams 'dev1@t,dev2@t' --headless
check "seat check's 64 is the launch's exit code" "64" "$RC"
check "  agent-bus was asked to check the list, nothing else" "seat check dev1@t,dev2@t --resolved" "$(bus_calls)"
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
    "seat check  dev1@team-a , qa2@team-b --resolved" "$(bus_calls)"

run_ccy with-bus --headless --prompt go --teams team-a
check "a team alone with nobody to choose (headless): refused, 64" "64" "$RC"
check "  nothing but the check ran, and no chooser" "seat check team-a --resolved" "$(bus_calls)"
check "  no SSH, engine or token work" "yes" "$(no_session_tools)"

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
        # The Quick Launch prompt answers QUICK_ANSWER as typed (empty is a bare Enter); with
        # QUICK_ANSWER unset it is end of input (Ctrl-D), so read fails and sets nothing.
        read() {
            if [ "$1" = -rp ]; then
                echo PROMPT >>"$PROMPT_LOG"
                [ -n "${QUICK_ANSWER+set}" ] || return 1
                printf -v "$3" "%s" "$QUICK_ANSWER"; return 0
            fi
            builtin read "$@"
        }
        ccy_launch_teams() { printf "%s\n" "$TEAMS_LIST" >"$TEAMS_LOG"; CCY_SEATS="$TEAMS_LIST"; }
        ccy_nobody_to_ask() { [ "${QUICK_NOBODY:-false}" = true ]; }
        ccy-teams() {
            if [ "$1" = describe-seats ]; then printf "%s%s%s\n" "$3" "${4:-none}" "${4:+ (worker)}"; return 0; fi
            printf "%s\n" "$*" >>"$TEAMS_REC"; return "${FAKE_RECORD_RC:-0}"
        }
        CONFIG_VERSION=2 CCY_VERSION=9.9.9 CCY_HASH=h TOKEN_DIR=/nonexistent CCY_PROMPT_QUICK_LAUNCH="?"
        HEADLESS_MODE="$1" SESSION_RESTORE="${QUICK_RESTORE:-false}" NO_SSH_MODE=false SSH_KEYS=() SPECIFIED_TOKEN=""
        SPECIFIED_NETWORK="" NO_NETWORK_MODE=false NETWORK_FROM_CONFIG=false
        TEAMS_GIVEN="${QUICK_TEAMS_GIVEN:-false}" TEAMS_LIST="${QUICK_TEAMS_LIST:-}" CCY_SEATS="" CCY_ORIG_ARGS=(--orig)
        CCY_SESSION_NAME="${QUICK_SESSION-ccy-proj-1}"
        [ "${2:-}" = --no-ssh ] && NO_SSH_MODE=true
        . "$0" >"$QUICK_OUT"
        printf "%s|%s" "$TEAMS_LIST" "${CCY_ORIG_ARGS[*]}" >"$TEAMS_END"
        printf "token=%s keys=%s no_ssh=%s net=%s no_net=%s" "$SPECIFIED_TOKEN" "${SSH_KEYS[*]}" \
            "$NO_SSH_MODE" "$SPECIFIED_NETWORK" "$NO_NETWORK_MODE"' "$work/quick.bash" "$headless" "$@" \
        2>"$work/quick.err")
    printf ' rc=%s prompt=%s' "$?" "$(if [ -s "$PROMPT_LOG" ]; then echo yes; else echo no; fi)"
}
export PROMPT_LOG="$work/prompt.log"
# What the block printed on stdout: a headless session's stdout is its output (StderrHygiene).
export QUICK_OUT="$work/quick.out"
export TEAMS_LOG="$work/teams-launched.log" TEAMS_END="$work/teams-end.txt" TEAMS_REC="$work/teams-recorded.log"
# recorded_after <quick args...>: what the launch put into its session's record (ccy-teams
# record-seats calls), or none.
recorded_after() {
    : >"$TEAMS_REC"
    quick "$@" >/dev/null
    if [ -s "$TEAMS_REC" ]; then paste -sd'|' "$TEAMS_REC"; else echo none; fi
}
# save <token> <keys> <network> [teams]: a format-2 file, as the launcher writes it.
save() {
    : >"$PROMPT_LOG"
    printf 'SAVED_CONFIG_VERSION=2\nSAVED_CCY_VERSION="9.9.9"\nSAVED_CCY_HASH="h"\nLAST_TOKEN="%s"\nLAST_SSH_KEYS="%s"\nLAST_NETWORK="%s"\nLAST_TEAMS="%s"\n' \
        "$1" "$2" "$3" "${4:-}" >"$proj/.claude/ccy/.last-launch.conf"
}
# teams_after <quick output args...>: whether the team launch ran, for what list, and the
# arguments a restart would replay.
teams_after() {
    : >"$TEAMS_LOG"
    quick "$@" >/dev/null
    printf 'ran=%s end=%s' "$(tr -d '\n' <"$TEAMS_LOG")" "$(cat "$TEAMS_END")"
}

save personal /keys/id_a proj_net
check "headless with saved choices: taken, no prompt" \
    "token=personal keys=/keys/id_a no_ssh=false net=proj_net no_net=false rc=0 prompt=no" "$(quick true)"
check "  the Quick Launch banner went to stderr, nothing to stdout" "yes" \
    "$(if [ ! -s "$QUICK_OUT" ]; then has "Headless launch: using the previous configuration" "$work/quick.err"; else
        echo no; fi)"
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

echo "=== Quick Launch remembers the team ==="

# Plan 00161 M2.9: a plain launch is on no team. Only --teams, or a person answering the
# Quick Launch offer that shows the team, may put a session in one; nobody answers a headless
# launch, a launch with no terminal or a restore, so their saved seats are never taken.
save personal /keys/id_a proj_net dev1@team-a
check "headless, a saved team: not launched into, a restart would replay none (M2.9)" \
    "ran= end=|--orig" "$(teams_after true)"
check "  the rest of the saved choices are still taken" "yes" \
    "$(save personal /keys/id_a proj_net dev1@team-a; quick true | grep -q 'token=personal keys=/keys/id_a' && echo yes || echo no)"
check "  it says the saved team is not taken" "yes" "$(has "not its team" "$work/quick.err")"
check "  and it records nothing" "none" "$(save personal /keys/id_a proj_net dev1@team-a; recorded_after true)"
save personal /keys/id_a proj_net dev1@team-a
check "no terminal to ask on, a saved team: not launched into" \
    "ran= end=|--orig" "$(QUICK_NOBODY=true teams_after false)"
check "  it says the saved team is not taken" "yes" "$(has "not its team" "$work/quick.err")"
save personal /keys/id_a proj_net dev1@team-a
check "  the summary says no team, never the saved one's seat and role" "yes no" \
    "$(has "Team:    none" "$work/quick.err") $(has "(worker)" "$work/quick.err")"
save personal /keys/id_a proj_net dev1@team-a
check "interactive: the summary shows the team, its seat and its role" "yes" \
    "$(QUICK_ANSWER=y quick false >/dev/null; has "Team:    dev1@team-a (worker)" "$work/quick.err")"
save personal /keys/id_a proj_net dev1@team-a,qa2@team-b
check "  several seats are kept together" "ran=dev1@team-a,qa2@team-b end=dev1@team-a,qa2@team-b|--orig --teams dev1@team-a,qa2@team-b" \
    "$(QUICK_ANSWER=y teams_after false)"
save personal /keys/id_a proj_net dev1@team-a
check "interactive, y typed: the team comes with it" \
    "ran=dev1@team-a end=dev1@team-a|--orig --teams dev1@team-a" "$(QUICK_ANSWER=y teams_after false)"
save personal /keys/id_a proj_net dev1@team-a
check "interactive, a bare Enter (the default yes): the team comes with it" \
    "ran=dev1@team-a end=dev1@team-a|--orig --teams dev1@team-a" "$(QUICK_ANSWER='' teams_after false)"
save personal /keys/id_a proj_net dev1@team-a
check "interactive, n typed: no team, and no saved choice is taken" \
    "ran= end=|--orig token= keys=" \
    "$(QUICK_ANSWER=n teams_after false) $(save personal /keys/id_a proj_net dev1@team-a; QUICK_ANSWER=n quick false | grep -o 'token=[^ ]* keys=[^ ]*')"
save personal /keys/id_a proj_net dev1@team-a
check "interactive, end of input (Ctrl-D) is not a yes: no team" \
    "ran= end=|--orig" "$(teams_after false)"
check "  it says the saved team is not taken" "yes" "$(has "not its team" "$work/quick.err")"
check "  and it records nothing" "none" "$(save personal /keys/id_a proj_net dev1@team-a; recorded_after false)"
save personal /keys/id_a proj_net dev1@team-a
check "a session restore never takes the saved team: it is in a team only if its own record says so" \
    "ran= end=|--orig" "$(QUICK_RESTORE=true teams_after false)"
check "  and it records nothing" "none" "$(QUICK_RESTORE=true recorded_after false)"
save personal /keys/id_a proj_net dev1@team-a
check "a quick-launched team goes into the session's own record, as an explicit --teams does" \
    "record-seats ccy-proj-1 dev1@team-a" "$(QUICK_ANSWER=y recorded_after false)"
save personal /keys/id_a proj_net dev1@team-a,qa2@team-b
check "  two quick-launched teams go into the record together" \
    "record-seats ccy-proj-1 dev1@team-a,qa2@team-b" "$(QUICK_ANSWER=y recorded_after false)"
save personal /keys/id_a proj_net dev1@team-a
check "  a launch that is no session of its own (run from inside another session's pane) records nothing" \
    "none" "$(QUICK_SESSION="" QUICK_ANSWER=y recorded_after false)"
save personal /keys/id_a proj_net dev1@team-a
check "  a record that cannot be written stops the launch" "yes" \
    "$(FAKE_RECORD_RC=1 QUICK_ANSWER=y quick false | grep -q 'rc=1 prompt=' && echo yes || echo no)"
save personal /keys/id_a proj_net dev1@team-a
check "an explicit --teams records nothing here (the launch's arguments carry it)" "none" \
    "$(QUICK_TEAMS_GIVEN=true QUICK_TEAMS_LIST=other-team QUICK_ANSWER=y recorded_after false)"
save personal /keys/id_a proj_net ""
check "a launch with no saved team records nothing" "none" "$(QUICK_ANSWER=y recorded_after false)"
save personal /keys/id_a proj_net dev1@team-a
save personal /keys/id_a proj_net ""
check "saved with no team: none is launched into" "ran= end=|--orig" "$(teams_after true)"
check "  the summary says none" "yes" "$(has "Team:    none" "$work/quick.err")"
save personal /keys/id_a proj_net dev1@team-a
check "an explicit --teams overrides the saved team" "ran= end=other-team|--orig" \
    "$(QUICK_TEAMS_GIVEN=true QUICK_TEAMS_LIST=other-team teams_after true)"
check "  the summary shows the team given, not the saved one" "yes" "$(has "Team:    other-team" "$work/quick.err")"
check "  the saved one is not shown" "no" "$(has "dev1@team-a" "$work/quick.err")"
save personal /keys/id_a proj_net dev1@team-a
check "only --teams given: the Quick Launch offer still runs (token, keys and network from the file)" \
    "token=personal keys=/keys/id_a no_ssh=false net=proj_net no_net=false rc=0 prompt=no" \
    "$(QUICK_TEAMS_GIVEN=true QUICK_TEAMS_LIST=other-team quick true)"
check "the launcher saves the team it launched into" "1" \
    "$(grep -c -F "LAST_TEAMS=\"\${CCY_SEATS}\"" "$LAUNCHER")"

echo "=== Quick Launch across ccy versions: kept while the file's format is unchanged ==="

conf="$proj/.claude/ccy/.last-launch.conf"
# save_as <format line, or ""> <ccy version> <hash> [key line to leave out]: a saved file as
# another ccy wrote it. The harness's own ccy is 9.9.9, hash h, format 2.
save_as() {
    : >"$PROMPT_LOG"
    {
        [ -z "$1" ] || printf '%s\n' "$1"
        printf 'SAVED_CCY_VERSION="%s"\nSAVED_CCY_HASH="%s"\n' "$2" "$3"
        printf '%s\n' 'LAST_TOKEN="personal"' 'LAST_SSH_KEYS="/keys/id_a"' 'LAST_NETWORK="proj_net"' 'LAST_TEAMS=""'
    } | grep -v "^${4:-NOTHING}=" >"$conf"
}
kept_choices="token=personal keys=/keys/id_a no_ssh=false net=proj_net no_net=false rc=0 prompt=no"
refused="rc=1 prompt=no"
file_left() { if [ -f "$conf" ]; then echo kept; else echo removed; fi; }

save_as SAVED_CONFIG_VERSION=2 3.80.0 old
check "another ccy version, same format: the saved choices are taken headless" "$kept_choices" "$(quick true)"
check "  the file is kept" "kept" "$(file_left)"
check "  no version-change discard is announced" "no" "$(has "version changed" "$work/quick.err")"
save_as SAVED_CONFIG_VERSION=2 3.80.0 old
check "another ccy version, same format: interactive still offers them" "yes" \
    "$(quick false | grep -q 'prompt=yes' && echo yes || echo no)"
save_as SAVED_CONFIG_VERSION=2 9.9.9 other-hash
check "same version, another hash (an unbumped change): warned, choices kept" "$kept_choices" "$(quick true)"
check "  the warning names the developer error" "yes" "$(has "without version bump" "$work/quick.err")"

save_as SAVED_CONFIG_VERSION=3 9.9.9 h
check "another format: discarded, so headless is refused" "$refused" "$(quick true | grep -o 'rc=.*')"
check "  the file is removed" "removed" "$(file_left)"
check "  the discard names the format" "yes" "$(has "format" "$work/quick.err")"
save_as "" 3.80.0 old
check "no format line: discarded" "$refused removed" "$(quick true | grep -o 'rc=.*') $(file_left)"
save_as SAVED_CONFIG_VERSION=2 3.80.0 old LAST_NETWORK
check "a format-2 file missing a key: discarded" "$refused removed" "$(quick true | grep -o 'rc=.*') $(file_left)"
check "  it names the missing key" "yes" "$(has "LAST_NETWORK" "$work/quick.err")"
save_as SAVED_CONFIG_VERSION=2 3.80.0 old LAST_TEAMS
check "a format-2 file without LAST_TEAMS: discarded" "$refused removed" "$(quick true | grep -o 'rc=.*') $(file_left)"
check "  it names the missing key" "yes" "$(has "LAST_TEAMS" "$work/quick.err")"
save_as SAVED_CONFIG_VERSION=2 3.80.0 old LAST_TOKEN
check "a missing key is not filled from the environment" "$refused removed" \
    "$(LAST_TOKEN=leaked quick true | grep -o 'rc=.*') $(file_left)"

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

for bus_lib in agent-bus-seats agent-bus-membership; do
    check "$bus_lib is in CCY_LIBS" "1" "$(grep -c -E "^CCY_LIBS=\\(.* $bus_lib( |\\))" "$LAUNCHER")"
    check "the launcher sources $bus_lib" "1" "$(grep -c -F "source \"\$SCRIPT_DIR/lib/$bus_lib.bash\"" "$LAUNCHER")"
    check "the play installs $bus_lib" "1" "$(grep -c -F "files/var/local/claude-yolo/lib/$bus_lib.bash" "$PLAY")"
done
check "--help documents --no-bus" "1" "$(grep -c -E '^  --no-bus ' "$LAUNCHER")"
check "the did-you-mean list knows --no-bus" "yes" \
    "$(awk '/^            ccy_flags=\(/,/^            \)/' "$LAUNCHER" | grep -q -- '--no-bus' && echo yes || echo no)"
check "the take call passes --no-bus and the container name" "1" \
    "$(grep -c -F "\"\$NO_BUS_MODE\" \"\$CONTAINER_NAME\" || exit \$?" "$LAUNCHER")"
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
choose_call="ccy-teams choose-teams --type=podman \"\$TEAMS_LIST\""
choose_line=$(grep -n -F "$choose_call" "$LAUNCHER" | cut -d: -f1)
check "the chooser runs once, before the rewrite (so the chosen seat is what is replayed)" "yes" \
    "$([ "$(grep -c -F "$choose_call" "$LAUNCHER")" = 1 ] && [ -n "$rewrite_line" ] \
        && [ "$choose_line" -lt "$rewrite_line" ] && echo yes || echo no)"
check "the launcher keeps no chooser logic of its own: no for-checkout, no resolve loop" "0" \
    "$(cat "$LAUNCHER" "$LIB_DIR/agent-bus-seats.bash" | grep -c -E 'for-checkout|ccy_seats_resolve|choose-seat' || true)"
# The chooser is asked, attended, for a bare --teams and for a team named alone, never for a
# list that already names every seat (so a saved Quick Launch list needs no ccy-teams).
needs_chooser() {
    local TEAMS_LIST="$1" attended=true cond
    cond=$(grep -F 'TEAMS_LIST" =~ ,[^@,]+' "$LAUNCHER")
    [ -n "$cond" ] || { echo missing; return; }
    : "$TEAMS_LIST" "$attended" # read by the condition eval'd below
    eval "$cond"$'\n echo yes\n else echo no\n fi'
}
check "a bare --teams asks the chooser" "yes" "$(needs_chooser '')"
check "a team alone asks it" "yes" "$(needs_chooser 'team-a')"
check "  and so does a team alone among seats" "yes" "$(needs_chooser '1@team-a, team-b')"
check "a list naming every seat does not" "no" "$(needs_chooser '1@team-a,qa2@team-b')"
check "a restore, a restart, headless and no terminal count as nobody to choose" "yes" \
    "$(grep -A2 '^    ccy_nobody_to_ask || \[ ! -t 2 \]' "$LAUNCHER" | tr '\n' ' ' | grep -F 'ccy_nobody_to_ask' \
        | grep -F 'CCY_SESSION_RESTORE' | grep -F 'CCY_RESTART_RELAUNCH_SESSION' \
        | grep -qF '|| attended=true' && echo yes || echo no)"
check "Quick Launch's summary names each seat with its role, through ccy-teams" "1" \
    "$(grep -c -F 'ccy-teams describe-seats' "$LAUNCHER")"
check "the flag and the Quick Launch team go through the one function (definition and two calls)" "3" \
    "$(grep -c -E '^ *ccy_launch_teams' "$LAUNCHER")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
