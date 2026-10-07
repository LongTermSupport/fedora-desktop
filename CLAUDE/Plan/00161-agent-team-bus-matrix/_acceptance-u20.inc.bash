# shellcheck shell=bash
# _acceptance-u20.inc.bash — the legs and checks of Plan 00161's acceptance.bash for unit U20,
# milestone M2 (DESIGN.md sections 5.3, 6 and 12): ccy members, and the idle session woken.
# Sourced after _acceptance-steps.inc.bash, never executed: no shell options, no `exit`. Every
# function returns non-zero on its first failure, explicitly, since a leg runs where errexit is
# off. A check returns 0 PASS, 1 FAIL, or 2 COULD NOT ESTABLISH (GitHub did not answer a send).
#
# THE SESSIONS are real ccy sessions: the owner's own launcher, image, entrypoint, plugin and
# settings, nothing stubbed. ccy runs headless (`--headless`, which is `claude -p`) with
# stream-json input, so each session reads its user messages from a pipe and sits idle between
# turns, exactly as U01's probe sessions did (`-p` is ignored as a prompt when the input is
# stream-json). Each runs in its own throwaway git checkout, a different project for ccy, whose
# untracked `.claude/ccy/ccy.env.local` sets PINGBUS_TEAMS and whose bundle is in
# `.claude/ccy/pingbus/acceptance/` (DESIGN.md section 5.3). Three members of the acceptance
# team: a (orchestrator) and b (worker) with the inbox socket, and c (worker) without it:
# c's tracked-style `.claude/ccy/ccy.env` exports CLAUDE_CODE_HARBOR_KITE=0, the Claude Code
# switch (read in its startup code, 2.1.292) that keeps the cross-session inbox from binding;
# M2.3 fails rather than passes if c turns out to have a watcher after all.
#
# AUTHENTICATION is ccy's own: `--token NAME`, the token this checkout last launched ccy with
# (LAST_TOKEN in .claude/ccy/.last-launch.conf, by U01's rules). ccy reads the file and passes
# the value into the container by environment name; this script only ever handles the name.
# After the sessions end, u20_check.py reads the value itself to scrub the evidence it keeps.
#
# WHAT IS JUDGED is what the bus or Claude Code recorded, never the model's prose: the room's
# ping events, read as the human by `curl`; each member's `pingbus status`, run on the host
# against the member's PINGBUS_HOME (offline, the lock and the inbox); the turns in each
# session's stream output; and the watcher's fixed-template notices in each transcript.
#
# Reads acceptance.bash's globals (TEAM, HUMAN, BUS_ADDRESS, CHECK, AGENT_BUS, PINGBUS, REPORT,
# PLAN_*), and the M1 slice's: REF_PATH, REF_COMMIT, A_HANDLE (M1's member a, the host
# orchestrator the harness sends from), SERVER_NAME, ROOM_PATH, BASE_URL, and its functions
# send_ping, human_login, human_logout, human_curl, evidence.

readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly CCY_IMAGE="claude-yolo:latest"
readonly CCY_KIT_PINGBUS="/opt/claude-yolo/optional/agent-bus/pingbus"
readonly U20=(python3 -I "${PLAN_SCRIPT_DIR}/u20_check.py")
#: The checkouts' parent: ccy names a project "<parent>-<dir>", so the containers are
#: agent-bus-acceptance-ccy-<m>_yolo, found again by the ccy-project label.
readonly U20_CHECKOUTS="${PLAN_RUN_DIR}/agent-bus-acceptance"
readonly U20_EVIDENCE="${PLAN_RUN_DIR}/u20"
readonly U20_MESSAGES="${U20_EVIDENCE}/messages.json"
readonly U20_MEMBERS=(a b c)
#: The model the sessions run on: the instructions are a handful of pingbus commands.
readonly U20_MODEL="haiku"
#: Bounds, in seconds. A ccy launch validates the token in a container and, on the day's first
#: launch, may update Claude Code in the image first.
readonly U20_START_S=600
readonly U20_TURN_S=240
readonly U20_EXIT_S=60
readonly U20_POLL_S=3
#: How long the members a human message does not address get to (wrongly) act on it.
readonly U20_SETTLE_S=30
declare -gA U20_PID=() U20_FD=() U20_HANDLE=() U20_UID=()
U20_TOKEN_NAME=""
U20_CHECKOUTS_PRESENT=0
REVIEW1_TS=""
REVIEW2_ID=""
REVIEW2_TS=""

u20_project() { printf 'agent-bus-acceptance-ccy-%s' "$1"; }
u20_checkout() { printf '%s/ccy-%s' "${U20_CHECKOUTS}" "$1"; }
u20_state() { printf '%s/.claude/ccy' "$(u20_checkout "$1")"; }
u20_home() { printf '%s/pingbus' "$(u20_state "$1")"; }

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

# u20_sessions_alive — every started session's ccy is still running; else FAIL with its stderr.
u20_sessions_alive() {
    local m
    for m in "${!U20_PID[@]}"; do
        if ! kill -0 "${U20_PID[${m}]}" 2>/dev/null; then
            printf '[FAIL] the ccy session of member %s ended; its stderr (%s):\n' "${m}" "${U20_EVIDENCE}/${m}/session.err" >&2
            cat -- "${U20_EVIDENCE}/${m}/session.err" >&2
            return 1
        fi
    done
}

# ── setup legs ───────────────────────────────────────────────────────────────────────────

# u20_remove_containers — every acceptance ccy container, running or not (a session of an
# interrupted run, or one that did not stop when its input closed).
u20_remove_containers() {
    local m names name
    for m in "${U20_MEMBERS[@]}"; do
        names="$(podman ps -a --filter "label=ccy-project=$(u20_project "${m}")" --format '{{.Names}}')" || return 1
        for name in ${names}; do
            podman rm -f -t 10 -- "${name}" >/dev/null || return 1
            printf '==> removed the ccy container %s\n' "${name}"
        done
    done
}

# u20_prerequisites — what M2 needs on this host, checked before M1 spends any time: ccy, its
# image at the version the launcher requires with the agent-bus kit in it, and the ccy token
# this checkout last launched with. Then no container of an interrupted run is left.
u20_prerequisites() {
    local want have
    if [[ ! -x "${CCY_LAUNCHER}" ]]; then
        printf '[FAIL] ccy is not installed (%s): OWNER: run play-claude-yolo.yml (deploy.bash runs it)\n' "${CCY_LAUNCHER}" >&2
        return 1
    fi
    want="$(awk -F'"' '/^REQUIRED_CONTAINER_VERSION=/ { print $2; exit }' "${CCY_LAUNCHER}")" || return 1
    if ! have="$(podman image inspect --format '{{index .Config.Labels "claude-yolo-version"}}' "${CCY_IMAGE}")"; then
        printf '[FAIL] no ccy image %s: OWNER: run play-claude-yolo.yml (deploy.bash runs it)\n' "${CCY_IMAGE}" >&2
        return 1
    fi
    if [[ -z "${want}" || "${have}" != "${want}" ]]; then
        printf '[FAIL] the ccy image is version %s, the launcher wants %s: OWNER: run play-claude-yolo.yml (deploy.bash runs it)\n' \
            "${have:-none}" "${want:-unknown}" >&2
        return 1
    fi
    if ! podman run --rm --network none --entrypoint test "${CCY_IMAGE}" -x "${CCY_KIT_PINGBUS}"; then
        printf '[FAIL] the ccy image has no %s: OWNER: run play-claude-yolo.yml (deploy.bash runs it)\n' "${CCY_KIT_PINGBUS}" >&2
        return 1
    fi
    if ! U20_TOKEN_NAME="$("${U20[@]}" ccy-token "${PLAN_REPO_ROOT}")"; then
        printf '[FAIL] the ccy sessions need the ccy token this checkout last launched with: OWNER: launch ccy in %s once, or renew the token\n' \
            "${PLAN_REPO_ROOT}" >&2
        return 1
    fi
    printf '==> ccy %s, image %s (version %s), ccy token %s\n' "${CCY_LAUNCHER}" "${CCY_IMAGE}" "${have}" "${U20_TOKEN_NAME}"
    u20_remove_containers
}

# u20_make_checkouts — one throwaway git checkout per member, each its own ccy project, opted
# in to the acceptance team through ccy.env.local; c's ccy.env turns its inbox socket off.
u20_make_checkouts() {
    local m checkout dist
    dist="$(awk -F= '/^CCY_ENV_LOCAL_DIST_VERSION=/ { print $2; exit }' "$(dirname "${CCY_LAUNCHER}")/lib/common.bash")" || return 1
    if [[ ! "${dist}" =~ ^[0-9]+$ ]]; then
        printf '[FAIL] no CCY_ENV_LOCAL_DIST_VERSION in the installed ccy\n' >&2
        return 1
    fi
    mkdir -p -- "${U20_CHECKOUTS}" || return 1
    U20_CHECKOUTS_PRESENT=1
    for m in "${U20_MEMBERS[@]}"; do
        checkout="$(u20_checkout "${m}")"
        mkdir -p -- "${U20_EVIDENCE}/${m}" || return 1
        git init -q -- "${checkout}" || return 1
        mkdir -p -- "$(u20_home "${m}")" || return 1
        printf '# based on ccy.env.local.dist version %s\nexport PINGBUS_TEAMS=%s\n' "${dist}" "${TEAM}" \
            >"$(u20_state "${m}")/ccy.env.local" || return 1
        if [[ "${m}" == "c" ]]; then
            printf '# Plan 00161 U20: this member has no inbox socket, so pingbus wait wakes it.\nexport CLAUDE_CODE_HARBOR_KITE=0\n' \
                >"$(u20_state "${m}")/ccy.env" || return 1
        fi
        printf '==> checkout %s (ccy project %s)\n' "${checkout}" "$(u20_project "${m}")"
    done
}

# u20_add_members — a podman member per checkout, its bundle written by `agent-bus add-member`
# straight into the checkout's .claude/ccy/pingbus/acceptance/.
u20_add_members() {
    local m role bundle
    for m in "${U20_MEMBERS[@]}"; do
        role=worker
        if [[ "${m}" == "a" ]]; then
            role=orchestrator
        fi
        bundle="$(u20_home "${m}")/${TEAM}"
        sudo -n "${AGENT_BUS}" add-member "${TEAM}" "--repo=ccy-${m}" "--host=${TEAM}" --type=podman \
            "--role=${role}" "--address=${BUS_ADDRESS}" "--out=${bundle}" || return 1
        U20_HANDLE[${m}]="$("${CHECK[@]}" handle "${bundle}/member.json")" || return 1
        U20_UID[${m}]="@${U20_HANDLE[${m}]}:${SERVER_NAME}"
        printf '==> ccy member %s: %s (%s)\n' "${m}" "${U20_HANDLE[${m}]}" "${role}"
    done
}

# u20_launch <m> — ccy, headless, in the member's checkout, reading stream-json from a fifo.
# Nothing is written to the fifo until the container runs, so a launch prompt reads nothing.
u20_launch() {
    local m="$1" ev="${U20_EVIDENCE}/$1" checkout
    checkout="$(u20_checkout "${m}")"
    mkfifo -- "${ev}/stdin.fifo" || return 1
    : >"${ev}/stdin.jsonl" || return 1
    (
        cd -- "${checkout}" && exec "${CCY_LAUNCHER}" --headless --no-ssh --no-network --token "${U20_TOKEN_NAME}" \
            --prompt "Plan 00161 U20 acceptance: the instructions arrive on stdin" \
            -- --input-format stream-json --output-format stream-json --verbose --model "${U20_MODEL}"
    ) <"${ev}/stdin.fifo" >"${ev}/session.out" 2>"${ev}/session.err" &
    U20_PID[${m}]=$!
    printf '==> member %s: ccy started (pid %d) in %s\n' "${m}" "${U20_PID[${m}]}" "${checkout}"
}

# u20_open_input <m> — the write end of the member's fifo, opened read-write so that it never
# blocks and a write never raises SIGPIPE. Opened only after every launch, so no ccy inherits
# another member's input and each session sees end of file once its own fd is closed.
u20_open_input() {
    local m="$1" fd
    exec {fd}<>"${U20_EVIDENCE}/${m}/stdin.fifo" || return 1
    U20_FD[${m}]="${fd}"
}

# u20_tell <m> <u20_check command...> — one stream-json user line to the member's session.
u20_tell() {
    local m="$1" line
    shift
    line="$("${U20[@]}" "$@")" || return 1
    printf '%s\n' "${line}" >&"${U20_FD[${m}]}" || return 1
    printf '%s\n' "${line}" >>"${U20_EVIDENCE}/${m}/stdin.jsonl"
}

u20_running() {
    local id
    id="$(podman ps --filter "label=ccy-project=$(u20_project "$1")" --filter status=running --format '{{.ID}}')" || return 1
    [[ -n "${id}" ]] || return 3
}

u20_turns() { "${U20[@]}" turns "${U20_EVIDENCE}/$1/session.out"; }

u20_turns_at_least() {
    local n
    n="$(u20_turns "$1")" || return 1
    ((n >= $2)) || return 3
}

# u20_status_field <m> <key> — one field of the member's `pingbus status`, run on the host
# against its PINGBUS_HOME. Before the session's first sync there is no state: not yet (3).
u20_status_field() {
    local m="$1" out="${U20_EVIDENCE}/$1/status.out"
    if ! PINGBUS_HOME="$(u20_home "${m}")" PINGBUS_TEAMS="${TEAM}" "${PINGBUS}" status \
        >"${out}" 2>"${U20_EVIDENCE}/${m}/status.err"; then
        return 3
    fi
    "${U20[@]}" status-field "${out}" "${TEAM}" "$2"
}

u20_wake_is() {
    local wake
    wake="$(u20_status_field "$1" wake)" || return 3
    if [[ "${wake}" == "$2" ]]; then
        return 0
    fi
    if [[ "$2" == "waiter" && "${wake}" == "watcher" ]]; then
        printf '[FAIL] member %s has a watcher, so it has an inbox socket: CLAUDE_CODE_HARBOR_KITE=0 did not turn it off\n' "$1" >&2
        return 1
    fi
    return 3
}

u20_pending_is() {
    local pending
    pending="$(u20_status_field "$1" pending)" || return 3
    [[ "${pending}" == "$2" ]] || return 3
}

u20_notices_at_least() {
    local count
    count="$("${U20[@]}" notices "$(u20_state "$1")" | wc -l)" || return 1
    ((count >= $2)) || return 3
}

# u20_start_sessions — launch all three, open their inputs, wait for each container, give
# each its standing orders; then a and b idle with a watcher, c idle with a background waiter.
u20_start_sessions() {
    local m
    for m in "${U20_MEMBERS[@]}"; do
        u20_launch "${m}" || return 1
    done
    for m in "${U20_MEMBERS[@]}"; do
        u20_open_input "${m}" || return 1
    done
    for m in "${U20_MEMBERS[@]}"; do
        u20_poll "${U20_START_S}" "member ${m}'s ccy container running" u20_running "${m}" || return 1
    done
    u20_tell a frame-orders socket || return 1
    u20_tell b frame-orders socket || return 1
    u20_tell c frame-orders wait || return 1
    for m in a b; do
        u20_poll "${U20_TURN_S}" "member ${m}'s watcher (pingbus status wake=watcher)" u20_wake_is "${m}" watcher || return 1
    done
    for m in "${U20_MEMBERS[@]}"; do
        u20_poll "${U20_TURN_S}" "member ${m}'s first turn (its orders) ended" u20_turns_at_least "${m}" 1 || return 1
    done
    u20_poll "${U20_TURN_S}" "member c's background waiter (pingbus status wake=waiter)" u20_wake_is c waiter || return 1
    printf '==> a and b idle with a watcher each; c idle with a background pingbus wait\n'
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

# M2.1 — b sits idle after its orders; a is told to send b a `review`; b, which nothing writes
# to again, wakes on its watcher's notice and acks it to a. Once b has drained its inbox, the
# second review for M2.2 goes out at once (from M1's host member a), so that its notice has
# the same count and falls inside the window in which the socket drops an identical body.
u20_check_review_ack() {
    local found review1 ack1 turns status=0
    turns="$(u20_turns b)" || return 1
    if [[ "${turns}" -ne 1 ]]; then
        printf '[FAIL] member b has ended %s turns before anything was sent to it, want 1\n' "${turns}" >&2
        return 1
    fi
    human_login || return 1
    u20_tell a frame-send review "${REF_PATH}" "${U20_HANDLE[b]}" || return 1
    u20_poll "${U20_TURN_S}" "a's review of ${REF_PATH} to b in the room" \
        u20_find_ping found "${U20_UID[a]}" review "${U20_UID[b]}" "${REF_PATH}" - || return 1
    review1="${found%%$'\t'*}"
    REVIEW1_TS="${found#*$'\t'}"
    u20_poll "${U20_TURN_S}" "b's watcher notice reached b's session" u20_notices_at_least b 1 || return 1
    u20_poll "${U20_TURN_S}" "b ran pingbus recv (its inbox empty again)" u20_pending_is b 0 || return 1
    send_ping a u20-send-review-2 review "${REF_COMMIT}" --to "${U20_HANDLE[b]}" || status=$?
    if [[ "${status}" -ne 0 ]]; then
        return "${status}"
    fi
    REVIEW2_ID="${SENT_ID}"
    u20_poll "${U20_TURN_S}" "b's ack of a's review in the room" \
        u20_find_ping found "${U20_UID[b]}" ack "${U20_UID[a]}" - "${review1}" || return 1
    ack1="${found%%$'\t'*}"
    u20_poll "${U20_TURN_S}" "b's woken turn ended (a second result in its stream)" u20_turns_at_least b 2 || return 1
    evidence "M2.1 review ${review1} from ccy member a (told on stdin), ack ${ack1} from ccy member b, whose only input was its orders; b's transcript holds the watcher's notice: u20/messages.json, u20/b/stdin.jsonl, u20/b/session.out"
}

# M2.2 — the second review reached b through a second notice with the same count (1 pending,
# 1 ping) and the next notice number, within the dedupe window, and b acked it.
u20_check_same_count() {
    local found ack2 m1a="@${A_HANDLE}:${SERVER_NAME}"
    if [[ -z "${REVIEW2_ID}" ]]; then
        printf '[FAIL] M2.1 sent no second review\n' >&2
        return 1
    fi
    u20_poll "${U20_TURN_S}" "the second review in the room" \
        u20_find_ping found "${m1a}" review "${U20_UID[b]}" "${REF_COMMIT}" - || return 1
    REVIEW2_TS="${found#*$'\t'}"
    u20_poll "${U20_TURN_S}" "b's ack of the second review in the room" \
        u20_find_ping found "${U20_UID[b]}" ack "${m1a}" - "${REVIEW2_ID}" || return 1
    ack2="${found%%$'\t'*}"
    "${U20[@]}" notices "$(u20_state b)" || return 1
    "${U20[@]}" expect-same-count "$(u20_state b)" 1 0 1 || return 1
    "${U20[@]}" expect-within-window "${REVIEW1_TS}" "${REVIEW2_TS}" || return 1
    if [[ "$(wc -l <"${U20_EVIDENCE}/b/stdin.jsonl")" -ne 1 ]]; then
        printf '[FAIL] the harness wrote to b after its orders\n' >&2
        return 1
    fi
    evidence "M2.2 review ${REVIEW2_ID} sent $(((REVIEW2_TS - REVIEW1_TS) / 1000)) s after the first, a second notice of 1 pending with a new number, ack ${ack2}: u20/b/transcript-*.jsonl"
}

# M2.3 — c has no inbox socket and waits with a background `pingbus wait`; M1's host member a
# sends it a review; the waiter ends, c wakes and acks, with no notice ever reaching it.
u20_check_wait_fallback() {
    local found ack3 review3 status=0 m1a="@${A_HANDLE}:${SERVER_NAME}"
    u20_poll "${U20_TURN_S}" "c's background waiter" u20_wake_is c waiter || return 1
    send_ping a u20-send-review-3 review "${REF_PATH}" --to "${U20_HANDLE[c]}" || status=$?
    if [[ "${status}" -ne 0 ]]; then
        return "${status}"
    fi
    review3="${SENT_ID}"
    u20_poll "${U20_TURN_S}" "c's ack of the review in the room" \
        u20_find_ping found "${U20_UID[c]}" ack "${m1a}" - "${review3}" || return 1
    ack3="${found%%$'\t'*}"
    u20_poll "${U20_TURN_S}" "c's woken turn ended (a second result in its stream)" u20_turns_at_least c 2 || return 1
    "${U20[@]}" expect-no-notices "$(u20_state c)" any || return 1
    if [[ "$(wc -l <"${U20_EVIDENCE}/c/stdin.jsonl")" -ne 1 ]]; then
        printf '[FAIL] the harness wrote to c after its orders\n' >&2
        return 1
    fi
    evidence "M2.3 review ${review3} to ccy member c (no inbox socket, a background pingbus wait), ack ${ack3}, no notice in c's transcript: u20/c/"
}

# M2.4 — the human posts a message mentioning a only, asking for an ack. a, woken by a notice
# counting a human, acks it; neither b nor c answers it, and no notice counting a human
# reaches b.
u20_check_human_addressed() {
    local message="${U20_EVIDENCE}/human-message.json" text response event txn found ack
    text="$("${U20[@]}" human-request "${HUMAN}")" || return 1
    "${CHECK[@]}" human-message "${text}" "${U20_UID[a]}" >"${message}" || return 1
    txn="acceptance-u20-$(date +%s%N)" || return 1
    response="$(human_curl PUT "/_matrix/client/v3/rooms/${ROOM_PATH}/send/m.room.message/${txn}" "${message}")" || return 1
    event="$(printf '%s' "${response}" | "${CHECK[@]}" event-id)" || return 1
    printf '==> %s posted %s, mentioning ccy member a only\n' "${HUMAN}" "${event}"
    u20_poll "${U20_TURN_S}" "a's ack of the human's message in the room" \
        u20_find_ping found "${U20_UID[a]}" ack "@${HUMAN}:${SERVER_NAME}" - "${event}" || return 1
    ack="${found%%$'\t'*}"
    printf '==> waiting %d s for any member it does not address to answer it\n' "${U20_SETTLE_S}"
    sleep "${U20_SETTLE_S}"
    human_get "/_matrix/client/v3/rooms/${ROOM_PATH}/messages?dir=b&limit=500" >"${U20_MESSAGES}" || return 1
    "${U20[@]}" expect-replies "${U20_MESSAGES}" "${event}" "${U20_UID[a]}" || return 1
    "${U20[@]}" expect-human-notice "$(u20_state a)" || return 1
    "${U20[@]}" expect-no-notices "$(u20_state b)" humans || return 1
    human_logout || return 1
    evidence "M2.4 human message ${event} to ccy member a only, ack ${ack} from a alone, no human notice for b: u20/human-message.json, u20/messages.json"
}

# ── the end of the sessions ──────────────────────────────────────────────────────────────

# u20_stop_session <m> — close its input (claude sees end of file and exits, and the container
# with it); after U20_EXIT_S, stop the container and the launcher. Its status is kept.
u20_stop_session() {
    local m="$1" fd="${U20_FD[$1]:-}" pid="${U20_PID[$1]:-}" deadline status=0
    if [[ -n "${fd}" ]]; then
        exec {fd}>&-
        unset 'U20_FD[$m]'
    fi
    if [[ -z "${pid}" ]]; then
        return 0
    fi
    deadline=$((SECONDS + U20_EXIT_S))
    while kill -0 "${pid}" 2>/dev/null && ((SECONDS < deadline)); do
        sleep 1
    done
    if kill -0 "${pid}" 2>/dev/null; then
        printf '==> member %s did not end within %d s of its input closing: stopping it\n' "${m}" "${U20_EXIT_S}"
        u20_remove_containers || return 1
        kill -TERM "${pid}" 2>/dev/null || printf '==> member %s ccy had already ended\n' "${m}"
    fi
    wait "${pid}" || status=$?
    unset 'U20_PID[$m]'
    printf '==> member %s: ccy exited %d\n' "${m}" "${status}"
}

# u20_keep_evidence <m> — the session's transcripts and its watcher's log, out of the checkout.
u20_keep_evidence() {
    local m="$1" state n=0 path
    state="$(u20_state "${m}")"
    for path in "${state}"/projects/*/*.jsonl; do
        if [[ -f "${path}" ]]; then
            n=$((n + 1))
            cp -- "${path}" "${U20_EVIDENCE}/${m}/transcript-${n}.jsonl" || return 1
        fi
    done
    if [[ -f "$(u20_home "${m}")/watch.log" ]]; then
        cp -- "$(u20_home "${m}")/watch.log" "${U20_EVIDENCE}/${m}/watch.log" || return 1
    fi
}

# u20_end_sessions — end every session, keep the evidence, scrub the ccy token's value from it
# (a hit is a FAIL: Claude Code wrote it somewhere), and remove the checkouts with their bundles.
u20_end_sessions() {
    local m held
    for m in "${U20_MEMBERS[@]}"; do
        u20_stop_session "${m}" || return 1
    done
    u20_remove_containers || return 1
    if [[ "${U20_CHECKOUTS_PRESENT}" -ne 1 ]]; then
        return 0
    fi
    for m in "${U20_MEMBERS[@]}"; do
        u20_keep_evidence "${m}" || return 1
    done
    rm -rf -- "${U20_CHECKOUTS}" || return 1
    U20_CHECKOUTS_PRESENT=0
    printf '==> the ccy checkouts are removed; their transcripts are in %s\n' "${U20_EVIDENCE}"
    held="$("${U20[@]}" scrub "${PLAN_REPO_ROOT}" "${U20_EVIDENCE}")" || return 1
    if [[ -n "${held}" ]]; then
        printf '[FAIL] the ccy token value was written into these files (now replaced with a placeholder):\n%s\n' "${held}" >&2
        return 1
    fi
}

# On the way out of any run that stopped early (plan_on_cleanup), before the team goes.
u20_teardown_after_stop() {
    if [[ "${#U20_PID[@]}" -eq 0 && "${U20_CHECKOUTS_PRESENT}" -ne 1 ]]; then
        return 0
    fi
    printf '==> the run stopped with ccy acceptance sessions or checkouts in place: removing them\n'
    u20_end_sessions
}
