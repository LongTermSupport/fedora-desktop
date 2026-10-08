# shellcheck shell=bash
# _acceptance-u20.inc.bash — the legs and checks of Plan 00161's acceptance.bash for unit U20,
# milestone M2 (DESIGN.md sections 5.3, 5.5, 5.6, 6 and 12 "U20"): ccy sessions in seats of
# this repository's own checkout, and the idle session woken. Sourced after
# _acceptance-steps.inc.bash, never executed: no shell options, no `exit`. Every function
# returns non-zero on its first failure, explicitly, since a leg runs where errexit is off. A
# check returns 0 PASS, 1 FAIL, or 2 COULD NOT ESTABLISH (GitHub did not answer a send).
#
# THE SESSIONS are real ccy sessions in the checkout acceptance.bash runs from: the owner's own
# launcher, image, entrypoint, plugin and settings, nothing stubbed, and no setup step. Each is
# `ccy --headless --no-restore --no-supervise --teams <seat>@acceptance --prompt … -- …` with
# stream-json input, so it reads its user messages from a fifo and sits idle between turns.
# The first launch into each seat creates it (`agent-bus seat take`, one `sudo -n agent-bus
# add-member`), so sudo's credential is refreshed first. No --token or --ssh-key: ccy's
# headless Quick Launch takes this checkout's saved choices (D38, D57), and the harness reads
# only the saved token's name, to scrub its value from the evidence afterwards. Three seats:
# acca (made orchestrator by `set-role`), accb and accc (workers). Later sessions: accb again
# (M2.7), accc after `seat remove` (M2.8), and a plain `ccy` (M2.9). Every container is found by
# its `ccy-seats` label, never by the project label the owner's own sessions carry too.
#
# WHAT IS JUDGED is what the bus, the host or Claude Code recorded, never the model's prose:
# the room's ping events, read as the human by `curl`; `agent-bus seat list` and `sudo agent-bus
# list`; each seat's `pingbus status`, run on the host through a home of its own whose one
# link leads to the seat's directory (offline: the lock and the inbox); each session's stream
# output (turns, its `init` line); and its transcript, found by the session ID the `init` line
# names (the owner's own transcripts are never read).
#
# THE CHECKOUT comes out as it went in: `ccy.env.local`, the seats in `.claude/ccy/pingbus/`,
# `git status --porcelain=v1` and HEAD are recorded before M2.0 and compared at M2.10, after
# `agent-bus seat remove` of every acceptance seat. Seats of other teams are never touched.
#
# Reads acceptance.bash's globals (TEAM, HUMAN, CHECK, AGENT_BUS, PINGBUS, REPORT, TEAM_PRESENT,
# PLAN_*), and the M1 slice's: REF_PATH, REF_COMMIT, A_HANDLE (M1's member a, the host
# orchestrator the harness sends from), SERVER_NAME, ROOM_PATH, BASE_URL, SENT_ID, HUMAN_TOKEN,
# and its functions send_ping, human_login, human_logout, human_curl, evidence.

readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly CCY_SSH_LIB="/var/local/claude-yolo/lib/ssh-handling.bash"
readonly CCY_RESTART_LIB="/var/local/claude-yolo/lib/restart-request.bash"
readonly CCY_IMAGE="claude-yolo:latest"
readonly CCY_KIT_PINGBUS="/opt/claude-yolo/optional/agent-bus/pingbus"
readonly U20=(python3 -I "${PLAN_SCRIPT_DIR}/u20_check.py")
readonly U20_CCY_DIR="${PLAN_REPO_ROOT}/.claude/ccy"
readonly U20_BUS_DIR="${U20_CCY_DIR}/pingbus"
readonly U20_SEATS_DIR="${U20_BUS_DIR}/seats"
readonly U20_TEAM_SEATS="${U20_SEATS_DIR}/${TEAM}"
readonly U20_EVIDENCE="${PLAN_RUN_DIR}/u20"
readonly U20_MESSAGES="${U20_EVIDENCE}/messages.json"
readonly U20_SEATS=(acca accb accc)
readonly U20_SEAT_LIST="acca@${TEAM},accb@${TEAM},accc@${TEAM}"
readonly U20_PROMPT="Plan 00161 U20 acceptance: the instructions arrive on stdin"
#: The model the sessions run on: the orders are a handful of pingbus commands.
readonly U20_MODEL="haiku"
#: Bounds, in seconds. A ccy launch validates the token in a container and, on the day's first
#: launch, may update Claude Code in the image first.
readonly U20_START_S=600
readonly U20_TURN_S=240
readonly U20_EXIT_S=60
readonly U20_POLL_S=3
#: How long the sessions a human message does not address get to (wrongly) act on it.
readonly U20_SETTLE_S=30
#: A refused launch stops before any container; this only bounds one that wrongly goes on.
readonly U20_REFUSED_S=600
#: Per session: ccy's pid, the write end of its input, its seat ("" for the plain one), its
#: container, its transcript. Per seat: its handle and user ID.
declare -gA U20_PID=() U20_FD=() U20_SEAT_OF=() U20_CTR=() U20_TRANSCRIPT=() U20_HANDLE=() U20_UID=()
#: Whether this run launched a session into this checkout, so its cleanup has work to do.
U20_STARTED=0
U20_TOKEN_NAME=""
U20_REVIEW1=""
U20_ACK1=""
U20_REVIEW3=""
U20_ACK3=""
REVIEW1_TS=""
REVIEW2_ID=""
REVIEW2_TS=""

u20_seat_dir() { printf '%s/%s' "${U20_TEAM_SEATS}" "$1"; }
u20_ev() { printf '%s/%s' "${U20_EVIDENCE}" "$1"; }

# u20_seat_cmd <args...> — `agent-bus seat …` as the desktop user, in the checkout (the seat
# commands act on the working directory's git top level).
u20_seat_cmd() {
    (cd -- "${PLAN_REPO_ROOT}" && "${AGENT_BUS}" seat "$@")
}

u20_seat_list() { u20_seat_cmd list >"$1"; }
# The list is the desktop user's own file, opened before sudo runs.
u20_member_list() { { sudo -n "${AGENT_BUS}" list "${TEAM}"; } >"$1"; }

# u20_poll <timeout-s> <what> <cmd...> — run cmd every U20_POLL_S s until it returns 0. It
# returns 3 for "not yet"; anything else, a session that ended, or the timeout is a FAIL.
u20_poll() {
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
        u20_sessions_alive || return 1
        if ((SECONDS >= deadline)); then
            printf '[FAIL] %s: not within %d s (the sessions'\'' output, status and stdin: %s)\n' \
                "${what}" "${timeout}" "${U20_EVIDENCE}" >&2
            return 1
        fi
        sleep "${U20_POLL_S}"
    done
}

# u20_sessions_alive — every live session's ccy is still running; else FAIL with its stderr.
u20_sessions_alive() {
    local s
    for s in "${!U20_PID[@]}"; do
        if ! kill -0 "${U20_PID[${s}]}" 2>/dev/null; then
            printf '[FAIL] the ccy session %s ended; its stderr (%s):\n' "${s}" "$(u20_ev "${s}")/session.err" >&2
            cat -- "$(u20_ev "${s}")/session.err" >&2
            return 1
        fi
    done
}

# ── before M1: what the owner must have, and what an interrupted run left ─────────────────

# u20_seat_label_listing <file> — every container labelled with seats, running or not, and its
# label, sorted.
u20_seat_label_listing() {
    podman ps -a --filter label=ccy-seats --format '{{.ID}}\t{{index .Labels "ccy-seats"}}' | LC_ALL=C sort >"$1"
}

# u20_remove_containers — every container holding an acceptance seat, running or not (a
# session of an interrupted run, or one that did not stop when its input closed).
u20_remove_containers() {
    local ids id listing
    mkdir -p -- "${U20_EVIDENCE}" || return 1
    listing="$(u20_ev seat-containers)"
    u20_seat_label_listing "${listing}" || return 1
    ids="$("${U20[@]}" seat-containers "${TEAM}" <"${listing}")" || return 1
    for id in ${ids}; do
        podman rm -f -t 10 -- "${id}" >/dev/null || return 1
        printf '==> removed the ccy container %s (an acceptance seat)\n' "${id}"
    done
}

# u20_prune_bus_dirs — `seats/` and then `.claude/ccy/pingbus/` when each is left empty, as
# `agent-bus seat remove` leaves them.
u20_prune_bus_dirs() {
    if [[ -d "${U20_SEATS_DIR}" ]]; then
        rmdir --ignore-fail-on-non-empty -- "${U20_SEATS_DIR}" || return 1
    fi
    if [[ -d "${U20_BUS_DIR}" ]]; then
        rmdir --ignore-fail-on-non-empty -- "${U20_BUS_DIR}" || return 1
    fi
}

# u20_remove_leftover_seats — the acceptance seats of an interrupted run: their team was purged
# at the start of this run, so they cannot be parked; their directories are deleted. Only the
# acceptance team's: every other team's seats stay.
u20_remove_leftover_seats() {
    if [[ ! -e "${U20_TEAM_SEATS}" && ! -L "${U20_TEAM_SEATS}" ]]; then
        return 0
    fi
    rm -rf -- "${U20_TEAM_SEATS}" || return 1
    u20_prune_bus_dirs || return 1
    printf '==> removed %s, left by an interrupted run\n' "${U20_TEAM_SEATS}"
}

# u20_owner_needs <reason> — the OWNER line every prerequisite fails with.
u20_owner_needs() {
    printf '[FAIL] OWNER: %s\n' "$1" >&2
    return 1
}

# u20_keys_unattended <key-file|ssh-agent>... — the installed ccy's own headless check
# (ccy_restart_keys_unattended in its lib/restart-request.bash, taking ccy_agent_forward_select
# from lib/ssh-handling.bash, Plan 00163), over the whole saved selection: every key opens with
# no passphrase or is forwarded from the ssh-agent of this terminal. Which key fails, and why,
# is on stderr. In a subshell, so the libraries' names never reach acceptance.bash.
#   status 0  ccy launches it asking nothing    1  it would not    2  the installed ccy has no such check
u20_keys_unattended() {
    (
        # shellcheck source=/dev/null
        source "${CCY_SSH_LIB}" || exit 2
        # shellcheck source=/dev/null
        source "${CCY_RESTART_LIB}" || exit 2
        declare -F ccy_agent_forward_select ccy_restart_keys_unattended >/dev/null || exit 2
        ccy_restart_keys_unattended "" "$@"
    )
}

# u20_prerequisites — what M2 needs on this host, checked before M1 spends any time: ccy and its
# image at the version the launcher requires with the agent-bus kit in it, agent-bus, and this
# checkout's saved Quick Launch choices as the installed ccy takes them headless: the record of
# the format this ccy reads, its token unexpired, every SSH key usable with nobody to type a
# passphrase (none needed, or the agent of this terminal holds it, as ccy itself decides).
# Then nothing of an interrupted run is left: no container in an acceptance seat, no seat.
u20_prerequisites() {
    local want have ccy_version config_version keys key rc
    local play="run play-claude-yolo.yml (deploy.bash runs it)"
    if [[ ! -x "${CCY_LAUNCHER}" ]]; then
        u20_owner_needs "ccy is not installed (${CCY_LAUNCHER}): ${play}"
        return 1
    fi
    if [[ ! -x "${AGENT_BUS}" ]]; then
        u20_owner_needs "agent-bus is not installed (${AGENT_BUS}): run play-agent-bus.yml (deploy.bash installs the software)"
        return 1
    fi
    want="$(awk -F'"' '/^REQUIRED_CONTAINER_VERSION=/ { print $2; exit }' "${CCY_LAUNCHER}")" || return 1
    ccy_version="$(awk -F'"' '/^CCY_VERSION=/ { print $2; exit }' "${CCY_LAUNCHER}")" || return 1
    config_version="$(awk -F= '/^CONFIG_VERSION=/ { print $2; exit }' "${CCY_LAUNCHER}")" || return 1
    if [[ -z "${want}" || -z "${ccy_version}" || -z "${config_version}" ]]; then
        printf '[FAIL] the installed ccy names no REQUIRED_CONTAINER_VERSION, CCY_VERSION or CONFIG_VERSION\n' >&2
        return 1
    fi
    if ! have="$(podman image inspect --format '{{index .Config.Labels "claude-yolo-version"}}' "${CCY_IMAGE}")"; then
        u20_owner_needs "no ccy image ${CCY_IMAGE}: ${play}"
        return 1
    fi
    if [[ "${have}" != "${want}" ]]; then
        u20_owner_needs "the ccy image is version ${have:-none}, the launcher wants ${want}: ${play}"
        return 1
    fi
    if ! podman run --rm --network none --entrypoint test "${CCY_IMAGE}" -x "${CCY_KIT_PINGBUS}"; then
        u20_owner_needs "the ccy image has no ${CCY_KIT_PINGBUS}: ${play}"
        return 1
    fi
    if ! keys="$("${U20[@]}" launch-keys "${PLAN_REPO_ROOT}" "${config_version}")"; then
        u20_owner_needs "launch ccy ${ccy_version} interactively in ${PLAN_REPO_ROOT} once, choosing the token and SSH keys the acceptance sessions reuse (Quick Launch saves them)"
        return 1
    fi
    if ! U20_TOKEN_NAME="$("${U20[@]}" ccy-token "${PLAN_REPO_ROOT}")"; then
        u20_owner_needs "the ccy sessions need the ccy token this checkout last launched with: launch ccy in ${PLAN_REPO_ROOT} once, or renew the token"
        return 1
    fi
    for key in ${keys}; do
        if [[ "${key}" == ssh-agent ]]; then
            if ! ssh-add -l >/dev/null; then
                u20_owner_needs "the saved launch uses your ssh-agent, and the terminal running meta-deploy.bash has none with a key in it: ssh-add your key there first"
                return 1
            fi
        fi
    done
    # launch-keys prints one saved key per line; none is no line at all.
    local -a key_list=()
    if [[ -n "${keys}" ]]; then
        mapfile -t key_list <<<"${keys}"
    fi
    rc=0
    u20_keys_unattended "${key_list[@]}" || rc=$?
    if [[ "${rc}" -eq 2 ]]; then
        u20_owner_needs "the installed ccy cannot forward a key from your ssh-agent (${CCY_SSH_LIB} or ${CCY_RESTART_LIB} predates it): ${play}"
        return 1
    fi
    if [[ "${rc}" -ne 0 ]]; then
        u20_owner_needs "a saved SSH key needs a passphrase (see above) and a headless launch never asks: ssh-add it in the terminal running meta-deploy.bash (ccy then forwards it from the agent, alone), or choose a key with no passphrase"
        return 1
    fi
    printf '==> ccy %s (image %s), saved launch choices of this version, ccy token %s, %s SSH key choice(s) usable unattended\n' \
        "${ccy_version}" "${have}" "${U20_TOKEN_NAME}" "$(wc -w <<<"${keys}")"
    u20_remove_containers || return 1
    u20_remove_leftover_seats
}

# ── the checkout, before and after ───────────────────────────────────────────────────────

# u20_record_checkout <dir> — what M2.10 compares: ccy.env.local's digest (or its absence),
# the seats in .claude/ccy/pingbus/ (to the seat level: a seat's own state may change while
# the owner's sessions in other teams run), git status and HEAD.
u20_record_checkout() {
    local dir="$1"
    mkdir -p -- "${dir}" || return 1
    if [[ -e "${U20_CCY_DIR}/ccy.env.local" || -L "${U20_CCY_DIR}/ccy.env.local" ]]; then
        sha256sum <"${U20_CCY_DIR}/ccy.env.local" >"${dir}/ccy.env.local.sha256" || return 1
    else
        printf 'absent\n' >"${dir}/ccy.env.local.sha256" || return 1
    fi
    if [[ -e "${U20_BUS_DIR}" ]]; then
        (cd -- "${U20_BUS_DIR}" && find . -mindepth 1 -maxdepth 3 -printf '%P\t%y\n') | LC_ALL=C sort >"${dir}/pingbus.list" || return 1
    else
        printf 'absent\n' >"${dir}/pingbus.list" || return 1
    fi
    git -C "${PLAN_REPO_ROOT}" status --porcelain=v1 >"${dir}/git-status" || return 1
    git -C "${PLAN_REPO_ROOT}" rev-parse HEAD >"${dir}/head" || return 1
}

# u20_expect_unchanged <before-dir> <after-dir> <file...> — each recorded file the same.
u20_expect_unchanged() {
    local before="$1" after="$2" name failed=0
    shift 2
    for name in "$@"; do
        if ! diff -u -- "${before}/${name}" "${after}/${name}" >&2; then
            printf '[FAIL] the checkout'\''s %s changed (above)\n' "${name}" >&2
            failed=1
        fi
    done
    return "${failed}"
}

# u20_record — M2.0's starting point, then sudo's credential refreshed so that each first
# launch can create its seat with `sudo -n`.
u20_record() {
    mkdir -p -- "${U20_EVIDENCE}" || return 1
    u20_record_checkout "$(u20_ev before)" || return 1
    u20_member_list "$(u20_ev members-before)" || return 1
    printf '==> the checkout recorded: %s\n' "$(u20_ev before)"
}

u20_refresh_sudo() {
    if ! sudo -n true; then
        printf '[FAIL] sudo'\''s credential has expired, and a headless launch creates a seat only with sudo -n: run acceptance.bash again (it asks for sudo once, at the start)\n' >&2
        return 1
    fi
}

# ── the sessions ─────────────────────────────────────────────────────────────────────────

# u20_launch <session> <seat|""> — ccy, headless, in the checkout, reading stream-json from a
# fifo; in seat <seat>@acceptance, or in no team for "". The child closes every other
# session's input first, so each session alone holds its own and sees end of file once the
# harness closes it. Opening the fifo here lets the child's own open, and so ccy, go on.
u20_launch() {
    local s="$1" seat="$2" ev args fd
    ev="$(u20_ev "${s}")"
    mkdir -p -- "${ev}" || return 1
    mkfifo -- "${ev}/stdin.fifo" || return 1
    : >"${ev}/stdin.jsonl" || return 1
    args=(--headless --no-restore --no-supervise)
    if [[ -n "${seat}" ]]; then
        args+=(--teams "${seat}@${TEAM}")
    fi
    args+=(--prompt "${U20_PROMPT}" -- --input-format stream-json --output-format stream-json --verbose --model "${U20_MODEL}")
    U20_STARTED=1
    (
        for fd in "${U20_FD[@]}"; do
            exec {fd}>&-
        done
        cd -- "${PLAN_REPO_ROOT}" && exec "${CCY_LAUNCHER}" "${args[@]}"
    ) <"${ev}/stdin.fifo" >"${ev}/session.out" 2>"${ev}/session.err" &
    U20_PID[${s}]=$!
    U20_SEAT_OF[${s}]="${seat}"
    exec {fd}<>"${ev}/stdin.fifo" || return 1
    U20_FD[${s}]="${fd}"
    printf '==> session %s: ccy %s started (pid %d)\n' "${s}" "${args[*]:0:5}" "${U20_PID[${s}]}"
}

# u20_ccy_containers — every ccy session container's ID, sorted (for the plain session).
u20_ccy_containers() {
    podman ps -a --filter label=ccy=true --format '{{.ID}}' | LC_ALL=C sort
}

# u20_container_up <session> — its container is running: the one labelled with its seat, or for
# the plain session the one new ccy container since its launch. Sets U20_CTR; 3 while none.
u20_container_up() {
    local s="$1" seat="${U20_SEAT_OF[$1]}" ids count
    if [[ -n "${seat}" ]]; then
        ids="$(podman ps --filter "label=ccy-seats=${seat}@${TEAM}" --filter status=running --format '{{.ID}}')" || return 1
    else
        ids="$(u20_ccy_containers | LC_ALL=C comm -13 "$(u20_ev "${s}")/containers-before" -)" || return 1
    fi
    count="$(wc -w <<<"${ids}")"
    if [[ "${count}" -eq 0 ]]; then
        return 3
    fi
    if [[ "${count}" -ne 1 ]]; then
        printf '[FAIL] session %s: %d containers where one was expected (%s); for the plain session another ccy session started meanwhile: run again\n' \
            "${s}" "${count}" "${ids//$'\n'/ }" >&2
        return 1
    fi
    U20_CTR[${s}]="${ids}"
}

# u20_start <session> <seat|""> — launch it and wait for its container.
u20_start() {
    local s="$1"
    if [[ -z "$2" ]]; then
        mkdir -p -- "$(u20_ev "${s}")" || return 1
        u20_ccy_containers >"$(u20_ev "${s}")/containers-before" || return 1
    fi
    u20_launch "$1" "$2" || return 1
    u20_poll "${U20_START_S}" "session ${s}'s ccy container running" u20_container_up "${s}" || return 1
    printf '==> session %s: container %s\n' "${s}" "${U20_CTR[${s}]}"
}

# u20_tell <session> <u20_check command...> — one stream-json user line to the session.
u20_tell() {
    local s="$1" line
    shift
    line="$("${U20[@]}" "$@")" || return 1
    printf '%s\n' "${line}" >&"${U20_FD[${s}]}" || return 1
    printf '%s\n' "${line}" >>"$(u20_ev "${s}")/stdin.jsonl"
}

u20_turns() { "${U20[@]}" turns "$(u20_ev "$1")/session.out"; }

u20_turns_above() {
    local n
    n="$(u20_turns "$1")" || return 1
    ((n > $2)) || return 3
}

# u20_find_transcript <session> — its transcript in the checkout, by the session ID of its
# init line. Sets U20_TRANSCRIPT; 3 while either is not written yet.
u20_find_transcript() {
    local s="$1" id path status=0
    id="$("${U20[@]}" session-id "$(u20_ev "${s}")/session.out")" || status=$?
    if [[ "${status}" -ne 0 ]]; then
        return "${status}"
    fi
    path="$("${U20[@]}" transcript "${U20_CCY_DIR}" "${id}")" || status=$?
    if [[ "${status}" -ne 0 ]]; then
        return "${status}"
    fi
    U20_TRANSCRIPT[${s}]="${path}"
}

# u20_ordered <session> <turns-before> — after an input: its turn ended and its transcript is
# known.
u20_ordered() {
    u20_poll "${U20_TURN_S}" "session $1's turn ended" u20_turns_above "$1" "$2" || return 1
    if [[ -z "${U20_TRANSCRIPT[$1]:-}" ]]; then
        u20_poll "${U20_TURN_S}" "session $1's transcript" u20_find_transcript "$1" || return 1
    fi
}

# u20_status_home <seat> — a host-side PINGBUS_HOME whose one link leads to the seat, as the
# session home in a container does (D50); `pingbus status` reads the seat through it.
u20_status_home() {
    local home
    home="$(u20_ev "home-$1")"
    if [[ ! -d "${home}" ]]; then
        mkdir -m 0700 -- "${home}" || return 1
        ln -s -- "$(u20_seat_dir "$1")" "${home}/${TEAM}" || return 1
    fi
    printf '%s' "${home}"
}

# u20_status_field <seat> <key> — one field of the seat's `pingbus status`, offline on the host.
# Before the session's first sync there is no state: not yet (3).
u20_status_field() {
    local seat="$1" home out
    out="$(u20_ev "status-${seat}.out")"
    home="$(u20_status_home "${seat}")" || return 1
    if ! PINGBUS_HOME="${home}" PINGBUS_TEAMS="${TEAM}" "${PINGBUS}" status >"${out}" 2>"$(u20_ev "status-${seat}.err")"; then
        return 3
    fi
    "${U20[@]}" status-field "${out}" "${TEAM}" "$2"
}

u20_wake_is() {
    local wake
    wake="$(u20_status_field "$1" wake)" || return 3
    [[ "${wake}" == "$2" ]] || return 3
}

u20_pending_is() {
    local pending
    pending="$(u20_status_field "$1" pending)" || return 3
    [[ "${pending}" == "$2" ]] || return 3
}

u20_notices_at_least() {
    local count
    count="$("${U20[@]}" notices "${U20_TRANSCRIPT[$1]}" | wc -l)" || return 1
    ((count >= $2)) || return 3
}

# u20_stdin_lines <session> <n> — the harness wrote exactly n lines to the session.
u20_stdin_lines() {
    local have
    have="$(wc -l <"$(u20_ev "$1")/stdin.jsonl")" || return 1
    if [[ "${have}" -ne "$2" ]]; then
        printf '[FAIL] the harness wrote %d lines to session %s, want %d\n' "${have}" "$1" "$2" >&2
        return 1
    fi
}

# ── M2.0: three sessions, their seats created by their launches ──────────────────────────

# u20_launch_seats — acca, accb and accc launched one after another (no two create a seat at
# the same moment), each running before the next starts; then acca made the orchestrator.
u20_launch_seats() {
    local seat
    u20_refresh_sudo || return 1
    for seat in "${U20_SEATS[@]}"; do
        u20_start "${seat}" "${seat}" || return 1
    done
    u20_seat_list "$(u20_ev seats-launched)" || return 1
    for seat in "${U20_SEATS[@]}"; do
        U20_HANDLE[${seat}]="$("${U20[@]}" seat-handle "$(u20_ev seats-launched)" "${TEAM}" "${seat}")" || return 1
        U20_UID[${seat}]="@${U20_HANDLE[${seat}]}:${SERVER_NAME}"
        printf '==> seat %s@%s: %s\n' "${seat}" "${TEAM}" "${U20_HANDLE[${seat}]}"
    done
    sudo -n "${AGENT_BUS}" set-role "${TEAM}" "${U20_HANDLE[acca]}" --role orchestrator || return 1
}

# M2.0 — ccy.env.local unchanged; each seat's directory the owner's own (0700, files 0600) and
# git-ignored; exactly three new members, their roles, each handle's seat and the <host> the
# checkout's rule gives; all three seats held.
u20_check_seats_created() {
    local seat dir host path
    u20_record_checkout "$(u20_ev after-launch)" || return 1
    u20_expect_unchanged "$(u20_ev before)" "$(u20_ev after-launch)" ccy.env.local.sha256 || return 1
    host="$("${U20[@]}" expected-host "${PLAN_REPO_ROOT}")" || return 1
    for seat in "${U20_SEATS[@]}"; do
        dir="$(u20_seat_dir "${seat}")"
        "${U20[@]}" seat-permissions "${dir}" || return 1
        for path in "${dir}" "${dir}/member.json" "${dir}/token"; do
            if ! git -C "${PLAN_REPO_ROOT}" check-ignore -q -- "${path}"; then
                printf '[FAIL] %s is not git-ignored\n' "${path}" >&2
                return 1
            fi
        done
        "${U20[@]}" expect-handle "${U20_HANDLE[${seat}]}" "${seat}" "${host}" || return 1
    done
    u20_member_list "$(u20_ev members-launched)" || return 1
    "${U20[@]}" expect-new-members "$(u20_ev members-before)" "$(u20_ev members-launched)" \
        "${U20_HANDLE[acca]}=orchestrator,${U20_HANDLE[accb]}=worker,${U20_HANDLE[accc]}=worker" || return 1
    "${U20[@]}" expect-seats "$(u20_ev seats-launched)" "${TEAM}" held acca,accb,accc || return 1
    evidence "M2.0 seats acca, accb and accc created by their launches (handles ${U20_HANDLE[acca]}, ${U20_HANDLE[accb]}, ${U20_HANDLE[accc]}; host ${host}), held, git-ignored, 0700/0600; ccy.env.local unchanged: u20/seats-launched, u20/members-launched"
}

# u20_give_orders — each session its standing orders; each idles with a watcher, and its init
# line shows the pingbus plugin loaded from the image.
u20_give_orders() {
    local seat
    for seat in "${U20_SEATS[@]}"; do
        u20_tell "${seat}" frame-orders bus || return 1
    done
    for seat in "${U20_SEATS[@]}"; do
        u20_ordered "${seat}" 0 || return 1
        "${U20[@]}" expect-plugin "$(u20_ev "${seat}")/session.out" present || return 1
        u20_poll "${U20_TURN_S}" "seat ${seat}'s watcher (pingbus status wake=watcher)" u20_wake_is "${seat}" watcher || return 1
    done
    printf '==> acca, accb and accc idle, each with a watcher\n'
}

# ── the room, as the human reads it ──────────────────────────────────────────────────────

human_get() {
    printf 'header = "Authorization: Bearer %s"\n' "${HUMAN_TOKEN}" \
        | curl --config - --silent --show-error --fail-with-body --noproxy '*' --max-time 30 "${BASE_URL}$1"
}

# u20_find_ping <var> <sender-uid> <verb> <to-uid> <ref|-> <re|-> — the earliest such ping in
# the room as "EVENT_ID<TAB>TS" into <var>; 3 while there is none.
u20_find_ping() {
    local u20_hit u20_status=0
    human_get "/_matrix/client/v3/rooms/${ROOM_PATH}/messages?dir=b&limit=500" >"${U20_MESSAGES}" || return 1
    u20_hit="$("${U20[@]}" find-ping "${U20_MESSAGES}" "$2" "$3" "$4" "$5" "$6")" || u20_status=$?
    if [[ "${u20_status}" -ne 0 ]]; then
        return "${u20_status}"
    fi
    # Into the caller's variable: its name must not be one of this function's locals.
    printf -v "$1" '%s' "${u20_hit}"
}

# ── the M2 checks ────────────────────────────────────────────────────────────────────────

# M2.1 — accb sits idle after its orders; acca is told to send accb a `review`; accb, which
# nothing writes to again, wakes on its watcher's notice and acks it. Once accb has drained its
# inbox, the second review for M2.2 goes out at once (from M1's host member a), so that its
# notice has the same count and falls inside the window in which the socket drops an
# identical body.
u20_check_review_ack() {
    local found turns status=0
    turns="$(u20_turns accb)" || return 1
    human_login || return 1
    u20_tell acca frame-send review "${REF_PATH}" "${U20_HANDLE[accb]}" || return 1
    u20_poll "${U20_TURN_S}" "acca's review of ${REF_PATH} to accb in the room" \
        u20_find_ping found "${U20_UID[acca]}" review "${U20_UID[accb]}" "${REF_PATH}" - || return 1
    U20_REVIEW1="${found%%$'\t'*}"
    REVIEW1_TS="${found#*$'\t'}"
    u20_poll "${U20_TURN_S}" "accb's watcher notice reached accb's session" u20_notices_at_least accb 1 || return 1
    u20_poll "${U20_TURN_S}" "accb ran pingbus recv (its inbox empty again)" u20_pending_is accb 0 || return 1
    send_ping a u20-send-review-2 review "${REF_COMMIT}" --to "${U20_HANDLE[accb]}" || status=$?
    if [[ "${status}" -ne 0 ]]; then
        return "${status}"
    fi
    REVIEW2_ID="${SENT_ID}"
    u20_poll "${U20_TURN_S}" "accb's ack of acca's review in the room" \
        u20_find_ping found "${U20_UID[accb]}" ack "${U20_UID[acca]}" - "${U20_REVIEW1}" || return 1
    U20_ACK1="${found%%$'\t'*}"
    u20_poll "${U20_TURN_S}" "accb's woken turn ended" u20_turns_above accb "${turns}" || return 1
    "${U20[@]}" expect-notice-before-recv "${U20_TRANSCRIPT[accb]}" || return 1
    u20_stdin_lines accb 1 || return 1
    evidence "M2.1 review ${U20_REVIEW1} from seat acca (told on stdin), ack ${U20_ACK1} from seat accb, whose only input was its orders; in accb's transcript the watcher's notice precedes the turn that ran pingbus recv: u20/messages.json, u20/accb/"
}

# M2.2 — the second review reached accb through a second notice with the same count (1
# pending, 1 ping) and the next notice number, within the dedupe window, and accb acked it.
u20_check_same_count() {
    local found ack2 m1a="@${A_HANDLE}:${SERVER_NAME}"
    if [[ -z "${REVIEW2_ID}" ]]; then
        printf '[FAIL] M2.1 sent no second review\n' >&2
        return 1
    fi
    u20_poll "${U20_TURN_S}" "the second review in the room" \
        u20_find_ping found "${m1a}" review "${U20_UID[accb]}" "${REF_COMMIT}" - || return 1
    REVIEW2_TS="${found#*$'\t'}"
    u20_poll "${U20_TURN_S}" "accb's ack of the second review in the room" \
        u20_find_ping found "${U20_UID[accb]}" ack "${m1a}" - "${REVIEW2_ID}" || return 1
    ack2="${found%%$'\t'*}"
    "${U20[@]}" notices "${U20_TRANSCRIPT[accb]}" || return 1
    "${U20[@]}" expect-same-count "${U20_TRANSCRIPT[accb]}" 1 0 1 || return 1
    "${U20[@]}" expect-within-window "${REVIEW1_TS}" "${REVIEW2_TS}" || return 1
    u20_stdin_lines accb 1 || return 1
    evidence "M2.2 review ${REVIEW2_ID} sent $(((REVIEW2_TS - REVIEW1_TS) / 1000)) s after the first, a second notice of 1 pending with a new number, ack ${ack2}: u20/accb/"
}

# u20_stop_watcher <session> — a signal to the `pingbus watch` process in its container, found
# by its argv; exactly one must be there.
u20_stop_watcher() {
    local s="$1" listing pids
    listing="$(podman exec "${U20_CTR[${s}]}" python3 -c '
import os
for pid in sorted(p for p in os.listdir("/proc") if p.isdigit()):
    try:
        with open(f"/proc/{pid}/cmdline", "rb") as f:
            argv = f.read()
    except OSError:
        continue
    print(pid, argv.replace(b"\0", b" ").decode("utf-8", "replace").strip(), sep="\t")
')" || return 1
    pids="$(printf '%s\n' "${listing}" | "${U20[@]}" watcher-pids)" || return 1
    if [[ "$(wc -w <<<"${pids}")" -ne 1 ]]; then
        printf '[FAIL] session %s: want one pingbus watch process, found "%s"; its processes:\n%s\n' "${s}" "${pids}" "${listing}" >&2
        return 1
    fi
    podman exec "${U20_CTR[${s}]}" kill -TERM "${pids}" || return 1
    printf '==> session %s: stopped its watcher (pid %s in its container)\n' "${s}" "${pids}"
}

# M2.3 — the watcher lost (D39, D59). accc idles with a watcher after its orders; the harness
# stops the watcher and gives accc a turn with nothing to do. At its end the Stop guard blocks
# once ("no waker") and accc starts `pingbus wait` in the background; M1's member a sends it a
# review; the waiter ends and accc acks, with no notice ever reaching it.
u20_check_wait_fallback() {
    local found turns status=0 m1a="@${A_HANDLE}:${SERVER_NAME}"
    u20_stop_watcher accc || return 1
    u20_poll "${U20_TURN_S}" "accc's seat with no waker (wake=none)" u20_wake_is accc none || return 1
    u20_tell accc frame-orders idle || return 1
    u20_poll "${U20_TURN_S}" "accc's background waiter (pingbus status wake=waiter)" u20_wake_is accc waiter || return 1
    turns="$(u20_turns accc)" || return 1
    send_ping a u20-send-review-3 review "${REF_PATH}" --to "${U20_HANDLE[accc]}" || status=$?
    if [[ "${status}" -ne 0 ]]; then
        return "${status}"
    fi
    U20_REVIEW3="${SENT_ID}"
    u20_poll "${U20_TURN_S}" "accc's ack of the review in the room" \
        u20_find_ping found "${U20_UID[accc]}" ack "${m1a}" - "${U20_REVIEW3}" || return 1
    U20_ACK3="${found%%$'\t'*}"
    u20_poll "${U20_TURN_S}" "accc's woken turn ended" u20_turns_above accc "${turns}" || return 1
    "${U20[@]}" expect-no-waker-block "${U20_TRANSCRIPT[accc]}" || return 1
    "${U20[@]}" expect-no-notices "${U20_TRANSCRIPT[accc]}" any || return 1
    u20_stdin_lines accc 2 || return 1
    evidence "M2.3 accc's watcher stopped; the Stop guard's no-waker block, a background pingbus wait; review ${U20_REVIEW3} from M1's a, ack ${U20_ACK3}, no notice in accc's transcript: u20/accc/"
}

# M2.4 — the human posts a message mentioning acca only, asking for an ack. acca, woken by a
# notice counting a human, acks it; neither accb nor accc answers it, and no notice counting
# a human reaches accb.
u20_check_human_addressed() {
    local message text response event txn found ack
    message="$(u20_ev human-message.json)"
    text="$("${U20[@]}" human-request "${HUMAN}")" || return 1
    "${CHECK[@]}" human-message "${text}" "${U20_UID[acca]}" >"${message}" || return 1
    txn="acceptance-u20-$(date +%s%N)" || return 1
    response="$(human_curl PUT "/_matrix/client/v3/rooms/${ROOM_PATH}/send/m.room.message/${txn}" "${message}")" || return 1
    event="$(printf '%s' "${response}" | "${CHECK[@]}" event-id)" || return 1
    printf '==> %s posted %s, mentioning seat acca only\n' "${HUMAN}" "${event}"
    u20_poll "${U20_TURN_S}" "acca's ack of the human's message in the room" \
        u20_find_ping found "${U20_UID[acca]}" ack "@${HUMAN}:${SERVER_NAME}" - "${event}" || return 1
    ack="${found%%$'\t'*}"
    printf '==> waiting %d s for any session it does not address to answer it\n' "${U20_SETTLE_S}"
    sleep "${U20_SETTLE_S}"
    human_get "/_matrix/client/v3/rooms/${ROOM_PATH}/messages?dir=b&limit=500" >"${U20_MESSAGES}" || return 1
    "${U20[@]}" expect-replies "${U20_MESSAGES}" "${event}" "${U20_UID[acca]}" || return 1
    "${U20[@]}" expect-human-notice "${U20_TRANSCRIPT[acca]}" || return 1
    "${U20[@]}" expect-no-notices "${U20_TRANSCRIPT[accb]}" humans || return 1
    human_logout || return 1
    evidence "M2.4 human message ${event} to seat acca only, ack ${ack} from acca alone, no human notice for accb: u20/human-message.json, u20/messages.json"
}

# u20_refused <label> <want-exit> <want-text> <teams-value> — one launch that must be refused
# before any container starts, with that exit code and that text on its stderr.
u20_refused() {
    local label="$1" want="$2" text="$3" value="$4" ev rc=0
    ev="$(u20_ev "refused-${label}")"
    mkdir -p -- "${ev}" || return 1
    (
        for fd in "${U20_FD[@]}"; do
            exec {fd}>&-
        done
        cd -- "${PLAN_REPO_ROOT}" && exec timeout "${U20_REFUSED_S}" "${CCY_LAUNCHER}" --headless --no-restore --no-supervise \
            --teams "${value}" --prompt "${U20_PROMPT}" -- --input-format stream-json --output-format stream-json \
            --verbose --model "${U20_MODEL}"
    ) </dev/null >"${ev}/session.out" 2>"${ev}/session.err" || rc=$?
    if [[ "${rc}" -ne "${want}" ]] || ! grep -qF -- "${text}" "${ev}/session.err"; then
        printf '[FAIL] ccy --teams %s exited %d, want %d with "%s"; its stderr:\n' "${value}" "${rc}" "${want}" "${text}" >&2
        cat -- "${ev}/session.err" >&2
        return 1
    fi
    printf '==> ccy --teams %s refused: exit %d, "%s"\n' "${value}" "${rc}" "${text}"
}

# M2.5 — while the three run, all three held; a launch on a held seat refused (75), one naming
# two seats of the team (64), one with a trailing comma (64); nothing changed by any of them.
u20_check_refused() {
    local name
    u20_seat_list "$(u20_ev seats-running)" || return 1
    "${U20[@]}" expect-seats "$(u20_ev seats-running)" "${TEAM}" held acca,accb,accc || return 1
    u20_member_list "$(u20_ev members-running)" || return 1
    u20_record_checkout "$(u20_ev refused-before)" || return 1
    u20_seat_label_listing "$(u20_ev refused-before)/containers" || return 1
    u20_refresh_sudo || return 1
    u20_refused held 75 "is held by another session" "accb@${TEAM}" || return 1
    u20_refused two-seats 64 "one seat per team per session" "accd@${TEAM},acce@${TEAM}" || return 1
    u20_refused trailing-comma 64 "trailing comma" "accd@${TEAM}," || return 1
    u20_seat_list "$(u20_ev seats-refused)" || return 1
    u20_member_list "$(u20_ev members-refused)" || return 1
    u20_record_checkout "$(u20_ev refused-after)" || return 1
    u20_seat_label_listing "$(u20_ev refused-after)/containers" || return 1
    for name in seats members; do
        if ! diff -u -- "$(u20_ev "${name}-running")" "$(u20_ev "${name}-refused")" >&2; then
            printf '[FAIL] a refused launch changed the %s (above)\n' "${name}" >&2
            return 1
        fi
    done
    u20_expect_unchanged "$(u20_ev refused-before)" "$(u20_ev refused-after)" pingbus.list containers || return 1
    evidence "M2.5 a launch on held seat accb refused (75), two seats of one team (64), a trailing comma (64); the seats, members, .claude/ccy/pingbus/ and the seat-labelled containers unchanged: u20/refused-*"
}

# ── the end of a session ─────────────────────────────────────────────────────────────────

# u20_keep_watch_log <session> — the session's watcher log, from its container (the session
# home dies with it). A seated session must have one.
u20_keep_watch_log() {
    local s="$1"
    if [[ -z "${U20_SEAT_OF[${s}]:-}" || -z "${U20_CTR[${s}]:-}" ]]; then
        return 0
    fi
    podman exec "${U20_CTR[${s}]}" cat /tmp/pingbus-home/watch.log >"$(u20_ev "${s}")/watch.log" || return 1
}

# u20_close <session> — close its input (claude sees end of file and exits, and the container
# with it); after U20_EXIT_S, stop the container and the launcher. Its status is kept.
u20_close() {
    local s="$1" fd="${U20_FD[$1]:-}" pid="${U20_PID[$1]:-}" deadline status=0
    if [[ -n "${fd}" ]]; then
        exec {fd}>&-
        unset 'U20_FD[$s]'
    fi
    if [[ -z "${pid}" ]]; then
        return 0
    fi
    deadline=$((SECONDS + U20_EXIT_S))
    while kill -0 "${pid}" 2>/dev/null && ((SECONDS < deadline)); do
        sleep 1
    done
    if kill -0 "${pid}" 2>/dev/null; then
        printf '==> session %s did not end within %d s of its input closing: stopping it\n' "${s}" "${U20_EXIT_S}"
        if [[ -n "${U20_CTR[${s}]:-}" ]]; then
            podman rm -f -t 10 -- "${U20_CTR[${s}]}" >/dev/null || return 1
        fi
        kill -TERM "${pid}" 2>/dev/null || printf '==> session %s: ccy had already ended\n' "${s}"
    fi
    wait "${pid}" || status=$?
    unset 'U20_PID[$s]'
    printf '==> session %s: ccy exited %d\n' "${s}" "${status}"
}

# u20_keep_transcript <session> — its transcript (and the directory Claude Code keeps beside
# it) moved out of the checkout's .claude/ccy/projects/ into the evidence.
u20_keep_transcript() {
    local s="$1" path ev status=0
    if [[ -z "${U20_TRANSCRIPT[${s}]:-}" ]]; then
        u20_find_transcript "${s}" || status=$?
        if [[ "${status}" -eq 3 ]]; then
            printf '==> session %s left no transcript\n' "${s}"
            return 0
        fi
        if [[ "${status}" -ne 0 ]]; then
            return 1
        fi
    fi
    path="${U20_TRANSCRIPT[${s}]}"
    ev="$(u20_ev "${s}")"
    if [[ "${path}" == "${ev}/transcript.jsonl" ]]; then
        return 0
    fi
    mv -- "${path}" "${ev}/transcript.jsonl" || return 1
    if [[ -d "${path%.jsonl}" ]]; then
        mv -- "${path%.jsonl}" "${ev}/transcript.d" || return 1
    fi
    U20_TRANSCRIPT[${s}]="${ev}/transcript.jsonl"
}

# u20_end <session...> — each one's watcher log kept, its input closed, its transcript moved.
u20_end() {
    local s
    for s in "$@"; do
        u20_keep_watch_log "${s}" || return 1
        u20_close "${s}" || return 1
        u20_keep_transcript "${s}" || return 1
    done
}

u20_end_seats() { u20_end "${U20_SEATS[@]}"; }

# M2.6 — once the sessions ended, the three seats are free again.
u20_check_seats_free() {
    u20_seat_list "$(u20_ev seats-ended)" || return 1
    "${U20[@]}" expect-seats "$(u20_ev seats-ended)" "${TEAM}" free acca,accb,accc || return 1
    evidence "M2.6 the three seats free once their sessions ended: u20/seats-ended"
}

# u20_one_command <session> <seat|""> <orders> — a new session given one order; it ends once
# that turn did.
u20_one_command() {
    local s="$1"
    u20_start "${s}" "$2" || return 1
    u20_tell "${s}" frame-orders "$3" || return 1
    u20_ordered "${s}" 0 || return 1
    u20_end "${s}"
}

# M2.7 — a seat outlives its session: a new session in accb is the same member (no new
# account) and its `pingbus history` shows acca's M2.1 review in and its own ack out.
u20_check_seat_returns() {
    u20_member_list "$(u20_ev members-before-accb2)" || return 1
    u20_one_command accb2 accb history || return 1
    u20_member_list "$(u20_ev members-after-accb2)" || return 1
    "${U20[@]}" expect-same-handles "$(u20_ev members-before-accb2)" "$(u20_ev members-after-accb2)" || return 1
    "${U20[@]}" expect-history "${U20_TRANSCRIPT[accb2]}" in "${U20_REVIEW1}" || return 1
    "${U20[@]}" expect-history "${U20_TRANSCRIPT[accb2]}" out "${U20_ACK1}" || return 1
    evidence "M2.7 a later session in seat accb is the same member and its pingbus history holds review ${U20_REVIEW1} in and ack ${U20_ACK1} out: u20/accb2/"
}

# M2.8 — removed and added again: `seat remove accc@acceptance` parks it (its directory gone,
# its handle kept with its role); a new session in accc returns it (no new member) and reads
# M2.3's review and its ack from the room after its local state started again.
u20_check_seat_removed_and_returned() {
    local handle
    u20_member_list "$(u20_ev members-before-remove)" || return 1
    u20_seat_cmd remove "accc@${TEAM}" || return 1
    if [[ -e "$(u20_seat_dir accc)" ]]; then
        printf '[FAIL] agent-bus seat remove left %s\n' "$(u20_seat_dir accc)" >&2
        return 1
    fi
    u20_member_list "$(u20_ev members-removed)" || return 1
    "${U20[@]}" expect-member "$(u20_ev members-removed)" "${U20_HANDLE[accc]}" worker parked || return 1
    u20_refresh_sudo || return 1
    u20_one_command accc2 accc history || return 1
    u20_seat_list "$(u20_ev seats-returned)" || return 1
    handle="$("${U20[@]}" seat-handle "$(u20_ev seats-returned)" "${TEAM}" accc)" || return 1
    if [[ "${handle}" != "${U20_HANDLE[accc]}" ]]; then
        printf '[FAIL] seat accc returned as %s, was %s\n' "${handle}" "${U20_HANDLE[accc]}" >&2
        return 1
    fi
    u20_member_list "$(u20_ev members-returned)" || return 1
    "${U20[@]}" expect-same-handles "$(u20_ev members-before-remove)" "$(u20_ev members-returned)" || return 1
    "${U20[@]}" expect-member "$(u20_ev members-returned)" "${U20_HANDLE[accc]}" worker active || return 1
    "${U20[@]}" expect-history "${U20_TRANSCRIPT[accc2]}" in "${U20_REVIEW3}" || return 1
    "${U20[@]}" expect-history "${U20_TRANSCRIPT[accc2]}" out "${U20_ACK3}" || return 1
    evidence "M2.8 seat accc removed (parked, directory gone) and returned by a launch as ${handle}, no new member; its pingbus history holds review ${U20_REVIEW3} in and ack ${U20_ACK3} out: u20/accc2/, u20/members-removed"
}

# M2.9 — a plain `ccy` is on no team: `pingbus` is not found in it, its init line names no
# pingbus plugin or skill, its container carries no ccy-seats label, and no seat or member
# changed.
u20_check_plain() {
    u20_seat_list "$(u20_ev seats-before-plain)" || return 1
    u20_member_list "$(u20_ev members-before-plain)" || return 1
    u20_start plain "" || return 1
    podman container inspect --format '{{json .Config.Labels}}' "${U20_CTR[plain]}" >"$(u20_ev plain)/labels.json" || return 1
    "${U20[@]}" expect-no-seats-label <"$(u20_ev plain)/labels.json" || return 1
    u20_tell plain frame-orders plain || return 1
    u20_ordered plain 0 || return 1
    u20_end plain || return 1
    "${U20[@]}" expect-plugin "$(u20_ev plain)/session.out" absent || return 1
    "${U20[@]}" expect-not-found "${U20_TRANSCRIPT[plain]}" || return 1
    u20_seat_list "$(u20_ev seats-after-plain)" || return 1
    u20_member_list "$(u20_ev members-after-plain)" || return 1
    if ! diff -u -- "$(u20_ev seats-before-plain)" "$(u20_ev seats-after-plain)" >&2 \
        || ! diff -u -- "$(u20_ev members-before-plain)" "$(u20_ev members-after-plain)" >&2; then
        printf '[FAIL] a plain ccy launch changed the seats or the members (above)\n' >&2
        return 1
    fi
    evidence "M2.9 a plain ccy launch: pingbus not found, no pingbus plugin in its init line, no ccy-seats label, seats and members unchanged: u20/plain/"
}

# ── the way out ──────────────────────────────────────────────────────────────────────────

# u20_scrub — the ccy token's value out of the evidence (a hit is a FAIL: Claude Code wrote it
# somewhere).
u20_scrub() {
    local held
    if [[ ! -d "${U20_EVIDENCE}" ]]; then
        return 0
    fi
    held="$("${U20[@]}" scrub "${PLAN_REPO_ROOT}" "${U20_EVIDENCE}")" || return 1
    if [[ -n "${held}" ]]; then
        printf '[FAIL] the ccy token value was written into these files (now replaced with a placeholder):\n%s\n' "${held}" >&2
        return 1
    fi
}

# u20_present_seats — the acceptance seats with a directory, as one <seat>@<team> list.
u20_present_seats() {
    local seat list=""
    for seat in "${U20_SEATS[@]}"; do
        if [[ -d "$(u20_seat_dir "${seat}")" ]]; then
            list+="${list:+,}${seat}@${TEAM}"
        fi
    done
    printf '%s' "${list}"
}

# u20_cleanup — after the checks: any session still live ended, every acceptance seat removed
# (parked; their directories, and seats/ and pingbus/ when left empty, deleted), the evidence
# scrubbed.
u20_cleanup() {
    local s list
    for s in "${!U20_PID[@]}"; do
        u20_end "${s}" || return 1
    done
    u20_remove_containers || return 1
    list="$(u20_present_seats)"
    if [[ -n "${list}" ]]; then
        u20_seat_cmd remove "${list}" || return 1
    fi
    U20_STARTED=0
    u20_scrub
}

# M2.10 — the checkout as it was before M2.0.
u20_check_checkout_unchanged() {
    u20_record_checkout "$(u20_ev after)" || return 1
    u20_expect_unchanged "$(u20_ev before)" "$(u20_ev after)" ccy.env.local.sha256 pingbus.list git-status head || return 1
    evidence "M2.10 after seat remove of ${U20_SEAT_LIST}: ccy.env.local, .claude/ccy/pingbus/, git status and HEAD as before M2.0: u20/before, u20/after"
}

# On the way out of any run that stopped early (plan_on_cleanup), before the team goes: every
# session ended (each step tried, each failure named), the seats removed while their team is
# still there, else deleted (the team purge that follows makes them worthless), the evidence
# scrubbed.
u20_teardown_after_stop() {
    local s list failed=0
    if [[ "${U20_STARTED}" -ne 1 ]]; then
        return 0
    fi
    printf '==> the run stopped with ccy acceptance sessions or seats in this checkout: removing them\n'
    for s in "${!U20_PID[@]}"; do
        u20_keep_watch_log "${s}" || printf '[WARN] session %s: its watcher log was not kept\n' "${s}" >&2
        u20_close "${s}" || failed=1
    done
    for s in "${!U20_SEAT_OF[@]}"; do
        u20_keep_transcript "${s}" || failed=1
    done
    u20_remove_containers || failed=1
    list="$(u20_present_seats)"
    if [[ -n "${list}" ]] && { [[ "${TEAM_PRESENT}" -ne 1 ]] || ! u20_seat_cmd remove "${list}"; }; then
        printf '[WARN] the acceptance seats could not be parked; deleting %s, as the team is purged next\n' "${U20_TEAM_SEATS}" >&2
        u20_remove_leftover_seats || failed=1
    fi
    U20_STARTED=0
    u20_scrub || failed=1
    return "${failed}"
}
