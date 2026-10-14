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

# The fake gum: `gum choose --header H --selected ROW ROWS...` writes its header, selected row
# and rows to $FAKE_GUM_LOG, one per line, and prints the row FAKE_GUM_PICK names: `default`
# (the --selected one), a row number from 1, or `cancel` (Esc: exit 1, nothing printed).
cat >"$bus_bin/gum" <<'FAKE'
#!/usr/bin/env bash
[ "$1" = choose ] || { echo "fake gum: unexpected call: $*" >&2; exit 70; }
shift
header="" selected=""
while [ "$#" -gt 0 ]; do
    case "$1" in
    --header) header="$2"; shift 2 ;;
    --selected) selected="$2"; shift 2 ;;
    *) break ;;
    esac
done
{ printf 'header=%s\n' "$header"; printf 'selected=%s\n' "$selected"; printf 'row=%s\n' "$@"; } >"$FAKE_GUM_LOG"
case "${FAKE_GUM_PICK:-default}" in
cancel) exit 1 ;;
default) printf '%s\n' "$selected" ;;
*) printf '%s\n' "${!FAKE_GUM_PICK}" ;;
esac
FAKE
chmod 755 "$bus_bin/gum"
export FAKE_GUM_LOG="$work/gum.log"
# A PATH with the fake agent-bus but no gum at all (the ccy image ships a real one).
nogum_bin="$work/nogum-bin"
mkdir -p "$nogum_bin"
ln -s "$bus_bin/agent-bus" "$nogum_bin/agent-bus"
nogum_path="$nogum_bin"
for dir in /usr/bin /bin; do
    [ -e "$dir/gum" ] || nogum_path+=":$dir"
done

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

echo "=== a team alone: the seat chooser (Task 4.8) ==="

check "a list naming every seat: printed as it is, nothing asked" "dev1@team-a,qa@team-b
rc=0" "$(lib_run ccy_seats_resolve dev1@team-a,qa@team-b)"
check "  agent-bus not called" "none" "$(bus_calls)"
check "a team alone: the default (the lowest free seat) is taken" "2@team-a,qa@team-b
rc=0" "$(FAKE_GUM_PICK=default lib_run ccy_seats_resolve team-a,qa@team-b)"
check "  the seats were listed, and the resolved list checked again with --resolved" \
    "seat choose team-a|seat check 2@team-a,qa@team-b --resolved" "$(bus_calls)"
check "  the full handle is said before it is taken" "yes" \
    "$(has "Taking seat 2@team-a: myrepo.2+local.podman (ccy (podman))." "$work/lib.err")"
check "  the chooser starts on the default row" "yes" "$(has "selected=seat 2 " "$FAKE_GUM_LOG")"
check "  each row shows the full handle and what runs in it" "3" \
    "$(grep -c '^row=seat [123] .*myrepo\.[123]+local\.podman  *ccy (podman)' "$FAKE_GUM_LOG")"
check "  a held seat says so" "yes" "$(has "held by a running session" "$FAKE_GUM_LOG")"
check "  the new one is marked new" "yes" "$(grep -q '^row=seat 3 .*new seat$' "$FAKE_GUM_LOG" && echo yes || echo no)"
check "  the header names the team" "yes" "$(has "header=Team team-a:" "$FAKE_GUM_LOG")"
check "another row chosen: that seat" "3@team-a
rc=0" "$(FAKE_GUM_PICK=3 lib_run ccy_seats_resolve team-a)"
check "Esc: nothing printed, status 0" "rc=0" "$(FAKE_GUM_PICK=cancel lib_run ccy_seats_resolve team-a)"
check "  it says nothing started" "yes" "$(has "nothing started" "$work/lib.err")"
check "  and nothing was checked or taken" "seat choose team-a" "$(bus_calls)"
check "seat choose's refusal is passed on" "rc=78" "$(FAKE_CHOOSE_RC=78 lib_run ccy_seats_resolve team-a)"
check "rows with no DEFAULT: refused, 78" "rc=78" \
    "$(FAKE_CHOOSE_OUT=$'CHOICE\t1\tnew\tmyrepo.1+local.podman\tccy (podman)\n' lib_run ccy_seats_resolve team-a)"
check "gum not installed: refused, 78" "rc=78" \
    "$(: >"$FAKE_BUS_LOG"; LIB_DIR="$LIB_DIR" PATH="$nogum_path" bash "$work/lib-run.bash" ccy_seats_resolve team-a 2>"$work/lib.err")"
check "  it names the play and the seat to name instead" "yes yes" \
    "$(has play-claude-yolo.yml "$work/lib.err") $(has "--teams 2@team-a" "$work/lib.err")"

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
        read() { if [ "$1" = -rp ]; then echo PROMPT >>"$PROMPT_LOG"; return 1; fi; builtin read "$@"; }
        CONFIG_VERSION=1 CCY_VERSION=9.9.9 CCY_HASH=h TOKEN_DIR=/nonexistent CCY_PROMPT_QUICK_LAUNCH="?"
        HEADLESS_MODE="$1" SESSION_RESTORE=false NO_SSH_MODE=false SSH_KEYS=() SPECIFIED_TOKEN=""
        SPECIFIED_NETWORK="" NO_NETWORK_MODE=false NETWORK_FROM_CONFIG=false
        [ "${2:-}" = --no-ssh ] && NO_SSH_MODE=true
        . "$0" >"$QUICK_OUT"
        printf "token=%s keys=%s no_ssh=%s net=%s no_net=%s" "$SPECIFIED_TOKEN" "${SSH_KEYS[*]}" \
            "$NO_SSH_MODE" "$SPECIFIED_NETWORK" "$NO_NETWORK_MODE"' "$work/quick.bash" "$headless" "$@" \
        2>"$work/quick.err")
    printf ' rc=%s prompt=%s' "$?" "$(if [ -s "$PROMPT_LOG" ]; then echo yes; else echo no; fi)"
}
export PROMPT_LOG="$work/prompt.log"
# What the block printed on stdout: a headless session's stdout is its output (StderrHygiene).
export QUICK_OUT="$work/quick.out"
save() {
    : >"$PROMPT_LOG"
    printf 'SAVED_CONFIG_VERSION=1\nSAVED_CCY_VERSION="9.9.9"\nSAVED_CCY_HASH="h"\nLAST_TOKEN="%s"\nLAST_SSH_KEYS="%s"\nLAST_NETWORK="%s"\n' \
        "$1" "$2" "$3" >"$proj/.claude/ccy/.last-launch.conf"
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

echo "=== Quick Launch across ccy versions: kept while the file's format is unchanged ==="

conf="$proj/.claude/ccy/.last-launch.conf"
# save_as <format line, or ""> <ccy version> <hash> [key line to leave out]: a saved file as
# another ccy wrote it. The harness's own ccy is 9.9.9, hash h, format 1.
save_as() {
    : >"$PROMPT_LOG"
    {
        [ -z "$1" ] || printf '%s\n' "$1"
        printf 'SAVED_CCY_VERSION="%s"\nSAVED_CCY_HASH="%s"\n' "$2" "$3"
        printf '%s\n' 'LAST_TOKEN="personal"' 'LAST_SSH_KEYS="/keys/id_a"' 'LAST_NETWORK="proj_net"'
    } | grep -v "^${4:-NOTHING}=" >"$conf"
}
kept_choices="token=personal keys=/keys/id_a no_ssh=false net=proj_net no_net=false rc=0 prompt=no"
refused="rc=1 prompt=no"
file_left() { if [ -f "$conf" ]; then echo kept; else echo removed; fi; }

save_as SAVED_CONFIG_VERSION=1 3.80.0 old
check "another ccy version, same format: the saved choices are taken headless" "$kept_choices" "$(quick true)"
check "  the file is kept" "kept" "$(file_left)"
check "  no version-change discard is announced" "no" "$(has "version changed" "$work/quick.err")"
save_as SAVED_CONFIG_VERSION=1 3.80.0 old
check "another ccy version, same format: interactive still offers them" "yes" \
    "$(quick false | grep -q 'prompt=yes' && echo yes || echo no)"
save_as SAVED_CONFIG_VERSION=1 9.9.9 other-hash
check "same version, another hash (an unbumped change): warned, choices kept" "$kept_choices" "$(quick true)"
check "  the warning names the developer error" "yes" "$(has "without version bump" "$work/quick.err")"

save_as SAVED_CONFIG_VERSION=2 9.9.9 h
check "another format: discarded, so headless is refused" "$refused" "$(quick true | grep -o 'rc=.*')"
check "  the file is removed" "removed" "$(file_left)"
check "  the discard names the format" "yes" "$(has "format" "$work/quick.err")"
save_as "" 3.80.0 old
check "no format line: discarded" "$refused removed" "$(quick true | grep -o 'rc=.*') $(file_left)"
save_as SAVED_CONFIG_VERSION=1 3.80.0 old LAST_NETWORK
check "a format-1 file missing a key: discarded" "$refused removed" "$(quick true | grep -o 'rc=.*') $(file_left)"
check "  it names the missing key" "yes" "$(has "LAST_NETWORK" "$work/quick.err")"
save_as SAVED_CONFIG_VERSION=1 3.80.0 old LAST_TOKEN
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
resolve_call="ccy_seats_resolve \"\$CCY_SEATS\""
resolve_line=$(grep -n -F "$resolve_call" "$LAUNCHER" | cut -d: -f1)
check "the chooser runs once, before the rewrite (so the chosen seat is what is replayed)" "yes" \
    "$([ "$(grep -c -F "$resolve_call" "$LAUNCHER")" = 1 ] && [ -n "$rewrite_line" ] \
        && [ "$resolve_line" -lt "$rewrite_line" ] && echo yes || echo no)"
check "a restore, a restart, headless and no terminal count as nobody to choose" "yes" \
    "$(grep -A2 '^    _ccy_teams_attended=false' "$LAUNCHER" | tr '\n' ' ' | grep -F 'ccy_nobody_to_ask' \
        | grep -F 'CCY_SESSION_RESTORE' | grep -F 'CCY_RESTART_RELAUNCH_SESSION' \
        | grep -qF '|| _ccy_teams_attended=true' && echo yes || echo no)"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
