# shellcheck shell=bash
# _acceptance-m3.inc.bash — Plan 00203 milestone M3, the first end-to-end hot add and remove
# (SPEC-hot-control.md section 6, M3; ACCEPTANCE.md "M3"), sourced by the acceptance.bash
# dispatcher, which runs acceptance_m3 after acceptance_m1. Sourced, never executed: no shell
# options, no `exit`. A leg runs where errexit is off, so every command that can fail returns
# non-zero from its function explicitly. REPORT and check_leg are the dispatcher's; m2_note,
# m2_container_up, M2_LAUNCHER and M2's cleanup are M2's.
#
# WHAT IT CHANGES, and removes on the way out however the run ends:
#   - two throwaway teams, acc-m3a and acc-m3b (agent-bus-install, human `tester`), declared
#     only in a host_vars file in the run directory (CCY_TEAMS_HOST_VARS), purged at the end,
#     with their seat directories in this checkout;
#   - one throwaway headless ccy session in ccy's tmux, m3-hot-<pid>, launched on acc-m3a
#     (`--no-restore`), so it accepts team messages from launch; removed by M2's cleanup (its
#     name is added to M2_NAMES), with its registry record and membership directory, and its
#     keeper's report here.
# The first join of a session launched on no team (one relaunch through the supervisor) needs
# an interactive session with the supervisor, which a headless acceptance session does not
# have: it is the owner's step M3 (g) in ACCEPTANCE.md.
#
# THE TEAM FILES AND THE HUMAN'S CLIENT CALLS reuse Plan 00201's m7_check.py (team-file,
# host-vars, bus-ports, github-owner, member-field, room-path, login-body, access-token,
# user-line, turns, session-id), by path.
#
# CHECKS (each judged from what a tool printed or recorded, never from the model's prose)
#   (a) `ccy-teams session add <s> acc-m3b --yes` exits 0, every step ✓ or -, no relaunch
#   (b) the session's transcript has the MEMBERSHIP notice naming acc-m3b; its status line
#       says 👥 acc-m3a,acc-m3b
#   (c) a message from the human in acc-m3b reaches the conversation (a HUMAN delivery)
#   (d) `session remove <s> acc-m3b --yes` exits 0 with the re-baseline; the transcript has the
#       notice that it left; the room has the goodbye; agent-bus seat list shows the seat free;
#       `pingbus --team acc-m3b say` in the session exits 64, "removed by the owner"
#   (e) a message to the seat while it is out, then `session add` again: not delivered (H23a)
#   (f) the same add again changes neither the record nor the membership

readonly M3_TEAM_A="acc-m3a"
readonly M3_TEAM_B="acc-m3b"
readonly M3_HUMAN="tester"
readonly M3_NAME="m3-hot-$$"
readonly M3_EV="${PLAN_RUN_DIR}/m3"
readonly M3_HOST_VARS="${M3_EV}/host_vars.yml"
readonly M3_INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly M3_AGENT_BUS="/usr/local/bin/agent-bus"
readonly M3_SEATS="${PLAN_REPO_ROOT}/.claude/ccy/pingbus/seats"
readonly M3_REPORTS="${PLAN_REPO_ROOT}/.claude/ccy/pingbus/sessions"
readonly M3_TRANSCRIPTS="${PLAN_REPO_ROOT}/.claude/ccy/projects/-workspace"
readonly M3_CHECK=(python3 -I "${PLAN_REPO_ROOT}/CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py")
readonly M3_TURN="This session is an automated acceptance test of fedora-desktop Plan 00203 (milestone M3) in the owner's checkout. Run no command, read no file and change nothing. Reply with the single word READY, then end your turn with the line: STOPPING BECAUSE: acceptance turn done"
readonly M3_MARK_IN="m3-acceptance-in-$$"
readonly M3_MARK_OUT="m3-acceptance-out-$$"
#: Bounds, in seconds.
readonly M3_TURN_S=240
readonly M3_SEEN_S=120
readonly M3_QUIET_S=45
M3_TEAMS_PRESENT=0
M3_SID=""
M3_SEAT=""
M3_TOKEN=""

m3_ev() { printf '%s/%s' "${M3_EV}" "$1"; }

# m3_poll <seconds> <what> <command...> — until the command succeeds; it returns 3 for "not yet".
m3_poll() {
    local bound="$1" what="$2" status deadline
    shift 2
    deadline=$((SECONDS + bound))
    while ((SECONDS < deadline)); do
        status=0
        "$@" || status=$?
        [[ "${status}" -eq 0 ]] && return 0
        [[ "${status}" -eq 3 ]] || return 1
        sleep 3
    done
    printf '[FAIL] %s: not within %d s\n' "${what}" "${bound}" >&2
    return 1
}

m3_teams() { (cd -- "${PLAN_REPO_ROOT}" && CCY_TEAMS_HOST_VARS="${M3_HOST_VARS}" ccy-teams "$@"); }

# m3_team_built <team> — 0 when the installer has built it (unit enabled, or state present).
m3_team_built() {
    local enabled
    enabled="$(systemctl show -P UnitFileState "agent-bus-hs@$1.service")" || return 1
    [[ "${enabled}" == enabled ]] || sudo -n test -e "/var/lib/agent-bus/$1"
}

# ── setup ─────────────────────────────────────────────────────────────────────────────────

m3_prepare() {
    local inventory address url org ports team i=0
    mkdir -p -- "${M3_EV}" || return 1
    for team in "${M3_TEAM_A}" "${M3_TEAM_B}"; do
        if m3_team_built "${team}" || [[ -e "${M3_SEATS}/${team}" ]]; then
            printf '[FAIL] %s exists already (an interrupted run?): sudo %s remove --team %s --purge; rm -rf %s\n' \
                "${team}" "${M3_INSTALLER}" "${team}" "${M3_SEATS}/${team}" >&2
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
    for team in "${M3_TEAM_A}" "${M3_TEAM_B}"; do
        "${M3_CHECK[@]}" team-file "${team}" "${ports[i]}" "${address}" "${M3_HUMAN}" "${org}" \
            "Plan 00203 M3 acceptance team ${team}" >"$(m3_ev "${team}.team.json")" || return 1
        i=$((i + 1))
    done
    "${M3_CHECK[@]}" host-vars "$(m3_ev "${M3_TEAM_A}.team.json")" "$(m3_ev "${M3_TEAM_B}.team.json")" >"${M3_HOST_VARS}" || return 1
    M3_TEAMS_PRESENT=1
    for team in "${M3_TEAM_A}" "${M3_TEAM_B}"; do
        sudo -n "${M3_INSTALLER}" team --team-file "$(m3_ev "${team}.team.json")" || return 1
    done
}

# m3_start — the headless session on acc-m3a in ccy's tmux, its one turn, its session ID.
# ccy runs as the pane shell's child, never exec'd, and the trailing status write keeps it so:
# a lone command would be exec'd, and tmux would hang the pane up (m2_launch says why).
m3_start() {
    local fifo fd command
    fifo="$(m3_ev stdin.fifo)"
    mkfifo -- "${fifo}" || return 1
    exec {fd}<>"${fifo}" || return 1
    m2_adopt "${M3_NAME}" "${fd}" "$(m3_ev session)"
    command="$(printf '%q ' env "CCY_TEAMS_HOST_VARS=${M3_HOST_VARS}" "${M2_LAUNCHER}" --headless --no-restore \
        --no-supervise --teams "1@${M3_TEAM_A}" --prompt "Plan 00203 M3 acceptance" -- --input-format stream-json \
        --output-format stream-json --verbose --model haiku)" || return 1
    command+="<$(printf '%q' "${fifo}") >$(printf '%q' "$(m3_ev session.out)") 2>$(printf '%q' "$(m3_ev session.err)")"
    command+="; echo \"\$?\" >$(printf '%q' "$(m3_ev session.status)")"
    tmux -L ccy new-session -d -s "${M3_NAME}" -c "${PLAN_REPO_ROOT}" "${command}" || return 1
    m2_container_up "${M3_NAME}" || return 1
    "${M3_CHECK[@]}" user-line "${M3_TURN}" >&"${fd}" || return 1
    m3_poll "${M3_TURN_S}" "the session's first turn" m3_turned 1 || return 1
    M3_SID="$("${M3_CHECK[@]}" session-id "$(m3_ev session.out)")" || return 1
    printf '==> %s: container %s, session %s\n' "${M3_NAME}" "${M2_CTR[${M3_NAME}]}" "${M3_SID}"
}

m3_turned() {
    local n
    n="$("${M3_CHECK[@]}" turns "$(m3_ev session.out)")" || return 1
    ((n >= $1)) || return 3
}

m3_transcript() { printf '%s/%s.jsonl' "${M3_TRANSCRIPTS}" "${M3_SID}"; }

# m3_seen <text> [count] — 0 when the session's transcript holds the text (count times), 3 not yet.
m3_seen() {
    local n=0
    [[ -f "$(m3_transcript)" ]] || return 3
    n="$(grep -cF -- "$1" "$(m3_transcript)")" || n=0
    ((n >= ${2:-1})) || return 3
}

# ── the human's client ────────────────────────────────────────────────────────────────────

m3_bundle() { printf '%s/%s/%s/member.json' "${M3_SEATS}" "${M3_TEAM_B}" "${M3_SEAT}"; }
m3_field() { "${M3_CHECK[@]}" member-field "$(m3_bundle)" "$1"; }
m3_room_path() { "${M3_CHECK[@]}" room-path "$(m3_field room)"; }

m3_human_curl() {
    local -a data=()
    [[ -z "${4:-}" ]] || data=(-H 'Content-Type: application/json' --data-binary "@$4")
    printf 'header = "Authorization: Bearer %s"\n' "${M3_TOKEN}" \
        | curl --config - --silent --show-error --fail-with-body --noproxy '*' --max-time 30 -X "$1" "${data[@]}" "$2$3"
}

m3_human_login() {
    local base password empty
    base="$(m3_field base_url)" || return 1
    password="$(sudo -n "${M3_AGENT_BUS}" human password "${M3_TEAM_B}" "${M3_HUMAN}")" || return 1
    M3_TOKEN="$(printf '%s' "${password}" | "${M3_CHECK[@]}" login-body "${M3_HUMAN}" \
        | curl --silent --show-error --fail-with-body --noproxy '*' --max-time 30 \
            -H 'Content-Type: application/json' --data-binary @- "${base}/_matrix/client/v3/login" \
        | "${M3_CHECK[@]}" access-token)" || return 1
    empty="$(m3_ev empty.json)"
    printf '{}' >"${empty}" || return 1
    m3_human_curl POST "${base}" "/_matrix/client/v3/rooms/$(m3_room_path)/join" "${empty}" >"$(m3_ev join.json)" || return 1
}

# m3_human_says <text> — the human posts text mentioning the seat's member.
m3_human_says() {
    local base user body
    base="$(m3_field base_url)" || return 1
    user="$(m3_field user_id)" || return 1
    body="$(m3_ev "say-${RANDOM}${RANDOM}.json")"
    python3 -I -c 'import json, sys; print(json.dumps({"msgtype": "m.text", "body": sys.argv[1] + " " + sys.argv[2], "m.mentions": {"user_ids": [sys.argv[2]]}}))' \
        "$1" "${user}" >"${body}" || return 1
    m3_human_curl PUT "${base}" "/_matrix/client/v3/rooms/$(m3_room_path)/send/m.room.message/m3-${RANDOM}${RANDOM}" \
        "${body}" >"${body}.sent" || return 1
}

m3_room_has() {
    local base
    base="$(m3_field base_url)" || return 1
    m3_human_curl GET "${base}" "/_matrix/client/v3/rooms/$(m3_room_path)/messages?dir=b&limit=30" >"$(m3_ev room.json)" || return 1
    grep -qF -- "$1" "$(m3_ev room.json)" || return 3
}

# ── the checks ────────────────────────────────────────────────────────────────────────────

# m3_add <name> — `session add`, exit 0, every step ✓ or -, and no relaunch; the seat chosen
# the first time is named after that.
m3_add() {
    local status=0 err
    err="$(m3_ev "add-$1.err")"
    m3_teams session add "${M3_NAME}" "${M3_TEAM_B}${M3_SEAT:+/${M3_SEAT}}" --yes </dev/null 2>"${err}" || status=$?
    cat -- "${err}"
    [[ "${status}" -eq 0 ]] || { printf '[FAIL] session add exited %d\n' "${status}" >&2; return 1; }
    if grep -q '^✗' "${err}"; then
        printf '[FAIL] a step failed in an add that exited 0\n' >&2
        return 1
    fi
    if grep -q 'relaunches once\|relaunched once' "${err}"; then
        printf '[FAIL] a session launched on a team was relaunched\n' >&2
        return 1
    fi
    if [[ -z "${M3_SEAT}" ]]; then
        M3_SEAT="$(grep -oP '^✓ seat \K[a-z0-9]+(?=: free)' "${err}")" \
            || { printf '[FAIL] the add named no seat\n' >&2; return 1; }
    fi
}

m3_check_add() {
    m3_add first || return 1
    m3_human_login || return 1
}

m3_status_line() {
    local line
    line="$(podman exec -i -e PINGBUS_HOME=/tmp/pingbus-home -e PINGBUS_STATUS_LINE_INNER=1 "${M2_CTR[${M3_NAME}]}" \
        pingbus status-line <<<'{}')" || return 1
    printf '%s\n' "${line}" >"$(m3_ev status-line)"
    [[ "${line}" == *"👥 ${M3_TEAM_A},${M3_TEAM_B}"* ]] || return 3
}

m3_check_notice_and_status() {
    m3_poll "${M3_SEEN_S}" "the joined notice in the transcript" m3_seen "you joined team ${M3_TEAM_B}" || return 1
    m3_seen "kind: MEMBERSHIP" || { printf '[FAIL] the notice is not a MEMBERSHIP envelope\n' >&2; return 1; }
    m3_poll 10 "the status line naming both teams" m3_status_line || return 1
}

m3_check_message() {
    m3_human_says "${M3_MARK_IN}" || return 1
    m3_poll "${M3_SEEN_S}" "the human's message in the conversation" m3_seen "${M3_MARK_IN}" || return 1
    m3_seen "kind: HUMAN" || { printf '[FAIL] the message did not arrive as a HUMAN delivery\n' >&2; return 1; }
}

m3_seat_free() {
    (cd -- "${PLAN_REPO_ROOT}" && "${M3_AGENT_BUS}" seat list) >"$(m3_ev seat-list)" || return 1
    grep -qP "^SEAT\t${M3_TEAM_B}\t${M3_SEAT}\tfree\t" "$(m3_ev seat-list)" || return 3
}

m3_check_remove() {
    local status=0 err out
    err="$(m3_ev remove.err)"
    m3_teams session remove "${M3_NAME}" "${M3_TEAM_B}" --yes </dev/null 2>"${err}" || status=$?
    cat -- "${err}"
    [[ "${status}" -eq 0 ]] || { printf '[FAIL] session remove exited %d\n' "${status}" >&2; return 1; }
    grep -q '^✓ re-baseline' "${err}" || { printf '[FAIL] no re-baseline step\n' >&2; return 1; }
    m3_poll "${M3_SEEN_S}" "the left notice in the transcript" m3_seen "you left team ${M3_TEAM_B}" || return 1
    m3_poll 30 "the goodbye in the room" m3_room_has "left the team: removed by the owner" || return 1
    m3_poll 10 "the seat free" m3_seat_free || return 1
    status=0
    out="$(podman exec "${M2_CTR[${M3_NAME}]}" sh -c 'echo hi | PINGBUS_HOME=/tmp/pingbus-home pingbus --team "$1" say' \
        sh "${M3_TEAM_B}" 2>&1)" || status=$?
    printf '%s\n' "${out}" >"$(m3_ev say-removed.out)"
    if [[ "${status}" -ne 64 || "${out}" != *"removed by the owner"* ]]; then
        printf '[FAIL] pingbus say to the removed team: exit %d, %s\n' "${status}" "${out}" >&2
        return 1
    fi
}

m3_check_no_backlog() {
    m3_human_says "${M3_MARK_OUT}" || return 1
    sleep 5
    m3_add again || return 1
    m3_poll "${M3_SEEN_S}" "the joined notice of the re-add" m3_seen "you joined team ${M3_TEAM_B}" 2 || return 1
    sleep "${M3_QUIET_S}"
    if grep -qF -- "${M3_MARK_OUT}" "$(m3_transcript)"; then
        printf '[FAIL] a message posted while the seat was out was delivered on the re-add (H23a)\n' >&2
        return 1
    fi
}

m3_check_noop() {
    local status=0 err
    err="$(m3_ev noop.err)"
    m3_teams session add "${M3_NAME}" "${M3_TEAM_B}/${M3_SEAT}" --yes </dev/null 2>"${err}" || status=$?
    cat -- "${err}"
    [[ "${status}" -eq 0 ]] || { printf '[FAIL] the re-run exited %d\n' "${status}" >&2; return 1; }
    if grep -q '^✓ \(record\|membership\)' "${err}"; then
        printf '[FAIL] the re-run changed the record or the membership\n' >&2
        return 1
    fi
}

# ── teardown ──────────────────────────────────────────────────────────────────────────────

m3_cleanup() {
    local status=0 team
    m2_cleanup || status=1
    if [[ "${M3_TEAMS_PRESENT}" -eq 1 ]]; then
        for team in "${M3_TEAM_A}" "${M3_TEAM_B}"; do
            if m3_team_built "${team}"; then
                sudo -n "${M3_INSTALLER}" remove --team "${team}" --purge || status=1
            fi
            sudo -n systemctl reset-failed "agent-bus-*@${team}.*" || status=1
        done
        M3_TEAMS_PRESENT=0
    fi
    rm -rf -- "${M3_SEATS:?}/${M3_TEAM_A}" "${M3_SEATS:?}/${M3_TEAM_B}" "${M3_REPORTS:?}/${M3_NAME}" || status=1
    [[ "${status}" -eq 0 ]] || printf '[FAIL] the M3 cleanup could not remove everything (see above)\n' >&2
    return "${status}"
}

acceptance_m3() {
    plan_on_cleanup m3_cleanup
    printf '## M3: hot add and remove\n\n' >>"${REPORT}"
    plan_deploy_leg "two throwaway teams, ${M3_TEAM_A} and ${M3_TEAM_B}" m3_prepare
    plan_deploy_leg "a throwaway headless session on ${M3_TEAM_A} in ccy's tmux, and its first turn" m3_start
    check_leg "(a) session add ${M3_TEAM_B}: every step done, no relaunch" m3_check_add
    check_leg "(b) the membership notice in the conversation, and the status line" m3_check_notice_and_status
    check_leg "(c) a message from the human reaches the conversation" m3_check_message
    check_leg "(d) session remove: notice, goodbye, seat free, commands refused 64" m3_check_remove
    check_leg "(e) re-added: nothing posted while it was out is delivered" m3_check_no_backlog
    check_leg "(f) the same add again changes neither the record nor the membership" m3_check_noop
    plan_deploy_leg "remove the throwaway session and teams" m3_cleanup
    m2_note "M3: (a)-(f) passed; (g), the first join of a plain interactive session, is the owner's step in ACCEPTANCE.md"
}
