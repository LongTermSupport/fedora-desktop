# shellcheck shell=bash
# _acceptance-stage2.inc.bash — the legs and checks of Plan 00211's acceptance.bash, stage 2
# (the joins, A1-A4, the test question) and --after-owner (A5), with their --fake fixtures.
# Sourced after _acceptance-stage1.inc.bash, never executed: no shell options, no `exit`. Every
# function returns non-zero with a message on stderr when it does not pass. The caller sets
# AGENT_BUS, CURL, HOST_PINGBUS, LXC_INFO, LXC_ATTACH, PODMAN, MEMBERS_JSON, TEAMS_JSON, RUN_TAG
# and REPORT.
#
# A SESSION is a member's running pingbus this host can drive, one tab-separated line:
#   ccy <container ID> - <pingbus home> <team> <user ID>   a seat held in a running ccy
#       session (podman, label ccy=true); `pingbus seat exec` links each team's seat into the
#       session home /tmp/pingbus-home, which a `podman exec` names as PINGBUS_HOME
#   lxc <container> <user> <home> <team> <user ID>          a present agent_bus_lxc_members
#       entry, driven through the guest path members.py uses
# The coordinator is found among them by the handle the registry gives it (`agent-bus list`),
# whatever kind of session it runs in.

readonly GUEST_PATH="/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
readonly CCY_PINGBUS_HOME="/tmp/pingbus-home"
# Each team's member.json in a ccy session home, one after another (none: no seat is held).
readonly CCY_SEATED_SH="for f in ${CCY_PINGBUS_HOME}/*/member.json; do if [ -f \"\$f\" ]; then cat \"\$f\"; fi; done"
# where.py <state dir> <event ID...>: `STATE present|missing`, then per event ID
# `<ID> <inbox,consumed|-> <dropped|kept>`, read in the session's own team state directory.
readonly WHERE_PY='import os, sys
state = sys.argv[1]
print("STATE", "present" if os.path.isdir(state) else "missing", sep="\t")
dropped = set()
log = os.path.join(state, "dropped.log")
if os.path.exists(log):
    with open(log, encoding="utf-8") as handle:
        dropped = {line.split("\t")[1] for line in handle if line.count("\t") >= 1}
for event in sys.argv[2:]:
    where = [d for d in ("inbox", "consumed") if os.path.exists(os.path.join(state, d, event + ".json"))]
    print(event, ",".join(where) or "-", "dropped" if event in dropped else "kept", sep="\t")'
# How long a report is given to reach an inbox it must not reach, before A3 looks.
REPORT_SETTLE_S=15
TEAMS=""
SESSIONS=""
QTEAM=""
QHANDLE=""
COORD=""
A3_HOME=""
A3_DIR=""
A3_HANDLE=""
A3_BASE=""
A3_EVENT=""

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

# ── the sessions ─────────────────────────────────────────────────────────────────────────

session_field() { cut -f "$2" <<<"$1"; }

# session_label <session> — who and where, for messages and the report.
session_label() {
    local kind target user team uid
    IFS=$'\t' read -r kind target user _ team uid <<<"$1"
    case "${kind}" in
        ccy) printf '%s (team %s) in ccy container %s' "${uid}" "${team}" "${target}" ;;
        *) printf '%s (team %s) as %s in LXC container %s' "${uid}" "${team}" "${user}" "${target}" ;;
    esac
}

# session_run <session> <argv...> — the member's command where its session runs, with its own
# pingbus home and team; stdin passes through.
session_run() {
    local kind target user home team
    IFS=$'\t' read -r kind target user home team _ <<<"$1"
    shift
    case "${kind}" in
        ccy)
            "${PODMAN[@]}" exec -i -e PINGBUS_HOME="${home}" -e PINGBUS_TEAMS="${team}" "${target}" "$@"
            ;;
        lxc) in_guest "${target}" "${user}" "${home}" "${team}" "$@" ;;
        *)
            printf 'a session of unknown kind %s\n' "${kind}" >&2
            return 1
            ;;
    esac
}

# session_state <session> — the team's state directory under the session's pingbus home.
session_state() {
    local kind home team
    IFS=$'\t' read -r kind _ _ home team _ <<<"$1"
    if [[ "${kind}" == "lxc" ]]; then
        home="${home}/.config/pingbus"
    fi
    printf '%s/%s/state\n' "${home}" "${team}"
}

# ccy_sessions — one session line per seat held in a running ccy container.
ccy_sessions() {
    local ids id out
    ids="$("${PODMAN[@]}" ps --filter label=ccy=true --format '{{.ID}}')" || return 1
    while IFS= read -r id; do
        if [[ -z "${id}" ]]; then
            continue
        fi
        out="$("${PODMAN[@]}" exec "${id}" sh -c "${CCY_SEATED_SH}")" || {
            printf 'podman exec into the running ccy container %s failed\n' "${id}" >&2
            return 1
        }
        if [[ -n "${out}" ]]; then
            jq -r --arg id "${id}" --arg home "${CCY_PINGBUS_HOME}" \
                '["ccy", $id, "-", $home, .team, .user_id] | @tsv' <<<"${out}" || return 1
        fi
    done <<<"${ids}"
}

# lxc_sessions — one session line per present agent_bus_lxc_members entry. Stage 1, run again
# as deploy 2's pre-flight, refused a declared container that is down.
lxc_sessions() {
    local entries entry container user team home uid
    entries="$(jq -c '.[] | select((.state // "present") == "present")' "${MEMBERS_JSON}")" || return 1
    while IFS= read -r entry; do
        if [[ -z "${entry}" ]]; then
            continue
        fi
        container="$(jq -r '.container' <<<"${entry}")" || return 1
        user="$(jq -r '.user' <<<"${entry}")" || return 1
        team="$(jq -r '.team' <<<"${entry}")" || return 1
        home="$(guest_home "${container}" "${user}")" || return 1
        uid="$(in_guest "${container}" "${user}" "${home}" "${team}" cat -- "${home}/.config/pingbus/${team}/member.json" </dev/null |
            jq -r '.user_id')" || {
            printf 'container %s: %s has no readable pingbus bundle for team %s: run play-agent-bus.yml with the container running\n' \
                "${container}" "${user}" "${team}" >&2
            return 1
        }
        printf 'lxc\t%s\t%s\t%s\t%s\t%s\n' "${container}" "${user}" "${home}" "${team}" "${uid}"
    done <<<"${entries}"
}

# read_sessions — SESSIONS, every session this host can drive; saved as sessions.tsv.
read_sessions() {
    local ccy lxc
    ccy="$(ccy_sessions)" || return 1
    lxc="$(lxc_sessions)" || return 1
    SESSIONS="$(printf '%s\n%s\n' "${ccy}" "${lxc}" | awk 'NF')"
    printf '%s\n' "${SESSIONS}" >"${PLAN_RUN_DIR}/sessions.tsv" || return 1
    printf 'sessions: %s seat(s) held in running ccy sessions, %s declared LXC member(s)\n' \
        "$(awk -F'\t' '$1 == "ccy"' <<<"${SESSIONS}" | wc -l)" "$(awk -F'\t' '$1 == "lxc"' <<<"${SESSIONS}" | wc -l)" |
        tee -a "${REPORT}"
}

# find_session <team> <handle> — the session line of that member, or a refusal naming it.
find_session() {
    local found
    found="$(awk -F'\t' -v team="$1" -v handle="$2" \
        '$5 == team && substr($6, 2, index($6, ":") - 2) == handle { print; exit }' <<<"${SESSIONS}")"
    if [[ -z "${found}" ]]; then
        printf 'team %s: the coordinator %s runs in no session this host can drive (no running ccy session holds its seat, and no present agent_bus_lxc_members entry is it), so no test question can be asked from it: start its session (a host member is not driven), then run meta-deploy again\n' \
            "$1" "$2" >&2
        return 1
    fi
    printf '%s\n' "${found}"
}

# pick_coordinator — QTEAM, the first team whose registry names a coordinator (`agent-bus
# list`), QHANDLE that coordinator and COORD its session. No team with a coordinator has no
# questions room to accept, which is a refusal, never a pass.
pick_coordinator() {
    local team roster handle
    while IFS= read -r team; do
        roster="$("${AGENT_BUS[@]}" list "${team}")" || return 1
        handle="$(awk -F'\t' '$1 == "MEMBER" && $3 == "coordinator" { print $2; exit }' <<<"${roster}")"
        if [[ -n "${handle}" ]]; then
            QTEAM="${team}"
            QHANDLE="${handle}"
            break
        fi
    done <<<"${TEAMS}"
    if [[ -z "${QTEAM}" ]]; then
        printf 'no team has a coordinator, so there is no questions room to accept: make one seat a team'\''s coordinator (ccy-teams: Make coordinator, or role: coordinator on its agent_bus_lxc_members entry), then run meta-deploy again\n' >&2
        return 1
    fi
    COORD="$(find_session "${QTEAM}" "${QHANDLE}")" || return 1
    printf 'the questions room is judged in team %s, through its coordinator %s\n' "${QTEAM}" "$(session_label "${COORD}")" |
        tee -a "${REPORT}"
}

# ── the joins (SPEC §5.3 leg 3, and A2's running members) ────────────────────────────────

# not_joined — one `<team> <user ID> <room> <membership>` line per account that has not joined
# a room it must: each coordinator its team's questions room, and each seat held in a running
# ccy session its team's reports room (membership `-` when there is no ROOM line). An LXC
# member's session cannot be seen from the host, so its reports join is judged by A4.
not_joined() {
    local team roster running
    while IFS= read -r team; do
        roster="$("${AGENT_BUS[@]}" list "${team}")" || return 1
        running="$(awk -F'\t' -v team="${team}" '$1 == "ccy" && $5 == team { print $6 }' <<<"${SESSIONS}")"
        awk -F'\t' -v team="${team}" -v running="${running}" '
            BEGIN { n = split(running, ids, "\n"); for (i = 1; i <= n; i++) if (ids[i] != "") want[ids[i] "\treports"] = 1 }
            $1 == "MEMBER" && $3 == "coordinator" { coordinator[$2] = 1 }
            $1 == "ROOM" { seen[$2 "\t" $3] = $4; local_[$2] = substr($2, 2, index($2, ":") - 2) }
            END {
                for (u in local_) if (local_[u] in coordinator) want[u "\tquestions"] = 1
                for (h in coordinator) { found = 0; for (u in local_) if (local_[u] == h) found = 1
                    if (!found) printf "%s\t@%s\tquestions\t-\n", team, h }
                for (k in want) if (seen[k] != "join") { split(k, p, "\t"); printf "%s\t%s\t%s\t%s\n", team, p[1], p[2], (k in seen ? seen[k] : "-") }
            }' <<<"${roster}" || return 1
    done <<<"${TEAMS}"
}

# rooms_joined — every coordinator has joined its questions room, and every running ccy seat
# its reports room, within JOIN_WAIT_S; else each that has not is named.
JOIN_WAIT_S=180
JOIN_POLL_S=10
rooms_joined() {
    local pending deadline=$((SECONDS + JOIN_WAIT_S)) team user room membership
    while true; do
        pending="$(not_joined)" || return 1
        if [[ -z "${pending}" ]]; then
            printf 'every coordinator has joined questions, and every running ccy seat (%s) reports\n' \
                "$(awk -F'\t' '$1 == "ccy"' <<<"${SESSIONS}" | wc -l)" | tee -a "${REPORT}"
            return 0
        fi
        if [[ "${SECONDS}" -ge "${deadline}" ]]; then
            while IFS=$'\t' read -r team user room membership; do
                printf 'team %s: %s has not joined %s (%s; invite or -: its pingbus has not synced since deploy 2, so its session is not running or started before deploy 1; leave: its running pingbus is older than deploy 1): restart that session, then run meta-deploy again\n' \
                    "${team}" "${user}" "${room}" "${membership}" >&2
            done <<<"${pending}"
            return 1
        fi
        printf 'waiting for %s join(s)...\n' "$(wc -l <<<"${pending}")"
        sleep "${JOIN_POLL_S}"
    done
}

# ── A1, A2 ───────────────────────────────────────────────────────────────────────────────

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

# ── A3 ───────────────────────────────────────────────────────────────────────────────────

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

# sent_event <pingbus output> — the event ID of its SENT line, or nothing.
sent_event() { awk -F'\t' '$1 == "SENT" { print $4; exit }' <<<"$1"; }

# sent_ok <label> <pingbus output> — the output holds a SENT line, so the homeserver took it.
sent_ok() {
    local event
    event="$(sent_event "$2")"
    printf -- '- %s: %s\n' "$1" "${event:-not sent}" >>"${REPORT}"
    if [[ "${event}" != \$* ]]; then
        printf '%s: pingbus printed no SENT line:\n%s\n' "$1" "$2" >&2
        return 1
    fi
    printf '%s: sent %s\n' "$1" "${event}"
}

# a3_take — a throwaway worker for this run in the coordinator's team, a host member on
# loopback. a3_release is registered before add-member runs, so a failure at any later step
# still removes it.
a3_take() {
    A3_HOME="${PLAN_RUN_DIR}/a3-pingbus"
    A3_DIR="${A3_HOME}/${QTEAM}"
    mkdir -m 0700 "${A3_HOME}" || return 1
    plan_on_cleanup a3_release || return 1
    "${AGENT_BUS[@]}" add-member "${QTEAM}" --repo=fedora-desktop --seat="${RUN_TAG}" --host=acceptance \
        --type=host --role=worker --address=127.0.0.1 --out="${A3_DIR}" || return 1
    A3_HANDLE="$(jq -r '.user_id | ltrimstr("@") | split(":")[0]' "${A3_DIR}/member.json")" || return 1
    A3_BASE="$(jq -r '.base_url' "${A3_DIR}/member.json")" || return 1
    (umask 077 && printf 'Authorization: Bearer %s\n' "$(cat -- "${A3_DIR}/token")" >"${A3_DIR}/auth.header") || return 1
    printf 'A3 worker: %s in team %s\n' "${A3_HANDLE}" "${QTEAM}" | tee -a "${REPORT}"
}

# a3_release — the throwaway worker out of the registry and deactivated. With no member.json,
# add-member did not finish, so its handle is not known here: that is said, and is a failure.
a3_release() {
    local handle
    if [[ -z "${A3_DIR}" ]]; then
        return 0
    fi
    rm -f -- "${A3_DIR}/auth.header"
    if [[ ! -f "${A3_DIR}/member.json" ]]; then
        printf 'A3 cleanup: add-member for seat %s of team %s wrote no member.json, so its handle is unknown here: sudo agent-bus list %s names the member of that seat, if there is one, to remove-member\n' \
            "${RUN_TAG}" "${QTEAM}" "${QTEAM}" >&2
        return 1
    fi
    handle="$(jq -r '.user_id | ltrimstr("@") | split(":")[0]' "${A3_DIR}/member.json")" || return 1
    "${AGENT_BUS[@]}" remove-member "${QTEAM}" "${handle}"
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
# joining and posting in questions are each refused. The team has a coordinator by
# pick_coordinator, so it has a questions room to be kept out of.
a3_check() {
    local out main questions code bad=0
    out="$(printf 'Plan 00211 acceptance %s: a worker report\n\nA3 checks that a worker posts here and wakes nobody. Nothing to do.\n' \
        "${RUN_TAG}" | env -i PATH="${GUEST_PATH}" HOME="${HOME}" PINGBUS_HOME="${A3_HOME}" PINGBUS_TEAMS="${QTEAM}" \
        "${HOST_PINGBUS}" report --team "${QTEAM}")" || {
        printf 'A3: pingbus report failed for %s\n' "${A3_HANDLE}" >&2
        return 1
    }
    sent_ok "A3 a worker's pingbus report" "${out}" || return 1
    A3_EVENT="$(sent_event "${out}")"
    main="$(jq -r '.room' "${A3_DIR}/member.json")" || return 1
    code="$(a3_status GET "/_matrix/client/v3/rooms/%21${main#!}/state/agent_bus.rooms/")" || return 1
    questions="$(jq -r '.questions // empty' "${A3_DIR}/answer.json")" || return 1
    if [[ "${code}" != "200" ]] || [[ "${questions}" != \!* ]]; then
        printf 'A3: the main room index of team %s names no questions room (%s)\n' "${QTEAM}" "${code}" >&2
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

# where_in <session> <event ID...> — WHERE_PY's lines, run where the session runs.
where_in() {
    local session="$1" state
    shift
    state="$(session_state "${session}")" || return 1
    session_run "${session}" python3 -I -c "${WHERE_PY}" "${state}" "$@" </dev/null
}

# inbox_clear <label> <where lines> — the session has a state directory, and the event is in
# neither its inbox nor its consumed directory.
inbox_clear() {
    printf -- '- %s: %s\n' "$1" "${2//$'\n'/; }" >>"${REPORT}"
    if ! grep -qxF $'STATE\tpresent' <<<"$2"; then
        printf '%s: the session has no state directory for the team, so its inbox cannot be judged\n' "$1" >&2
        return 1
    fi
    if awk -F'\t' '$1 != "STATE" && $2 != "-" { bad = 1 } END { exit !bad }' <<<"$2"; then
        printf '%s: the report reached the session'\''s inbox:\n%s\n' "$1" "$2" >&2
        return 1
    fi
    printf '%s: not in its inbox\n' "$1"
}

# a3_inbox_check — A3: after REPORT_SETTLE_S, the worker's report is in no inbox of any session
# of its team this host can drive (the coordinator's at least). Reports is never synced.
a3_inbox_check() {
    local sessions session out count=0 bad=0
    sessions="$(awk -F'\t' -v team="${QTEAM}" '$5 == team' <<<"${SESSIONS}")"
    sleep "${REPORT_SETTLE_S}"
    while IFS= read -r session; do
        out="$(where_in "${session}" "${A3_EVENT}")" || {
            printf 'A3: the inbox of %s could not be read\n' "$(session_label "${session}")" >&2
            return 1
        }
        inbox_clear "A3 inbox of $(session_label "${session}")" "${out}" || bad=1
        count=$((count + 1))
    done <<<"${sessions}"
    printf 'A3: the report %s is in none of the %s inbox(es) of team %s after %s s\n' \
        "${A3_EVENT}" "${count}" "${QTEAM}" "${REPORT_SETTLE_S}" | tee -a "${REPORT}"
    return "${bad}"
}

# ── A4 ───────────────────────────────────────────────────────────────────────────────────

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

# a4_member — the first LXC session, or nothing when no LXC member is declared.
a4_member() { awk -F'\t' '$1 == "lxc" { print; exit }' <<<"${SESSIONS}"; }

# a4_check — A4: the first declared LXC member's `pingbus report`, run inside its container,
# lands. The caller runs it only when there is one; with none, A4 is reported NOT JUDGED.
a4_check() {
    local session out
    session="$(a4_member)"
    out="$(printf 'Plan 00211 acceptance %s: an LXC member report\n\nA4 checks that a member in an LXC container posts here. Nothing to do.\n' \
        "${RUN_TAG}" | session_run "${session}" pingbus report --team "$(session_field "${session}" 5)")" || {
        printf 'A4: pingbus report failed for %s\n' "$(session_label "${session}")" >&2
        return 1
    }
    sent_ok "A4 the LXC member $(session_label "${session}")" "${out}"
}

# ── the test question, and A5 ────────────────────────────────────────────────────────────

# ask_test_question — the question the owner answers in Element (ACCEPTANCE-owner.md), asked by
# the team's coordinator in its own session, whatever kind. Saved as test-question in the run
# directory: the team, the coordinator's handle and the question number. A5 finds the
# coordinator's session again, so a restart between the two does not matter.
ask_test_question() {
    local out q
    out="$(printf '%s\n' "Plan 00211 acceptance ${RUN_TAG}: a test question" "" \
        "Context: Plan 00211's acceptance asked this to check the questions room end to end." \
        "Question: Reply in this thread with any text, then edit that reply (ACCEPTANCE-owner.md)." |
        session_run "${COORD}" pingbus ask-human --team "${QTEAM}")" || {
        printf 'the test question: pingbus ask-human failed for %s\n' "$(session_label "${COORD}")" >&2
        return 1
    }
    sent_ok "the test question, asked by $(session_label "${COORD}")" "${out}" || return 1
    q="$(awk -F'\t' '$1 == "QUESTION" { print $4; exit }' <<<"${out}")"
    if [[ "${q}" != Q* ]]; then
        printf 'the test question: ask-human printed no QUESTION line:\n%s\n' "${out}" >&2
        return 1
    fi
    printf '%s\t%s\t%s\n' "${QTEAM}" "${QHANDLE}" "${q}" >"${PLAN_RUN_DIR}/test-question"
    printf 'the test question is %s in team %s: answer it in Element (ACCEPTANCE-owner.md)\n' "${q}" "${QTEAM}" |
        tee -a "${REPORT}"
}

# attached_ids <Q<n>> <history output> — the event IDs of the HUMAN lines received in the
# questions room attached to the question.
attached_ids() {
    awk -F'\t' -v q="questions:$1" '$1 == "HISTORY" && $3 == "in" && $5 == "HUMAN" && $NF == q { print $7 }' <<<"$2"
}

# replies_ok <Q<n>> <history output> — A5's first verdict: at least two HUMAN lines received in
# the questions room attached to the question (the owner's thread reply and its edit).
replies_ok() {
    local count
    count="$(attached_ids "$1" "$2" | awk 'NF' | wc -l)"
    printf -- '- %s: %s HUMAN line(s) attached\n' "$1" "${count}" >>"${REPORT}"
    if [[ "${count}" -lt 2 ]]; then
        printf '%s has %s human message(s) attached, not the reply and its edit: do the Element steps of ACCEPTANCE-owner.md, then run --after-owner again\n' \
            "$1" "${count}" >&2
        return 1
    fi
    printf '%s: the reply and its edit reached the coordinator\n' "$1"
}

# envelopes_ok <label> <where lines> — A5's second verdict: the session has a state directory,
# and every event is in its consumed directory, none dropped: it was taken by the session (an
# envelope delivery, or its own `recv`), not left in the inbox nor refused.
envelopes_ok() {
    printf -- '- %s: %s\n' "$1" "${2//$'\n'/; }" >>"${REPORT}"
    if ! grep -qxF $'STATE\tpresent' <<<"$2"; then
        printf '%s: the session has no state directory for the team\n' "$1" >&2
        return 1
    fi
    if awk -F'\t' '$1 != "STATE" && ($2 != "consumed" || $3 != "kept") { bad = 1 } END { exit !bad }' <<<"$2"; then
        printf '%s: not every attached message was taken by the session (inbox: not yet delivered, run --after-owner again shortly; dropped: refused):\n%s\n' \
            "$1" "$2" >&2
        return 1
    fi
    printf '%s: the session took every attached message\n' "$1"
}

# a5_check — A5: the coordinator's `pingbus history --room questions`, in its session, shows the
# owner's reply and edit attached to the newest stage-2 run's test question, and its session
# took both. The raw history lines are kept in this run directory for Task 4.2.
a5_check() {
    local runs dirs saved team handle q session out ids where
    runs="${PLAN_REPO_ROOT}/untracked/plan-runs/$(basename "${PLAN_SCRIPT_DIR}")/acceptance"
    dirs=()
    if [[ -d "${runs}" ]]; then
        dirs=("${runs}"/*/test-question)
    fi
    if [[ "${#dirs[@]}" -eq 0 ]] || [[ ! -f "${dirs[0]}" ]]; then
        printf 'no stage-2 run asked a test question (under %s): run deploy 2 until its acceptance is ACCEPTED\n' \
            "${runs}" >&2
        return 1
    fi
    saved="${dirs[${#dirs[@]} - 1]}"
    IFS=$'\t' read -r team handle q <"${saved}" || return 1
    session="$(find_session "${team}" "${handle}")" || return 1
    printf 'test question %s of team %s, asked by %s, read through %s (%s)\n' "${q}" "${team}" "${handle}" \
        "$(session_label "${session}")" "${saved}" | tee -a "${REPORT}"
    out="$(session_run "${session}" pingbus history --room questions --team "${team}" </dev/null)" || {
        printf 'A5: pingbus history failed for %s\n' "$(session_label "${session}")" >&2
        return 1
    }
    printf '%s\n' "${out}" >"${PLAN_RUN_DIR}/questions-history.tsv"
    replies_ok "${q}" "${out}" || return 1
    mapfile -t ids < <(attached_ids "${q}" "${out}")
    where="$(where_in "${session}" "${ids[@]}")" || {
        printf 'A5: the state of %s could not be read\n' "$(session_label "${session}")" >&2
        return 1
    }
    envelopes_ok "A5 ${q} taken by $(session_label "${session}")" "${where}"
}

# ── --fake ───────────────────────────────────────────────────────────────────────────────
# A fake agent-bus prints <team>.out and exits with <team>.rc from its own directory. A fake
# podman answers `ps` with ps.out and `exec <ID>` with <ID>.seated.

fake2_setup() {
    cat >"${FIX}/agent-bus" <<'EOF' || return 1
#!/usr/bin/env bash
set -euo pipefail
here="$(dirname "$0")"
cat -- "${here}/$2.out"
exit "$(cat -- "${here}/$2.rc")"
EOF
    cat >"${FIX}/podman" <<'EOF' || return 1
#!/usr/bin/env bash
set -euo pipefail
here="$(dirname "$0")"
case "$1" in
    ps) cat -- "${here}/ps.out" ;;
    exec) cat -- "${here}/$2.seated" ;;
    *) exit 64 ;;
esac
EOF
    chmod 0755 "${FIX}/agent-bus" "${FIX}/podman" || return 1
    AGENT_BUS=("${FIX}/agent-bus")
    PODMAN=("${FIX}/podman")
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

# fake_rosters <t1's coordinator: alpha|none> <alpha's questions membership> <beta's reports
# membership> — t1 and t2; t2 has the worker beta, seated in a running ccy session.
fake_rosters() {
    local role=coordinator
    printf '%s\n' '[{"team":"t1"},{"team":"t2"}]' >"${TEAMS_JSON}" || return 1
    if [[ "$1" == "none" ]]; then
        role=worker
    fi
    {
        printf 'HUMAN\talice\tjoin\nMEMBER\talpha\t%s\tjoin\tactive\n' "${role}"
        printf 'ROOM\t@alice:t1.internal\tquestions\tjoin\n'
        if [[ "$2" != "absent" ]]; then
            printf 'ROOM\t@alpha:t1.internal\tquestions\t%s\n' "$2"
        fi
    } >"${FIX}/t1.out"
    printf 'HUMAN\talice\tjoin\nMEMBER\tbeta\tworker\tjoin\tactive\nROOM\t@beta:t2.internal\treports\t%s\n' "$3" >"${FIX}/t2.out"
    printf '0\n' >"${FIX}/t1.rc"
    printf '0\n' >"${FIX}/t2.rc"
}

# fake_sessions <alpha's session: ccy|lxc|none> — beta is always in ccy container c2.
fake_sessions() {
    printf 'c2\n' >"${FIX}/ps.out"
    printf '{"team":"t2","user_id":"@beta:t2.internal"}\n' >"${FIX}/c2.seated"
    MEMBERS_JSON="${FIX}/members.json"
    printf '[]\n' >"${MEMBERS_JSON}"
    case "$1" in
        ccy)
            printf 'c2\nc1\n' >"${FIX}/ps.out"
            printf '{"team":"t1","user_id":"@alpha:t1.internal"}\n{"team":"t9","user_id":"@alpha:t9.internal"}\n' >"${FIX}/c1.seated"
            ;;
        lxc) SESSIONS_LXC=$'lxc\tlxc-a\tu1\t/home/u1\tt1\t@alpha:t1.internal' ;;
        none) ;;
    esac
}

# fake_join <alpha questions> <beta reports> — deploy 2's wait over fixtures, with no waiting.
fake_join() {
    fake_rosters alpha "$1" "$2" || return 1
    fake_sessions none || return 1
    SESSIONS="$(ccy_sessions)" || return 1
    JOIN_WAIT_S=0
    pick_teams && rooms_joined
}

# fake_join_nocoord — t1 has no coordinator: no questions join is owed.
fake_join_nocoord() {
    fake_rosters none absent join || return 1
    fake_sessions none || return 1
    SESSIONS="$(ccy_sessions)" || return 1
    JOIN_WAIT_S=0
    pick_teams && rooms_joined
}

# fake_pick <t1's coordinator: alpha|none> <alpha's session: ccy|lxc|none> — the coordinator
# is found in its session, of either kind, or refused.
SESSIONS_LXC=""
fake_pick() {
    fake_rosters "$1" join join || return 1
    SESSIONS_LXC=""
    fake_sessions "$2" || return 1
    SESSIONS="$(ccy_sessions)" || return 1
    if [[ -n "${SESSIONS_LXC}" ]]; then
        SESSIONS="${SESSIONS}"$'\n'"${SESSIONS_LXC}"
    fi
    QTEAM=""
    QHANDLE=""
    COORD=""
    pick_teams && pick_coordinator || return 1
    if [[ "$(session_field "${COORD}" 1)" != "$2" ]] || [[ "$(session_field "${COORD}" 6)" != "@alpha:t1.internal" ]]; then
        printf 'the fixture coordinator was found as %s\n' "${COORD}" >&2
        return 1
    fi
}

# attached_are <IDs, space separated> <Q<n>> <history output> — attached_ids picks exactly these.
attached_are() {
    local got
    got="$(attached_ids "$2" "$3" | paste -sd ' ')"
    if [[ "${got}" != "$1" ]]; then
        printf 'attached to %s: "%s", not "%s"\n' "$2" "${got}" "$1" >&2
        return 1
    fi
}

# fake_refused <status> <answer JSON>
fake_refused() {
    printf '%s\n' "$2" >"${FIX}/answer.json" && refused "a fixture request" "$1" "${FIX}/answer.json"
}
