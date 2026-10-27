# shellcheck shell=bash
#
# _acceptance-sessions.inc.bash — the two throwaway sessions acceptance.bash launches, judges and
# removes: their launch, the waits for their container and seat, their end, the parking of
# their seats and the teardown. Sourced by acceptance.bash after its constants (SEAT_A, SEAT_B,
# TEAM, CCY_LAUNCHER, AGENT_BUS, LAST_LAUNCH, CHECKS and the waits), never executed: no shell
# options, no exit.

# Each session's launcher pid, the write end of its input fifo, and its container, by seat.
declare -A PID=() FD=() CTR=()
LAUNCH_CONF_SAVED=""

ccy_containers() { podman ps -a --filter label=ccy=true --format '{{.ID}}' | LC_ALL=C sort; }

alive() {
    if ps -p "${PID[$1]}" -o pid= >/dev/null; then
        return 0
    fi
    printf '[FAIL] the ccy session %s ended; its stderr (%s):\n' "$1" "${PLAN_RUN_DIR}/$1.err" >&2
    cat -- "${PLAN_RUN_DIR}/$1.err" >&2
    return 1
}

# new_container <seat> <before-file> — the one ccy container that was not there before the
# launch: 3 while there is none yet, 1 for more than one (another ccy started meanwhile).
new_container() {
    local ids count
    ids="$(ccy_containers | LC_ALL=C comm -13 "$2" -)" || return 1
    count="$(wc -w <<<"${ids}")"
    [[ "${count}" -ne 0 ]] || return 3
    if [[ "${count}" -ne 1 ]]; then
        printf '[FAIL] %d new ccy containers where one was expected (%s): another ccy session started meanwhile; run again\n' "${count}" "${ids//$'\n'/ }" >&2
        return 1
    fi
    CTR[$1]="${ids}"
}

# seat_ready <seat> — 0 once the session's entrypoint has claimed its seat (its pingbus answers),
# else 3; the last answer is kept in <seat>.ready.err for the timeout's report.
seat_ready() {
    "${CHECKS}" ready "${CTR[$1]}" "${TEAM}" 2>"${PLAN_RUN_DIR}/$1.ready.err" || return 3
}

# poll <seat> <timeout-s> <what> <cmd...> — cmd every POLL_S s until 0; 3 is "not yet". The
# timeout starts again while an image build runs (plan_image_build_grace).
poll() {
    local seat="$1" timeout="$2" what="$3" status deadline
    shift 3
    deadline=$((SECONDS + timeout))
    while true; do
        status=0
        "$@" || status=$?
        case "${status}" in
            0) return 0 ;;
            3) ;;
            *) return 1 ;;
        esac
        alive "${seat}" || return 1
        status=0
        plan_image_build_grace "${what}" "${timeout}" || status=$?
        case "${status}" in
            0) deadline=$((SECONDS + timeout)) ;;
            3) ;;
            *) return 1 ;;
        esac
        if ((SECONDS >= deadline)); then
            printf '[FAIL] %s: not within %d s (evidence in %s)\n' "${what}" "${timeout}" "${PLAN_RUN_DIR}" >&2
            return 1
        fi
        sleep "${POLL_S}"
    done
}

# start <seat> — ccy headless in the checkout, seated, reading stream-json from a fifo this
# script holds open; then its container, and its seat claimed.
start() {
    local seat="$1" before="${PLAN_RUN_DIR}/$1.containers-before" fifo="${PLAN_RUN_DIR}/$1.stdin" fd
    ccy_containers >"${before}" || return 1
    mkfifo -- "${fifo}" || return 1
    # Outside any tmux pane: a launch inside one would take that pane's session identity.
    (cd -- "${PLAN_REPO_ROOT}" && unset TMUX TMUX_PANE && exec "${CCY_LAUNCHER}" --headless --no-restore --no-supervise \
        --teams "${seat}@${TEAM}" --prompt "Plan 00232 acceptance: a throwaway session; no turn will be sent" \
        -- --input-format stream-json --output-format stream-json --verbose --model haiku) \
        <"${fifo}" >"${PLAN_RUN_DIR}/${seat}.out" 2>"${PLAN_RUN_DIR}/${seat}.err" &
    PID[${seat}]=$!
    exec {fd}<>"${fifo}" || return 1
    FD[${seat}]="${fd}"
    printf '==> session %s: ccy started (pid %d)\n' "${seat}" "${PID[${seat}]}"
    poll "${seat}" "${START_S}" "session ${seat}'s container" new_container "${seat}" "${before}" || return 1
    poll "${seat}" "${READY_S}" "session ${seat}'s seat claimed" seat_ready "${seat}" || return 1
    printf '==> session %s: container %s, seat %s@%s claimed\n' "${seat}" "${CTR[${seat}]}" "${seat}" "${TEAM}"
}

start_both() {
    cp -p -- "${LAST_LAUNCH}" "${PLAN_RUN_DIR}/last-launch.conf.before" || return 1
    LAUNCH_CONF_SAVED=1
    start "${SEAT_A}" && start "${SEAT_B}"
}

# end_session <seat> — close its input (claude ends, the container goes); after EXIT_S, remove
# the container and stop the launcher.
end_session() {
    local seat="$1" fd="${FD[$1]:-}" pid="${PID[$1]:-}" deadline status=0
    if [[ -n "${fd}" ]]; then
        exec {fd}>&-
        FD[${seat}]=""
    fi
    [[ -n "${pid}" ]] || return 0
    deadline=$((SECONDS + EXIT_S))
    while ps -p "${pid}" -o pid= >/dev/null && ((SECONDS < deadline)); do
        sleep 1
    done
    if ps -p "${pid}" -o pid= >/dev/null; then
        printf '==> session %s did not end within %d s of its input closing: removing it\n' "${seat}" "${EXIT_S}"
        if [[ -n "${CTR[${seat}]:-}" ]]; then
            podman rm -f -t 10 -- "${CTR[${seat}]}" >/dev/null || return 1
        fi
        kill -TERM "${pid}" || printf '==> session %s: ccy had already ended\n' "${seat}"
    fi
    wait "${pid}" || status=$?
    PID[${seat}]=""
    printf '==> session %s: ccy exited %d\n' "${seat}" "${status}"
}

# park_seats — the throwaway seats this checkout holds in the team, parked and deleted.
park_seats() {
    local list
    list="$(seat_cmd list | awk -F '\t' -v t="${TEAM}" -v a="${SEAT_A}" -v b="${SEAT_B}" \
        '$1 == "SEAT" && $2 == t && ($3 == a || $3 == b) { printf "%s%s@%s", sep, $3, $2; sep = "," }')" || return 1
    [[ -n "${list}" ]] || return 0
    seat_cmd remove "${list}" || return 1
    printf '==> parked %s\n' "${list}"
}

# teardown — both sessions ended, the seats parked, the Quick Launch record put back. Run on
# every way out (plan_on_cleanup), and each step tried whatever the one before did.
teardown() {
    local failed=0
    end_session "${SEAT_A}" || failed=1
    end_session "${SEAT_B}" || failed=1
    park_seats || failed=1
    if [[ -n "${LAUNCH_CONF_SAVED}" ]]; then
        cp -p -- "${PLAN_RUN_DIR}/last-launch.conf.before" "${LAST_LAUNCH}" || failed=1
        LAUNCH_CONF_SAVED=""
        printf '==> the checkout'\''s Quick Launch record restored\n'
    fi
    if [[ "${failed}" -ne 0 ]]; then
        printf '[FAIL] the teardown did not finish; run %s again, which parks leftover seats first\n' "${0##*/}" >&2
    fi
    return "${failed}"
}
