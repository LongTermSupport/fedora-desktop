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

for fn in ccy_session_containers ccy_network_word ccy_tmux_row; do
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

# ── the picker row: the column appears only when a network was asked for ─────────────
HOME_SAVED="$HOME"
HOME="/home/<user>"
three="$(ccy_tmux_row ccy-alpha 0 "$HOME/code/project")"
four="$(ccy_tmux_row ccy-alpha 0 "$HOME/code/project" alpha-network)"
attached_row="$(ccy_tmux_row ccy-alpha 1 "$HOME/code/project" "no container")"
HOME="$HOME_SAVED"

read -r -a three_fields <<<"$three"
read -r -a four_fields <<<"$four"

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

# ── the probe itself, over stubbed tmux / ps / engine ────────────────────────────────
#
# The pure halves above are only worth what the function that joins them is worth, so this
# drives ccy_tmux_network_rows — the thing ccy-sessions actually calls — with every outside
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
410 400 claude --dangerously-skip-permissions"

STUB_PODMAN_PS="alpha_yolo [alpha-network]
beta_yolo []"

PANES_RC=0
# The probe's output is captured with $(...), which runs it in a subshell, so a counter
# variable would never come back. The engine stub records its calls in a file instead.
CALL_LOG="$(mktemp)"
trap 'rm -f "$CALL_LOG"' EXIT
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
    "ps --filter label=ccy=true --format {{.Names}} {{.Networks}}") printf '%s\n' "$STUB_PODMAN_PS" ;;
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
    "$(case "$(podman ps --filter label=ccy=true --format '{{.Names}} {{.Networks}}')" in
    *alpha-network*) echo yes ;;
    *) echo no ;;
    esac)"
: >"$CALL_LOG"

# The probe is expected to succeed here, so its status is consumed: a failure must show up
# as a failed case, not as an empty string that quietly mismatches every expectation below.
# Nothing reaches stderr on the success path, so folding it in only adds the reason.
probe_out=""
if ! probe_out="$(ccy_tmux_network_rows 2>&1)"; then
    probe_out="UNEXPECTED PROBE FAILURE: ${probe_out}"
fi
check "the probe names the network of a connected session" \
    "alpha-network" "$(printf '%s\n' "$probe_out" | awk '$1 == "ccy-alpha" { print $2 }')"
check "the probe reports none for a container on no named network" \
    "none" "$(printf '%s\n' "$probe_out" | awk '$1 == "ccy-beta" { print $2 }')"
check "the probe reports no container for a host cc session" \
    "no container" "$(printf '%s\n' "$probe_out" | awk '$1 == "cc-gamma" { $1 = ""; sub(/^ /, ""); print }')"
check "one row per session, no more" "3" "$(printf '%s\n' "$probe_out" | grep -c .)"

# Flat in the session count is the whole reason this is one pass: the picker rebuilds its
# rows on every loop, and a lookup per row would be felt.
check "the engine is asked exactly once for all sessions" "1" "$(engine_calls)"

# No session has a container, so there is no engine to ask and nothing to ask it.
STUB_PROCESSES="1 0 /usr/lib/systemd/systemd --user
400 1 bash -c trampoline
410 400 claude --dangerously-skip-permissions"
STUB_PANES="cc-gamma 400"
: >"$CALL_LOG"
if ! probe_out="$(ccy_tmux_network_rows 2>&1)"; then
    probe_out="UNEXPECTED PROBE FAILURE: ${probe_out}"
fi
check "no container anywhere means the engine is never called" "0" "$(engine_calls)"
check "and the row still says why it is empty" "cc-gamma no container" "$probe_out"

# A here-string over empty text still yields one blank line. Without a guard that blank
# line becomes an empty engine name, which is then RUN — so no sessions at all has to be
# silent, not an error.
STUB_PANES=""
STUB_PROCESSES=""
: >"$CALL_LOG"
if ! probe_out="$(ccy_tmux_network_rows 2>&1)"; then
    probe_out="UNEXPECTED PROBE FAILURE: ${probe_out}"
fi
check "no sessions at all produces no rows and no error" "" "$probe_out"
check "no sessions at all calls no engine" "0" "$(engine_calls)"

# A probe that cannot answer must SAY so and fail, so the caller can show "unknown". A
# silent empty result would reach the picker as a blank column, which reads as "no network".
PANES_RC=1
PANES_ERR="server exited unexpectedly"
probe_err="$(ccy_tmux_network_rows 2>&1 >/dev/null)"
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
noserver_err="$(ccy_tmux_network_rows 2>&1 >/dev/null)"
noserver_rc=$?
check "no tmux server is not an error" "0" "$noserver_rc"
check "and it complains about nothing" "" "$noserver_err"

unset -f ps podman

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
