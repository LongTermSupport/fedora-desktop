# shellcheck shell=bash
# _acceptance-stage2.inc.bash — the legs and checks of Plan 00211's acceptance.bash, stage 2
# (the joins, A1-A4, the test question) and --after-owner (A5), with their --fake fixtures.
# Sourced after _acceptance-stage1.inc.bash, never executed: no shell options, no `exit`. Every
# function returns non-zero with a message on stderr when it does not pass. The caller sets
# AGENT_BUS, CURL, HOST_PINGBUS, LXC_ATTACH, PODMAN, MEMBERS_JSON, TEAMS_JSON, RUN_TAG and
# REPORT. An offline member (its container stopped, or no running session) is named and passed
# over, never a failure (CLAUDE/AgentNotes.md); a check with no online member to run on is NOT
# JUDGED.
#
# A SESSION is a member's running pingbus this host can drive, one tab-separated line:
#   ccy <container ID> - <pingbus home> <team> <user ID>   a seat held in a running ccy
#       session (podman, label ccy=true); `pingbus seat exec` links each team's seat into the
#       session home /tmp/pingbus-home, which a `podman exec` names as PINGBUS_HOME
#   lxc <container> <user> <home> <team> <user ID>          a present agent_bus_lxc_members
#       entry whose container is online, driven through the guest path members.py uses
# The coordinator is found among them by the handle the registry gives it (`agent-bus list`),
# whatever kind of session it runs in.

readonly GUEST_PATH="/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
# The ccy session home is this checkout's seat.py SESSION_HOME, read through Plan 00201's
# m7_check session-paths (its first line), never copied. Sourced under the caller's errexit,
# so a reader that fails ends the run here.
CCY_PINGBUS_HOME="$(python3 -I "${PLAN_REPO_ROOT}/CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py" session-paths)"
CCY_PINGBUS_HOME="${CCY_PINGBUS_HOME%%$'\n'*}"
[[ "${CCY_PINGBUS_HOME}" == /* ]] || { printf '[FATAL] m7_check session-paths named no session home: %s\n' "${CCY_PINGBUS_HOME}" >&2; return 1; }
readonly CCY_PINGBUS_HOME
# stage2_value <file> <name> — m7_check product-value: a named text of this checkout's product,
# never a copy. Called at source time, under the caller's errexit, like the read above.
stage2_value() {
    python3 -I "${PLAN_REPO_ROOT}/CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py" \
        product-value "$@"
}
# A team's state directory in its bundle (config.py STATE_DIR); its inbox, consumed and dropped
# log there (inbox.py), which WHERE_PY is handed; and the human's Element acceptance record and
# the Flatpak Element profile under a home (config.py), which the A3 fixture writes.
TEAM_STATE_DIR="$(stage2_value helpers/pingbus/config.py STATE_DIR)"
# One read per assignment: in an array literal only the last substitution's status reaches errexit.
INBOX_NAMES=()
for inbox_name in INBOX_DIR CONSUMED_DIR DROPPED_LOG; do
    INBOX_NAMES+=("$(stage2_value helpers/pingbus/inbox.py "${inbox_name}")")
done
unset inbox_name
ELEMENT_ACCEPTED_DIR="$(stage2_value helpers/pingbus/config.py ELEMENT_ACCEPTED_DIR)"
ELEMENT_ACCEPTED_FILE="$(stage2_value helpers/pingbus/config.py ELEMENT_ACCEPTED_FILE)"
ELEMENT_FLATPAK_DIR="$(stage2_value helpers/pingbus/config.py ELEMENT_FLATPAK_DIR)"
readonly TEAM_STATE_DIR INBOX_NAMES ELEMENT_ACCEPTED_DIR ELEMENT_ACCEPTED_FILE ELEMENT_FLATPAK_DIR
# Each team's member.json in a ccy session home, one after another (none: no seat is held).
readonly CCY_SEATED_SH="for f in ${CCY_PINGBUS_HOME}/*/member.json; do if [ -f \"\$f\" ]; then cat \"\$f\"; fi; done"
# where.py <state dir> <inbox> <consumed> <dropped log> <event ID...>: `STATE present|missing`,
# then per event ID `<ID> <inbox,consumed|-> <dropped|kept>`, read in the session's own team
# state directory by the names INBOX_NAMES holds.
readonly WHERE_PY='import os, sys
state, inbox, consumed, dropped_log = sys.argv[1:5]
print("STATE", "present" if os.path.isdir(state) else "missing", sep="\t")
dropped = set()
log = os.path.join(state, dropped_log)
if os.path.exists(log):
    with open(log, encoding="utf-8") as handle:
        dropped = {line.split("\t")[1] for line in handle if line.count("\t") >= 1}
for event in sys.argv[5:]:
    where = [d for d in (inbox, consumed) if os.path.exists(os.path.join(state, d, event + ".json"))]
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
    printf '%s/%s/%s\n' "${home}" "${team}" "${TEAM_STATE_DIR}"
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

# lxc_sessions — one session line per present agent_bus_lxc_members entry whose container is
# online. An entry whose container is not running is offline: named, and not a session.
lxc_sessions() {
    local entries entry containers online container user team home uid
    local -a names
    containers="$(declared_containers)" || return 1
    if [[ -z "${containers}" ]]; then
        return 0
    fi
    mapfile -t names <<<"${containers}"
    online="$(online_containers "${names[@]}")" || return 1
    entries="$(jq -c --arg online "${online}" '($online | split("\n")) as $up
        | .[] | select((.state // "present") == "present" and (.container as $c | any($up[]; . == $c)))' \
        "${MEMBERS_JSON}")" || return 1
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
            printf 'container %s (online): %s has no readable pingbus bundle for team %s: play-agent-bus.yml writes it into an online container, so run that play (meta-deploy)\n' \
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
    printf 'sessions: %s seat(s) held in running ccy sessions, %s online declared LXC member(s)\n' \
        "$(awk -F'\t' '$1 == "ccy"' <<<"${SESSIONS}" | wc -l)" "$(awk -F'\t' '$1 == "lxc"' <<<"${SESSIONS}" | wc -l)" |
        tee -a "${REPORT}"
}

# find_session <team> <handle> — the session line of that member, or nothing when it is offline
# (no running ccy session holds its seat and no online LXC member is it; a host member is not
# driven from here), which is said, naming it.
find_session() {
    local found
    found="$(awk -F'\t' -v team="$1" -v handle="$2" \
        '$5 == team && substr($6, 2, index($6, ":") - 2) == handle { print; exit }' <<<"${SESSIONS}")"
    if [[ -z "${found}" ]]; then
        printf 'team %s: the coordinator %s is not driven from here (offline, or a host member): the test question is NOT JUDGED\n' "$1" "$2" |
            tee -a "${REPORT}" >&2
        return 0
    fi
    printf '%s\n' "${found}"
}

# pick_coordinator — QTEAM, the first team whose registry names a coordinator (`agent-bus
# list`), QHANDLE that coordinator and COORD its session, empty when it is offline. No team with
# a coordinator has no questions room to accept, which is a refusal, never a pass.
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
    if [[ -z "${COORD}" ]]; then
        printf 'the questions room is judged in team %s; its coordinator %s is not driven from here (offline, or a host member), so the test question is NOT JUDGED\n' \
            "${QTEAM}" "${QHANDLE}" | tee -a "${REPORT}"
        return 0
    fi
    printf 'the questions room is judged in team %s, through its coordinator %s\n' "${QTEAM}" "$(session_label "${COORD}")" |
        tee -a "${REPORT}"
}

# ── the joins (SPEC §5.3 leg 3, and A2's running members) ────────────────────────────────

# not_joined <online|offline> — online: one `<team> <user ID> <room> <membership>` line per
# account that has not joined a room it must: each online coordinator its team's questions
# room, and each seat held in a running ccy session its team's reports room (membership `-`
# when there is no ROOM line). offline: one `<team> <handle>` line per coordinator in no session
# this host can drive, which is offline and whose join is not judged. An LXC member's session
# cannot be seen from the host, so its reports join is judged by A4.
not_joined() {
    local which="$1" team roster running online
    while IFS= read -r team; do
        roster="$("${AGENT_BUS[@]}" list "${team}")" || return 1
        running="$(awk -F'\t' -v team="${team}" '$1 == "ccy" && $5 == team { print $6 }' <<<"${SESSIONS}")"
        online="$(awk -F'\t' -v team="${team}" '$5 == team { print substr($6, 2, index($6, ":") - 2) }' <<<"${SESSIONS}")"
        awk -F'\t' -v team="${team}" -v running="${running}" -v online="${online}" -v which="${which}" '
            BEGIN { n = split(running, ids, "\n"); for (i = 1; i <= n; i++) if (ids[i] != "") want[ids[i] "\treports"] = 1
                    n = split(online, hs, "\n"); for (i = 1; i <= n; i++) if (hs[i] != "") on[hs[i]] = 1 }
            $1 == "MEMBER" && $3 == "coordinator" && !($2 in on) { if (which == "offline") printf "%s\t%s\n", team, $2 }
            $1 == "MEMBER" && $3 == "coordinator" && ($2 in on) { coordinator[$2] = 1 }
            which == "offline" { next }
            $1 == "ROOM" { seen[$2 "\t" $3] = $4; local_[$2] = substr($2, 2, index($2, ":") - 2) }
            END {
                if (which == "offline") exit
                for (u in local_) if (local_[u] in coordinator) want[u "\tquestions"] = 1
                for (h in coordinator) { found = 0; for (u in local_) if (local_[u] == h) found = 1
                    if (!found) printf "%s\t@%s\tquestions\t-\n", team, h }
                for (k in want) if (seen[k] != "join") { split(k, p, "\t"); printf "%s\t%s\t%s\t%s\n", team, p[1], p[2], (k in seen ? seen[k] : "-") }
            }' <<<"${roster}" || return 1
    done <<<"${TEAMS}"
}

# rooms_joined — every online coordinator has joined its questions room, and every running ccy
# seat its reports room, within JOIN_WAIT_S; else each that has not is named. An offline
# coordinator is named and its join is not judged: it joins when it comes online.
JOIN_WAIT_S=180
JOIN_POLL_S=10
rooms_joined() {
    local pending offline deadline=$((SECONDS + JOIN_WAIT_S)) team user room membership
    offline="$(not_joined offline)" || return 1
    while IFS=$'\t' read -r team user; do
        if [[ -n "${team}" ]]; then
            printf 'team %s: the coordinator %s is not driven from here (offline, or a host member): not waited for here; A1/A2 judges its joins by its live status\n' \
                "${team}" "${user}" | tee -a "${REPORT}"
        fi
    done <<<"${offline}"
    while true; do
        pending="$(not_joined online)" || return 1
        if [[ -z "${pending}" ]]; then
            printf 'every online coordinator has joined questions, and every running ccy seat (%s) reports\n' \
                "$(awk -F'\t' '$1 == "ccy"' <<<"${SESSIONS}" | wc -l)" | tee -a "${REPORT}"
            return 0
        fi
        if [[ "${SECONDS}" -ge "${deadline}" ]]; then
            while IFS=$'\t' read -r team user room membership; do
                if [[ "${membership}" == "leave" ]]; then
                    printf 'team %s: %s is online and left %s (leave): a pingbus older than deploy 1 rejected the invite; relaunch that session, then run meta-deploy again, whose play invites it again\n' \
                        "${team}" "${user}" "${room}" >&2
                else
                    printf 'team %s: %s is online and has not joined %s (%s): pingbus status in that session names why (a session started before deploy 1 runs the old pingbus and joins once relaunched; a bundle with human_text false refuses questions)\n' \
                        "${team}" "${user}" "${room}" "${membership}" >&2
                fi
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
# in questions, and every coordinator whose session is listening has joined both rooms; one
# with no live status that has not joined is offline, an OFFLINE line on a passing (rc 0) run.
# Every team is judged first.
a12_check() {
    local team out rc bad=0
    while IFS= read -r team; do
        rc=0
        out="$("${AGENT_BUS[@]}" check-rooms "${team}")" || rc=$?
        case "${rc}" in
            0)
                printf -- '- team %s: the side rooms are sound\n' "${team}" | tee -a "${REPORT}"
                if [[ -n "${out}" ]]; then
                    printf '%s\n' "${out}" | tee -a "${REPORT}"
                fi
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
# loopback, taken as a seat the way `agent-bus seat take` and the keeper make one: the bundle in
# a seat directory of its own (add-member makes --out 0700, the user's), and a session home
# whose team entry is an absolute link to it. pingbus runs a host member beside the owner's
# Element profile only through that link (helpers/pingbus/config.py load_bundle), so a plain
# bundle in the home is refused. a3_release is registered before add-member runs, so a failure
# at any later step still removes it.
a3_take() {
    A3_HOME="${PLAN_RUN_DIR}/a3-pingbus"
    A3_DIR="${PLAN_RUN_DIR}/a3-seats/${QTEAM}/${RUN_TAG}"
    mkdir -m 0700 "${A3_HOME}" "${PLAN_RUN_DIR}/a3-seats" "${PLAN_RUN_DIR}/a3-seats/${QTEAM}" || return 1
    plan_on_cleanup a3_release || return 1
    "${AGENT_BUS[@]}" add-member "${QTEAM}" --repo=fedora-desktop --seat="${RUN_TAG}" --host=acceptance \
        --type=host --role=worker --address=127.0.0.1 --out="${A3_DIR}" || return 1
    ln -s -- "${A3_DIR}" "${A3_HOME}/${QTEAM}" || return 1
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
        printf 'A3: pingbus report failed for %s, for the reason above (a refusal to run a host member beside an Element profile means this user has not accepted that yet: run cc --teams once from a terminal and accept, then run meta-deploy again)\n' \
            "${A3_HANDLE}" >&2
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
    session_run "${session}" python3 -I -c "${WHERE_PY}" "${state}" "${INBOX_NAMES[@]}" "$@" </dev/null
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

# qteam_sessions — the sessions of QTEAM's online members this host can drive.
qteam_sessions() { awk -F'\t' -v team="${QTEAM}" '$5 == team' <<<"${SESSIONS}"; }

# a3_inbox_check — A3: after REPORT_SETTLE_S, the worker's report is in no inbox of any online
# session of its team this host can drive. Reports is never synced. The caller runs it only
# when there is one; with none, the inbox half of A3 is NOT JUDGED.
a3_inbox_check() {
    local sessions session out count=0 bad=0
    sessions="$(qteam_sessions)"
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

# a4_member — the first LXC session, or nothing when no declared LXC member is online.
a4_member() { awk -F'\t' '$1 == "lxc" { print; exit }' <<<"${SESSIONS}"; }

# a4_check — A4: the first online declared LXC member's `pingbus report`, run inside its
# container, lands. The caller runs it only when there is one; with none, A4 is NOT JUDGED.
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
    if awk -F'\t' -v consumed="${INBOX_NAMES[1]}" '$1 != "STATE" && ($2 != consumed || $3 != "kept") { bad = 1 } END { exit !bad }' <<<"$2"; then
        printf '%s: not every attached message was taken by the session (inbox: not yet delivered, run --after-owner again shortly; dropped: refused):\n%s\n' \
            "$1" "$2" >&2
        return 1
    fi
    printf '%s: the session took every attached message\n' "$1"
}

# a5_find — A5's question and coordinator: A5_TEAM, A5_HANDLE and A5_Q from the newest stage-2
# run's test-question file, and A5_SESSION the coordinator's session, empty when it is offline.
A5_TEAM=""
A5_HANDLE=""
A5_Q=""
A5_SESSION=""
a5_find() {
    local runs dirs saved
    runs="${PLAN_REPO_ROOT}/untracked/plan-runs/$(basename "${PLAN_SCRIPT_DIR}")/acceptance"
    dirs=()
    if [[ -d "${runs}" ]]; then
        dirs=("${runs}"/*/test-question)
    fi
    if [[ "${#dirs[@]}" -eq 0 ]] || [[ ! -f "${dirs[0]}" ]]; then
        printf 'no stage-2 run asked a test question (under %s): run deploy-stage-2.bash with the coordinator online (an earlier run had it offline, or not driven from here, and asked none)\n' \
            "${runs}" >&2
        return 1
    fi
    saved="${dirs[${#dirs[@]} - 1]}"
    IFS=$'\t' read -r A5_TEAM A5_HANDLE A5_Q <"${saved}" || return 1
    printf 'test question %s of team %s, asked by %s (%s)\n' "${A5_Q}" "${A5_TEAM}" "${A5_HANDLE}" "${saved}" |
        tee -a "${REPORT}"
    A5_SESSION="$(find_session "${A5_TEAM}" "${A5_HANDLE}")" || return 1
}

# a5_check — A5: the coordinator's `pingbus history --room questions`, in its session, shows the
# owner's reply and edit attached to the test question a5_find found, and its session took
# both. The raw history lines are kept in this run directory for Task 4.2.
a5_check() {
    local team="${A5_TEAM}" q="${A5_Q}" session="${A5_SESSION}" out ids where
    printf 'read through %s\n' "$(session_label "${session}")" | tee -a "${REPORT}"
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

# fake_a12 <teams json> [<team>=<rc>|offline...] — check-rooms answers rc (1 prints a PROBLEM
# line); `offline` answers 0 with an OFFLINE line, as for a coordinator with no live status.
fake_a12() {
    printf '%s\n' "$1" >"${TEAMS_JSON}" || return 1
    shift
    local spec team rc
    for spec in "$@"; do
        team="${spec%%=*}"
        rc="${spec#*=}"
        : >"${FIX}/${team}.out"
        if [[ "${rc}" == "1" ]]; then
            printf 'PROBLEM\tteam %s: alpha has not joined questions (leave): update pingbus\n' "${team}" >"${FIX}/${team}.out"
        elif [[ "${rc}" == "offline" ]]; then
            rc=0
            printf 'OFFLINE\tteam %s: alpha is offline (no live status in the team room)\n' "${team}" >"${FIX}/${team}.out"
        fi
        printf '%s\n' "${rc}" >"${FIX}/${team}.rc"
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
        printf 'ROOM\t@alice:t1.internal\tquestions\tjoin\nROOM\t@alpha:t1.internal\treports\tjoin\n'
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

# fake_join <alpha questions> <beta reports> [<alpha's session: ccy|none>] — deploy 2's wait
# over fixtures, with no waiting; alpha is online in ccy container c1 unless none is given.
fake_join() {
    fake_rosters alpha "$1" "$2" || return 1
    fake_sessions "${3:-ccy}" || return 1
    SESSIONS="$(ccy_sessions)" || return 1
    JOIN_WAIT_S=0
    pick_teams && rooms_joined
}

# fake_lxc_offline — a declared LXC member whose container is stopped is no session, and the
# read of the sessions does not fail on it.
fake_lxc_offline() {
    MEMBERS_JSON="${FIX}/members.json"
    printf '%s\n' '[{"team":"t1","container":"lxc-down","user":"u1"}]' >"${MEMBERS_JSON}" || return 1
    printf 'STOPPED\n' >"${FIX}/lxc-down.state" || return 1
    local got
    got="$(lxc_sessions)" || return 1
    if [[ -n "${got}" ]]; then
        printf 'the stopped member was read as a session: %s\n' "${got}" >&2
        return 1
    fi
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
# is found in its session, of either kind, or found offline (COORD empty, no failure).
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
    if [[ "$2" == "none" ]]; then
        if [[ -n "${COORD}" ]]; then
            printf 'the offline fixture coordinator was found as %s\n' "${COORD}" >&2
            return 1
        fi
        return 0
    fi
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

# fake_a3_seat <accepted|unaccepted> — a3_take over a fake agent-bus whose add-member places a
# host bundle under --out as the wrapper does (0700 directory, 0600 token), then this
# checkout's pingbus loads the team from A3_HOME as `pingbus report` does, for a user with an
# Element profile who has (or has not) accepted a seat beside it.
readonly FAKE_A3_LOAD='import pathlib, sys
sys.path.insert(0, sys.argv[1])
from helpers.pingbus import config
member = config.load_bundle(pathlib.Path(sys.argv[2]), sys.argv[3], user_home=pathlib.Path(sys.argv[4]))
print(member.member_type)'
fake_a3_seat() {
    local user="${FIX}/a3-user-$1" type
    cat >"${FIX}/a3-agent-bus" <<'EOF' || return 1
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
    remove-member) exit 0 ;;
    add-member) ;;
    *) exit 64 ;;
esac
out=""
for arg in "$@"; do
    case "${arg}" in --out=*) out="${arg#--out=}" ;; esac
done
install -d -m 0700 -- "${out}"
sn="$2.agent-bus.internal"
printf '{"protocol": 1, "team": "%s", "user_id": "@fedora-desktop.a1+acceptance.host:%s", "server_name": "%s", "base_url": "http://127.0.0.1:8448", "plain_http_hosts": ["127.0.0.1"], "token_file": "token", "admin": "@admin:%s", "room": "!AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}\n' \
    "$2" "${sn}" "${sn}" "${sn}" >"${out}/member.json"
(umask 077 && printf 'syt_ZXhhbXBsZQ_notarealtoken_0123' >"${out}/token")
EOF
    chmod 0755 "${FIX}/a3-agent-bus" || return 1
    AGENT_BUS=("${FIX}/a3-agent-bus")
    QTEAM=t1
    local PLAN_RUN_DIR="${FIX}/a3-run-$1"
    mkdir -p -- "${PLAN_RUN_DIR}" "${user}/${ELEMENT_FLATPAK_DIR}" || return 1
    if [[ "$1" == "accepted" ]]; then
        mkdir -p -- "${user}/${ELEMENT_ACCEPTED_DIR}" || return 1
        (umask 077 && printf 'accepted\n' >"${user}/${ELEMENT_ACCEPTED_DIR}/${ELEMENT_ACCEPTED_FILE}") || return 1
    fi
    a3_take || return 1
    if [[ "$1" != "accepted" ]]; then
        # Never accepted: refused, and for the Element profile, not for any other reason.
        if python3 -I -c "${FAKE_A3_LOAD}" "${PLAN_REPO_ROOT}" "${A3_HOME}" "${QTEAM}" "${user}" 2>"${FIX}/a3-refusal"; then
            printf 'the A3 seat loaded beside an Element profile never accepted\n' >&2
            return 1
        fi
        grep -qF 'a host member must not run as a user with an Element profile' "${FIX}/a3-refusal" || {
            printf 'the A3 seat was refused, but not for the Element profile:\n' >&2
            cat -- "${FIX}/a3-refusal" >&2
            return 1
        }
        return 0
    fi
    type="$(python3 -I -c "${FAKE_A3_LOAD}" "${PLAN_REPO_ROOT}" "${A3_HOME}" "${QTEAM}" "${user}")" || return 1
    if [[ "${type}" != "host" ]]; then
        printf 'the A3 worker loaded as a %s member, not host\n' "${type}" >&2
        return 1
    fi
}

# fake_refused <status> <answer JSON>
fake_refused() {
    printf '%s\n' "$2" >"${FIX}/answer.json" && refused "a fixture request" "$1" "${FIX}/answer.json"
}
