# shellcheck shell=bash
# _acceptance-stage2.inc.bash — the legs and checks of Plan 00211's acceptance.bash, stage 2
# (A1-A4, the test question) and --after-owner (A5), with their --fake fixtures. Sourced after
# _acceptance-stage1.inc.bash, never executed: no shell options, no `exit`. Every function
# returns non-zero with a message on stderr when it does not pass. The caller sets AGENT_BUS,
# CURL, HOST_PINGBUS, LXC_ATTACH, MEMBERS_JSON, TEAMS_JSON, RUN_TAG and REPORT.

readonly GUEST_PATH="/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
TEAMS=""
A3_TEAM=""
A3_COORDINATED=0
A3_HOME=""
A3_DIR=""
A3_HANDLE=""
A3_BASE=""

# read_teams — the present agent_bus_teams entries, through an ad-hoc Ansible copy into the run
# directory (0600), as read_members does: nothing here parses the host_vars.
read_teams() {
    local args
    args="$(jq -cn --arg dest "${TEAMS_JSON}" \
        '{content: "{{ agent_bus_teams | default([]) | to_json }}\n", dest: $dest, mode: "0600"}')" || return 1
    plan_ansible_adhoc localhost -m ansible.builtin.copy -a "${args}" || return 1
    pick_teams
}

# pick_teams — TEAMS, one per line, from TEAMS_JSON; none is a refusal (deploy 2 made no room).
pick_teams() {
    TEAMS="$(jq -r '[.[] | select((.state // "present") == "present") | .team] | .[]' "${TEAMS_JSON}")" || return 1
    if [[ -z "${TEAMS}" ]]; then
        printf 'no present agent_bus_teams entry: there is no side room to judge\n' >&2
        return 1
    fi
    printf 'teams: %s\n' "${TEAMS//$'\n'/ }" | tee -a "${REPORT}"
}

# a12_check — A1 and A2: `agent-bus check-rooms` passes for every team. It reads as admin that
# each side room exists (questions once the team has a coordinator), the index and back-links
# agree, power levels and join rules are exact, every human is joined or invited, no worker is
# in questions, and every coordinator has joined both rooms. Every team is judged first.
a12_check() {
    local team out rc bad=0
    while IFS= read -r team; do
        rc=0
        out="$("${AGENT_BUS[@]}" check-rooms "${team}")" || rc=$?
        case "${rc}" in
            0)
                printf -- '- team %s: the side rooms are sound\n' "${team}" | tee -a "${REPORT}"
                ;;
            1)
                printf 'team %s: agent-bus check-rooms found problems:\n%s\n' "${team}" "${out}" >&2
                printf -- '- team %s: REJECTED\n%s\n' "${team}" "${out}" >>"${REPORT}"
                bad=1
                ;;
            *)
                printf 'team %s: agent-bus check-rooms failed (exit %s)\n' "${team}" "${rc}" >&2
                bad=1
                ;;
        esac
    done <<<"${TEAMS}"
    return "${bad}"
}

# refused <label> <status> <answer file> — the homeserver refused the request: 403 M_FORBIDDEN.
refused() {
    local errcode
    errcode="$(jq -r '.errcode // "-"' "$3")" || errcode="(no JSON)"
    printf -- '- %s: %s %s\n' "$1" "$2" "${errcode}" >>"${REPORT}"
    if [[ "$2" != "403" ]] || [[ "${errcode}" != "M_FORBIDDEN" ]]; then
        printf '%s: answered %s %s, not 403 M_FORBIDDEN: a worker reached the questions room\n' "$1" "$2" "${errcode}" >&2
        return 1
    fi
    printf '%s: refused (403 M_FORBIDDEN)\n' "$1"
}

# sent_ok <label> <pingbus output> — the output holds a SENT line, so the homeserver took it.
sent_ok() {
    local event
    event="$(awk -F'\t' '$1 == "SENT" { print $4; exit }' <<<"$2")"
    printf -- '- %s: %s\n' "$1" "${event:-not sent}" >>"${REPORT}"
    if [[ "${event}" != \$* ]]; then
        printf '%s: pingbus printed no SENT line:\n%s\n' "$1" "$2" >&2
        return 1
    fi
    printf '%s: sent %s\n' "$1" "${event}"
}

# a3_pick — the first team whose roster names a coordinator (it has a questions room to be kept
# out of), else the first team, judged for the report alone.
a3_pick() {
    local team roster coordinator=none
    while IFS= read -r team; do
        roster="$("${AGENT_BUS[@]}" list "${team}")" || return 1
        if awk -F'\t' '$1 == "MEMBER" && $3 == "coordinator" { found = 1 } END { exit !found }' <<<"${roster}"; then
            A3_TEAM="${team}"
            A3_COORDINATED=1
            coordinator=yes
            break
        fi
    done <<<"${TEAMS}"
    if [[ -z "${A3_TEAM}" ]]; then
        A3_TEAM="${TEAMS%%$'\n'*}"
    fi
    printf 'A3 runs in team %s (coordinator: %s)\n' "${A3_TEAM}" "${coordinator}" | tee -a "${REPORT}"
}

# a3_take — a throwaway worker for this run, a host member on loopback; a3_release removes it.
a3_take() {
    A3_HOME="${PLAN_RUN_DIR}/a3-pingbus"
    A3_DIR="${A3_HOME}/${A3_TEAM}"
    mkdir -m 0700 "${A3_HOME}" || return 1
    "${AGENT_BUS[@]}" add-member "${A3_TEAM}" --repo=fedora-desktop --seat="${RUN_TAG}" --host=acceptance \
        --type=host --role=worker --address=127.0.0.1 --out="${A3_DIR}" || return 1
    A3_HANDLE="$(jq -r '.user_id | ltrimstr("@") | split(":")[0]' "${A3_DIR}/member.json")" || return 1
    A3_BASE="$(jq -r '.base_url' "${A3_DIR}/member.json")" || return 1
    plan_on_cleanup a3_release || return 1
    (umask 077 && printf 'Authorization: Bearer %s\n' "$(cat -- "${A3_DIR}/token")" >"${A3_DIR}/auth.header") || return 1
    printf 'A3 worker: %s\n' "${A3_HANDLE}" | tee -a "${REPORT}"
}

a3_release() {
    rm -f -- "${A3_DIR}/auth.header"
    "${AGENT_BUS[@]}" remove-member "${A3_TEAM}" "${A3_HANDLE}"
}

# a3_status <method> <path> [<json body>] — the A3 worker's request; prints the status code and
# leaves the answer in answer.json. The token travels in a header file, never in argv.
a3_status() {
    local args=(-sS -o "${A3_DIR}/answer.json" -w '%{http_code}' -X "$1" -H "@${A3_DIR}/auth.header")
    if [[ -n "${3:-}" ]]; then
        args+=(-H 'Content-Type: application/json' --data "$3")
    fi
    "${CURL[@]}" "${args[@]}" "${A3_BASE}$2"
}

# a3_check — A3: the worker's `pingbus report` lands in reports; with its token, reading,
# joining and posting in questions are each refused.
a3_check() {
    local out main questions code bad=0
    out="$(printf 'Plan 00211 acceptance %s: a worker report\n\nA3 checks that a worker posts here and wakes nobody. Nothing to do.\n' \
        "${RUN_TAG}" | env -i PATH="${GUEST_PATH}" HOME="${HOME}" PINGBUS_HOME="${A3_HOME}" PINGBUS_TEAMS="${A3_TEAM}" \
        "${HOST_PINGBUS}" report --team "${A3_TEAM}")" || {
        printf 'A3: pingbus report failed for %s\n' "${A3_HANDLE}" >&2
        return 1
    }
    sent_ok "A3 a worker's pingbus report" "${out}" || return 1
    if [[ "${A3_COORDINATED}" -eq 0 ]]; then
        printf 'team %s has no coordinator, so no questions room: the refusals are not judged\n' "${A3_TEAM}" |
            tee -a "${REPORT}"
        return 0
    fi
    main="$(jq -r '.room' "${A3_DIR}/member.json")" || return 1
    code="$(a3_status GET "/_matrix/client/v3/rooms/%21${main#!}/state/agent_bus.rooms/")" || return 1
    questions="$(jq -r '.questions // empty' "${A3_DIR}/answer.json")" || return 1
    if [[ "${code}" != "200" ]] || [[ "${questions}" != \!* ]]; then
        printf 'A3: the main room index names no questions room (%s)\n' "${code}" >&2
        return 1
    fi
    code="$(a3_status GET "/_matrix/client/v3/rooms/%21${questions#!}/messages?dir=b&limit=1")" || return 1
    refused "A3 a worker reads questions" "${code}" "${A3_DIR}/answer.json" || bad=1
    code="$(a3_status POST "/_matrix/client/v3/join/%21${questions#!}" '{}')" || return 1
    refused "A3 a worker joins questions" "${code}" "${A3_DIR}/answer.json" || bad=1
    code="$(a3_status PUT "/_matrix/client/v3/rooms/%21${questions#!}/send/m.room.message/a00211${RUN_TAG}" \
        '{"msgtype":"m.text","body":"Plan 00211 acceptance: a worker must not post here"}')" || return 1
    refused "A3 a worker posts in questions" "${code}" "${A3_DIR}/answer.json" || bad=1
    return "${bad}"
}

# in_guest <container> <user> <home> <team> <argv...> — a member's command in its container,
# through the guest path members.py uses (lxc-attach, runuser, a cleared environment); stdin
# passes through.
in_guest() {
    local container="$1" user="$2" home="$3" team="$4"
    shift 4
    "${LXC_ATTACH[@]}" -n "${container}" --clear-env -- env -i PATH="${GUEST_PATH}" HOME=/root \
        runuser -u "${user}" -- env -i -C "${home}" PATH="${GUEST_PATH}" HOME="${home}" PINGBUS_TEAMS="${team}" \
        "$@"
}

# guest_home <container> <user> — the user's home in the container.
guest_home() {
    local home
    home="$("${LXC_ATTACH[@]}" -n "$1" --clear-env -- env -i PATH="${GUEST_PATH}" HOME=/root getent passwd "$2" |
        cut -d: -f6)" || return 1
    if [[ "${home}" != /* ]]; then
        printf 'container %s: user %s has no home\n' "$1" "$2" >&2
        return 1
    fi
    printf '%s\n' "${home}"
}

# a4_check — A4: one present LXC member's `pingbus report`, run inside its container, lands.
a4_check() {
    local entry container user team home out
    entry="$(jq -c '[.[] | select((.state // "present") == "present")][0] // empty' "${MEMBERS_JSON}")" || return 1
    if [[ -z "${entry}" ]]; then
        printf 'no present agent_bus_lxc_members entry: A4 has no LXC member to run\n' | tee -a "${REPORT}"
        return 0
    fi
    container="$(jq -r '.container' <<<"${entry}")"
    user="$(jq -r '.user' <<<"${entry}")"
    team="$(jq -r '.team' <<<"${entry}")"
    home="$(guest_home "${container}" "${user}")" || return 1
    out="$(printf 'Plan 00211 acceptance %s: an LXC member report\n\nA4 checks that a member in an LXC container posts here. Nothing to do.\n' \
        "${RUN_TAG}" | in_guest "${container}" "${user}" "${home}" "${team}" pingbus report --team "${team}")" || {
        printf 'A4: pingbus report failed for %s in container %s\n' "${user}" "${container}" >&2
        return 1
    }
    sent_ok "A4 the LXC member ${user} in ${container} (team ${team})" "${out}"
}

# ask_test_question — the question the owner answers in Element (ACCEPTANCE-owner.md), asked by
# the first present LXC member declared role coordinator, whose history A5 then reads. Saved
# as test-question in the run directory. With no LXC coordinator nothing is asked, and
# --after-owner refuses, saying so.
ask_test_question() {
    local entry container user team home out q
    entry="$(jq -c '[.[] | select((.state // "present") == "present" and .role == "coordinator")][0] // empty' \
        "${MEMBERS_JSON}")" || return 1
    if [[ -z "${entry}" ]]; then
        printf 'no LXC member is declared role coordinator: no test question is asked, so --after-owner (A5) cannot run\n' |
            tee -a "${REPORT}"
        return 0
    fi
    container="$(jq -r '.container' <<<"${entry}")"
    user="$(jq -r '.user' <<<"${entry}")"
    team="$(jq -r '.team' <<<"${entry}")"
    home="$(guest_home "${container}" "${user}")" || return 1
    out="$(printf '%s\n' "Plan 00211 acceptance ${RUN_TAG}: a test question" "" \
        "Context: Plan 00211's acceptance asked this to check the questions room end to end." \
        "Question: Reply in this thread with any text, then edit that reply (ACCEPTANCE-owner.md)." |
        in_guest "${container}" "${user}" "${home}" "${team}" pingbus ask-human --team "${team}")" || {
        printf 'the test question: pingbus ask-human failed for %s in container %s\n' "${user}" "${container}" >&2
        return 1
    }
    sent_ok "the test question, asked by ${user} in ${container} (team ${team})" "${out}" || return 1
    q="$(awk -F'\t' '$1 == "QUESTION" { print $4; exit }' <<<"${out}")"
    if [[ "${q}" != Q* ]]; then
        printf 'the test question: ask-human printed no QUESTION line:\n%s\n' "${out}" >&2
        return 1
    fi
    printf '%s\t%s\t%s\t%s\t%s\n' "${container}" "${user}" "${home}" "${team}" "${q}" >"${PLAN_RUN_DIR}/test-question"
    printf 'the test question is %s in team %s: answer it in Element (ACCEPTANCE-owner.md)\n' "${q}" "${team}" |
        tee -a "${REPORT}"
}

# replies_ok <Q<n>> <history output> — A5's verdict: at least two HUMAN lines received in the
# questions room attached to the question (the owner's thread reply and its edit).
replies_ok() {
    local count
    count="$(awk -F'\t' -v q="questions:$1" '$1 == "HISTORY" && $3 == "in" && $5 == "HUMAN" && $NF == q { n++ } END { print n + 0 }' <<<"$2")"
    printf -- '- %s: %s HUMAN line(s) attached\n' "$1" "${count}" >>"${REPORT}"
    if [[ "${count}" -lt 2 ]]; then
        printf '%s has %s human message(s) attached, not the reply and its edit: do the Element steps of ACCEPTANCE-owner.md, then run --after-owner again\n' \
            "$1" "${count}" >&2
        return 1
    fi
    printf '%s: the reply and its edit reached the coordinator\n' "$1"
}

# a5_check — A5: the coordinator's `pingbus history --room questions`, in its container, shows
# the owner's reply and edit attached to the newest stage-2 run's test question. The raw
# history lines are kept in this run directory for Task 4.2.
a5_check() {
    local runs dirs saved container user home team q out
    runs="${PLAN_REPO_ROOT}/untracked/plan-runs/$(basename "${PLAN_SCRIPT_DIR}")/acceptance"
    dirs=()
    if [[ -d "${runs}" ]]; then
        dirs=("${runs}"/*/test-question)
    fi
    if [[ "${#dirs[@]}" -eq 0 ]] || [[ ! -f "${dirs[0]}" ]]; then
        printf 'no stage-2 run asked a test question (under %s): run deploy 2 with an LXC member declared role coordinator\n' \
            "${runs}" >&2
        return 1
    fi
    saved="${dirs[${#dirs[@]} - 1]}"
    IFS=$'\t' read -r container user home team q <"${saved}" || return 1
    printf 'test question %s of team %s, asked by %s in %s (%s)\n' "${q}" "${team}" "${user}" "${container}" "${saved}" |
        tee -a "${REPORT}"
    out="$(in_guest "${container}" "${user}" "${home}" "${team}" pingbus history --room questions --team "${team}" </dev/null)" || {
        printf 'A5: pingbus history failed for %s in container %s\n' "${user}" "${container}" >&2
        return 1
    }
    printf '%s\n' "${out}" >"${PLAN_RUN_DIR}/questions-history.tsv"
    replies_ok "${q}" "${out}"
}

# not_joined — one `<team> <handle> <membership>` line per coordinator that has not joined its
# team's questions room, from `agent-bus list` (membership `-` when there is no ROOM line).
not_joined() {
    local team roster
    while IFS= read -r team; do
        roster="$("${AGENT_BUS[@]}" list "${team}")" || return 1
        awk -F'\t' -v team="${team}" '
            $1 == "MEMBER" && $3 == "coordinator" { coordinator[$2] = 1 }
            $1 == "ROOM" && $3 == "questions" { seen[substr($2, 2, index($2, ":") - 2)] = $4 }
            END { for (h in coordinator) if (seen[h] != "join") printf "%s %s %s\n", team, h, (h in seen ? seen[h] : "-") }
        ' <<<"${roster}" || return 1
    done <<<"${TEAMS}"
}

# questions_joined — deploy 2's wait (SPEC §5.3 leg 3): every coordinator has joined its
# team's questions room within JOIN_WAIT_S, or each that has not is named.
JOIN_WAIT_S=180
JOIN_POLL_S=10
questions_joined() {
    local pending deadline=$((SECONDS + JOIN_WAIT_S)) team handle membership
    while true; do
        pending="$(not_joined)" || return 1
        if [[ -z "${pending}" ]]; then
            printf 'every coordinator has joined its team'\''s questions room\n'
            return 0
        fi
        if [[ "${SECONDS}" -ge "${deadline}" ]]; then
            while read -r team handle membership; do
                printf 'team %s: %s has not joined questions (%s; invite: its session is not running; leave: its running pingbus is older than deploy 1): restart that session, then run meta-deploy again\n' \
                    "${team}" "${handle}" "${membership}" >&2
            done <<<"${pending}"
            return 1
        fi
        printf 'waiting for %s coordinator(s) to join questions...\n' "$(wc -l <<<"${pending}")"
        sleep "${JOIN_POLL_S}"
    done
}

# ── --fake ───────────────────────────────────────────────────────────────────────────────
# A fake agent-bus prints <team>.out and exits with <team>.rc from its own directory.

fake2_setup() {
    cat >"${FIX}/agent-bus" <<'EOF' || return 1
#!/usr/bin/env bash
set -euo pipefail
here="$(dirname "$0")"
cat -- "${here}/$2.out"
exit "$(cat -- "${here}/$2.rc")"
EOF
    chmod 0755 "${FIX}/agent-bus" || return 1
    AGENT_BUS=("${FIX}/agent-bus")
    TEAMS_JSON="${FIX}/teams.json"
}

# fake_a12 <teams json> [<team>=<rc>...] — check-rooms answers rc (1 prints a PROBLEM line).
fake_a12() {
    printf '%s\n' "$1" >"${TEAMS_JSON}" || return 1
    shift
    local spec team rc
    for spec in "$@"; do
        team="${spec%%=*}"
        rc="${spec#*=}"
        printf '%s\n' "${rc}" >"${FIX}/${team}.rc"
        if [[ "${rc}" == "1" ]]; then
            printf 'PROBLEM\tteam %s: alpha has not joined questions (invite): restart\n' "${team}" >"${FIX}/${team}.out"
        else
            : >"${FIX}/${team}.out"
        fi
    done
    pick_teams && a12_check
}

# fake_join <membership of t1's coordinator: join|invite|leave|none|absent> — deploy 2's wait over
# fixture rosters, with no waiting: t2 has no coordinator; `none` gives t1's coordinator no ROOM
# line, and `absent` gives t1 no coordinator.
fake_join() {
    local role=coordinator room=""
    printf '%s\n' '[{"team":"t1"},{"team":"t2"}]' >"${TEAMS_JSON}" || return 1
    case "$1" in
        none) ;;
        absent) role=worker room=$'\nROOM\t@alpha:t1.internal\tquestions\t-' ;;
        *) room=$'\nROOM\t@alpha:t1.internal\tquestions\t'"$1" ;;
    esac
    printf 'HUMAN\talice\tjoin\nMEMBER\talpha\t%s\tjoin\tactive\nROOM\t@alice:t1.internal\tquestions\tjoin%s\n' \
        "${role}" "${room}" >"${FIX}/t1.out"
    printf 'HUMAN\talice\tjoin\nMEMBER\tbeta\tworker\tjoin\tactive\nROOM\t@beta:t2.internal\treports\tjoin\n' >"${FIX}/t2.out"
    printf '0\n' >"${FIX}/t1.rc"
    printf '0\n' >"${FIX}/t2.rc"
    JOIN_WAIT_S=0
    pick_teams && questions_joined
}

# fake_refused <status> <answer JSON>
fake_refused() {
    printf '%s\n' "$2" >"${FIX}/answer.json" && refused "a fixture request" "$1" "${FIX}/answer.json"
}
