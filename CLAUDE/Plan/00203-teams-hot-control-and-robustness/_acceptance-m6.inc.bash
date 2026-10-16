# shellcheck shell=bash
# _acceptance-m6.inc.bash — Plan 00203 milestone M6, cc sessions hot-controlled (SPEC-hot-control.md
# H31, H32, section 6 M6; ACCEPTANCE.md "M6"), sourced by the acceptance.bash dispatcher, which
# runs acceptance_m6 after acceptance_m4. Sourced, never executed: no shell options, no `exit`. A
# leg runs where errexit is off, so every command that can fail returns non-zero from its
# function explicitly. REPORT and check_leg are the dispatcher's; m2_note is M2's; M3_CHECK
# (Plan 00201's m7_check.py) is M3's; m4_poll and m4_team_built are M4's.
#
# WHAT IT CHANGES, and removes on the way out however the run ends (owner sessions untouched):
#   - two throwaway teams, acc-m6a and acc-m6b (agent-bus-install, human `tester`), declared only
#     in a host_vars file in the run directory (CCY_TEAMS_HOST_VARS), purged at the end, with
#     their seat directories and the sessions' reports in this checkout;
#   - three throwaway cc sessions in ccy's tmux, cc-acc-m6a-<pid> (A), cc-acc-m6b-<pid> (B) and
#     cc-acc-m6c-<pid> (C), each with a registry record (restore=no) written as cc's own tmux
#     start writes one, removed with their records and membership directories;
#   - the detached `sleep` that A's stand-in claude starts for (b), killed at the end.
#
# HOW A SESSION RUNS: as cc runs it from `--teams` or `--bus` on, through the deployed
# /var/local/claude-code/cc-teams.bash: the pane sources it and runs cc_teams_ccy_dir,
# cc_teams_settings and cc_teams_command (which writes the membership with `ccy-teams internal
# membership-init --kind=cc` under the pane's own session name), then runs the command, so
# `pingbus seat exec` and its seat keeper are the production ones. Two steps are not cc's: the
# seats are taken here with `agent-bus seat take --type=host`, as cc_teams_take does it, because
# sudo is primed on this terminal and not in the pane; and the command's `claude` is a stand-in
# first on PATH (a real one needs a token and a person), which records its pid and sleeps. cc's
# chooser and token prompt are scripts/test-cc-teams.bash's and the owner's step.
#
# CHECKS (each judged from what ccy-teams printed and exited, the membership files, the records,
# agent-bus seat list and the processes; never from a model)
#   (a) move: `ccy-teams session move A acc-m6a acc-m6b --yes` exits 0, the route probed from
#       the host; A desires only acc-m6b, its record names it, seat 1@acc-m6a is free; session
#       list shows A as kind cc, ok
#   (b) audit F5: A's stand-in claude started `setsid sleep 600`; with A's claude ended, its seat
#       is free within 5 s while that sleep still runs, and B, started at once in the same seat,
#       holds it
#   (c) cc --bus: C starts on no team; its keeper reports inbound; `session add C acc-m6a --yes`
#       is hot ("Nothing restarted", no relaunch request); with C's keeper killed, the list says
#       "unguarded: end it ..." and `session repair C --yes` exits 78; `session remove C acc-m6a
#       --yes` leaves its record on --bus

readonly M6_TEAM_A="acc-m6a"
readonly M6_TEAM_B="acc-m6b"
readonly M6_HUMAN="tester"
readonly M6_A="cc-acc-m6a-$$"
readonly M6_B="cc-acc-m6b-$$"
readonly M6_C="cc-acc-m6c-$$"
readonly M6_EV="${PLAN_RUN_DIR}/m6"
readonly M6_HOST_VARS="${M6_EV}/host_vars.yml"
readonly M6_STANDIN_DIR="${M6_EV}/bin"
readonly M6_DRIVER="${M6_EV}/cc-session.bash"
readonly M6_INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly M6_AGENT_BUS="/usr/local/bin/agent-bus"
readonly M6_CCY_LIB="/var/local/claude-yolo/lib"
readonly M6_CC_LAUNCHER="/var/local/claude-code/cc"
readonly M6_SEATS="${PLAN_REPO_ROOT}/.claude/ccy/pingbus/seats"
readonly M6_REPORTS="${PLAN_REPO_ROOT}/.claude/ccy/pingbus/sessions"
readonly M6_RECORDS="${XDG_STATE_HOME:-${HOME}/.local/state}/ccy/sessions"
readonly M6_ACCEPTED="${HOME}/.config/pingbus/element-accepted"
#: Bounds, in seconds.
readonly M6_UP_S=60
M6_TEAMS_PRESENT=0
M6_NAMES=()

m6_ev() { printf '%s/%s' "${M6_EV}" "$1"; }

m6_teams() { (cd -- "${PLAN_REPO_ROOT}" && CCY_TEAMS_HOST_VARS="${M6_HOST_VARS}" ccy-teams "$@"); }

m6_membership() { printf '%s/pingbus/membership/%s' "${XDG_RUNTIME_DIR:?}" "$1"; }

# m6_run <label> <exit> <session args...> — ccy-teams session <args>, with no terminal; its
# stderr saved as <label>.err and shown; fails unless it exited <exit>.
m6_run() {
    local label="$1" want="$2" status=0
    shift 2
    m6_teams session "$@" </dev/null 2>"$(m6_ev "${label}.err")" || status=$?
    cat -- "$(m6_ev "${label}.err")"
    if [[ "${status}" -ne "${want}" ]]; then
        printf '[FAIL] ccy-teams session %s exited %d, not %d\n' "$*" "${status}" "${want}" >&2
        return 1
    fi
}

# m6_said <label> <pattern> [no] — the run's stderr has (or, with "no", has not) a matching line.
m6_said() {
    if grep -qE -- "$2" "$(m6_ev "$1.err")"; then
        [[ "${3:-}" != no ]] && return 0
        printf '[FAIL] %s said what it must not: %s\n' "$1" "$2" >&2
        return 1
    fi
    [[ "${3:-}" == no ]] && return 0
    printf '[FAIL] %s did not say: %s\n' "$1" "$2" >&2
    return 1
}

# m6_desired <session> — the seats of its desired membership, seat@team comma-separated.
m6_desired() {
    python3 -I -c 'import json, sys; d = json.load(open(sys.argv[1])); print(",".join(s["seat"] + "@" + s["team"] for s in d["seats"]))' \
        "$(m6_membership "$1")/membership.json"
}

m6_desired_is() {
    local got
    got="$(m6_desired "$1")" || return 1
    [[ "${got}" == "$2" ]] && return 0
    printf '[FAIL] %s desires %s, not %s\n' "$1" "${got:-nothing}" "$2" >&2
    return 1
}

# m6_seat_field <team> <seat> <field> — a field of the seat's SEAT line (4: free or held).
m6_seat_field() {
    (cd -- "${PLAN_REPO_ROOT}" && "${M6_AGENT_BUS}" seat list) >"$(m6_ev seat-list)" || return 1
    awk -F '\t' -v team="$1" -v seat="$2" -v field="$3" '$1 == "SEAT" && $2 == team && $3 == seat { print $field }' "$(m6_ev seat-list)"
}

# m6_seat_is <team> <seat> <state> — 3 (not yet) until the seat reads the state.
m6_seat_is() {
    local state
    state="$(m6_seat_field "$1" "$2" 4)" || return 1
    [[ "${state}" == "$3" ]] || return 3
}

# m6_list_state <session> <state> — session list's line for the session is kind cc and its STATE
# begins with state.
m6_list_state() {
    local kind state
    m6_teams session list </dev/null >"$(m6_ev list.out)" 2>"$(m6_ev list.err)" || return 1
    kind="$(awk -F '\t' -v name="$1" '$1 == "SESSION" && $2 == name { print $3 }' "$(m6_ev list.out)")"
    state="$(awk -F '\t' -v name="$1" '$1 == "SESSION" && $2 == name { print $6 }' "$(m6_ev list.out)")"
    [[ "${kind}" == cc && "${state}" == "$2"* ]] || return 3
}

# m6_pid <file> — the pid a stand-in wrote, alive.
m6_pid() {
    local pid
    pid="$(cat -- "$1")" || return 1
    [[ "${pid}" =~ ^[0-9]+$ ]] || { printf '[FAIL] %s holds no pid\n' "$1" >&2; return 1; }
    kill -0 "${pid}" || { printf '[FAIL] process %s (from %s) is not running\n' "${pid}" "$1" >&2; return 1; }
    printf '%s\n' "${pid}"
}

# m6_keeper_of <session> — the pid of the session's seat keeper, the child of its claude; "" none.
# ps exits 1 when the claude has no child at all, which is an answer, not a failure.
m6_keeper_of() {
    local claude status=0
    claude="$(m6_pid "$(m6_ev "$1.claude.pid")")" || return 1
    ps -o pid=,args= --ppid "${claude}" >"$(m6_ev "children-$1")" || status=$?
    [[ "${status}" -le 1 ]] || return 1
    awk '/pingbus/ && / seat keep/ { print $1; exit }' "$(m6_ev "children-$1")"
}

# ── setup ─────────────────────────────────────────────────────────────────────────────────

m6_write_helpers() {
    mkdir -p -- "${M6_STANDIN_DIR}" || return 1
    # The stand-in for claude: its pid where M6_STANDIN_PID says, and, with M6_DETACH, a sleep
    # started in a session of its own (its pid in M6_DETACH), as a Bash tool's `setsid` would.
    cat >"${M6_STANDIN_DIR}/claude" <<'STANDIN' || return 1
#!/bin/bash
printf '%s\n' "$$" >"${M6_STANDIN_PID:?}"
if [[ -n "${M6_DETACH:-}" ]]; then
    setsid sh -c 'printf "%s\n" "$$" >"$1"; exec sleep 600' sh "${M6_DETACH}" </dev/null >>"${M6_DETACH}.log" 2>&1 &
fi
exec sleep 100000
STANDIN
    # The pane's program: cc's own library from its seats on, the stand-in first on PATH.
    cat >"${M6_DRIVER}" <<'DRIVER' || return 1
#!/bin/bash
set -eo pipefail
source /var/local/claude-yolo/lib/common-pure.bash
source /var/local/claude-yolo/lib/tmux-session.bash
source /var/local/claude-code/cc-teams.bash
checkout="$(git rev-parse --show-toplevel)"
tmp="$(mktemp -d "${XDG_RUNTIME_DIR:?}/cc-teams.XXXXXX")"
CC_TEAMS_SEATS="$1"
cc_teams_ccy_dir "${checkout}"
cc_teams_settings "${tmp}/settings.json"
cc_teams_command "${tmp}" "${checkout}"
status=0
PATH="${M6_STANDIN_DIR:?}:${CC_TEAMS_KIT}:${PATH}" "${CC_TEAMS_CMD[@]}" || status=$?
rm -rf -- "${tmp}"
exit "${status}"
DRIVER
    chmod 0700 "${M6_STANDIN_DIR}/claude" "${M6_DRIVER}" || return 1
}

m6_prepare() {
    local inventory address url org ports team i=0
    mkdir -p -- "${M6_EV}" || return 1
    if [[ ! -f "${M6_ACCEPTED}" ]]; then
        printf '[FAIL] no Element consent in %s: pingbus runs no cc seat without it. Run cc --teams once from a terminal and accept, then run this again.\n' \
            "${M6_ACCEPTED}" >&2
        return 1
    fi
    for team in "${M6_TEAM_A}" "${M6_TEAM_B}"; do
        if m4_team_built "${team}" || [[ -e "${M6_SEATS}/${team}" ]]; then
            printf '[FAIL] %s exists already (an interrupted run?): sudo %s remove --team %s --purge; rm -rf %s\n' \
                "${team}" "${M6_INSTALLER}" "${team}" "${M6_SEATS}/${team}" >&2
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
    for team in "${M6_TEAM_A}" "${M6_TEAM_B}"; do
        "${M3_CHECK[@]}" team-file "${team}" "${ports[i]}" "${address}" "${M6_HUMAN}" "${org}" \
            "Plan 00203 M6 acceptance team ${team}" >"$(m6_ev "${team}.team.json")" || return 1
        i=$((i + 1))
    done
    "${M3_CHECK[@]}" host-vars "$(m6_ev "${M6_TEAM_A}.team.json")" "$(m6_ev "${M6_TEAM_B}.team.json")" >"${M6_HOST_VARS}" || return 1
    M6_TEAMS_PRESENT=1
    for team in "${M6_TEAM_A}" "${M6_TEAM_B}"; do
        sudo -n "${M6_INSTALLER}" team --team-file "$(m6_ev "${team}.team.json")" || return 1
    done
    m6_write_helpers
}

# m6_record <session> <args...> — the session's registry record, as cc's tmux start writes one,
# kept out of a reboot's restore.
m6_record() {
    (
        # shellcheck source-path=SCRIPTDIR source=../../../files/var/local/claude-yolo/lib/common-pure.bash
        source "${M6_CCY_LIB}/common-pure.bash" || exit 1
        # shellcheck source-path=SCRIPTDIR source=../../../files/var/local/claude-yolo/lib/session-registry.bash
        source "${M6_CCY_LIB}/session-registry.bash" || exit 1
        ccy_registry_write "$1" "${PLAN_REPO_ROOT}" "${M6_CC_LAUNCHER}" cc no "${@:2}"
    )
}

# m6_up <session> — 3 until the session's keeper has written its report; its pane ending first is
# a failure, with what it said.
m6_up() {
    if [[ -e "$(m6_ev "$1.status")" ]]; then
        printf '[FAIL] the cc session %s ended (exit %s): %s\n' "$1" "$(cat -- "$(m6_ev "$1.status")")" "$(cat -- "$(m6_ev "$1.err")")" >&2
        return 1
    fi
    [[ -e "${M6_REPORTS}/$1/applied.json" && -e "$(m6_ev "$1.claude.pid")" ]] || return 3
}

# m6_launch <session> <seats> [detach] — a cc session in ccy's tmux on the seats ("" for --bus),
# which its pane takes through cc's library; with detach, its claude starts a detached sleep.
# The pane's shell runs it as a child, never exec'd, so tmux keeps the pane (_acceptance-m2).
m6_launch() {
    local name="$1" seats="$2" command
    local -a record=(--bus) env=("M6_STANDIN_DIR=${M6_STANDIN_DIR}" "M6_STANDIN_PID=$(m6_ev "${name}.claude.pid")")
    [[ -z "${seats}" ]] || record=(--teams "${seats}")
    [[ -z "${3:-}" ]] || env+=("M6_DETACH=$(m6_ev "${name}.detached.pid")")
    M6_NAMES+=("${name}")
    m6_record "${name}" "${record[@]}" || return 1
    command="$(printf '%q ' env "${env[@]}" bash "${M6_DRIVER}" "${seats}")" || return 1
    command+=">$(printf '%q' "$(m6_ev "${name}.out")") 2>$(printf '%q' "$(m6_ev "${name}.err")")"
    command+="; echo \"\$?\" >$(printf '%q' "$(m6_ev "${name}.status")")"
    tmux -L ccy new-session -d -s "${name}" -c "${PLAN_REPO_ROOT}" "${command}" || return 1
    m4_poll "${M6_UP_S}" "the seat keeper of ${name}" m6_up "${name}"
}

m6_start() {
    (cd -- "${PLAN_REPO_ROOT}" && "${M6_AGENT_BUS}" seat take "1@${M6_TEAM_A}" --type=host) || return 1
    m6_launch "${M6_A}" "1@${M6_TEAM_A}" detach || return 1
    m6_desired_is "${M6_A}" "1@${M6_TEAM_A}" || return 1
    m4_poll 20 "seat 1@${M6_TEAM_A} held" m6_seat_is "${M6_TEAM_A}" 1 held
}

# ── the checks ────────────────────────────────────────────────────────────────────────────

m6_check_move() {
    local seat
    m6_run move 0 move "${M6_A}" "${M6_TEAM_A}" "${M6_TEAM_B}" --yes || return 1
    m6_said move "^✓ session ${M6_A}: running, can be hot-controlled$" || return 1
    m6_said move "^✓ route: this host reaches ${M6_TEAM_B}'s homeserver$" || return 1
    m6_said move "^Moved: ${M6_A} is in ${M6_TEAM_B}, and no longer in ${M6_TEAM_A}\\.$" || return 1
    seat="$(m6_desired "${M6_A}")" || return 1
    [[ "${seat}" == *"@${M6_TEAM_B}" && "${seat}" != *,* ]] || { printf '[FAIL] %s desires %s, not one seat of %s\n' "${M6_A}" "${seat}" "${M6_TEAM_B}" >&2; return 1; }
    grep -qx -- "arg=${seat}" "${M6_RECORDS}/${M6_A}" || { printf '[FAIL] the record of %s does not name %s\n' "${M6_A}" "${seat}" >&2; return 1; }
    m4_poll 20 "seat 1@${M6_TEAM_A} free" m6_seat_is "${M6_TEAM_A}" 1 free || return 1
    m4_poll 20 "session list showing ${M6_A} as a cc session, ok" m6_list_state "${M6_A}" ok
}

m6_check_detached() {
    local seat claude detached
    seat="$(m6_desired "${M6_A}")" || return 1
    detached="$(m6_pid "$(m6_ev "${M6_A}.detached.pid")")" || return 1
    claude="$(m6_pid "$(m6_ev "${M6_A}.claude.pid")")" || return 1
    kill "${claude}" || return 1
    m4_poll 5 "seat ${seat} free once ${M6_A}'s claude ended" m6_seat_is "${M6_TEAM_B}" "${seat%@*}" free || return 1
    kill -0 "${detached}" || { printf '[FAIL] the detached sleep %s ended too: it proves nothing\n' "${detached}" >&2; return 1; }
    m6_launch "${M6_B}" "${seat}" || return 1
    m4_poll 20 "seat ${seat} held by ${M6_B}" m6_seat_is "${M6_TEAM_B}" "${seat%@*}" held || return 1
    m6_desired_is "${M6_B}" "${seat}"
}

m6_inbound() {
    python3 -I -c 'import json, sys; sys.exit(0 if json.load(open(sys.argv[1])).get("inbound") is True else 1)' \
        "${M6_REPORTS}/$1/applied.json" || { printf '[FAIL] the keeper of %s does not report inbound\n' "$1" >&2; return 1; }
}

m6_keeper_gone() {
    local pid
    pid="$(m6_keeper_of "$1")" || return 1
    [[ -z "${pid}" ]] || return 3
}

m6_check_bus() {
    local keeper
    m6_launch "${M6_C}" "" || return 1
    m6_desired_is "${M6_C}" "" || return 1
    m6_inbound "${M6_C}" || return 1
    m6_run bus-add 0 add "${M6_C}" "${M6_TEAM_A}" --yes || return 1
    m6_said bus-add 'Nothing restarted\.$' || return 1
    m6_said bus-add 'relaunch' no || return 1
    [[ ! -e "$(m6_membership "${M6_C}")/relaunch.json" ]] || { printf '[FAIL] a relaunch was asked of the cc session %s\n' "${M6_C}" >&2; return 1; }
    m4_poll 20 "session list showing ${M6_C} as a cc session, ok" m6_list_state "${M6_C}" ok || return 1
    keeper="$(m6_keeper_of "${M6_C}")" || return 1
    [[ -n "${keeper}" ]] || { printf '[FAIL] no seat keeper under the claude of %s\n' "${M6_C}" >&2; return 1; }
    kill "${keeper}" || return 1
    m4_poll 20 "the seat keeper of ${M6_C} gone" m6_keeper_gone "${M6_C}" || return 1
    m4_poll 20 "session list saying ${M6_C} is unguarded" m6_list_state "${M6_C}" "unguarded: end it" || return 1
    m6_run bus-repair 78 repair "${M6_C}" --yes || return 1
    m6_said bus-repair "is a cc session: its seat keeper ends with its claude" || return 1
    m6_run bus-remove 0 remove "${M6_C}" "${M6_TEAM_A}" --yes || return 1
    m6_said bus-remove '^✓ record: no --teams \(cc keeps --bus\)$' || return 1
    grep -qx -- 'arg=--bus' "${M6_RECORDS}/${M6_C}" || { printf '[FAIL] the record of %s left the bus\n' "${M6_C}" >&2; return 1; }
    if grep -q -- '^arg=--teams' "${M6_RECORDS}/${M6_C}"; then
        printf '[FAIL] the record of %s still names a team\n' "${M6_C}" >&2
        return 1
    fi
}

# ── teardown ──────────────────────────────────────────────────────────────────────────────

# m6_stop <pid file> — the process a stand-in recorded, ended if it still runs.
m6_stop() {
    local pid
    [[ -f "$1" ]] || return 0
    pid="$(cat -- "$1")" || return 1
    [[ "${pid}" =~ ^[0-9]+$ ]] || return 0
    if kill -0 "${pid}"; then
        kill "${pid}" || return 1
    fi
}

m6_cleanup() {
    local status=0 name team
    for name in "${M6_NAMES[@]}"; do
        if tmux -L ccy has-session -t "=${name}"; then
            tmux -L ccy kill-session -t "=${name}" || status=1
        fi
        m6_stop "$(m6_ev "${name}.claude.pid")" || status=1
        m6_stop "$(m6_ev "${name}.detached.pid")" || status=1
        rm -f -- "${M6_RECORDS:?}/${name}" || status=1
        rm -rf -- "$(m6_membership "${name}")" "${M6_REPORTS:?}/${name}" || status=1
    done
    M6_NAMES=()
    if [[ "${M6_TEAMS_PRESENT}" -eq 1 ]]; then
        for team in "${M6_TEAM_A}" "${M6_TEAM_B}"; do
            if m4_team_built "${team}"; then
                sudo -n "${M6_INSTALLER}" remove --team "${team}" --purge || status=1
            fi
            sudo -n systemctl reset-failed "agent-bus-*@${team}.*" || status=1
        done
        M6_TEAMS_PRESENT=0
    fi
    rm -rf -- "${M6_SEATS:?}/${M6_TEAM_A}" "${M6_SEATS:?}/${M6_TEAM_B}" || status=1
    [[ "${status}" -eq 0 ]] || printf '[FAIL] the M6 cleanup could not remove everything (see above)\n' >&2
    return "${status}"
}

acceptance_m6() {
    plan_on_cleanup m6_cleanup
    printf '## M6: cc sessions\n\n' >>"${REPORT:?}"
    plan_deploy_leg "two throwaway teams, ${M6_TEAM_A} and ${M6_TEAM_B}" m6_prepare
    plan_deploy_leg "a throwaway cc session on 1@${M6_TEAM_A} in ccy's tmux" m6_start
    check_leg "(a) move: a cc session moved between teams while it runs" m6_check_move
    check_leg "(b) audit F5: a detached process keeps no seat; the seat is free at once" m6_check_detached
    check_leg "(c) cc --bus: the first join is hot; no repair; left on the bus" m6_check_bus
    plan_deploy_leg "remove the throwaway cc sessions and teams" m6_cleanup
    m2_note "M6: (a)-(c) passed; cc's own launch (token chooser, consent, a real claude) is the owner's step in ACCEPTANCE.md"
}
