# shellcheck shell=bash
# _acceptance-m4.inc.bash — Plan 00203 milestone M4, the full non-interactive hot-control
# surface (SPEC-hot-control.md section 6, M4; ACCEPTANCE.md "M4"), sourced by the
# acceptance.bash dispatcher, which runs acceptance_m4 after acceptance_m3. Sourced, never
# executed: no shell options, no `exit`. A leg runs where errexit is off, so every command that
# can fail returns non-zero from its function explicitly. REPORT and check_leg are the
# dispatcher's; m2_note, m2_adopt, m2_container_up, M2_LAUNCHER, M2_CTR and M2's cleanup are
# M2's; M3_CHECK (Plan 00201's m7_check.py) and M3_TURN are M3's.
#
# WHAT IT CHANGES, and removes on the way out however the run ends:
#   - two throwaway teams, acc-m4a and acc-m4b (agent-bus-install, human `tester`), declared
#     only in a host_vars file in the run directory (CCY_TEAMS_HOST_VARS), purged at the end,
#     with their seat directories and the sessions' reports in this checkout;
#   - two throwaway headless ccy sessions in ccy's tmux, m4-a-<pid> on 1@acc-m4a and
#     m4-b-<pid> on 2@acc-m4a (`--no-restore`), removed by M2's cleanup (their names are added
#     to M2_NAMES) with their records and membership directories.
# Owner sessions are never touched.
#
# CHECKS (each judged from what ccy-teams printed and exited, the membership files, the
# records, agent-bus seat list and the team's roster; never from the model's prose)
#   (a) role: session A made acc-m4a's coordinator, the session told; the same again is a no-op
#   (b) the coordinator: remove without --coordinator is refused (64, nothing changed); move
#       A from acc-m4a to acc-m4b handing the role to B's seat; the same move again is a no-op
#   (c) two changes at once on one session: with A's keeper killed the first holds the lock
#       through its 10 s wait; the second is refused (75, naming the first); the first is
#       rolled back, and after a repair the add is made for real
#   (d) a homeserver down: A's status line says acc-m4b(down); add and role refused, nothing
#       changed (H26); remove releases the seat with the goodbye owed (exit 1); with the
#       homeserver back the same remove finishes
#   (e) B's seat keeper killed: list says unguarded; repair; list says ok; then remove B's
#       launch seat
#   (f) A's container ended while an add waits for its keeper (H28): the add is rolled back,
#       the record restored and no desired membership left behind
# Not run here, the owner's (ACCEPTANCE.md M4): the same 1@<team> in two checkouts, and the
# interactive questions (no --yes, no --coordinator) on a terminal.

readonly M4_TEAM_A="acc-m4a"
readonly M4_TEAM_B="acc-m4b"
readonly M4_HUMAN="tester"
readonly M4_A="m4-a-$$"
readonly M4_B="m4-b-$$"
readonly M4_EV="${PLAN_RUN_DIR}/m4"
readonly M4_HOST_VARS="${M4_EV}/host_vars.yml"
readonly M4_INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly M4_AGENT_BUS="/usr/local/bin/agent-bus"
readonly M4_SEATS="${PLAN_REPO_ROOT}/.claude/ccy/pingbus/seats"
readonly M4_REPORTS="${PLAN_REPO_ROOT}/.claude/ccy/pingbus/sessions"
readonly M4_RECORDS="${XDG_STATE_HOME:-${HOME}/.local/state}/ccy/sessions"
#: Bounds, in seconds.
readonly M4_TURN_S=240
readonly M4_UP_S=120
M4_TEAMS_PRESENT=0

m4_ev() { printf '%s/%s' "${M4_EV}" "$1"; }

m4_teams() { (cd -- "${PLAN_REPO_ROOT}" && CCY_TEAMS_HOST_VARS="${M4_HOST_VARS}" ccy-teams "$@"); }

# m4_poll <seconds> <what> <command...> — until the command succeeds; it returns 3 for "not yet".
m4_poll() {
    local bound="$1" what="$2" status deadline
    shift 2
    deadline=$((SECONDS + bound))
    while ((SECONDS < deadline)); do
        status=0
        "$@" || status=$?
        [[ "${status}" -eq 0 ]] && return 0
        [[ "${status}" -eq 3 ]] || return 1
        sleep 2
    done
    printf '[FAIL] %s: not within %d s\n' "${what}" "${bound}" >&2
    return 1
}

m4_team_built() {
    local enabled
    enabled="$(systemctl show -P UnitFileState "agent-bus-hs@$1.service")" || return 1
    [[ "${enabled}" == enabled ]] || sudo -n test -e "/var/lib/agent-bus/$1"
}

# m4_run <label> <exit> <session args...> — ccy-teams session <args>, with no terminal; its
# stderr saved as <label>.err and shown; fails unless it exited <exit>.
m4_run() {
    local label="$1" want="$2" status=0
    shift 2
    m4_teams session "$@" </dev/null 2>"$(m4_ev "${label}.err")" || status=$?
    cat -- "$(m4_ev "${label}.err")"
    if [[ "${status}" -ne "${want}" ]]; then
        printf '[FAIL] ccy-teams session %s exited %d, not %d\n' "$*" "${status}" "${want}" >&2
        return 1
    fi
}

# m4_said <label> <pattern> [no] — the run's stderr has (or, with "no", has not) a line
# matching the extended pattern.
m4_said() {
    if grep -qE -- "$2" "$(m4_ev "$1.err")"; then
        [[ "${3:-}" != no ]] && return 0
        printf '[FAIL] %s said what it must not: %s\n' "$1" "$2" >&2
        return 1
    fi
    [[ "${3:-}" == no ]] && return 0
    printf '[FAIL] %s did not say: %s\n' "$1" "$2" >&2
    return 1
}

# m4_desired <session> — the teams of its desired membership, in order, comma-separated; "gone"
# when it has none.
m4_desired() {
    local file="${XDG_RUNTIME_DIR:?}/ccy/membership/$1/membership.json"
    [[ -e "${file}" ]] || { printf 'gone\n'; return 0; }
    python3 -I -c 'import json, sys; d = json.load(open(sys.argv[1])); print(",".join(s["team"] for s in d["seats"]))' "${file}"
}

# m4_desired_is <session> <teams> — the desired membership names exactly these teams.
m4_desired_is() {
    local got
    got="$(m4_desired "$1")" || return 1
    [[ "${got}" == "$2" ]] && return 0
    printf '[FAIL] %s desires %s, not %s\n' "$1" "${got}" "$2" >&2
    return 1
}

# m4_seat_field <team> <seat> <field> — a field of the seat's SEAT line in this checkout
# (4: free or held; 6: its handle).
m4_seat_field() {
    (cd -- "${PLAN_REPO_ROOT}" && "${M4_AGENT_BUS}" seat list) >"$(m4_ev seat-list)" || return 1
    awk -F '\t' -v team="$1" -v seat="$2" -v field="$3" '$1 == "SEAT" && $2 == team && $3 == seat { print $field }' "$(m4_ev seat-list)"
}

m4_seat_free() {
    local state
    state="$(m4_seat_field "$1" "$2" 4)" || return 1
    [[ "${state}" == free ]] && return 0
    printf '[FAIL] seat %s@%s is %s, not free\n' "$2" "$1" "${state:-missing}" >&2
    return 1
}

# m4_role_is <team> <handle> <role> — the member's role in the team's roster.
m4_role_is() {
    local got roster
    roster="$(sudo -n "${M4_AGENT_BUS}" list "$1")" || return 1
    printf '%s\n' "${roster}" >"$(m4_ev "roster-$1")" || return 1
    got="$(awk -F '\t' -v handle="$2" '$1 == "MEMBER" && $2 == handle { print $3 }' <<<"${roster}")"
    [[ "${got}" == "$3" ]] && return 0
    printf '[FAIL] %s is a %s in %s, not a %s\n' "$2" "${got:-non-member}" "$1" "$3" >&2
    return 1
}

# m4_keeper_pid <session> — the container's pid of its seat keeper, "" when none runs.
m4_keeper_pid() {
    podman top "${M2_CTR[$1]}" pid args >"$(m4_ev "top-$1")" || return 1
    awk '/pingbus/ && / seat keep/ { print $1; exit }' "$(m4_ev "top-$1")"
}

m4_keeper_gone() {
    local pid
    pid="$(m4_keeper_pid "$1")" || return 1
    [[ -z "${pid}" ]] || return 3
}

m4_kill_keeper() {
    local pid
    pid="$(m4_keeper_pid "$1")" || return 1
    [[ -n "${pid}" ]] || { printf '[FAIL] %s runs no seat keeper to kill\n' "$1" >&2; return 1; }
    podman exec "${M2_CTR[$1]}" kill "${pid}" || return 1
    m4_poll 20 "the seat keeper of $1 gone" m4_keeper_gone "$1"
}

# ── setup ─────────────────────────────────────────────────────────────────────────────────

m4_prepare() {
    local inventory address url org ports team i=0
    mkdir -p -- "${M4_EV}" || return 1
    for team in "${M4_TEAM_A}" "${M4_TEAM_B}"; do
        if m4_team_built "${team}" || [[ -e "${M4_SEATS}/${team}" ]]; then
            printf '[FAIL] %s exists already (an interrupted run?): sudo %s remove --team %s --purge; rm -rf %s\n' \
                "${team}" "${M4_INSTALLER}" "${team}" "${M4_SEATS}/${team}" >&2
            return 1
        fi
    done
    inventory="$(cd -- "${PLAN_REPO_ROOT}" && ansible-inventory --host localhost </dev/null)" || return 1
    address="$(python3 -I -c 'import json, sys; v = json.load(sys.stdin).get("agent_bus_address", ""); print(v if isinstance(v, str) else "")' <<<"${inventory}")" || return 1
    [[ -n "${address}" ]] || { printf '[FAIL] no agent_bus_address in the host_vars\n' >&2; return 1; }
    url="$(git -C "${PLAN_REPO_ROOT}" remote get-url origin)" || return 1
    org="$("${M3_CHECK[@]}" github-owner "${url}")" || return 1
    mapfile -t ports < <("${M3_CHECK[@]}" bus-ports 2 "${PLAN_REPO_ROOT}/environment/localhost/host_vars/localhost.yml")
    [[ "${#ports[@]}" -eq 2 ]] || { printf '[FAIL] no two free bus ports\n' >&2; return 1; }
    for team in "${M4_TEAM_A}" "${M4_TEAM_B}"; do
        "${M3_CHECK[@]}" team-file "${team}" "${ports[i]}" "${address}" "${M4_HUMAN}" "${org}" \
            "Plan 00203 M4 acceptance team ${team}" >"$(m4_ev "${team}.team.json")" || return 1
        i=$((i + 1))
    done
    "${M3_CHECK[@]}" host-vars "$(m4_ev "${M4_TEAM_A}.team.json")" "$(m4_ev "${M4_TEAM_B}.team.json")" >"${M4_HOST_VARS}" || return 1
    M4_TEAMS_PRESENT=1
    for team in "${M4_TEAM_A}" "${M4_TEAM_B}"; do
        sudo -n "${M4_INSTALLER}" team --team-file "$(m4_ev "${team}.team.json")" || return 1
    done
}

# m4_launch <name> <seat> — a headless session on <seat>@acc-m4a in ccy's tmux, and its first
# turn. ccy runs as the pane shell's child, never exec'd, and the trailing status write keeps
# it so (a lone command would be exec'd, and tmux would hang the pane up: _acceptance-m2).
m4_launch() {
    local name="$1" seat="$2" fifo fd command
    fifo="$(m4_ev "${name}.stdin")"
    mkfifo -- "${fifo}" || return 1
    exec {fd}<>"${fifo}" || return 1
    m2_adopt "${name}" "${fd}" "$(m4_ev "${name}")"
    command="$(printf '%q ' env "CCY_TEAMS_HOST_VARS=${M4_HOST_VARS}" "${M2_LAUNCHER}" --headless --no-restore \
        --no-supervise --teams "${seat}@${M4_TEAM_A}" --prompt "Plan 00203 M4 acceptance" -- --input-format stream-json \
        --output-format stream-json --verbose --model haiku)" || return 1
    command+="<$(printf '%q' "${fifo}") >$(printf '%q' "$(m4_ev "${name}.out")") 2>$(printf '%q' "$(m4_ev "${name}.err")")"
    command+="; echo \"\$?\" >$(printf '%q' "$(m4_ev "${name}.status")")"
    tmux -L ccy new-session -d -s "${name}" -c "${PLAN_REPO_ROOT}" "${command}" || return 1
    m2_container_up "${name}" || return 1
    "${M3_CHECK[@]}" user-line "${M3_TURN}" >&"${fd}" || return 1
    m4_poll "${M4_TURN_S}" "the first turn of ${name}" m4_turned "${name}" || return 1
}

m4_turned() {
    local n
    n="$("${M3_CHECK[@]}" turns "$(m4_ev "$1.out")")" || return 1
    ((n >= 1)) || return 3
}

m4_start() {
    m4_launch "${M4_A}" 1 || return 1
    m4_launch "${M4_B}" 2 || return 1
}

# ── the checks ────────────────────────────────────────────────────────────────────────────

m4_check_role() {
    local handle
    handle="$(m4_seat_field "${M4_TEAM_A}" 1 6)" || return 1
    [[ -n "${handle}" ]] || { printf '[FAIL] no seat 1 in %s\n' "${M4_TEAM_A}" >&2; return 1; }
    m4_run role 0 role "${M4_A}" "${M4_TEAM_A}" coordinator --yes || return 1
    m4_said role '^✓ role: [^ ]+ set to coordinator$' || return 1
    m4_said role "^✓ session: told it is ${M4_TEAM_A}'s coordinator" || return 1
    m4_role_is "${M4_TEAM_A}" "${handle}" coordinator || return 1
    m4_run role-again 0 role "${M4_A}" "${M4_TEAM_A}" coordinator --yes || return 1
    m4_said role-again '^✓ (role|record|membership)' no || return 1
}

m4_check_coordinator_move() {
    local heir
    heir="$(m4_seat_field "${M4_TEAM_A}" 2 6)" || return 1
    m4_run remove-refused 64 remove "${M4_A}" "${M4_TEAM_A}" --yes || return 1
    m4_desired_is "${M4_A}" "${M4_TEAM_A}" || return 1
    m4_run move 0 move "${M4_A}" "${M4_TEAM_A}" "${M4_TEAM_B}" --coordinator=hand-to=2 --yes || return 1
    m4_said move '^✓ goodbye: ' || return 1
    m4_desired_is "${M4_A}" "${M4_TEAM_B}" || return 1
    m4_role_is "${M4_TEAM_A}" "${heir}" coordinator || return 1
    m4_seat_free "${M4_TEAM_A}" 1 || return 1
    m4_run move-again 0 move "${M4_A}" "${M4_TEAM_A}" "${M4_TEAM_B}" --coordinator=hand-to=2 --yes || return 1
    m4_said move-again '^✓ (role|record|membership)' no || return 1
}

# m4_wait_line <file> <pattern> <pid> — until the background command <pid> has written a line
# matching pattern into file; it ending first, or 30 s passing, is a failure.
m4_wait_line() {
    local file="$1" pattern="$2" pid="$3" deadline=$((SECONDS + 30))
    until grep -qE -- "${pattern}" "${file}"; do
        if ! kill -0 "${pid}"; then
            printf '[FAIL] the command ended before it wrote %s\n' "${pattern}" >&2
            return 1
        fi
        if ((SECONDS >= deadline)); then
            printf '[FAIL] the command did not write %s within 30 s\n' "${pattern}" >&2
            return 1
        fi
        sleep 1
    done
}

# (c): the first add is held in a known window: with A's seat keeper killed it commits, then
# waits its 10 s for the seat to be held, holding the change lock, and is rolled back. The
# second add is run inside that window. Then the keeper is repaired and the add made for real.
m4_check_two_at_once() {
    local first status=0
    m4_kill_keeper "${M4_A}" || return 1
    m4_teams session add "${M4_A}" "${M4_TEAM_A}" --yes </dev/null 2>"$(m4_ev concurrent-1.err)" &
    first=$!
    if ! m4_wait_line "$(m4_ev concurrent-1.err)" '^✓ membership: generation' "${first}" \
        || ! m4_run concurrent-2 75 add "${M4_A}" "${M4_TEAM_A}" --yes \
        || ! m4_said concurrent-2 'another change to it is in progress: ccy-teams session add'; then
        wait "${first}"
        return 1
    fi
    wait "${first}" || status=$?
    cat -- "$(m4_ev concurrent-1.err)"
    [[ "${status}" -eq 1 ]] || { printf '[FAIL] the held first add exited %d, not 1 (rolled back)\n' "${status}" >&2; return 1; }
    m4_desired_is "${M4_A}" "${M4_TEAM_B}" || return 1
    m4_run concurrent-repair 0 repair "${M4_A}" --yes || return 1
    m4_run concurrent-again 0 add "${M4_A}" "${M4_TEAM_A}" --yes || return 1
    m4_desired_is "${M4_A}" "${M4_TEAM_B},${M4_TEAM_A}" || return 1
}

m4_hs_up() { systemctl is-active --quiet "agent-bus-hs@${M4_TEAM_B}.service" || return 3; }

# m4_status_down — `pingbus status-line` run under podman exec in A's container, with the
# session home given by hand, marks acc-m4b down (H26). It proves the status line's code path
# over the watcher's real mark; it is not claude's own rendering of its status line.
m4_status_down() {
    local line
    line="$(podman exec -i -e PINGBUS_HOME=/tmp/pingbus-home -e PINGBUS_STATUS_LINE_INNER=1 "${M2_CTR[${M4_A}]}" \
        pingbus status-line <<<'{}')" || return 1
    printf '%s\n' "${line}" >"$(m4_ev status-line)"
    [[ "${line}" == *"${M4_TEAM_B}(down)"* ]] || return 3
}

m4_check_homeserver_down() {
    local seat
    seat="$(python3 -I -c 'import json, sys; d = json.load(open(sys.argv[1])); print([s["seat"] for s in d["seats"] if s["team"] == sys.argv[2]][0])' \
        "${XDG_RUNTIME_DIR:?}/ccy/membership/${M4_A}/membership.json" "${M4_TEAM_B}")" || return 1
    sudo -n systemctl stop "agent-bus-hs@${M4_TEAM_B}.service" || return 1
    m4_poll 60 "${M4_A}'s status line marking ${M4_TEAM_B} down" m4_status_down || return 1
    m4_run down-add 1 add "${M4_B}" "${M4_TEAM_B}" --yes || return 1
    m4_said down-add "homeserver for ${M4_TEAM_B} is down" || return 1
    m4_desired_is "${M4_B}" "${M4_TEAM_A}" || return 1
    m4_run down-role 1 role "${M4_A}" "${M4_TEAM_B}" coordinator --yes || return 1
    m4_said down-role '^✓ role' no || return 1
    m4_run down-remove 1 remove "${M4_A}" "${M4_TEAM_B}" --yes || return 1
    m4_said down-remove 'goodbye and status not sent: homeserver down' || return 1
    m4_desired_is "${M4_A}" "${M4_TEAM_A}" || return 1
    m4_seat_free "${M4_TEAM_B}" "${seat}" || return 1
    sudo -n systemctl start "agent-bus-hs@${M4_TEAM_B}.service" || return 1
    m4_poll "${M4_UP_S}" "the homeserver of ${M4_TEAM_B} back" m4_hs_up || return 1
    m4_run owed-remove 0 remove "${M4_A}" "${M4_TEAM_B}" --yes || return 1
    m4_said owed-remove '^✓ goodbye: ' || return 1
    m4_said owed-remove 'written \(farewell done\)$' || return 1
}

# m4_list_state <session> <state> — session list's STATE for the session begins with state.
m4_list_state() {
    local line
    m4_teams session list </dev/null >"$(m4_ev list.out)" 2>"$(m4_ev list.err)" || return 1
    line="$(awk -F '\t' -v name="$1" '$1 == "SESSION" && $2 == name { print $6 }' "$(m4_ev list.out)")"
    [[ "${line}" == "$2"* ]] || return 3
}

m4_check_repair() {
    m4_kill_keeper "${M4_B}" || return 1
    m4_poll 20 "session list saying ${M4_B} is unguarded" m4_list_state "${M4_B}" unguarded || return 1
    m4_run repair 0 repair "${M4_B}" --yes || return 1
    m4_said repair '^✓ keeper: running again, 1 seat held' || return 1
    m4_poll 20 "session list saying ${M4_B} is ok" m4_list_state "${M4_B}" ok || return 1
    m4_run remove-launch-seat 0 remove "${M4_B}" "${M4_TEAM_A}" --coordinator=demote --yes || return 1
    m4_desired_is "${M4_B}" "" || return 1
    m4_seat_free "${M4_TEAM_A}" 2 || return 1
}

m4_check_session_ends() {
    local add status=0
    m4_kill_keeper "${M4_A}" || return 1
    m4_teams session add "${M4_A}" "${M4_TEAM_B}" --yes </dev/null 2>"$(m4_ev ended.err)" &
    add=$!
    if ! m4_wait_line "$(m4_ev ended.err)" '^✓ membership: generation' "${add}" || ! podman kill "${M2_CTR[${M4_A}]}" >/dev/null; then
        wait "${add}"
        return 1
    fi
    wait "${add}" || status=$?
    cat -- "$(m4_ev ended.err)"
    [[ "${status}" -eq 1 ]] || { printf '[FAIL] the add exited %d, not 1\n' "${status}" >&2; return 1; }
    m4_said ended 'session ended during the add' || return 1
    m4_said ended '^✓ membership: removed' || return 1
    m4_desired_is "${M4_A}" gone || return 1
    if [[ ! -f "${M4_RECORDS}/${M4_A}" ]]; then
        printf '[FAIL] the record of %s is gone: the add did not restore it\n' "${M4_A}" >&2
        return 1
    fi
    if grep -qF -- "${M4_TEAM_B}" "${M4_RECORDS}/${M4_A}"; then
        printf '[FAIL] the record of %s still names %s\n' "${M4_A}" "${M4_TEAM_B}" >&2
        return 1
    fi
}

# ── teardown ──────────────────────────────────────────────────────────────────────────────

m4_cleanup() {
    local status=0 team
    m2_cleanup || status=1
    if [[ "${M4_TEAMS_PRESENT}" -eq 1 ]]; then
        for team in "${M4_TEAM_A}" "${M4_TEAM_B}"; do
            if m4_team_built "${team}"; then
                sudo -n "${M4_INSTALLER}" remove --team "${team}" --purge || status=1
            fi
            sudo -n systemctl reset-failed "agent-bus-*@${team}.*" || status=1
        done
        M4_TEAMS_PRESENT=0
    fi
    rm -rf -- "${M4_SEATS:?}/${M4_TEAM_A}" "${M4_SEATS:?}/${M4_TEAM_B}" "${M4_REPORTS:?}/${M4_A}" "${M4_REPORTS:?}/${M4_B}" || status=1
    [[ "${status}" -eq 0 ]] || printf '[FAIL] the M4 cleanup could not remove everything (see above)\n' >&2
    return "${status}"
}

acceptance_m4() {
    plan_on_cleanup m4_cleanup
    printf '## M4: role, move, locks and edge cases\n\n' >>"${REPORT}"
    plan_deploy_leg "two throwaway teams, ${M4_TEAM_A} and ${M4_TEAM_B}" m4_prepare
    plan_deploy_leg "two throwaway headless sessions on ${M4_TEAM_A} in ccy's tmux" m4_start
    check_leg "(a) role: coordinator set and told; the same again changes nothing" m4_check_role
    check_leg "(b) the coordinator: remove refused without --coordinator; move handing the role on; again a no-op" m4_check_coordinator_move
    check_leg "(c) two changes at once on one session: the second refused (75)" m4_check_two_at_once
    check_leg "(d) a homeserver down: add and role refused, remove owes its goodbye, the re-run finishes" m4_check_homeserver_down
    check_leg "(e) a killed seat keeper: unguarded, repaired, then a launch seat removed" m4_check_repair
    check_leg "(f) the session ends while an add waits: rolled back, no membership left" m4_check_session_ends
    plan_deploy_leg "remove the throwaway sessions and teams" m4_cleanup
    m2_note "M4: (a)-(f) passed; the same seat in two checkouts and the interactive questions are the owner's steps in ACCEPTANCE.md"
}
