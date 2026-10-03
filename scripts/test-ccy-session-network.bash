#!/usr/bin/env bash
# Unit-test the session→container→network join used by the ccy-sessions picker
# (files/var/local/claude-yolo/lib/tmux-session.bash).
#
# WHY THIS EXISTS. A tmux session knows nothing about containers, and a container knows
# nothing about tmux. The only link between the two is the process tree: the engine client
# that names the container on its own command line is a descendant of the session's pane.
# Get that walk wrong and the picker labels a session with ANOTHER session's network, which
# is worse than showing nothing — a reader would connect to the wrong thing on purpose.
#
# The engine's rendering of a container's network list is the second trap. Podman's `ps`
# template prints a Go slice (`[name]`, `[]`, `[a b]`); Docker prints a comma string
# (`a,b`). Both must reduce to the same word, and an empty list must NOT be reported the
# same way as "no container was found" — those are different facts about the session.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for lib in common-pure.bash tmux-session.bash; do
    if [ ! -f "$LIB_DIR/$lib" ]; then
        echo "FAIL: $lib not found at $LIB_DIR/$lib" >&2
        exit 1
    fi
done

# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/tmux-session.bash"

for fn in ccy_session_containers ccy_network_word ccy_identity_words ccy_session_pids ccy_cpu_words \
    ccy_tmux_row ccy_tmux_row_heading; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: $fn is not defined after sourcing the libraries" >&2
        exit 1
    fi
done

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# ── the process-tree walk: which container belongs to which session ──────────────────
#
# Shaped like the real thing: the pane runs the trampoline bash, which runs the launcher,
# which runs the engine client. Rootless podman re-executes itself in a user namespace, so
# the same container legitimately appears twice in one tree (pids 112/113 below).
PANES="$(
    cat <<'EOF'
ccy-alpha 100
ccy-beta 200
ccy-deep 300
cc-gamma 400
ccy-gone 500
EOF
)"

PROCESSES="$(
    cat <<'EOF'
1 0 /usr/lib/systemd/systemd --user
50 1 tmux -L ccy
100 50 bash -c trampoline
110 100 /bin/bash /var/local/claude-yolo/claude-yolo
112 110 podman run --rm -it --name alpha_yolo --network alpha-network img claude
113 112 podman run --rm -it --name alpha_yolo --network alpha-network img claude
200 50 bash -c trampoline
210 200 /bin/bash /var/local/claude-yolo/claude-yolo
211 210 /usr/bin/podman run --rm --name=beta_yolo img claude
300 50 bash -c trampoline
310 300 /bin/bash /var/local/claude-yolo/claude-yolo
311 310 /bin/sh -c wrapper
312 311 /usr/bin/docker run --rm --name deep_yolo img claude
400 50 bash -c trampoline
410 400 claude --dangerously-skip-permissions
500 50 bash -c trampoline
510 500 /usr/bin/editor --name not-a-container
900 1 podman run --rm --name orphan_yolo img claude
EOF
)"

containers="$(ccy_session_containers "$PANES" "$PROCESSES")"

row_for() {
    local want="$1" line
    while IFS= read -r line; do
        [ "${line%% *}" = "$want" ] && {
            printf '%s' "$line"
            return 0
        }
    done <<<"$containers"
    printf 'MISSING'
}

check "a direct engine client is matched to its session" \
    "ccy-alpha podman alpha_yolo" "$(row_for ccy-alpha)"
check "--name=value is read as well as --name value" \
    "ccy-beta podman beta_yolo" "$(row_for ccy-beta)"
check "the walk climbs past intermediate processes, whatever the engine" \
    "ccy-deep docker deep_yolo" "$(row_for ccy-deep)"

# A cc session runs claude on the host; there is no container to name, and that is its
# ordinary state, not a failure.
check "a session with no engine client reports no container" \
    "cc-gamma - -" "$(row_for cc-gamma)"
# The negative control that matters: --name is not an engine-only flag.
check "a non-engine process carrying --name is ignored" \
    "ccy-gone - -" "$(row_for ccy-gone)"

# An engine client outside every pane belongs to no session here and must not be adopted
# by one. Losing this check is how a picker starts telling confident lies.
check "a container outside the session trees is not adopted" \
    "MISSING" "$(row_for ccy-orphan)"
check "every session gets exactly one line" "5" "$(printf '%s\n' "$containers" | grep -c .)"
check "sessions are reported in the order the panes arrived" \
    "ccy-alpha ccy-beta ccy-deep cc-gamma ccy-gone" \
    "$(printf '%s\n' "$containers" | awk '{ printf "%s%s", sep, $1; sep = " " } END { print "" }')"

# ── the network word: both engines' renderings, and the two empty states ─────────────
NETWORKS="$(
    cat <<'EOF'
alpha_yolo [alpha-network]
beta_yolo []
deep_yolo [front back]
docker_yolo front,back
solo_yolo lone-network
bare_yolo
EOF
)"

check "a single network is printed by name" \
    "alpha-network" "$(ccy_network_word alpha_yolo "$NETWORKS")"
check "podman's empty slice means connected to no network" \
    "none" "$(ccy_network_word beta_yolo "$NETWORKS")"
check "podman's multi-element slice is joined with commas" \
    "front,back" "$(ccy_network_word deep_yolo "$NETWORKS")"
check "docker's comma string is read the same way" \
    "front,back" "$(ccy_network_word docker_yolo "$NETWORKS")"
check "an unbracketed single name is printed as-is" \
    "lone-network" "$(ccy_network_word solo_yolo "$NETWORKS")"
check "a container the engine lists with no networks at all is none" \
    "none" "$(ccy_network_word bare_yolo "$NETWORKS")"

# The distinction the whole feature rests on: "connected to nothing" and "there is nothing
# to ask about" are different facts and must never share a word.
check "no container is a different word from none" \
    "no container" "$(ccy_network_word - "$NETWORKS")"
check "a container the engine no longer lists reports no container" \
    "no container" "$(ccy_network_word ghost_yolo "$NETWORKS")"
check "an empty network table does not turn into none" \
    "no container" "$(ccy_network_word alpha_yolo "")"

# ── the token and key words: the container's ccy-token and ccy-ssh-keys labels ───────
TAB=$'\t'
LABELS="$(
    cat <<'EOF'
alpha_yolo|work|key_0
beta_yolo|none|none
deep_yolo|personal|key_0 key_1
old_yolo||
EOF
)"

check "a session's token name and key are read off its container's labels" \
    "work${TAB}key_0" "$(ccy_identity_words alpha_yolo "$LABELS")"
check "none from the launcher stays none" \
    "none${TAB}none" "$(ccy_identity_words beta_yolo "$LABELS")"
check "several keys are joined with commas, one column" \
    "personal${TAB}key_0,key_1" "$(ccy_identity_words deep_yolo "$LABELS")"
check "a container without the labels says so, not none" \
    "unlabelled${TAB}unlabelled" "$(ccy_identity_words old_yolo "$LABELS")"
check "no container gives - in both" \
    "-${TAB}-" "$(ccy_identity_words - "$LABELS")"
check "a container the engine no longer lists gives - in both" \
    "-${TAB}-" "$(ccy_identity_words ghost_yolo "$LABELS")"

# ── CPU: which processes are a session's, and how busy they were ─────────────────────
#
# A session's processes are everything under its pane AND everything under its container's
# first process: rootless podman's container is a child of conmon, not of the podman client
# in the pane, so the pane's tree alone would show a busy ccy session as idle.
CPU_ROOTS="ccy-alpha 100
ccy-alpha 700
cc-gamma 400
ccy-idle 800"
CPU_PROCESSES="1 0 systemd
100 1 bash -c trampoline
110 100 /bin/bash claude-yolo
111 110 podman run --name alpha_yolo img
650 1 conmon
700 650 /entrypoint.sh
701 700 claude
702 701 npm test
400 1 bash -c trampoline
410 400 claude
411 410 git status
800 1 bash -c trampoline
900 1 unrelated"
check "a ccy session owns its pane's tree and its container's" \
    "ccy-alpha 100 110 111 700 701 702" "$(ccy_session_pids "$CPU_ROOTS" "$CPU_PROCESSES" | grep '^ccy-alpha')"
check "a cc session owns its pane's tree" \
    "cc-gamma 400 410 411" "$(ccy_session_pids "$CPU_ROOTS" "$CPU_PROCESSES" | grep '^cc-gamma')"
check "a session with only its pane still gets a line" \
    "ccy-idle 800" "$(ccy_session_pids "$CPU_ROOTS" "$CPU_PROCESSES" | grep '^ccy-idle')"
check "a process under no root belongs to no session" \
    "" "$(ccy_session_pids "$CPU_ROOTS" "$CPU_PROCESSES" | grep -w 900)"

# Ticks at 100 per second, over one second: 100 ticks is one whole core, 100%.
SESSION_PIDS="busy 1 2
idle 3
newcomer 9
twocores 4
gone 5"
BEFORE="1 100
2 50
3 10
4 0
5 70"
AFTER="1 150
2 60
3 10
4 150
9 5"
cpu="$(ccy_cpu_words "$SESSION_PIDS" "$BEFORE" "$AFTER" 1000000 100)"
check "the session's processes' ticks are summed over the interval" "busy 60%" "$(grep '^busy' <<<"$cpu")"
check "a session that used nothing is 0%" "idle 0%" "$(grep '^idle' <<<"$cpu")"
check "a process born during the interval counts all its time" "newcomer 5%" "$(grep '^newcomer' <<<"$cpu")"
check "more than one core reads above 100%, as top does" "twocores 150%" "$(grep '^twocores' <<<"$cpu")"
check "a process that exited during the interval is not counted against anything" \
    "gone 0%" "$(grep '^gone' <<<"$cpu")"
check "half a second at the same ticks is twice the share" \
    "busy 120%" "$(ccy_cpu_words "busy 1 2" "$BEFORE" "$AFTER" 500000 100)"

# ── the picker row: the column appears only when a network was asked for ─────────────
HOME_SAVED="$HOME"
HOME="/home/<user>"
three="$(ccy_tmux_row ccy-alpha 0 "$HOME/code/project")"
four="$(ccy_tmux_row ccy-alpha 0 "$HOME/code/project" alpha-network)"
attached_row="$(ccy_tmux_row ccy-alpha 1 "$HOME/code/project" "no container")"
full="$(ccy_tmux_row ccy-alpha 0 "$HOME/code/project" alpha-network max-plan key_0,key_1 37%)"
heading="$(ccy_tmux_row_heading)"
HOME="$HOME_SAVED"

read -r -a three_fields <<<"$three"
read -r -a four_fields <<<"$four"
read -r -a full_fields <<<"$full"

# ccy's own offer picker passes three arguments. A blank column there would be a lie by
# omission — it would read as "no network" — so the column has to be absent, not empty.
check "three arguments produce three columns" "3" "${#three_fields[@]}"
check "the three-column row is name, state, directory" \
    "ccy-alpha detached ~/code/project" "${three_fields[*]}"

check "a fourth argument produces four columns" "4" "${#four_fields[@]}"
check "the network sits between the state and the directory" \
    "ccy-alpha detached alpha-network ~/code/project" "${four_fields[*]}"

# The pickers read the session name off the front of the row and test the row TEXT for the
# state words, so the new column must disturb neither.
check "the session name is still the first field" "ccy-alpha" "${four%% *}"
check "an attached session still says open elsewhere" \
    "yes" "$(case "$attached_row" in *"open elsewhere"*) echo yes ;; *) echo "no: $attached_row" ;; esac)"
check "a multi-word network word does not break the name field" \
    "ccy-alpha" "${attached_row%% *}"

check "cpu, token and key make seven columns" "7" "${#full_fields[@]}"
check "cpu follows the state; token and key sit between the network and the directory" \
    "ccy-alpha detached 37% alpha-network max-plan key_0,key_1 ~/code/project" "${full_fields[*]}"
# The heading is printed above the rows by ccy-sessions --list, so each title must start
# where its column does.
check "the heading's columns line up with the row's" \
    "$(awk '{ print index($0, "37%"), index($0, "alpha-network"), index($0, "max-plan"), index($0, "key_0") }' <<<"$full")" \
    "$(awk '{ print index($0, "CPU"), index($0, "NETWORK"), index($0, "TOKEN"), index($0, "SSH KEY") }' <<<"$heading")"

# ── the probe itself, over stubbed tmux / ps / engine ────────────────────────────────
#
# The pure halves above are only worth what the function that joins them is worth, so this
# drives ccy_tmux_detail_rows — the thing ccy-sessions actually calls — with every outside
# command replaced by a shell function. A real tmux server and a real container are out of
# reach in a checkout, and this is the path they would exercise.
STUB_PANES="ccy-alpha 100
ccy-beta 200
cc-gamma 400"

STUB_PROCESSES="1 0 /usr/lib/systemd/systemd --user
100 1 bash -c trampoline
110 100 /bin/bash /var/local/claude-yolo/claude-yolo
111 110 podman run --rm -it --name alpha_yolo img claude
200 1 bash -c trampoline
210 200 /bin/bash /var/local/claude-yolo/claude-yolo
211 210 podman run --rm -it --name beta_yolo img claude
400 1 bash -c trampoline
410 400 claude --dangerously-skip-permissions
700 1 /entrypoint.sh
701 700 claude
800 1 /entrypoint.sh"

STUB_PODMAN_PS="alpha_yolo|[alpha-network]|work|key_0 key_1
beta_yolo|[]|none|none"
# The two queries the probe may make. Pinned here so a change to either is a deliberate
# change to the test as well: the stub engine answers nothing else.
PS_FORMAT='{{.Names}}|{{.Networks}}|{{.Label "ccy-token"}}|{{.Label "ccy-ssh-keys"}}'
INSPECT_FORMAT='{{.Name}} {{.State.Pid}}'

# The CPU sample: the clock in microseconds, then "<pid> <ticks>". The stub answers the
# first sample and the second alternately: one second apart, with alpha's claude having used
# half a core and nothing else anything. Counted in a file, as the probe runs in a subshell.
TICKS_BEFORE="0
701 100
800 50
410 7"
TICKS_AFTER="1000000
701 150
800 50
410 7"
SAMPLE_LOG="$(mktemp)"
ccy_cpu_sample() {
    printf 'sample %s\n' "$*" >>"$SAMPLE_LOG"
    if [ $(($(grep -c . "$SAMPLE_LOG") % 2)) -eq 1 ]; then
        printf '%s\n' "$TICKS_BEFORE"
    else
        printf '%s\n' "$TICKS_AFTER"
    fi
}
getconf() { printf '100\n'; }
export CCY_CPU_SAMPLE_SECONDS=0

PANES_RC=0
# The probe's output is captured with $(...), which runs it in a subshell, so a counter
# variable would never come back. The engine stub records its calls in a file instead.
CALL_LOG="$(mktemp)"
trap 'rm -f "$CALL_LOG" "$SAMPLE_LOG"' EXIT
engine_calls() { grep -c . "$CALL_LOG"; }

PANES_ERR="lost server"
ccy_tmux() {
    [ "$PANES_RC" -eq 0 ] || {
        echo "$PANES_ERR"
        return 1
    }
    case "$*" in
    "list-panes -a -F #{session_name} #{pane_pid}") printf '%s\n' "$STUB_PANES" ;;
    *)
        echo "unexpected tmux call: $*"
        return 1
        ;;
    esac
}
ps() { printf '%s\n' "$STUB_PROCESSES"; }
podman() {
    printf 'call\n' >>"$CALL_LOG"
    case "$*" in
    "ps --filter label=ccy=true --format $PS_FORMAT") printf '%s\n' "$STUB_PODMAN_PS" ;;
    # Docker prefixes the name with a slash and podman does not; beta answers as docker would.
    "inspect --format $INSPECT_FORMAT alpha_yolo beta_yolo") printf '%s\n' "alpha_yolo 700" "/beta_yolo 800" ;;
    *)
        echo "unexpected engine call: $*"
        return 1
        ;;
    esac
}

# The stubs must actually shadow the outside world. If `ps` still reached the real process
# table, everything below would be testing whichever host happened to run it — and would
# pass or fail for reasons that have nothing to do with this code.
check "the ps stub shadows the real process table" "yes" \
    "$(case "$(ps)" in *alpha_yolo*) echo yes ;; *) echo no ;; esac)"
check "the engine stub shadows the real engine" "yes" \
    "$(case "$(podman ps --filter label=ccy=true --format "$PS_FORMAT")" in
    *alpha-network*) echo yes ;;
    *) echo no ;;
    esac)"
check "the engine stub answers the pid question" "alpha_yolo 700" \
    "$(podman inspect --format "$INSPECT_FORMAT" alpha_yolo beta_yolo | grep '^alpha')"
check "the tick-rate stub shadows the real one" "100" "$(getconf CLK_TCK)"
check "the CPU sample stub gives the first sample first" "0" "$(ccy_cpu_sample 701 | awk 'NR == 1')"
: >"$CALL_LOG"
: >"$SAMPLE_LOG"

# The probe is expected to succeed here, so its status is consumed: a failure must show up
# as a failed case, not as an empty string that quietly mismatches every expectation below.
# Nothing reaches stderr on the success path, so folding it in only adds the reason.
probe_out=""
if ! probe_out="$(ccy_tmux_detail_rows 2>&1)"; then
    probe_out="UNEXPECTED PROBE FAILURE: ${probe_out}"
fi
check "the probe gives a connected session its network, token, keys and CPU" \
    "ccy-alpha${TAB}alpha-network${TAB}work${TAB}key_0,key_1${TAB}50%" "$(grep '^ccy-alpha' <<<"$probe_out")"
check "the probe reports none for a container on no named network, with no token or key" \
    "ccy-beta${TAB}none${TAB}none${TAB}none${TAB}0%" "$(grep '^ccy-beta' <<<"$probe_out")"
check "the probe reports no container for a host cc session" \
    "cc-gamma${TAB}no container${TAB}-${TAB}-${TAB}0%" "$(grep '^cc-gamma' <<<"$probe_out")"
check "one row per session, no more" "3" "$(printf '%s\n' "$probe_out" | grep -c .)"
check "the CPU is sampled twice, over every session's processes and its container's" \
    "2 yes" "$(grep -c . "$SAMPLE_LOG") $(grep -q 'sample .*\b701\b' "$SAMPLE_LOG" && grep -q '\b410\b' "$SAMPLE_LOG" && echo yes)"

# Flat in the session count is the whole reason this is one pass: the picker rebuilds its
# rows on every loop, and a lookup per row would be felt.
check "the engine is asked twice for all sessions, the list and the pids" "2" "$(engine_calls)"

# No session has a container, so there is no engine to ask and nothing to ask it.
STUB_PROCESSES="1 0 /usr/lib/systemd/systemd --user
400 1 bash -c trampoline
410 400 claude --dangerously-skip-permissions"
STUB_PANES="cc-gamma 400"
: >"$CALL_LOG"
if ! probe_out="$(ccy_tmux_detail_rows 2>&1)"; then
    probe_out="UNEXPECTED PROBE FAILURE: ${probe_out}"
fi
check "no container anywhere means the engine is never called" "0" "$(engine_calls)"
check "and the row still says why it is empty" "cc-gamma${TAB}no container${TAB}-${TAB}-${TAB}0%" "$probe_out"

# A here-string over empty text still yields one blank line. Without a guard that blank
# line becomes an empty engine name, which is then RUN — so no sessions at all has to be
# silent, not an error.
STUB_PANES=""
STUB_PROCESSES=""
: >"$CALL_LOG"
if ! probe_out="$(ccy_tmux_detail_rows 2>&1)"; then
    probe_out="UNEXPECTED PROBE FAILURE: ${probe_out}"
fi
check "no sessions at all produces no rows and no error" "" "$probe_out"
check "no sessions at all calls no engine" "0" "$(engine_calls)"

# A probe that cannot answer must SAY so and fail, so the caller can show "unknown". A
# silent empty result would reach the picker as a blank column, which reads as "no network".
PANES_RC=1
PANES_ERR="server exited unexpectedly"
probe_err="$(ccy_tmux_detail_rows 2>&1 >/dev/null)"
probe_rc=$?
check "a failed probe returns non-zero" "1" "$probe_rc"
case "$probe_err" in
*"list-panes"*) check "a failed probe says what failed" "yes" "yes" ;;
*) check "a failed probe says what failed" "yes" "no: ${probe_err}" ;;
esac

# No tmux server at all is the ordinary first-run state, not a failure. ccy-sessions calls
# this BEFORE it discovers there are no sessions, so a complaint here would be the first
# thing a user with nothing running ever sees from the command.
PANES_ERR="no server running on /tmp/tmux-1000/ccy"
noserver_err="$(ccy_tmux_detail_rows 2>&1 >/dev/null)"
noserver_rc=$?
check "no tmux server is not an error" "0" "$noserver_rc"
check "and it complains about nothing" "" "$noserver_err"

unset -f ps podman getconf ccy_cpu_sample

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
