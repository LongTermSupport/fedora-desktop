# shellcheck shell=bash
# _acceptance-m7.inc.bash — the legs and checks of Plan 00201's acceptance.bash (milestone M7,
# the two-team acceptance leg). A separate file for the reason Plan 00161's are: legs are called
# through plan_deploy_leg and check_leg, which ShellCheck cannot see through (SC2329), and
# suppression is banned. Sourced, never executed: no shell options, no `exit`. Every function
# returns non-zero on its first failure, explicitly, since a leg runs where errexit is off. A
# check returns 0 PASS or 1 FAIL.
#
# THE TEAMS are two throwaway teams, made by agent-bus-install from this checkout as the play
# runs it, with descriptions, both trusting this checkout's GitHub owner, and purged at the end.
# They are declared only in a host_vars file in the run directory, which ccy-teams and the
# launches read through CCY_TEAMS_HOST_VARS; the owner's host_vars and teams are never read for
# anything but the check that neither name is declared there.
#
# THE SESSIONS are real headless ccy sessions in this checkout (the owner's launcher, image,
# plugin and hooks), one at a time: `multi` on `--teams 1@A,1@B`, then `plain` with no --teams.
# Each reads stream-json from a fifo and gets one user line, so its `init` line names its
# session ID. What a session does is judged inside its container with `podman exec` (the
# session's own pingbus, its SessionStart record, the hooks daemon's handler) and from what ccy
# printed, never from the model's prose.
#
# Reads acceptance.bash's globals (TEAM_A, TEAM_B, DESC_A, DESC_B, SEAT, SEAT_LIST, HUMAN,
# BUS_ADDRESS, INSTALLER, AGENT_BUS, CCY_LAUNCHER, CHECK, REPORT, HOST_VARS_FILE, OWNER_HOST_VARS)
# and the plan library's PLAN_RUN_DIR, PLAN_REPO_ROOT and PLAN_SCRIPT_DIR.

readonly M7_TEAMS=("${TEAM_A}" "${TEAM_B}")
readonly M7_CCY_DIR="${PLAN_REPO_ROOT}/.claude/ccy"
readonly M7_SEATS_DIR="${M7_CCY_DIR}/pingbus/seats"
readonly M7_LAST_LAUNCH="${M7_CCY_DIR}/.last-launch.conf"
readonly M7_LAST_LAUNCH_SAVED="${PLAN_RUN_DIR}/last-launch.conf.before"
readonly M7_MEMBERS="${PLAN_RUN_DIR}/members"
readonly M7_INSTALL_STATE="/var/lib/agent-bus-install"
readonly M7_STATE="/var/lib/agent-bus"
readonly M7_APPLIED="/var/lib/agent-bus-applied"
#: The checkout as a ccy container sees it (claude-yolo mounts $PWD there).
readonly M7_CTR_ROOT="/workspace"
readonly M7_CTR_RUN_DIR="${M7_CTR_ROOT}/${PLAN_RUN_DIR#"${PLAN_REPO_ROOT}"/}"
#: The member kit's settings in the image, which the entrypoint passes to a bus session's claude.
readonly M7_CTR_KIT_SETTINGS="/opt/claude-yolo/optional/agent-bus/settings.json"
#: The session home `pingbus seat exec` builds in a container (helpers/pingbus/seat.py).
readonly M7_CTR_PINGBUS_HOME="/tmp/pingbus-home"
#: Members added by hand, to address: a short name in both teams, and one in team A only.
readonly M7_PEER_REPO="m7peer"
readonly M7_SOLO_REPO="m7solo"
readonly M7_MEMBER_HOST="m7lab"
readonly M7_PEER_SHORT="${M7_PEER_REPO}.1"
readonly M7_SOLO_SHORT="${M7_SOLO_REPO}.1"
readonly M7_PROMPT="Plan 00201 M7 acceptance: the instructions arrive on stdin"
readonly M7_TURN="This session is an automated acceptance test of fedora-desktop Plan 00201 (milestone M7) in the owner's checkout. Run no command, read no file and change nothing. Reply with the single word READY, then end your turn with the line: STOPPING BECAUSE: acceptance turn done"
readonly M7_MODEL="haiku"
#: In a container: a session's per-session record (helpers/pingbus/hooks.py's default directory,
#: as root there): `read` prints it, `exists` prints present or absent.
readonly M7_RECORD_PY='
import os, sys
path = os.path.join(f"/tmp/pingbus-sessions-{os.getuid()}", sys.argv[2])
if sys.argv[1] == "read":
    with open(path, encoding="ascii") as handle:
        print(handle.read())
else:
    print("present" if os.path.lexists(path) else "absent")
'
#: Bounds, in seconds. A ccy launch may update Claude Code in the image first.
readonly M7_START_S=600
readonly M7_TURN_S=240
readonly M7_EXIT_S=60
readonly M7_READY_S=120
readonly M7_POLL_S=3
TEAMS_PRESENT=0
SESSION_STARTED=0
LAUNCH_CONF_STATE=""
SESSION_NAME=""
SESSION_PID=""
SESSION_FD=""
SESSION_CTR=""
SESSION_ID=""

m7_ev() { printf '%s/%s' "${PLAN_RUN_DIR}" "$1"; }
evidence() { printf -- '- %s\n' "$*" >>"${REPORT}"; }

# m7_installer <label> <args...> — agent-bus-install as root; stdout kept as <label>.out.
m7_installer() {
    local label="$1" status=0
    shift
    { sudo -n "${INSTALLER}" "$@"; } >"$(m7_ev "${label}.out")" || status=$?
    cat -- "$(m7_ev "${label}.out")"
    if [[ "${status}" -ne 0 ]]; then
        printf '[FAIL] agent-bus-install %s exited %d (sudo saying a password is required means its timestamp lapsed: run again)\n' \
            "$1" "${status}" >&2
        return 1
    fi
}

m7_sudo_alive() {
    if ! sudo -n true; then
        printf '[FAIL] sudo'\''s credential lapsed; a headless launch and the teardown need it: run acceptance.bash again\n' >&2
        return 1
    fi
}

m7_seat_cmd() { (cd -- "${PLAN_REPO_ROOT}" && "${AGENT_BUS}" seat "$@"); }

# m7_alive — the live session's ccy is still running; else FAIL with its stderr.
m7_alive() {
    if [[ -z "${SESSION_PID}" ]] || ps -p "${SESSION_PID}" -o pid= >/dev/null; then
        return 0
    fi
    printf '[FAIL] the ccy session %s ended; its stderr (%s):\n' "${SESSION_NAME}" "$(m7_ev "${SESSION_NAME}/session.err")" >&2
    cat -- "$(m7_ev "${SESSION_NAME}/session.err")" >&2
    return 1
}

# m7_poll <timeout-s> <what> <cmd...> — run cmd every M7_POLL_S s until it returns 0; 3 is "not
# yet", anything else, a session that ended, a lapsed sudo or the timeout is a FAIL.
m7_poll() {
    local timeout="$1" what="$2" status deadline
    shift 2
    deadline=$((SECONDS + timeout))
    while true; do
        status=0
        "$@" || status=$?
        case "${status}" in
            0) return 0 ;;
            3) ;;
            *)
                printf '[FAIL] %s: the check itself failed (status %d)\n' "${what}" "${status}" >&2
                return 1
                ;;
        esac
        m7_alive || return 1
        m7_sudo_alive || return 1
        if ((SECONDS >= deadline)); then
            printf '[FAIL] %s: not within %d s (evidence: %s)\n' "${what}" "${timeout}" "${PLAN_RUN_DIR}" >&2
            return 1
        fi
        sleep "${M7_POLL_S}"
    done
}

# ── before anything: what the run needs, and that the names are free ──────────────────────

m7_prerequisites() {
    local tool declared team
    for tool in "${CCY_LAUNCHER}" "${AGENT_BUS}" "${INSTALLER}"; do
        if [[ ! -x "${tool}" ]]; then
            printf '[FAIL] %s is not installed: run play-claude-yolo.yml and play-agent-bus.yml\n' "${tool}" >&2
            return 1
        fi
    done
    for tool in ccy-teams podman curl firewall-cmd ansible-inventory; do
        if ! command -v "${tool}" >/dev/null; then
            printf '[FAIL] %s is not on PATH: its play installs it (ccy-teams: play-claude-yolo.yml)\n' "${tool}" >&2
            return 1
        fi
    done
    if [[ ! -f "${PLAN_REPO_ROOT}/.claude/hooks-daemon/scripts/lib/resolve_venv.sh" ]]; then
        printf '[UNKNOWN] OWNER: this checkout has no hooks daemon (.claude/hooks-daemon): install it (hooks-daemon skill), then run again\n' >&2
        return 2
    fi
    if [[ ! -f "${M7_LAST_LAUNCH}" ]]; then
        printf '[UNKNOWN] OWNER: launch ccy interactively in %s once: a headless launch reuses the token and SSH choice Quick Launch saves\n' \
            "${PLAN_REPO_ROOT}" >&2
        return 2
    fi
    if [[ -e "${OWNER_HOST_VARS}" ]]; then
        declared="$(cd -- "${PLAN_REPO_ROOT}" && python3 -m helpers.agent_bus.declare declared --file "${OWNER_HOST_VARS}")" || return 1
        for team in "${M7_TEAMS[@]}"; do
            if awk -F'\t' -v team="${team}" '$1 == "TEAM" && $2 == team { found = 1 } END { exit !found }' <<<"${declared}"; then
                printf '[FAIL] the host_vars declare a team named %s: this run would purge it. Rename the acceptance teams in acceptance.bash\n' "${team}" >&2
                return 1
            fi
        done
    fi
    printf '==> ccy, agent-bus, ccy-teams and the hooks daemon are here; %s and %s are not declared by the owner\n' "${TEAM_A}" "${TEAM_B}"
}

# m7_seat_listing [running] — "ID<TAB><seat>@<team>[,...]" for every ccy container launched with a
# desired membership (Plan 00203 M2: PINGBUS_MEMBERSHIP, the read-only mount at
# /run/ccy/membership), its seats read from the host's own membership.json, the mount's source;
# every container, or with `running` the running ones. It replaces the ccy-seats label M2 retired.
m7_seat_listing() {
    local id src seats
    local filter=()
    [[ "${1:-}" != running ]] || filter=(--filter status=running)
    while read -r id; do
        [[ -n "${id}" ]] || continue
        src="$(podman inspect --format '{{range .Mounts}}{{if eq .Destination "/run/ccy/membership"}}{{.Source}}{{end}}{{end}}' "${id}")" || return 1
        [[ -n "${src}" ]] || continue
        seats="$(jq -r '[.seats[] | "\(.seat)@\(.team)"] | join(",")' "${src}/membership.json")" || return 1
        [[ -z "${seats}" ]] || printf '%s\t%s\n' "${id}" "${seats}"
    done < <(podman ps -a --filter label=ccy=true "${filter[@]}" --format '{{.ID}}')
}

# m7_remove_containers — every container holding a seat of the acceptance teams, running or not.
m7_remove_containers() {
    local listing ids id
    listing="$(m7_seat_listing)" || return 1
    ids="$("${CHECK[@]}" seat-containers "${TEAM_A},${TEAM_B}" <<<"${listing}")" || return 1
    for id in ${ids}; do
        podman rm -f -t 10 -- "${id}" >/dev/null || return 1
        printf '==> removed the ccy container %s (an acceptance seat)\n' "${id}"
    done
}

# m7_delete_seat_dirs — the acceptance teams' seat directories in this checkout, for a team that
# is purged (or about to be): parking needs the team. Every other team's seats stay.
m7_delete_seat_dirs() {
    local team
    for team in "${M7_TEAMS[@]}"; do
        if [[ -e "${M7_SEATS_DIR}/${team}" || -L "${M7_SEATS_DIR}/${team}" ]]; then
            rm -rf -- "${M7_SEATS_DIR:?}/${team}" || return 1
            printf '==> deleted %s/%s\n' "${M7_SEATS_DIR}" "${team}"
        fi
    done
}

m7_remove_teams() {
    local team
    for team in "${M7_TEAMS[@]}"; do
        m7_installer "remove-$1-${team}" remove --team "${team}" --purge || return 1
    done
    TEAMS_PRESENT=0
}

# m7_restore_polluted_launch_conf — a run killed outright (no trap) after its first launch
# leaves the Quick Launch record naming the acceptance seats. Put back the newest clean copy an
# earlier run saved, or FAIL naming the file: saving it now would make the pollution permanent.
m7_restore_polluted_launch_conf() {
    local runs="${PLAN_RUN_DIR%/*}" stamp copy
    if "${CHECK[@]}" launch-conf-clean "${M7_LAST_LAUNCH}" "${TEAM_A},${TEAM_B}"; then
        return 0
    fi
    while IFS= read -r stamp; do
        copy="${runs}/${stamp}/last-launch.conf.before"
        if [[ "${runs}/${stamp}" == "${PLAN_RUN_DIR}" || ! -f "${copy}" ]]; then
            continue
        fi
        if "${CHECK[@]}" launch-conf-clean "${copy}" "${TEAM_A},${TEAM_B}"; then
            cp -p -- "${copy}" "${M7_LAST_LAUNCH}" || return 1
            printf '==> the Quick Launch record named the acceptance seats (a killed run): restored from %s\n' "${copy}"
            return 0
        fi
    done < <(find "${runs}" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | LC_ALL=C sort -r)
    printf '[FAIL] %s names the acceptance seats (a run was killed after launching) and no earlier run saved a clean copy under %s: launch ccy interactively in this checkout once, then run again\n' \
        "${M7_LAST_LAUNCH}" "${runs}" >&2
    return 1
}

# m7_own_or_absent <team> — an installed team of this name is this acceptance's own (its
# description says so), else FAIL: it is never purged.
m7_own_or_absent() {
    local team="$1" file
    file="$(m7_ev "${team}.installed-team.json")"
    if sudo -n test -e "${M7_STATE}/${team}/team.json"; then
        { sudo -n cat -- "${M7_STATE}/${team}/team.json"; } >"${file}" || return 1
        "${CHECK[@]}" expect-own-team "${file}" "${team}" "Plan 00201 acceptance" || return 1
        printf '==> %s is installed, left by an earlier acceptance run\n' "${team}"
    elif sudo -n test -e "${M7_INSTALL_STATE}/${team}"; then
        printf '[FAIL] %s has install state (%s) but no team file to say whose it is: not purged; remove it by hand if it is this acceptance'\''s\n' \
            "${team}" "${M7_INSTALL_STATE}/${team}" >&2
        return 1
    fi
}

# m7_leftovers — what an interrupted run left: the Quick Launch record, containers, seat
# directories, and the teams, each purged only once it is known to be this acceptance's.
m7_leftovers() {
    local team
    m7_restore_polluted_launch_conf || return 1
    for team in "${M7_TEAMS[@]}"; do
        m7_own_or_absent "${team}" || return 1
    done
    m7_remove_containers || return 1
    m7_delete_seat_dirs || return 1
    m7_remove_teams leftover
}

# ── the teams ────────────────────────────────────────────────────────────────────────────

m7_resolve_bus_address() {
    local inventory
    if [[ -z "${BUS_ADDRESS}" ]]; then
        inventory="$(cd -- "${PLAN_REPO_ROOT}" && ansible-inventory --host localhost </dev/null)" || return 1
        BUS_ADDRESS="$(python3 -I -c '
import json, sys
value = json.load(sys.stdin).get("agent_bus_address", "")
print(value if isinstance(value, str) else "")
' <<<"${inventory}")" || return 1
    fi
    if [[ -z "${BUS_ADDRESS}" ]]; then
        printf '[FAIL] no bus address: set agent_bus_address in the host_vars localhost.yml, or pass --bus-address=<ip>\n' >&2
        return 1
    fi
    printf '==> the teams listen on the bus address %s\n' "${BUS_ADDRESS}"
}

# m7_write_teams — the two team files (descriptions, the checkout's GitHub owner trusted, ports
# free now) and the host_vars file declaring exactly them.
m7_write_teams() {
    local url org ports
    url="$(git -C "${PLAN_REPO_ROOT}" remote get-url origin)" || return 1
    org="$("${CHECK[@]}" github-owner "${url}")" || return 1
    mapfile -t ports < <("${CHECK[@]}" bus-ports 2 "${OWNER_HOST_VARS}")
    if [[ "${#ports[@]}" -ne 2 ]]; then
        printf '[FAIL] no two free ports in the bus range (declare.py'\''s finder; the reason is above)\n' >&2
        return 1
    fi
    "${CHECK[@]}" team-file "${TEAM_A}" "${ports[0]}" "${BUS_ADDRESS}" "${HUMAN}" "${org}" "${DESC_A}" \
        >"$(m7_ev "${TEAM_A}.team.json")" || return 1
    "${CHECK[@]}" team-file "${TEAM_B}" "${ports[1]}" "${BUS_ADDRESS}" "${HUMAN}" "${org}" "${DESC_B}" \
        >"$(m7_ev "${TEAM_B}.team.json")" || return 1
    "${CHECK[@]}" host-vars "$(m7_ev "${TEAM_A}.team.json")" "$(m7_ev "${TEAM_B}.team.json")" >"${HOST_VARS_FILE}" || return 1
    printf '==> team files (org %s, ports %s and %s) and %s:\n' "${org}" "${ports[0]}" "${ports[1]}" "${HOST_VARS_FILE}"
    cat -- "${HOST_VARS_FILE}"
}

m7_install_teams() {
    local team
    TEAMS_PRESENT=1 # before the call: a half-installed team still needs removing
    for team in "${M7_TEAMS[@]}"; do
        m7_installer "team-${team}" team --team-file "$(m7_ev "${team}.team.json")" || return 1
    done
}

# m7_rule_present <zone> <rule> <store> — 0 present, 1 absent, 2 firewall-cmd could not say.
m7_rule_present() {
    local zone="$1" rule="$2" store="$3" answer status=0
    local -a scope=()
    [[ "${store}" == runtime ]] || scope=(--permanent)
    answer="$(sudo -n firewall-cmd "${scope[@]}" --zone="${zone}" --query-rich-rule="${rule}")" || status=$?
    case "${status}:${answer}" in
        0:yes) return 0 ;;
        1:no) return 1 ;;
        *)
            printf '[FAIL] firewall-cmd --query-rich-rule (%s, zone %s) exited %d saying %s\n' "${store}" "${zone}" "${status}" "${answer}" >&2
            return 2
            ;;
    esac
}

# m7_record_rules — each team's firewalld rules as the installer recorded them, and each one
# present now: the teardown must remove exactly these.
m7_record_rules() {
    local team record zone rule store status count
    for team in "${M7_TEAMS[@]}"; do
        record="$(m7_ev "${team}.firewalld.rules")"
        { sudo -n cat -- "${M7_INSTALL_STATE}/${team}/firewalld.rules"; } >"${record}" || return 1
        count="$(grep -c . "${record}")" || {
            printf '[FAIL] the installer recorded no firewalld rule for %s\n' "${team}" >&2
            return 1
        }
        while IFS=$'\t' read -r zone rule; do
            for store in permanent runtime; do
                status=0
                m7_rule_present "${zone}" "${rule}" "${store}" || status=$?
                if [[ "${status}" -ne 0 ]]; then
                    printf '[FAIL] %s rule of %s not present (%s): %s\n' "${store}" "${team}" "${zone}" "${rule}" >&2
                    return 1
                fi
            done
        done <"${record}"
        printf '==> %s: %d firewalld rule(s) recorded and present\n' "${team}" "${count}"
    done
}

# m7_add_member <team> <repo> — a worker of type host, added by hand; its token is not needed.
m7_add_member() {
    local out="${M7_MEMBERS}/$1/$2"
    mkdir -p -- "${M7_MEMBERS}/$1" || return 1
    sudo -n "${AGENT_BUS}" add-member "$1" "--repo=$2" --seat=1 "--host=${M7_MEMBER_HOST}" --type=host \
        --role=worker "--address=${BUS_ADDRESS}" "--out=${out}" || return 1
    rm -f -- "${out}/token" || return 1
}

m7_add_members() {
    m7_add_member "${TEAM_A}" "${M7_PEER_REPO}" || return 1
    m7_add_member "${TEAM_B}" "${M7_PEER_REPO}" || return 1
    m7_add_member "${TEAM_A}" "${M7_SOLO_REPO}" || return 1
    printf '==> %s in both teams, %s in %s only\n' "${M7_PEER_SHORT}" "${M7_SOLO_SHORT}" "${TEAM_A}"
}

# ── descriptions on the host ─────────────────────────────────────────────────────────────

# D1 — `ccy-teams --list`, piped, prints each team's description whole on its team row.
m7_check_list() {
    local out err status=0
    out="$(m7_ev ccy-teams-list.out)"
    err="$(m7_ev ccy-teams-list.err)"
    CCY_TEAMS_HOST_VARS="${HOST_VARS_FILE}" NO_COLOR=1 ccy-teams --list </dev/null >"${out}" 2>"${err}" || status=$?
    cat -- "${out}"
    if [[ "${status}" -ne 0 ]]; then
        printf '[FAIL] ccy-teams --list exited %d:\n' "${status}" >&2
        cat -- "${err}" >&2
        return 1
    fi
    "${CHECK[@]}" expect-list-row "${out}" "${TEAM_A}" "${DESC_A}" || return 1
    "${CHECK[@]}" expect-list-row "${out}" "${TEAM_B}" "${DESC_B}" || return 1
    evidence "D1 ccy-teams --list shows both descriptions whole: ccy-teams-list.out"
}

# human_curl <token> <method> <base> <path> [body-file] — one client API call as the human, the
# token on stdin as a curl config line, never in argv.
human_curl() {
    local -a data=()
    if [[ -n "${5:-}" ]]; then
        data=(-H 'Content-Type: application/json' --data-binary "@$5")
    fi
    printf 'header = "Authorization: Bearer %s"\n' "$1" \
        | curl --config - --silent --show-error --fail-with-body --noproxy '*' --max-time 30 \
            -X "$2" "${data[@]}" "$3$4"
}

# m7_topic <team> <description> — the human logs in with a fresh password, joins the team room,
# reads its state and logs out; the room's topic is the description.
m7_topic() {
    local team="$1" bundle base room path password token state empty
    bundle="${M7_MEMBERS}/${team}/${M7_PEER_REPO}/member.json"
    state="$(m7_ev "${team}.room-state.json")"
    empty="$(m7_ev empty.json)"
    base="$("${CHECK[@]}" member-field "${bundle}" base_url)" || return 1
    room="$("${CHECK[@]}" member-field "${bundle}" room)" || return 1
    path="$("${CHECK[@]}" room-path "${room}")" || return 1
    password="$(sudo -n "${AGENT_BUS}" human password "${team}" "${HUMAN}")" || return 1
    token="$(printf '%s' "${password}" | "${CHECK[@]}" login-body "${HUMAN}" \
        | curl --silent --show-error --fail-with-body --noproxy '*' --max-time 30 \
            -H 'Content-Type: application/json' --data-binary @- "${base}/_matrix/client/v3/login" \
        | "${CHECK[@]}" access-token)" || return 1
    printf '{}' >"${empty}" || return 1
    human_curl "${token}" POST "${base}" "/_matrix/client/v3/rooms/${path}/join" "${empty}" >/dev/null || return 1
    human_curl "${token}" GET "${base}" "/_matrix/client/v3/rooms/${path}/state" >"${state}" || return 1
    human_curl "${token}" POST "${base}" /_matrix/client/v3/logout "${empty}" >/dev/null || return 1
    "${CHECK[@]}" expect-topic "${state}" "$2"
}

# D2 — each team room's topic is its team's description (bootstrap sets it, S-A9).
m7_check_topics() {
    m7_topic "${TEAM_A}" "${DESC_A}" || return 1
    m7_topic "${TEAM_B}" "${DESC_B}" || return 1
    evidence "D2 each team room's m.room.topic is its description: ${TEAM_A}.room-state.json, ${TEAM_B}.room-state.json"
}

# ── the sessions ─────────────────────────────────────────────────────────────────────────

# m7_save_launch_conf — the owner's Quick Launch record, which every launch rewrites (LAST_TEAMS
# included): kept here, put back by m7_restore_launch_conf.
m7_save_launch_conf() {
    "${CHECK[@]}" launch-conf-clean "${M7_LAST_LAUNCH}" "${TEAM_A},${TEAM_B}" || return 1
    cp -p -- "${M7_LAST_LAUNCH}" "${M7_LAST_LAUNCH_SAVED}" || return 1
    LAUNCH_CONF_STATE="saved"
}

m7_restore_launch_conf() {
    if [[ "${LAUNCH_CONF_STATE}" != saved ]]; then
        return 0
    fi
    cp -p -- "${M7_LAST_LAUNCH_SAVED}" "${M7_LAST_LAUNCH}" || return 1
    LAUNCH_CONF_STATE=""
    printf '==> the checkout'\''s Quick Launch record restored\n'
}

# m7_launch <name> <seats|""> — ccy headless in the checkout, reading stream-json from a fifo,
# with the throwaway host_vars for ccy-teams. Opening the fifo here lets the child's open go on.
m7_launch() {
    local ev args fd
    ev="$(m7_ev "$1")"
    mkdir -p -- "${ev}" || return 1
    mkfifo -- "${ev}/stdin.fifo" || return 1
    args=(--headless --no-restore --no-supervise)
    if [[ -n "$2" ]]; then
        args+=(--teams "$2")
    fi
    args+=(--prompt "${M7_PROMPT}" -- --input-format stream-json --output-format stream-json --verbose --model "${M7_MODEL}")
    SESSION_STARTED=1
    SESSION_NAME="$1" SESSION_CTR="" SESSION_ID=""
    # Unnamed whatever tmux pane this run is in: without TMUX a launch takes no session identity.
    (cd -- "${PLAN_REPO_ROOT}" && unset TMUX TMUX_PANE && CCY_TEAMS_HOST_VARS="${HOST_VARS_FILE}" exec "${CCY_LAUNCHER}" "${args[@]}") \
        <"${ev}/stdin.fifo" >"${ev}/session.out" 2>"${ev}/session.err" &
    SESSION_PID=$!
    exec {fd}<>"${ev}/stdin.fifo" || return 1
    SESSION_FD="${fd}"
    printf '==> session %s: ccy %s started (pid %d)\n' "$1" "${args[*]:0:5}" "${SESSION_PID}"
}

m7_ccy_containers() { podman ps -a --filter label=ccy=true --format '{{.ID}}' | LC_ALL=C sort; }

# m7_container_up <seated|plain> — the session's container is running: the one holding an
# acceptance seat, or for the plain session the one new ccy container since its launch.
m7_container_up() {
    local ids count listing
    if [[ "$1" == seated ]]; then
        listing="$(m7_seat_listing running)" || return 1
        ids="$("${CHECK[@]}" seat-containers "${TEAM_A},${TEAM_B}" <<<"${listing}")" || return 1
    else
        ids="$(m7_ccy_containers | LC_ALL=C comm -13 "$(m7_ev plain/containers-before)" -)" || return 1
    fi
    count="$(wc -w <<<"${ids}")"
    if [[ "${count}" -eq 0 ]]; then
        return 3
    fi
    if [[ "${count}" -ne 1 ]]; then
        printf '[FAIL] %d containers where one was expected (%s); for the plain session another ccy session started meanwhile: run again\n' \
            "${count}" "${ids//$'\n'/ }" >&2
        return 1
    fi
    SESSION_CTR="${ids}"
}

m7_seats_held() {
    m7_seat_cmd list >"$(m7_ev seat-list)" || return 1
    "${CHECK[@]}" expect-held "$(m7_ev seat-list)" "${SEAT_LIST}"
}

m7_turns_above_zero() {
    local n
    n="$("${CHECK[@]}" turns "$(m7_ev "${SESSION_NAME}/session.out")")" || return 1
    ((n > 0)) || return 3
}

m7_session_id() {
    SESSION_ID="$("${CHECK[@]}" session-id "$(m7_ev "${SESSION_NAME}/session.out")")"
}

# m7_start <name> <seats|""> — launch, wait for its container (and its seats held), give it its
# one turn, and read its session ID.
m7_start() {
    m7_sudo_alive || return 1
    if [[ -z "$2" ]]; then
        mkdir -p -- "$(m7_ev "$1")" || return 1
        m7_ccy_containers >"$(m7_ev "$1/containers-before")" || return 1
    fi
    m7_launch "$1" "$2" || return 1
    m7_poll "${M7_START_S}" "session $1's container running" m7_container_up "$([[ -n "$2" ]] && printf seated || printf plain)" || return 1
    printf '==> session %s: container %s\n' "$1" "${SESSION_CTR}"
    if [[ -n "$2" ]]; then
        m7_poll "${M7_START_S}" "seats ${SEAT_LIST} held" m7_seats_held || return 1
    fi
    "${CHECK[@]}" user-line "${M7_TURN}" >&"${SESSION_FD}" || return 1
    m7_poll "${M7_TURN_S}" "session $1's turn ended" m7_turns_above_zero || return 1
    m7_poll "${M7_TURN_S}" "session $1's init line" m7_session_id || return 1
    printf '==> session %s: session ID %s\n' "$1" "${SESSION_ID}"
}

# m7_end — close the session's input (claude ends, SessionEnd runs, the container goes); after
# M7_EXIT_S, stop the container and the launcher.
m7_end() {
    local fd="${SESSION_FD}" pid="${SESSION_PID}" deadline status=0
    if [[ -n "${fd}" ]]; then
        exec {fd}>&-
        SESSION_FD=""
    fi
    if [[ -z "${pid}" ]]; then
        return 0
    fi
    deadline=$((SECONDS + M7_EXIT_S))
    while ps -p "${pid}" -o pid= >/dev/null && ((SECONDS < deadline)); do
        sleep 1
    done
    if ps -p "${pid}" -o pid= >/dev/null; then
        printf '==> session %s did not end within %d s of its input closing: stopping it\n' "${SESSION_NAME}" "${M7_EXIT_S}"
        if [[ -n "${SESSION_CTR}" ]]; then
            podman rm -f -t 10 -- "${SESSION_CTR}" >/dev/null || return 1
        fi
        kill -TERM "${pid}" || printf '==> session %s: ccy had already ended\n' "${SESSION_NAME}"
    fi
    wait "${pid}" || status=$?
    SESSION_PID=""
    printf '==> session %s: ccy exited %d\n' "${SESSION_NAME}" "${status}"
}

# m7_exec <label> <stdin-file> <argv...> — a command in the live session's container, as the
# session runs (root in the container); stdout and stderr kept as <label>.out and .err.
m7_exec() {
    local base
    base="$(m7_ev "${SESSION_NAME}/$1")"
    shift
    local input="$1"
    shift
    podman exec -i "${SESSION_CTR}" "$@" <"${input}" >"${base}.out" 2>"${base}.err"
}

# m7_bus_exec <label> <stdin-file> <pingbus args...> — the session's pingbus, with the
# environment `pingbus seat exec` gives the session (its home, both teams).
m7_bus_exec() {
    local label="$1" input="$2"
    shift 2
    m7_exec "${label}" "${input}" env "PINGBUS_HOME=${M7_CTR_PINGBUS_HOME}" "PINGBUS_TEAMS=${TEAM_A},${TEAM_B}" pingbus "$@"
}

m7_show() {
    local base
    base="$(m7_ev "${SESSION_NAME}/$1")"
    printf -- '--- %s: stdout\n' "$1"
    cat -- "${base}.out"
    printf -- '--- %s: stderr\n' "$1"
    cat -- "${base}.err"
}

m7_start_multi() { m7_save_launch_conf && m7_start multi "${SEAT_LIST}"; }

# M1 — the launch block on ccy's stderr names both teams, their seats and descriptions (S-C4).
m7_check_block() {
    "${CHECK[@]}" expect-block "$(m7_ev multi/session.err)" "${SEAT}" "${TEAM_A}" "${DESC_A}" "${SEAT}" "${TEAM_B}" "${DESC_B}" || return 1
    evidence "M1 ccy's launch block names ${TEAM_A} and ${TEAM_B}, seat ${SEAT}, with their descriptions: multi/session.err"
}

m7_status_ready() {
    m7_bus_exec status /dev/null status || return 3
    "${CHECK[@]}" status-ready "$(m7_ev multi/status.out)" "${TEAM_A}" "${TEAM_B}"
}

# D3 — the session's `pingbus status` prints both teams trusted, each with its ABOUT line.
m7_check_about() {
    m7_poll "${M7_READY_S}" "pingbus status trusts both teams and prints their ABOUT lines" m7_status_ready || return 1
    m7_show status
    "${CHECK[@]}" expect-about "$(m7_ev multi/status.out)" "${TEAM_A}" "${DESC_A}" || return 1
    "${CHECK[@]}" expect-about "$(m7_ev multi/status.out)" "${TEAM_B}" "${DESC_B}" || return 1
    evidence "D3 the session's pingbus status prints an ABOUT line with each description: multi/status.out"
}

# M2 — the pingbus SessionStart hook recorded both teams for this session ID.
m7_check_record() {
    local record
    m7_exec record /dev/null python3 -c "${M7_RECORD_PY}" read "${SESSION_ID}" || {
        m7_show record
        printf '[FAIL] no per-session record for %s in the container\n' "${SESSION_ID}" >&2
        return 1
    }
    record="$(cat -- "$(m7_ev multi/record.out)")"
    if [[ "${record}" != "${TEAM_A},${TEAM_B}" ]]; then
        printf '[FAIL] the per-session record lists %s, want %s,%s\n' "${record}" "${TEAM_A}" "${TEAM_B}" >&2
        return 1
    fi
    evidence "M2 the per-session record of ${SESSION_ID} lists ${record}: multi/record.out"
}

# M3 — the session's status line command is the kit's `pingbus status-line`, which prints this
# checkout's own status line and then `bus: A,B` for this session ID: the same in any checkout.
m7_check_segment() {
    local payload
    m7_exec kit-settings /dev/null cat "${M7_CTR_KIT_SETTINGS}" || { m7_show kit-settings; return 1; }
    "${CHECK[@]}" expect-kit-status-line "$(m7_ev multi/kit-settings.out)" || return 1
    payload="$(m7_ev multi/status-line.in)"
    printf '{"session_id": "%s", "workspace": {"project_dir": "%s"}}\n' "${SESSION_ID}" "${M7_CTR_ROOT}" >"${payload}"
    if ! m7_exec status-line "${payload}" env "CLAUDE_PROJECT_DIR=${M7_CTR_ROOT}" "PINGBUS_TEAMS=${TEAM_A},${TEAM_B}" pingbus status-line; then
        m7_show status-line
        printf '[FAIL] pingbus status-line failed in the container (the reason is above)\n' >&2
        return 1
    fi
    m7_show status-line
    "${CHECK[@]}" expect-segment "$(m7_ev multi/status-line.out)" "${TEAM_A},${TEAM_B}" || return 1
    evidence "M3 the kit's settings make pingbus status-line the status line (multi/kit-settings.out), and it prints this checkout's own status line then bus: ${TEAM_A},${TEAM_B} for ${SESSION_ID}: multi/status-line.out"
}

# R1 — `say --to` a seat on team A only, no --team: the team is inferred and the SENT line names A.
m7_say_solo() {
    local status=0 outcome=0
    m7_bus_exec say-solo "$(m7_ev multi/say.in)" say --to "${M7_SOLO_SHORT}" || status=$?
    "${CHECK[@]}" say-outcome "${status}" "$(m7_ev multi/say-solo.err)" || outcome=$?
    case "${outcome}" in
        0) return 0 ;;
        3)
            printf '==> pingbus say waits for a verified team record before it can infer the team\n'
            return 3
            ;;
    esac
    m7_show say-solo
    printf '[FAIL] pingbus say --to %s exited %d\n' "${M7_SOLO_SHORT}" "${status}" >&2
    return 1
}

# R1 is polled: inference refuses (64, "no verified team record yet") until every active team's
# record is cached; that is "not yet", anything else a FAIL. Only a successful say sends.
m7_check_infer() {
    local input event
    input="$(m7_ev multi/say.in)"
    printf 'Plan 00201 M7: a note to a member of one team only\n' >"${input}" || return 1
    m7_poll "${M7_READY_S}" "pingbus say --to ${M7_SOLO_SHORT} (both team records verified)" m7_say_solo || return 1
    m7_show say-solo
    event="$("${CHECK[@]}" expect-sent "$(m7_ev multi/say-solo.out)" "${TEAM_A}")" || return 1
    evidence "R1 say --to ${M7_SOLO_SHORT} without --team went to ${TEAM_A} (event ${event}): multi/say-solo.out"
}

# R2 — `say --to` a name on both teams: exit 64, the active teams listed with their descriptions.
m7_check_ambiguous() {
    local status=0
    m7_bus_exec say-peer "$(m7_ev multi/say.in)" say --to "${M7_PEER_SHORT}" || status=$?
    m7_show say-peer
    if [[ "${status}" -ne 64 ]]; then
        printf '[FAIL] pingbus say --to %s exited %d, want 64\n' "${M7_PEER_SHORT}" "${status}" >&2
        return 1
    fi
    "${CHECK[@]}" expect-choices "$(m7_ev multi/say-peer.err)" "${TEAM_A}" "${DESC_A}" "${TEAM_B}" "${DESC_B}" || return 1
    evidence "R2 say --to ${M7_PEER_SHORT} (on both teams) refused with 64, listing both teams with their descriptions: multi/say-peer.err"
}

m7_marker_is() { [[ -f "$1" && "$(cat -- "$1")" == "$2" ]] || return 3; }

# M4 — SessionEnd removes the record. A watcher in the container, started before the input
# closes, writes into the run directory (mounted there) the moment the record is gone, while
# the container still runs; once the session ends the marker must say so.
m7_check_session_end() {
    local marker ctr_marker="${M7_CTR_RUN_DIR}/multi/session-end.marker" said
    marker="$(m7_ev multi/session-end.marker)"
    if ! podman exec "${SESSION_CTR}" test -d "${M7_CTR_RUN_DIR}/multi"; then
        printf '[FAIL] the run directory is not at %s in the container\n' "${M7_CTR_RUN_DIR}" >&2
        return 1
    fi
    podman exec -d "${SESSION_CTR}" python3 -c '
import os, sys, time
session, marker = sys.argv[1], sys.argv[2]
record = os.path.join(f"/tmp/pingbus-sessions-{os.getuid()}", session)
def say(word):
    with open(marker + ".tmp", "w") as handle:
        handle.write(word + "\n")
    os.replace(marker + ".tmp", marker)
if not os.path.exists(record):
    say("absent-at-start")
    sys.exit(0)
say("watching")
while os.path.exists(record):
    time.sleep(0.02)
say("removed")
' "${SESSION_ID}" "${ctr_marker}" >/dev/null || return 1
    m7_poll 30 "the SessionEnd watcher started" m7_marker_is "${marker}" watching || return 1
    m7_end || return 1
    said="$(cat -- "${marker}")" || return 1
    if [[ "${said}" != removed ]]; then
        printf '[FAIL] the session ended with its per-session record still in place (the watcher says %s): SessionEnd did not remove it\n' "${said}" >&2
        return 1
    fi
    evidence "M4 SessionEnd removed the record of ${SESSION_ID} before the container ended: multi/session-end.marker"
}

# P1 — a plain `ccy` is on no team: no seat label, and no PINGBUS_ variable but the marker
# PINGBUS_MEMBERSHIP=off (Plan 00203 M2; launched with no TMUX, so unnamed), on its container, no
# pingbus on its PATH, no per-session record for its session ID. With no pingbus there is no
# `pingbus status-line` either, so its status line is the checkout's own, with no bus segment.
m7_check_plain() {
    local labels env
    m7_start plain "" || return 1
    labels="$(m7_ev plain/labels.json)"
    env="$(m7_ev plain/env.json)"
    podman container inspect --format '{{json .Config.Labels}}' "${SESSION_CTR}" >"${labels}" || return 1
    podman container inspect --format '{{json .Config.Env}}' "${SESSION_CTR}" >"${env}" || return 1
    "${CHECK[@]}" expect-plain "${labels}" "${env}" || return 1
    m7_exec pingbus /dev/null sh -c 'if [ -e /usr/local/bin/pingbus ]; then echo present; else echo absent; fi' || return 1
    m7_exec record /dev/null python3 -c "${M7_RECORD_PY}" exists "${SESSION_ID}" || return 1
    if [[ "$(cat -- "$(m7_ev plain/pingbus.out)")" != absent || "$(cat -- "$(m7_ev plain/record.out)")" != absent ]]; then
        printf '[FAIL] the plain session has pingbus on its PATH or a per-session record\n' >&2
        return 1
    fi
    m7_end || return 1
    evidence "P1 a plain ccy session: no seat label or PINGBUS_ variable but PINGBUS_MEMBERSHIP=off, no pingbus (so no bus status line), no record: plain/"
}

# ── the way out ──────────────────────────────────────────────────────────────────────────

# m7_park_seats — the acceptance seats parked by `agent-bus seat remove` while their teams run.
m7_park_seats() {
    local team list=""
    for team in "${M7_TEAMS[@]}"; do
        if [[ -d "${M7_SEATS_DIR}/${team}/${SEAT}" ]]; then
            list+="${list:+,}${SEAT}@${team}"
        fi
    done
    if [[ -n "${list}" ]]; then
        m7_seat_cmd remove "${list}" || return 1
        printf '==> parked %s\n' "${list}"
    fi
}

# m7_cleanup — every session ended, the seats parked, the Quick Launch record put back.
m7_cleanup() {
    m7_end || return 1
    m7_remove_containers || return 1
    m7_sudo_alive || return 1
    m7_park_seats || return 1
    m7_delete_seat_dirs || return 1
    m7_restore_launch_conf || return 1
    SESSION_STARTED=0
}

m7_purge_teams() {
    m7_sudo_alive || return 1
    m7_remove_teams final
}

# C1 — after the purge: no firewalld rule the installer recorded, no unit running, no state
# directory or applied stamp, no seat directory; the Quick Launch record as it was.
m7_check_gone() {
    local team zone rule store status state
    for team in "${M7_TEAMS[@]}"; do
        while IFS=$'\t' read -r zone rule; do
            for store in permanent runtime; do
                status=0
                m7_rule_present "${zone}" "${rule}" "${store}" || status=$?
                if [[ "${status}" -ne 1 ]]; then
                    printf '[FAIL] the %s rule of %s is still there or unknown (%s): %s\n' "${store}" "${team}" "${zone}" "${rule}" >&2
                    return 1
                fi
            done
        done <"$(m7_ev "${team}.firewalld.rules")"
        state="$(systemctl show -p ActiveState --value "agent-bus-hs@${team}.service")" || return 1
        if [[ "${state}" == active ]]; then
            printf '[FAIL] agent-bus-hs@%s.service is still active\n' "${team}" >&2
            return 1
        fi
        if sudo -n test -e "${M7_STATE}/${team}" || sudo -n test -e "${M7_INSTALL_STATE}/${team}" \
            || [[ -e "${M7_APPLIED}/${team}.json" || -e "${M7_SEATS_DIR}/${team}" ]]; then
            printf '[FAIL] something of %s remains: its state, install record, applied stamp or seats\n' "${team}" >&2
            return 1
        fi
    done
    if ! cmp -s -- "${M7_LAST_LAUNCH_SAVED}" "${M7_LAST_LAUNCH}"; then
        printf '[FAIL] the Quick Launch record differs from the one saved before the launches\n' >&2
        return 1
    fi
    evidence "C1 both teams purged: their firewalld rules (permanent and runtime) gone, no unit active, no state, stamp or seat left; the Quick Launch record restored"
}

# On the way out of a run that stopped early (plan_on_cleanup): each step tried, each failure
# named, the teams purged last.
m7_teardown_after_stop() {
    local failed=0
    if [[ "${SESSION_STARTED}" -ne 1 && "${TEAMS_PRESENT}" -ne 1 && -z "${LAUNCH_CONF_STATE}" ]]; then
        return 0
    fi
    printf '==> the run stopped with acceptance sessions, seats or teams in place: removing them\n'
    m7_end || failed=1
    m7_remove_containers || failed=1
    if [[ "${TEAMS_PRESENT}" -eq 1 ]] && ! m7_park_seats; then
        printf '[WARN] the acceptance seats could not be parked; deleting them, as their teams are purged next\n' >&2
    fi
    m7_delete_seat_dirs || failed=1
    m7_restore_launch_conf || failed=1
    SESSION_STARTED=0
    if [[ "${TEAMS_PRESENT}" -eq 1 ]]; then
        m7_remove_teams after-stop || failed=1
    fi
    if [[ "${failed}" -ne 0 ]]; then
        printf '[FAIL] the teardown did not finish; run acceptance.bash again, which removes what is left first\n' >&2
    fi
    return "${failed}"
}
