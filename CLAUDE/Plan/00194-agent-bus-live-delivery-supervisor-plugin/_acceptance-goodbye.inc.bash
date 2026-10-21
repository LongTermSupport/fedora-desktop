# shellcheck shell=bash
# _acceptance-goodbye.inc.bash — the legs and checks of Plan 00194's acceptance.bash (Task 2.0b,
# the goodbye host checks). A separate file because the legs are called through
# plan_deploy_leg and check_leg, which ShellCheck cannot see through (SC2329), and suppression
# is banned. Sourced, never executed: no shell options, no `exit`. Every function returns
# non-zero on its first failure, explicitly, since a leg runs where errexit is off.
#
# THE TEAMS are made and purged by Plan 00201's acceptance machinery (_acceptance-m7.inc.bash,
# sourced first in a host run): two throwaway teams declared only in a host_vars file in the
# run directory. This file adds what the goodbye checks need on top of it.
#
# THE SESSIONS are real interactive ccy sessions (the owner's launcher and image, the
# supervisor, the pingbus plugin), each on both teams' seat 1, one at a time, run in a scratch
# checkout made for the run (its own git repository with this checkout's origin, so the teams
# trust it, and the tracked supervisor files) on a tmux server of the run's own whose socket
# is named `ccy`, so the launcher takes the session's name from it as it does on ccy's own
# server. The owner's checkout, sessions, containers and tmux server are never touched: a
# container is matched only by its ccy-session label, which carries one of GB_SESSIONS.
#
# THE VERDICT is read from each team's room as the team's human reads it (the client API's
# /messages, the account the run logs in): the texts the seat's member posted since the
# scenario started, counted by goodbye_check.py.
#
# Reads acceptance.bash's globals (TEAM_A, TEAM_B, SEAT, SEAT_LIST, HUMAN, AGENT_BUS,
# CCY_LAUNCHER, CHECK, GB_CHECK, REPORT, HOST_VARS_FILE, OWNER_HOST_VARS, INSTALLER) and the
# plan library's PLAN_RUN_DIR, PLAN_REPO_ROOT and PLAN_SCRIPT_DIR.

readonly GB_TEAMS=("${TEAM_A}" "${TEAM_B}")
#: The tmux session names, which the launcher makes the container's ccy-session label.
readonly GB_SESSIONS=(accept194-ctrlc accept194-stop accept194-window)
#: The description every team of this acceptance starts with: how a leftover is known as ours.
readonly GB_DESC_PREFIX="Plan 00194 goodbye acceptance"
#: What a goodbye after a double Ctrl-C gives as its reason (SessionEnd `prompt_input_exit`).
readonly GB_REASON="exit"
#: The tracked files of .claude/ccy a session needs: the ignore rule and the supervisor.
readonly GB_CCY_FILES=(.gitignore ccy.env claude-supervise claude-supervise.py)
#: In a container: 0 once the pingbus SessionStart hook has written a per-session record.
readonly GB_RECORD_PY='
import os, sys
path = f"/tmp/pingbus-sessions-{os.getuid()}"
sys.exit(0 if os.path.isdir(path) and os.listdir(path) else 3)
'
#: Bounds, in seconds. A launch may update Claude Code in the image first.
readonly GB_START_S=600
readonly GB_UP_S=180
readonly GB_MASH_MAX_S=120
readonly GB_MASH_GAP="0.2"
readonly GB_EXIT_S=180
readonly GB_GONE_S=60
readonly GB_SETTLE_S=5
readonly GB_POLL_S=2
readonly GB_MAX_PAGES=40
#: The launcher's libraries, as this checkout deploys them: its signing check and the text of
#: every question a launch can wait at (ccy_known_prompts).
readonly GB_CCY_LIB="${PLAN_REPO_ROOT}/files/var/local/claude-yolo/lib"
GB_CHECKOUT=""
GB_SEATS_DIR=""
GB_SOCKET=""
GB_TOKEN=""
GB_CTR=""
GB_TMUX_STARTED=0
GB_CHECKOUT_MADE=0
GB_SEATS_TAKEN=0
GB_LIVE_PIDS=()
GB_PASSWORDS=()
GB_TOKENS=()
GB_BASES=()
GB_ROOM_PATHS=()
GB_USERS=()
GB_HANDLES=()
GB_PAGES=()
GB_FIXTURE_CASE=""

gb_ev() { printf '%s/%s' "${PLAN_RUN_DIR}" "$1"; }
gb_evidence() { printf -- '- %s\n' "$*" >>"${REPORT}"; }

# gb_paths — where the run's own checkout and tmux socket live (the socket under
# XDG_RUNTIME_DIR: a socket path is limited to 108 bytes).
gb_paths() {
    GB_CHECKOUT="${PLAN_RUN_DIR}/checkout/accept194"
    GB_SEATS_DIR="${GB_CHECKOUT}/.claude/ccy/pingbus/seats"
    GB_SOCKET="${XDG_RUNTIME_DIR}/accept194-tmux/ccy"
}

gb_tmux() { env -u TMUX -u TMUX_PANE tmux -S "${GB_SOCKET}" "$@"; }

gb_alive() { [[ -n "$1" ]] && ps -p "$1" -o pid= >/dev/null; }

# gb_poll <timeout-s> <what> <pid|""> <cmd...> — run cmd every GB_POLL_S s until it returns 0;
# 3 is "not yet". Anything else, the launcher <pid> ending, or the timeout is a FAIL.
gb_poll() {
    local timeout="$1" what="$2" pid="$3" status deadline
    shift 3
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
        if [[ -n "${pid}" ]] && ! gb_alive "${pid}"; then
            printf '[FAIL] %s: the ccy launcher (pid %s) ended first\n' "${what}" "${pid}" >&2
            return 1
        fi
        if ((SECONDS >= deadline)); then
            printf '[FAIL] %s: not within %d s (evidence: %s)\n' "${what}" "${timeout}" "${PLAN_RUN_DIR}" >&2
            return 1
        fi
        sleep "${GB_POLL_S}"
    done
}

# ── before anything: what the run needs, and that the names are free ──────────────────────

# gb_prerequisites — 0 ready; 2 an owner step is missing (nothing changed); 1 anything else.
gb_prerequisites() {
    local tool declared team conf="${PLAN_REPO_ROOT}/.claude/ccy/.last-launch.conf"
    for tool in "${CCY_LAUNCHER}" "${AGENT_BUS}" "${INSTALLER}"; do
        if [[ ! -x "${tool}" ]]; then
            printf '[FAIL] %s is not installed: run play-claude-yolo.yml and play-agent-bus.yml\n' "${tool}" >&2
            return 1
        fi
    done
    for tool in tmux podman curl git jq python3 ccy-teams firewall-cmd ansible-inventory systemctl; do
        if ! command -v "${tool}" >/dev/null; then
            printf '[FAIL] %s is not on PATH: its play installs it (tmux: play-tmux-sessions.yml, ccy-teams: play-claude-yolo.yml)\n' "${tool}" >&2
            return 1
        fi
    done
    if [[ -z "${XDG_RUNTIME_DIR:-}" || ! -d "${XDG_RUNTIME_DIR}" ]]; then
        printf '[FAIL] XDG_RUNTIME_DIR is not set to a directory: run this from the desktop session\n' >&2
        return 1
    fi
    for tool in "${GB_CCY_FILES[@]}"; do
        if [[ ! -f "${PLAN_REPO_ROOT}/.claude/ccy/${tool}" ]]; then
            printf '[FAIL] %s/.claude/ccy/%s is missing: it is tracked, so the checkout is damaged\n' "${PLAN_REPO_ROOT}" "${tool}" >&2
            return 1
        fi
    done
    if [[ -e "${OWNER_HOST_VARS}" ]]; then
        declared="$(cd -- "${PLAN_REPO_ROOT}" && python3 -m helpers.agent_bus.declare declared --file "${OWNER_HOST_VARS}")" || return 1
        for team in "${GB_TEAMS[@]}"; do
            if awk -F'\t' -v team="${team}" '$1 == "TEAM" && $2 == team { found = 1 } END { exit !found }' <<<"${declared}"; then
                printf '[FAIL] the host_vars declare a team named %s: this run would purge it. Rename the acceptance teams in acceptance.bash\n' "${team}" >&2
                return 1
            fi
        done
    fi
    if [[ ! -f "${conf}" ]]; then
        printf '[UNKNOWN] OWNER: launch ccy interactively in %s once and choose a token: the sessions use the token Quick Launch saved\n' "${PLAN_REPO_ROOT}" >&2
        return 2
    fi
    if ! GB_TOKEN="$("${GB_CHECK[@]}" last-token "${conf}")"; then
        printf '[UNKNOWN] OWNER: the saved Quick Launch choices in %s name no usable token: launch ccy interactively there once and choose one\n' "${conf}" >&2
        return 2
    fi
    printf '==> the tools are here; the sessions use the saved token %s; %s and %s are not declared by the owner\n' \
        "${GB_TOKEN}" "${TEAM_A}" "${TEAM_B}"
}

# gb_tmux_server — 0 running, 1 not running, 2 tmux could not say.
gb_tmux_server() {
    local out
    [[ -e "${GB_SOCKET}" ]] || return 1
    if out="$(gb_tmux list-sessions -F '#{session_name}' 2>&1)"; then
        return 0
    fi
    case "${out}" in
        *"no server running"* | *"error connecting"* | *"No such file"*) return 1 ;;
    esac
    printf '[FAIL] tmux list-sessions on %s: %s\n' "${GB_SOCKET}" "${out}" >&2
    return 2
}

# gb_kill_tmux — the run's own tmux server stopped (any live session's launcher gets SIGHUP and
# runs its cleanup) and its socket gone.
gb_kill_tmux() {
    local status=0
    gb_tmux_server || status=$?
    case "${status}" in
        0)
            gb_tmux kill-server || return 1
            printf '==> stopped the acceptance tmux server %s\n' "${GB_SOCKET}"
            ;;
        1) ;;
        *) return 1 ;;
    esac
    if [[ -e "${GB_SOCKET}" ]]; then
        rm -f -- "${GB_SOCKET}" || return 1
    fi
    GB_TMUX_STARTED=0
}

# gb_containers <session> [running] — the IDs of the ccy containers labelled with that session.
gb_containers() {
    local filter=(--filter "label=ccy-session=$1")
    [[ "${2:-}" != running ]] || filter+=(--filter status=running)
    podman ps -a "${filter[@]}" --format '{{.ID}}'
}

gb_remove_my_containers() {
    local name ids id
    for name in "${GB_SESSIONS[@]}"; do
        ids="$(gb_containers "${name}")" || return 1
        for id in ${ids}; do
            podman rm -f -t 10 -- "${id}" >/dev/null || return 1
            printf '==> removed the ccy container %s (session %s)\n' "${id}" "${name}"
        done
    done
}

# gb_remove_membership_dirs — the desired-membership directory ccy-teams made for each named
# session (XDG_RUNTIME_DIR/ccy/membership/<session>), the acceptance's names only.
gb_remove_membership_dirs() {
    local name dir
    for name in "${GB_SESSIONS[@]}"; do
        dir="${XDG_RUNTIME_DIR:?}/ccy/membership/${name}"
        if [[ -e "${dir}" || -L "${dir}" ]]; then
            rm -rf -- "${dir}" || return 1
            printf '==> deleted %s\n' "${dir}"
        fi
    done
}

# gb_own_or_absent <team> — an installed team of this name is this acceptance's own (its
# description says so), else FAIL: it is never purged.
gb_own_or_absent() {
    local team="$1" file
    file="$(gb_ev "${team}.installed-team.json")"
    if sudo -n test -e "/var/lib/agent-bus/${team}/team.json"; then
        { sudo -n cat -- "/var/lib/agent-bus/${team}/team.json"; } >"${file}" || return 1
        "${CHECK[@]}" expect-own-team "${file}" "${team}" "${GB_DESC_PREFIX}" || return 1
        printf '==> %s is installed, left by an earlier acceptance run\n' "${team}"
    elif sudo -n test -e "/var/lib/agent-bus-install/${team}"; then
        printf '[FAIL] %s has install state but no team file to say whose it is: not purged; remove it by hand if it is this acceptance'\''s\n' "${team}" >&2
        return 1
    fi
}

# gb_leftovers — what an interrupted run left: its tmux server, containers, membership
# directories, scratch checkouts and teams, each team purged only once it is known to be ours.
gb_leftovers() {
    local team dir
    gb_kill_tmux || return 1
    gb_remove_my_containers || return 1
    gb_remove_membership_dirs || return 1
    for dir in "${PLAN_RUN_DIR%/*}"/*/checkout; do
        if [[ -d "${dir}" && "${dir}" != "${PLAN_RUN_DIR}/checkout" ]]; then
            rm -rf -- "${dir}" || return 1
            printf '==> deleted the scratch checkout of an earlier run: %s\n' "${dir}"
        fi
    done
    for team in "${GB_TEAMS[@]}"; do
        gb_own_or_absent "${team}" || return 1
    done
    m7_remove_containers || return 1
    m7_remove_teams leftover
}

# ── the scratch checkout, the seats and the human ───────────────────────────────────────

# gb_signing_off <checkout> — the checkout's own config says its commits and tags are not
# signed. The scratch checkout never commits, and ccy reads these from the project's local
# config before ~/.gitconfig (configure_git_signing), so a session launched there with
# --no-ssh needs no key: no key of the owner's is mounted or forwarded into it.
gb_signing_off() {
    git -C "$1" config --local commit.gpgsign false || return 1
    git -C "$1" config --local tag.gpgsign false
}

# gb_make_checkout — a git repository with this checkout's origin (the teams trust its owner),
# signing off, and the tracked .claude/ccy files a session runs on: the ignore rule and the
# supervisor.
gb_make_checkout() {
    local url file
    url="$(git -C "${PLAN_REPO_ROOT}" remote get-url origin)" || return 1
    GB_CHECKOUT_MADE=1
    mkdir -p -- "${GB_CHECKOUT}/.claude/ccy" || return 1
    git -c init.defaultBranch=main init -q -- "${GB_CHECKOUT}" || return 1
    git -C "${GB_CHECKOUT}" remote add origin "${url}" || return 1
    gb_signing_off "${GB_CHECKOUT}" || return 1
    for file in "${GB_CCY_FILES[@]}"; do
        cp -p -- "${PLAN_REPO_ROOT}/.claude/ccy/${file}" "${GB_CHECKOUT}/.claude/ccy/${file}" || return 1
    done
    if ! git -C "${GB_CHECKOUT}" check-ignore -q .claude/ccy/pingbus/; then
        printf '[FAIL] the scratch checkout does not ignore .claude/ccy/pingbus/: a seat token could be committed there\n' >&2
        return 1
    fi
    printf '==> scratch checkout %s (origin %s)\n' "${GB_CHECKOUT}" "${url}"
}

# gb_human_passwords — each team human's password, asked while sudo is fresh (a seat take
# drops sudo's cached credential, agent-bus's own rule); kept in memory only.
gb_human_passwords() {
    local i
    for i in "${!GB_TEAMS[@]}"; do
        GB_PASSWORDS[i]="$(sudo -n "${AGENT_BUS}" human password "${GB_TEAMS[i]}" "${HUMAN}")" || return 1
    done
    printf '==> a password for %s in each team\n' "${HUMAN}"
}

# gb_take_seats — both seats created in the scratch checkout from this terminal, where sudo is
# primed, as Plan 00203's M6 takes them: a launch in a tmux pane has no sudo credential to use.
gb_take_seats() {
    GB_SEATS_TAKEN=1
    (cd -- "${GB_CHECKOUT}" && CCY_TEAMS_HOST_VARS="${HOST_VARS_FILE}" "${AGENT_BUS}" seat take "${SEAT_LIST}" --no-prompt) || return 1
}

# gb_human_logins — each team's human logged in and joined to the room, its token in memory;
# each seat's member (the sender to count) read from its bundle.
gb_human_logins() {
    local i team bundle base room path token empty
    empty="$(gb_ev empty.json)"
    printf '{}' >"${empty}" || return 1
    for i in "${!GB_TEAMS[@]}"; do
        team="${GB_TEAMS[i]}"
        bundle="${GB_SEATS_DIR}/${team}/${SEAT}/member.json"
        base="$("${CHECK[@]}" member-field "${bundle}" base_url)" || return 1
        room="$("${CHECK[@]}" member-field "${bundle}" room)" || return 1
        path="$("${CHECK[@]}" room-path "${room}")" || return 1
        GB_USERS[i]="$("${CHECK[@]}" member-field "${bundle}" user_id)" || return 1
        GB_HANDLES[i]="$("${GB_CHECK[@]}" handle "${GB_USERS[i]}")" || return 1
        token="$(printf '%s' "${GB_PASSWORDS[i]}" | "${CHECK[@]}" login-body "${HUMAN}" \
            | curl --silent --show-error --fail-with-body --noproxy '*' --max-time 30 \
                -H 'Content-Type: application/json' --data-binary @- "${base}/_matrix/client/v3/login" \
            | "${CHECK[@]}" access-token)" || return 1
        human_curl "${token}" POST "${base}" "/_matrix/client/v3/rooms/${path}/join" "${empty}" >/dev/null || return 1
        GB_TOKENS[i]="${token}" GB_BASES[i]="${base}" GB_ROOM_PATHS[i]="${path}"
        printf '==> %s reads the room of %s; seat %s@%s is %s\n' "${HUMAN}" "${team}" "${SEAT}" "${team}" "${GB_USERS[i]}"
        gb_evidence "seat ${SEAT}@${team} posts as ${GB_USERS[i]} (handle ${GB_HANDLES[i]})"
    done
    GB_PASSWORDS=()
}

gb_human_logouts() {
    local i empty
    empty="$(gb_ev empty.json)"
    for i in "${!GB_TOKENS[@]}"; do
        human_curl "${GB_TOKENS[i]}" POST "${GB_BASES[i]}" /_matrix/client/v3/logout "${empty}" >/dev/null || return 1
    done
    GB_TOKENS=()
}

# gb_tmux_start — the run's own tmux server, started with nothing of this run's stdio (the run
# log's pipe would never close otherwise), kept running with no session (exit-empty off) and
# keeping a dead pane (remain-on-exit) so the launcher's exit can be seen.
gb_tmux_start() {
    local conf
    conf="$(gb_ev tmux.conf)"
    if [[ ! -d "${GB_SOCKET%/*}" ]]; then
        mkdir -m 0700 -- "${GB_SOCKET%/*}" || return 1
    fi
    printf 'set-option -g exit-empty off\nset-option -g remain-on-exit on\nset-option -g history-limit 20000\n' >"${conf}" || return 1
    GB_TMUX_STARTED=1
    gb_tmux -f "${conf}" start-server </dev/null >"$(gb_ev tmux-start.out)" 2>&1 9>&- || {
        cat -- "$(gb_ev tmux-start.out)" >&2
        return 1
    }
    if [[ "$(gb_tmux show-options -g -v remain-on-exit)" != on ]]; then
        printf '[FAIL] the acceptance tmux server did not take its configuration %s\n' "${conf}" >&2
        return 1
    fi
    printf '==> tmux server %s\n' "${GB_SOCKET}"
}

# ── a session ───────────────────────────────────────────────────────────────────────────

# gb_has_session <session> — 0 the run's tmux server has it, 1 not, 2 tmux could not say.
# Once its last session has ended, the server (kept by exit-empty off) has no target to
# resolve and says exactly "no current target", which is also "not".
gb_has_session() {
    local out
    if out="$(gb_tmux has-session -t "=$1" 2>&1)"; then
        return 0
    fi
    case "${out}" in
        *"can't find session"* | *"no server running"* | *"error connecting"* | "no current target") return 1 ;;
    esac
    printf '[FAIL] tmux has-session %s: %s\n' "$1" "${out}" >&2
    return 2
}

gb_show_pane() {
    local log status=0
    log="$(gb_ev "$1/pane.log")"
    gb_has_session "$1" || status=$?
    if [[ "${status}" -eq 0 ]]; then
        gb_tmux capture-pane -p -t "=$1:" -S -80 >"$(gb_ev "$1/pane.capture")" || return 1
        printf -- '--- the pane of %s now:\n' "$1" >&2
        cat -- "$(gb_ev "$1/pane.capture")" >&2
    elif [[ -f "${log}" ]]; then
        printf -- '--- the end of what the pane of %s showed (%s):\n' "$1" "${log}" >&2
        tail -n 80 -- "${log}" >&2
    fi
}

# gb_container_up <session> — 0 with GB_CTR set once its one container runs, 3 before.
gb_container_up() {
    local ids count
    ids="$(gb_containers "$1" running)" || return 1
    count="$(wc -w <<<"${ids}")"
    if [[ "${count}" -eq 0 ]]; then
        return 3
    fi
    if [[ "${count}" -ne 1 ]]; then
        printf '[FAIL] %d containers are labelled ccy-session=%s\n' "${count}" "$1" >&2
        return 1
    fi
    GB_CTR="${ids}"
}

# gb_waiting_prompt <screen-file> — the name of the launch question the screen's last non-blank
# line starts with (ccy_known_prompts, the same text the launcher asks with), else nothing.
gb_waiting_prompt() {
    local last="" line name text prompts
    # In a bash of its own: the library's names stay out of this run's.
    prompts="$(bash -c 'source "$1" && ccy_known_prompts' gb-prompts "${GB_CCY_LIB}/common-pure.bash")" || return 1
    while IFS= read -r line; do
        line="${line%"${line##*[![:space:]]}"}"
        [[ -z "${line}" ]] || last="${line#"${line%%[![:space:]]*}"}"
    done <"$1" || return 1
    while IFS=$'\t' read -r name text; do
        if [[ -n "${name}" && -n "${last}" && "${last}" == "${text}"* ]]; then
            printf '%s\n' "${name}"
            return 0
        fi
    done <<<"${prompts}"
}

# gb_starting <session> — gb_container_up, and while the container is not up a FAIL when the
# launcher is waiting at a question: every one a flag can answer is given one (gb_launch), so
# one that stops it here has no flag, and nobody is at the run's tmux to answer it.
gb_starting() {
    local status=0 screen prompt
    gb_container_up "$1" || status=$?
    [[ "${status}" -eq 3 ]] || return "${status}"
    screen="$(gb_ev "$1/pane.screen")"
    gb_tmux capture-pane -p -t "=$1:" >"${screen}" || return 1
    prompt="$(gb_waiting_prompt "${screen}")" || return 1
    if [[ -n "${prompt}" ]]; then
        printf '[FAIL] session %s: the ccy launcher is waiting at its %s question, which no launch flag answers and nobody here can (its screen is below). Deal with what it asks about, then run this again\n' \
            "$1" "${prompt}" >&2
        gb_evidence "$1: the launcher stopped at its ${prompt} question"
        return 1
    fi
    return 3
}

# gb_launch_reason <session> — the launcher's own ERROR lines from what its pane showed (the
# recorded log, else the dead pane's capture), and nothing else of the pane: see pane-errors.
gb_launch_reason() {
    local file reason=""
    for file in "$(gb_ev "$1/pane.log")" "$(gb_ev "$1/pane.capture")"; do
        if [[ -f "${file}" ]]; then
            reason="$("${GB_CHECK[@]}" pane-errors "${file}")" || return 1
            [[ -z "${reason}" ]] || break
        fi
    done
    printf '%s\n' "${reason}"
}

# gb_launcher_ended <session> — the FAIL for a launcher that ended before its session was up,
# with its ERROR lines as the reason.
gb_launcher_ended() {
    local reason
    reason="$(gb_launch_reason "$1")" || return 1
    if [[ -z "${reason}" ]]; then
        printf '[FAIL] session %s: the ccy launcher ended before its session was up, and printed no ERROR line (its pane is above)\n' "$1" >&2
        gb_evidence "$1: the launcher ended before its session was up, with no ERROR line"
        return 0
    fi
    printf '[FAIL] session %s: the ccy launcher ended before its session was up. It said:\n' "$1" >&2
    printf '    %s\n' "${reason}" >&2
    gb_evidence "$1: the launcher ended before its session was up: ${reason//$'\n'/ / }"
}

gb_seats_held() {
    (cd -- "${GB_CHECKOUT}" && "${AGENT_BUS}" seat list) >"$(gb_ev "$1/seat-list")" || return 1
    "${CHECK[@]}" expect-held "$(gb_ev "$1/seat-list")" "${SEAT_LIST}"
}

gb_session_started() { podman exec "${GB_CTR}" python3 -c "${GB_RECORD_PY}"; }

# gb_launch <session> — ccy on both seats in its own tmux session in the scratch checkout, its
# terminal recorded to pane.log; back once its container runs, both seats are held and the
# pingbus SessionStart hook has run (Claude Code is up), plus GB_SETTLE_S for its prompt.
# Every choice a launch would ask that has a flag is given: --teams with the seats, --token,
# --no-ssh (the checkout's signing is off, gb_signing_off), --no-network, --compose skip,
# --no-restore. A question with no flag (ccy's menu of containers left with no terminal, an
# expired or failing token) is caught by gb_starting, and a launcher that ends before its
# session is up is reported by its ERROR lines. The Quick Launch record it writes is the
# scratch checkout's.
#
# gb_launch <session> stderr — the same, and the launcher's stderr copied to launcher.stderr: a
# closed window takes the pane, and every line written after it, with it. The launcher is still
# the pane's own process (the wrapper execs it), so the hangup still reaches it; tee ignores
# SIGHUP and SIGINT and carries on once the pane has gone. The launcher's stderr is then a pipe,
# so the launch counts as unattended, which with every seat named asks nothing more.
gb_launch() {
    local name="$1" ev pid wrap=()
    ev="$(gb_ev "${name}")"
    mkdir -p -- "${ev}" || return 1
    date +%s%3N >"${ev}/start-ms" || return 1
    if [[ "${2:-}" == stderr ]]; then
        wrap=(bash -c "exec \"\$@\" 2> >(trap '' HUP INT; exec tee --output-error=warn -a \"\$0\" >&2)" "${ev}/launcher.stderr")
    fi
    gb_tmux new-session -d -s "${name}" -x 200 -y 50 -c "${GB_CHECKOUT}" \
        -e "CCY_TEAMS_HOST_VARS=${HOST_VARS_FILE}" -- "${wrap[@]+"${wrap[@]}"}" \
        "${CCY_LAUNCHER}" --teams "${SEAT_LIST}" --token "${GB_TOKEN}" --no-ssh --no-network --compose skip --no-restore \; \
        pipe-pane -o "cat >>$(printf '%q' "${ev}/pane.log")" || return 1
    pid="$(gb_tmux display-message -p -t "=${name}:" '#{pane_pid}')" || return 1
    printf '%s\n' "${pid}" >"${ev}/launcher.pid" || return 1
    GB_LIVE_PIDS+=("${pid}")
    printf '==> session %s: ccy --teams %s started (launcher pid %s)\n' "${name}" "${SEAT_LIST}" "${pid}"
    if ! gb_poll "${GB_START_S}" "session ${name}: its container running" "${pid}" gb_starting "${name}" \
        || ! gb_poll "${GB_UP_S}" "session ${name}: seats ${SEAT_LIST} held" "${pid}" gb_seats_held "${name}" \
        || ! gb_poll "${GB_UP_S}" "session ${name}: the pingbus SessionStart hook ran" "${pid}" gb_session_started; then
        gb_show_pane "${name}"
        if ! gb_alive "${pid}"; then
            gb_launcher_ended "${name}"
        fi
        return 1
    fi
    printf '==> session %s: container %s, seats held, Claude Code started\n' "${name}" "${GB_CTR}"
    sleep "${GB_SETTLE_S}"
}

gb_pane_dead() { gb_tmux display-message -p -t "=$1:" '#{pane_dead}'; }

# gb_mash_ctrl_c <session> — Ctrl-C into the pane every GB_MASH_GAP s until the launcher has
# exited, so the presses go on past Claude Code's exit and the container's, through the
# launcher's cleanup.
gb_mash_ctrl_c() {
    local name="$1" start="${SECONDS}" presses=0
    while [[ "$(gb_pane_dead "${name}")" != 1 ]]; do
        if ((SECONDS - start >= GB_MASH_MAX_S)); then
            printf '[FAIL] session %s: still running after %d s of Ctrl-C (%d presses)\n' "${name}" "${GB_MASH_MAX_S}" "${presses}" >&2
            gb_show_pane "${name}"
            return 1
        fi
        gb_tmux send-keys -t "=${name}:" C-c || return 1
        presses=$((presses + 1))
        sleep "${GB_MASH_GAP}"
    done
    printf '==> session %s: %d Ctrl-C presses over %d s, until the launcher exited\n' "${name}" "${presses}" "$((SECONDS - start))"
    gb_evidence "${name}: Ctrl-C pressed ${presses} times over $((SECONDS - start)) s, on until the launcher had exited"
}

# gb_finished <session> — the launcher has exited and its container has gone; its exit status
# kept when tmux still has the dead pane, which is then closed.
gb_finished() {
    local name="$1" pid status has=0
    pid="$(cat -- "$(gb_ev "${name}/launcher.pid")")" || return 1
    gb_poll "${GB_EXIT_S}" "session ${name}: the launcher exited" "" gb_ended "${pid}" || return 1
    gb_poll "${GB_GONE_S}" "session ${name}: its container gone" "" gb_no_container "${name}" || return 1
    gb_has_session "${name}" || has=$?
    [[ "${has}" -le 1 ]] || return 1
    if [[ "${has}" -eq 0 ]]; then
        status="$(gb_tmux display-message -p -t "=${name}:" '#{pane_dead_status}')" || return 1
        printf '%s\n' "${status}" >"$(gb_ev "${name}/launcher.status")" || return 1
        printf '==> session %s: the launcher exited %s\n' "${name}" "${status}"
        gb_tmux kill-session -t "=${name}" || return 1
    fi
    gb_remove_membership_dirs
}

gb_ended() { if gb_alive "$1"; then return 3; fi; }

gb_no_container() {
    local ids
    ids="$(gb_containers "$1")" || return 1
    [[ -z "${ids}" ]] || return 3
}

# ── the verdicts ────────────────────────────────────────────────────────────────────────

# gb_page_http <team-index> <n> <from> <file> — one /messages page (newest first) as the human.
gb_page_http() {
    local query="dir=b&limit=500" from
    if [[ -n "$3" ]]; then
        from="$("${CHECK[@]}" room-path "$3")" || return 1
        query+="&from=${from}"
    fi
    human_curl "${GB_TOKENS[$1]}" GET "${GB_BASES[$1]}" "/_matrix/client/v3/rooms/${GB_ROOM_PATHS[$1]}/messages?${query}" >"$4"
}

# gb_page_fixture <team-index> <n> <from> <file> — the fake mode's page, written beforehand.
gb_page_fixture() { cp -- "$(gb_ev "fixtures/${GB_FIXTURE_CASE}/$1.$2.json")" "$4"; }

# gb_fetch_pages <pager> <team-index> <file-prefix> — GB_PAGES: every page of the room, newest
# first, until the server says there are no more.
gb_fetch_pages() {
    local pager="$1" index="$2" prefix="$3" from="" n=0 file
    GB_PAGES=()
    mkdir -p -- "${prefix%/*}" || return 1
    while ((n < GB_MAX_PAGES)); do
        n=$((n + 1))
        file="${prefix}.${n}.json"
        "${pager}" "${index}" "${n}" "${from}" "${file}" || return 1
        GB_PAGES+=("${file}")
        from="$("${GB_CHECK[@]}" next-from "${file}")" || return 1
        [[ -n "${from}" ]] || return 0
    done
    printf '[FAIL] the room has more than %d pages of history: not counted\n' "${GB_MAX_PAGES}" >&2
    return 1
}

# gb_judge <label> <sender> <handle> <since-ms> <goodbyes> <disconnects> — over GB_PAGES:
# exactly that many goodbyes and disconnected notices from the seat since the start.
gb_judge() {
    local tally
    tally="$(gb_ev "$1.tally.json")"
    "${GB_CHECK[@]}" tally "$2" "$3" "$4" "${GB_PAGES[@]}" >"${tally}" || return 1
    printf -- '--- %s: what %s posted since the start\n' "$1" "$3"
    cat -- "${tally}"
    "${GB_CHECK[@]}" expect "${tally}" "$3" "$5" "$6" "${GB_REASON}" || return 1
    gb_evidence "$1: $3 posted $5 goodbye(s) and $6 disconnected notice(s), and not both: ${tally#"${PLAN_RUN_DIR}"/}"
}

# gb_room_says <session> <goodbyes> <disconnects> — each team's room, read now.
gb_room_says() {
    local name="$1" since i
    since="$(cat -- "$(gb_ev "${name}/start-ms")")" || return 1
    for i in "${!GB_TEAMS[@]}"; do
        gb_fetch_pages gb_page_http "${i}" "$(gb_ev "${name}/${GB_TEAMS[i]}.page")" || return 1
        gb_judge "${name}/${GB_TEAMS[i]}" "${GB_USERS[i]}" "${GB_HANDLES[i]}" "${since}" "$2" "$3" || return 1
    done
}

gb_pane_clean() {
    "${GB_CHECK[@]}" pane-clean "$(gb_ev "$1/pane.log")" || return 1
    gb_evidence "$1: its terminal shows no traceback and no failed disconnect notice: $1/pane.log"
}

# ── the scenarios ───────────────────────────────────────────────────────────────────────

# G1 — Ctrl-C mashed until the launcher exits: one goodbye per team, no disconnected, and the
# launcher's exit prints no traceback and no failed `pingbus seat disconnected`.
gb_check_ctrl_c() {
    local name="${GB_SESSIONS[0]}"
    gb_launch "${name}" || return 1
    gb_mash_ctrl_c "${name}" || return 1
    gb_finished "${name}" || return 1
    gb_pane_clean "${name}" || return 1
    gb_room_says "${name}" 1 0
}

# G2 — `podman stop` of the session's own container: one disconnected per team, no goodbye.
gb_check_podman_stop() {
    local name="${GB_SESSIONS[1]}"
    gb_launch "${name}" || return 1
    printf '==> podman stop %s (labelled ccy-session=%s)\n' "${GB_CTR}" "${name}"
    podman stop -t 10 -- "${GB_CTR}" >/dev/null || return 1
    gb_finished "${name}" || return 1
    gb_pane_clean "${name}" || return 1
    gb_room_says "${name}" 0 1
}

# G3 — the tmux window of the two-seat session closed: one disconnected per seat, no goodbye.
gb_check_window_closed() {
    local name="${GB_SESSIONS[2]}"
    gb_launch "${name}" stderr || return 1
    printf '==> tmux kill-window: the window of %s closed\n' "${name}"
    gb_tmux kill-window -t "=${name}:" || return 1
    gb_finished "${name}" || return 1
    gb_keep_disconnect_log "${name}" || return 1
    gb_room_says "${name}" 0 1
}

# gb_keep_disconnect_log <session> — the launcher's stderr and the checkout's disconnect log
# (one line per step of the launcher's notice and per seat pingbus judged) shown and kept as
# evidence, whatever the verdict.
gb_keep_disconnect_log() {
    local log="${GB_SEATS_DIR%/*}/disconnect.log"
    printf -- '--- %s: the launcher stderr after the window closed\n' "$1"
    cat -- "$(gb_ev "$1/launcher.stderr")" || return 1
    [[ -f "${log}" ]] || { printf '[FAIL] %s: no disconnect log at %s\n' "$1" "${log}" >&2; return 1; }
    cp -- "${log}" "$(gb_ev "$1/disconnect.log")" || return 1
    printf -- '--- %s: the disconnect log\n' "$1"
    cat -- "${log}"
    gb_evidence "$1: launcher stderr $1/launcher.stderr, disconnect log $1/disconnect.log"
}

# ── the way out ──────────────────────────────────────────────────────────────────────────

gb_park_seats() {
    if [[ -d "${GB_SEATS_DIR}/${TEAM_A}/${SEAT}" || -d "${GB_SEATS_DIR}/${TEAM_B}/${SEAT}" ]]; then
        (cd -- "${GB_CHECKOUT}" && "${AGENT_BUS}" seat remove "${SEAT_LIST}") || return 1
        printf '==> parked %s\n' "${SEAT_LIST}"
    fi
}

# gb_wait_launchers — every launcher this run started has exited (its cleanup may still be
# telling the teams), so nothing is still posting when the teams go.
gb_wait_launchers() {
    local pid
    for pid in "${GB_LIVE_PIDS[@]+"${GB_LIVE_PIDS[@]}"}"; do
        gb_poll "${GB_EXIT_S}" "the ccy launcher pid ${pid} exited" "" gb_ended "${pid}" || return 1
    done
    GB_LIVE_PIDS=()
}

gb_delete_checkout() {
    if [[ -e "${GB_CHECKOUT}" ]]; then
        rm -rf -- "${GB_CHECKOUT}" || return 1
        printf '==> deleted the scratch checkout %s\n' "${GB_CHECKOUT}"
    fi
    GB_CHECKOUT_MADE=0
}

# gb_cleanup — the human logged out, the tmux server stopped, the seats parked, the checkout
# and the membership directories deleted.
gb_cleanup() {
    gb_human_logouts || return 1
    gb_kill_tmux || return 1
    gb_wait_launchers || return 1
    gb_remove_my_containers || return 1
    m7_sudo_alive || return 1
    gb_park_seats || return 1
    GB_SEATS_TAKEN=0
    gb_delete_checkout || return 1
    gb_remove_membership_dirs
}

# C1 — after the purge: no firewalld rule the installer recorded, no unit active, no state
# directory or applied stamp; no container, tmux server, checkout or membership directory.
gb_check_gone() {
    local team zone rule store status state name
    for team in "${GB_TEAMS[@]}"; do
        while IFS=$'\t' read -r zone rule; do
            for store in permanent runtime; do
                status=0
                m7_rule_present "${zone}" "${rule}" "${store}" || status=$?
                if [[ "${status}" -ne 1 ]]; then
                    printf '[FAIL] the %s rule of %s is still there or unknown (%s): %s\n' "${store}" "${team}" "${zone}" "${rule}" >&2
                    return 1
                fi
            done
        done <"$(gb_ev "${team}.firewalld.rules")"
        state="$(systemctl show -p ActiveState --value "agent-bus-hs@${team}.service")" || return 1
        if [[ "${state}" == active ]]; then
            printf '[FAIL] agent-bus-hs@%s.service is still active\n' "${team}" >&2
            return 1
        fi
        if sudo -n test -e "/var/lib/agent-bus/${team}" || sudo -n test -e "/var/lib/agent-bus-install/${team}" \
            || [[ -e "/var/lib/agent-bus-applied/${team}.json" ]]; then
            printf '[FAIL] something of %s remains: its state, install record or applied stamp\n' "${team}" >&2
            return 1
        fi
    done
    for name in "${GB_SESSIONS[@]}"; do
        if [[ -n "$(gb_containers "${name}")" || -e "${XDG_RUNTIME_DIR:?}/ccy/membership/${name}" ]]; then
            printf '[FAIL] a container or the membership directory of %s remains\n' "${name}" >&2
            return 1
        fi
    done
    status=0
    gb_tmux_server || status=$?
    if [[ "${status}" -ne 1 || -e "${GB_CHECKOUT}" ]]; then
        printf '[FAIL] the acceptance tmux server or the scratch checkout %s remains\n' "${GB_CHECKOUT}" >&2
        return 1
    fi
    gb_evidence "C1 both teams purged (firewalld rules, units, state, stamps); no acceptance container, tmux server, checkout or membership directory left"
}

# On the way out of a run that stopped early (plan_on_cleanup): each step tried, each failure
# named, the teams purged last.
gb_teardown() {
    local failed=0
    if [[ "${GB_TMUX_STARTED}" -ne 1 && "${GB_CHECKOUT_MADE}" -ne 1 && "${TEAMS_PRESENT}" -ne 1 ]]; then
        return 0
    fi
    printf '==> the run stopped with acceptance sessions, seats or teams in place: removing them\n'
    gb_kill_tmux || failed=1
    gb_wait_launchers || failed=1
    gb_remove_my_containers || failed=1
    if [[ "${TEAMS_PRESENT}" -eq 1 && "${GB_SEATS_TAKEN}" -eq 1 ]] && ! gb_park_seats; then
        printf '[WARN] the acceptance seats could not be parked; their checkout is deleted, as their teams are purged next\n' >&2
    fi
    gb_delete_checkout || failed=1
    gb_remove_membership_dirs || failed=1
    if [[ "${TEAMS_PRESENT}" -eq 1 ]]; then
        m7_remove_teams after-stop || failed=1
    fi
    if [[ "${failed}" -ne 0 ]]; then
        printf '[FAIL] the teardown did not finish; run acceptance.bash again, which removes what is left first\n' >&2
    fi
    return "${failed}"
}

# ── the fake mode: the verdict legs over fixture rooms, no host needed ───────────────────

#: The fixture seat members, one per team, and the scenario's start.
readonly GB_FAKE_USERS=("@fedora-desktop.1:${TEAM_A}.bus" "@fedora-desktop.1:${TEAM_B}.bus")
readonly GB_FAKE_HANDLE="fedora-desktop.1"
readonly GB_FAKE_SINCE=1700000000000

# gb_fake_room <case> <team-index> <end|''> <n> [<text>...] — page <n> of a fixture room: the
# seat's texts after the start, one before it (never counted) and another member's goodbye.
gb_fake_room() {
    local case="$1" index="$2" end="$3" n="$4" ts dir args=()
    shift 4
    dir="$(gb_ev "fixtures/${case}")"
    mkdir -p -- "${dir}" || return 1
    ts=$((GB_FAKE_SINCE + n * 1000))
    args+=("$((GB_FAKE_SINCE - 1))" "${GB_FAKE_USERS[index]}" "${GB_FAKE_HANDLE} left the team: session ended (exit)")
    args+=("$((ts + 1))" "@coordinator.1:${GB_TEAMS[index]}.bus" "coordinator.1 left the team: session ended (exit)")
    for text in "$@"; do
        ts=$((ts + 10))
        args+=("${ts}" "${GB_FAKE_USERS[index]}" "${text}")
    done
    "${GB_CHECK[@]}" fake-page "${end}" "${args[@]}" >"${dir}/${index}.${n}.json"
}

# gb_fake_rooms — the fixture cases, each the same in both teams: what each scenario should
# leave (one over two pages), and what it must not.
gb_fake_rooms() {
    local i bye gone
    bye="${GB_FAKE_HANDLE} left the team: session ended (exit)"
    gone="${GB_FAKE_HANDLE} disconnected: session ended without a goodbye"
    for i in "${!GB_TEAMS[@]}"; do
        gb_fake_room goodbye "${i}" t1 1 "${bye}" || return 1
        gb_fake_room goodbye "${i}" "" 2 || return 1
        gb_fake_room disconnected "${i}" "" 1 "${gone}" || return 1
        gb_fake_room both "${i}" "" 1 "${bye}" "${gone}" || return 1
        gb_fake_room two-goodbyes "${i}" "" 1 "${bye}" "${bye}" || return 1
        gb_fake_room silent "${i}" "" 1 || return 1
        gb_fake_room other-reason "${i}" "" 1 "${GB_FAKE_HANDLE} left the team: session ended (other)" || return 1
    done
    mkdir -p -- "$(gb_ev fake-panes/clean)" "$(gb_ev fake-panes/traceback)" || return 1
    printf 'Supervisor: on by default\n✓ seat 1@%s: the session ended without a goodbye, so the team was told it disconnected\n' "${TEAM_A}" \
        >"$(gb_ev fake-panes/clean/pane.log)" || return 1
    printf 'Traceback (most recent call last):\n  File "cli.py", line 1\n' >"$(gb_ev fake-panes/traceback/pane.log)"
}

# gb_fake_case <case> <goodbyes> <disconnects> — gb_judge over a fixture room in both teams.
gb_fake_case() {
    local i
    GB_FIXTURE_CASE="$1"
    for i in "${!GB_TEAMS[@]}"; do
        gb_fetch_pages gb_page_fixture "${i}" "$(gb_ev "fake/$1/${GB_TEAMS[i]}.page")" || return 1
        gb_judge "fake/$1/${GB_TEAMS[i]}" "${GB_FAKE_USERS[i]}" "${GB_FAKE_HANDLE}" "${GB_FAKE_SINCE}" "$2" "$3" || return 1
    done
}

gb_fake_pane() { gb_pane_clean "fake-panes/$1"; }

# gb_fake_signing <off|inherited> — ccy's own signing check (configure_git_signing, as the
# launcher calls it with --no-ssh) on a checkout made as the scratch one is (off) or without
# gb_signing_off (inherited), under a ~/.gitconfig that signs every commit and tag with SSH as
# the owner's does. Only the fixture's config is read: never this host's.
gb_fake_signing() {
    local dir gitconfig setting
    dir="$(gb_ev "fake-signing/$1")"
    gitconfig="${dir}/gitconfig"
    mkdir -p -- "${dir}/project" || return 1
    git -c init.defaultBranch=main init -q -- "${dir}/project" || return 1
    if [[ "$1" == off ]]; then
        gb_signing_off "${dir}/project" || return 1
    fi
    : >"${gitconfig}" || return 1
    for setting in gpg.format=ssh "user.signingkey=${dir}/id" commit.gpgsign=true tag.gpgsign=true; do
        git config --file "${gitconfig}" "${setting%%=*}" "${setting#*=}" || return 1
    done
    env -u GIT_CONFIG_COUNT -u GIT_CONFIG_PARAMETERS HOME="${dir}" GIT_CONFIG_GLOBAL="${gitconfig}" GIT_CONFIG_NOSYSTEM=1 \
        bash -c "source \"\$1/common-pure.bash\" && source \"\$1/ssh-handling.bash\" && configure_git_signing \"\$2\" \"\$3\" '' '' 0" \
        gb-signing "${GB_CCY_LIB}" "${gitconfig}" "${dir}/project"
}

# gb_fake_launcher_died — a pane where the launcher refused to start: the reason given is its
# ERROR line, and nothing else the pane showed.
gb_fake_launcher_died() {
    local name=fake-panes/died reason want
    want="ERROR: Commit signing is on in ~/.gitconfig, but the session has no SSH identity to sign with."
    mkdir -p -- "$(gb_ev "${name}")" || return 1
    printf 'Validating token...\r\n\033[0;31mERROR:\033[0m %s\r\n  Every commit in the container would fail.\r\n' "${want#ERROR: }" \
        >"$(gb_ev "${name}/pane.log")" || return 1
    reason="$(gb_launch_reason "${name}")" || return 1
    if [[ "${reason}" != "${want}" ]]; then
        printf '[FAIL] the reason read %q, want %q\n' "${reason}" "${want}" >&2
        return 1
    fi
    printf '==> the reason given: %s\n' "${reason}"
}

# gb_fake_prompt <waiting|starting> — a screen at ccy's menu of containers left with no
# terminal is named as waiting at it; one still starting is not.
gb_fake_prompt() {
    local screen prompt
    screen="$(gb_ev "fake-panes/prompt-$1.screen")"
    mkdir -p -- "${screen%/*}" || return 1
    case "$1" in
        waiting) printf 'Found 1 container(s) with no terminal attached\n\n  [i] Ignore\n\nChoice [a/s/i/q]:\n\n\n' >"${screen}" ;;
        *) printf 'Validating token...\n✓ Token validated successfully\n\n' >"${screen}" ;;
    esac || return 1
    prompt="$(gb_waiting_prompt "${screen}")" || return 1
    printf '==> waiting at: %s\n' "${prompt:-nothing}"
    [[ "${prompt}" == zombie-containers ]]
}
