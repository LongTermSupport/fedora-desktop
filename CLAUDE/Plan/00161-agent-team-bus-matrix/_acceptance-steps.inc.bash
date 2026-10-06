# shellcheck shell=bash
# _acceptance-steps.inc.bash — the leg and check logic of Plan 00161's acceptance.bash (U17,
# milestone M1). A separate file for the reason _deploy-steps.inc.bash is one: legs are called
# through plan_deploy_leg and check_leg, which ShellCheck cannot see through (SC2329), and
# suppression is banned. Sourced, never executed: no shell options, no `exit`. Every function
# returns non-zero on its first failure, explicitly, since a leg runs where errexit is off.
# A check returns 0 PASS, 1 FAIL, or 2 COULD NOT ESTABLISH (only when GitHub, the forge every
# reference is checked at, did not answer).
#
# THE MEMBERS are two transient users, not accounts on the host: each pingbus command runs as
# its own `systemd-run` service with DynamicUser=yes and the member's own User= name, so the
# two members are two different UIDs, neither of them the desktop user (who may hold a human's
# Element session, DESIGN.md section 8) and neither written to /etc/passwd. Each member's
# PINGBUS_HOME is its StateDirectory (/var/lib/agent-bus-acceptance-<m>), which outlives the
# single commands and is removed on the way out. The bundle reaches it as a tar extracted by
# the member itself, so the member owns its token, as pingbus requires.
#
# Reads acceptance.bash's globals: TEAM, HUMAN, BUS_ADDRESS, TEAM_FILE, MEMBER_DIR, CHECK,
# AGENT_BUS, PINGBUS, REPORT, and the plan library's PLAN_RUN_DIR, PLAN_REPO_ROOT and
# PLAN_SCRIPT_DIR. Uses _deploy-steps.inc.bash's run_installer, install_team and remove_team.
# Sets TEAM_PRESENT, MEMBERS_PRESENT, the REF_* and member globals below.

readonly UNIT_PREFIX="agent-bus-acceptance"
readonly MEMBER_REPO="acceptance"
readonly MEMBER_HOST="acceptance"
#: Member a's ack deadline, the lowest pingbus accepts (§10), so TIMEOUT falls due in a minute.
readonly ACK_TIMEOUT_S=60
readonly HUMAN_TEXT="M1 acceptance: a message addressed to one member only"
MEMBERS_PRESENT=0
B_WAIT_PID=""

member_name() { printf '%s-%s' "${UNIT_PREFIX}" "$1"; }

# member_run <member> <label> <stdin-file> <argv...> — run argv as the member's transient
# user, with its PINGBUS_HOME and the acceptance team active. Its stdout and stderr are kept
# as <label>.out and <label>.err in the member's run directory. Returns the command's status
# (systemd-run --wait passes it through).
member_run() {
    local member="$1" label="$2" input="$3" name
    shift 3
    name="$(member_name "${member}")"
    # The redirections are the desktop user's own files, opened before sudo runs: --pipe
    # hands the open descriptors to the service.
    {
        sudo -n systemd-run --quiet --pipe --wait --collect --service-type=exec \
            "--unit=${name}-${label}" --property=DynamicUser=yes "--property=User=${name}" \
            "--property=StateDirectory=${name}" \
            "--setenv=PINGBUS_HOME=/var/lib/${name}" "--setenv=PINGBUS_TEAMS=${TEAM}" -- "$@"
    } <"${input}" >"${MEMBER_DIR}/${member}/${label}.out" 2>"${MEMBER_DIR}/${member}/${label}.err"
}

# show <member> <label> — what a member_run printed, for the log.
show() {
    local base="${MEMBER_DIR}/$1/$2"
    printf -- '--- %s %s: stdout\n' "$1" "$2"
    cat -- "${base}.out"
    printf -- '--- %s %s: stderr\n' "$1" "$2"
    cat -- "${base}.err"
}

# expect_status <member> <label> <status> <allowed...> — FAIL unless status is one allowed.
expect_status() {
    local member="$1" label="$2" status="$3" allowed
    shift 3
    for allowed in "$@"; do
        if [[ "${status}" -eq "${allowed}" ]]; then
            return 0
        fi
    done
    printf '[FAIL] member %s %s exited %d, want %s\n' "${member}" "${label}" "${status}" "$*" >&2
    return 1
}

evidence() {
    printf -- '- %s\n' "$*" >>"${REPORT}"
}

# ── setup legs ───────────────────────────────────────────────────────────────────────────

# resolve_reference — what the pings reference: this plan's DESIGN.md, and the commit, at the
# tip of this checkout's upstream branch as last fetched. Every commit on it is identical to or
# behind the branch on GitHub, so it resolves there (§6) without anything being pushed now.
resolve_reference() {
    local current remote merge url sha planPath
    if ! current="$(git -C "${PLAN_REPO_ROOT}" symbolic-ref --quiet --short HEAD)"; then
        printf '[FAIL] the checkout is on no branch: check out the branch it deploys from\n' >&2
        return 1
    fi
    if ! remote="$(git -C "${PLAN_REPO_ROOT}" config --get "branch.${current}.remote")" \
        || ! merge="$(git -C "${PLAN_REPO_ROOT}" config --get "branch.${current}.merge")"; then
        printf '[FAIL] branch %s tracks no upstream: the pings need a commit GitHub holds\n' "${current}" >&2
        return 1
    fi
    REF_BRANCH="${merge#refs/heads/}"
    url="$(git -C "${PLAN_REPO_ROOT}" remote get-url "${remote}")" || return 1
    REF_REPO="$("${CHECK[@]}" github-repo "${url}")" || return 1
    if ! sha="$(git -C "${PLAN_REPO_ROOT}" rev-parse --verify --quiet "refs/remotes/${remote}/${REF_BRANCH}^{commit}")"; then
        printf '[FAIL] no remote-tracking ref %s/%s: fetch it\n' "${remote}" "${REF_BRANCH}" >&2
        return 1
    fi
    planPath="${PLAN_SCRIPT_DIR#"${PLAN_REPO_ROOT}"/}"
    if ! git -C "${PLAN_REPO_ROOT}" cat-file -e "${sha}:${planPath}/DESIGN.md"; then
        printf '[FAIL] %s/DESIGN.md is not in %s/%s at %s: fetch it\n' "${planPath}" "${remote}" "${REF_BRANCH}" "${sha}" >&2
        return 1
    fi
    REF_PREFIX="${planPath}/"
    REF_PATH="path:${REF_REPO}@${sha}:${planPath}/DESIGN.md"
    REF_COMMIT="commit:${REF_REPO}@${sha}"
    printf '==> references: %s and %s (branch %s trusted)\n' "${REF_PATH}" "${REF_COMMIT}" "${REF_BRANCH}"
}

# write_acceptance_team_file — the team on the bus address, on a port free on loopback now.
write_acceptance_team_file() {
    local port
    port="$(python3 -I -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')" || return 1
    "${CHECK[@]}" team-file "${TEAM}" "${port}" "${BUS_ADDRESS}" "${HUMAN}" "${REF_REPO}" \
        "${REF_BRANCH}" "${REF_PREFIX}" >"${TEAM_FILE}" || return 1
    printf '==> team file %s:\n' "${TEAM_FILE}"
    cat -- "${TEAM_FILE}"
}

# add_member <member> <role> — `agent-bus add-member` writes the bundle into the run directory
# for the desktop user (the wrapper's --out); member a's gets the short ack deadline.
add_member() {
    local member="$1" role="$2" out="${MEMBER_DIR}/$1/${TEAM}"
    mkdir -p -- "${MEMBER_DIR}/${member}" || return 1
    sudo -n "${AGENT_BUS}" add-member "${TEAM}" "--repo=${MEMBER_REPO}" "--host=${MEMBER_HOST}" \
        --type=host "--role=${role}" "--address=${BUS_ADDRESS}" "--out=${out}" || return 1
    if [[ "${member}" == "a" ]]; then
        "${CHECK[@]}" set-limit "${out}/member.json" ack_timeout_s "${ACK_TIMEOUT_S}" || return 1
    fi
}

add_members() {
    add_member a orchestrator || return 1
    add_member b worker || return 1
    local bundle="${MEMBER_DIR}/a/${TEAM}/member.json"
    A_HANDLE="$("${CHECK[@]}" handle "${bundle}")" || return 1
    B_HANDLE="$("${CHECK[@]}" handle "${MEMBER_DIR}/b/${TEAM}/member.json")" || return 1
    SERVER_NAME="$("${CHECK[@]}" member-field "${bundle}" server_name)" || return 1
    ROOM_ID="$("${CHECK[@]}" member-field "${bundle}" room)" || return 1
    ROOM_PATH="$("${CHECK[@]}" room-path "${ROOM_ID}")" || return 1
    BASE_URL="$("${CHECK[@]}" member-field "${bundle}" base_url)" || return 1
    printf '==> member a %s (orchestrator, ack deadline %d s), member b %s (worker), at %s\n' \
        "${A_HANDLE}" "${ACK_TIMEOUT_S}" "${B_HANDLE}" "${BASE_URL}"
}

# place_member <member> — the member extracts its own bundle into its PINGBUS_HOME, so it owns
# the token; the run directory's copy of the token is then removed. Then `config check`, and a
# first `recv`, which accepts admin's invite, joins and verifies the room (§8): nothing is
# pending yet, so it must exit 3.
place_member() {
    local member="$1" dir="${MEMBER_DIR}/$1" status=0
    tar -c --no-recursion -f "${dir}/bundle.tar" -C "${dir}" \
        "${TEAM}" "${TEAM}/member.json" "${TEAM}/token" || return 1
    MEMBERS_PRESENT=1
    member_run "${member}" place "${dir}/bundle.tar" \
        tar -x --no-same-owner -f - -C "/var/lib/$(member_name "${member}")" || status=$?
    rm -f -- "${dir}/bundle.tar" "${dir}/${TEAM}/token" || return 1
    show "${member}" place
    expect_status "${member}" place "${status}" 0 || return 1
    status=0
    member_run "${member}" config-check /dev/null "${PINGBUS}" config check || status=$?
    show "${member}" config-check
    expect_status "${member}" config-check "${status}" 0 || return 1
    status=0
    member_run "${member}" join /dev/null "${PINGBUS}" recv || status=$?
    show "${member}" join
    expect_status "${member}" join "${status}" 3
}

place_members() {
    place_member a || return 1
    place_member b
}

# ── teardown ─────────────────────────────────────────────────────────────────────────────

# remove_members — stop every acceptance member unit still running (a waiter of a failed run),
# remove both members' state directories and the run directory's bundle copies.
remove_members() {
    local units unit rest member name
    units="$(systemctl list-units --all --plain --no-legend --type=service "${UNIT_PREFIX}-*")" || return 1
    while read -r unit rest; do
        if [[ -z "${unit}" ]]; then
            continue
        fi
        sudo -n systemctl stop "${unit}" || return 1
        printf '==> stopped %s (%s)\n' "${unit}" "${rest}"
    done <<<"${units}"
    for member in a b; do
        name="$(member_name "${member}")"
        sudo -n rm -rf -- "/var/lib/private/${name}" "/var/lib/${name}" || return 1
        rm -f -- "${MEMBER_DIR}/${member}/bundle.tar" "${MEMBER_DIR}/${member}/${TEAM}/token" || return 1
    done
    MEMBERS_PRESENT=0
    printf '==> no acceptance member units or state directories remain\n'
}

# teardown <label> — the members, then the team with its data (--purge).
teardown() {
    remove_members || return 1
    remove_team "remove-$1"
}

# On the way out of any run that stopped early (plan_on_cleanup).
teardown_after_stop() {
    if [[ "${TEAM_PRESENT}" -ne 1 && "${MEMBERS_PRESENT}" -ne 1 ]]; then
        return 0
    fi
    printf '==> the run stopped with acceptance members or the %s team in place: removing them\n' "${TEAM}"
    if ! teardown after-stop; then
        printf '[FAIL] the acceptance members or team could not be removed; run acceptance.bash again, which removes them first\n' >&2
        return 1
    fi
}

# ── the human, by curl ───────────────────────────────────────────────────────────────────

# human_curl <method> <path> <body-file> — one client API call as the human. The token
# reaches curl as a config line on stdin, never in argv; no proxy.
human_curl() {
    printf 'header = "Authorization: Bearer %s"\n' "${HUMAN_TOKEN}" \
        | curl --config - --silent --show-error --fail-with-body --noproxy '*' --max-time 30 \
            -X "$1" -H 'Content-Type: application/json' --data-binary "@$3" "${BASE_URL}$2"
}

# human_login — a fresh password from `agent-bus human password` (printed once, kept only in
# this shell), a password login, and the human's join of the team room it was invited to.
human_login() {
    local password response empty="${PLAN_RUN_DIR}/empty.json"
    password="$(sudo -n "${AGENT_BUS}" human password "${TEAM}" "${HUMAN}")" || return 1
    response="$(printf '%s' "${password}" | "${CHECK[@]}" login-body "${HUMAN}" \
        | curl --silent --show-error --fail-with-body --noproxy '*' --max-time 30 \
            -H 'Content-Type: application/json' --data-binary @- "${BASE_URL}/_matrix/client/v3/login")" || return 1
    HUMAN_TOKEN="$(printf '%s' "${response}" | "${CHECK[@]}" access-token)" || return 1
    printf '{}' >"${empty}" || return 1
    human_curl POST "/_matrix/client/v3/rooms/${ROOM_PATH}/join" "${empty}" \
        >"${PLAN_RUN_DIR}/human-join.json" || return 1
    printf '==> %s logged in with a fresh password and joined the team room\n' "${HUMAN}"
}

human_logout() {
    human_curl POST /_matrix/client/v3/logout "${PLAN_RUN_DIR}/empty.json" >/dev/null || return 1
    HUMAN_TOKEN=""
}

# ── the M1 checks ────────────────────────────────────────────────────────────────────────

# send_ping <member> <label> <argv...> — `pingbus send` as the member; 0 sent, else its
# verdict (send-outcome: 2 when the forge did not answer). SENT_ID is the event ID.
send_ping() {
    local member="$1" label="$2" status=0 outcome=0
    shift 2
    member_run "${member}" "${label}" /dev/null "${PINGBUS}" send "$@" || status=$?
    show "${member}" "${label}"
    "${CHECK[@]}" send-outcome "${status}" "${MEMBER_DIR}/${member}/${label}.err" || outcome=$?
    if [[ "${outcome}" -eq 2 ]]; then
        printf '[UNKNOWN] member %s send exited %d: GitHub, where the reference is checked, did not answer\n' \
            "${member}" "${status}" >&2
        return 2
    fi
    if [[ "${outcome}" -ne 0 ]]; then
        printf '[FAIL] member %s send exited %d\n' "${member}" "${status}" >&2
        return 1
    fi
    SENT_ID="$("${CHECK[@]}" sent-event "${MEMBER_DIR}/${member}/${label}.out" "${TEAM}")"
}

# stop_waiter — end member b's background `wait` early (a send failed) and reap it.
stop_waiter() {
    sudo -n systemctl stop "$(member_name b)-wait-review.service" || return 1
    wait "${B_WAIT_PID}" || printf '==> member b wait ended by stop (status %d)\n' "$?"
    B_WAIT_PID=""
}

# check_review_ack — b waits; a sends `review` (a path reference) to b; b's wait prints exactly
# that PING; b sends `ack --re` it to a; a's recv prints exactly that ack.
check_review_ack() {
    local status=0 reviewId ackId
    member_run b wait-review /dev/null "${PINGBUS}" wait --timeout 180 &
    B_WAIT_PID=$!
    send_ping a send-review review "${REF_PATH}" --to "${B_HANDLE}" || status=$?
    if [[ "${status}" -ne 0 ]]; then
        stop_waiter || return 1
        return "${status}"
    fi
    reviewId="${SENT_ID}"
    wait "${B_WAIT_PID}" || status=$?
    B_WAIT_PID=""
    show b wait-review
    expect_status b wait-review "${status}" 0 || return 1
    "${CHECK[@]}" expect-ping "${MEMBER_DIR}/b/wait-review.out" "${TEAM}" "${reviewId}" \
        "${A_HANDLE}" review "${REF_PATH}" - || return 1
    evidence "review ${reviewId} sent by a, received by b's wait: members/a/send-review.out, members/b/wait-review.out"
    send_ping b send-ack ack --re "${reviewId}" --to "${A_HANDLE}" || return $?
    ackId="${SENT_ID}"
    member_run a recv-ack /dev/null "${PINGBUS}" recv || status=$?
    show a recv-ack
    expect_status a recv-ack "${status}" 0 || return 1
    "${CHECK[@]}" expect-ping "${MEMBER_DIR}/a/recv-ack.out" "${TEAM}" "${ackId}" "${B_HANDLE}" \
        ack - "${reviewId}" || return 1
    evidence "ack ${ackId} from b received by a's recv: members/b/send-ack.out, members/a/recv-ack.out"
}

# check_human_addressed — the human, by curl, posts an m.text mentioning a only. a's recv
# prints exactly that HUMAN line; b's recv prints nothing naming it, and no HUMAN line.
check_human_addressed() {
    local status=0 message="${PLAN_RUN_DIR}/human-message.json" response eventId txn
    human_login || return 1
    "${CHECK[@]}" human-message "${HUMAN_TEXT}" "@${A_HANDLE}:${SERVER_NAME}" >"${message}" || return 1
    txn="acceptance-$(date +%s%N)" || return 1
    response="$(human_curl PUT "/_matrix/client/v3/rooms/${ROOM_PATH}/send/m.room.message/${txn}" "${message}")" || return 1
    eventId="$(printf '%s' "${response}" | "${CHECK[@]}" event-id)" || return 1
    human_logout || return 1
    printf '==> %s posted %s, mentioning %s only\n' "${HUMAN}" "${eventId}" "${A_HANDLE}"
    member_run a recv-human /dev/null "${PINGBUS}" recv || status=$?
    show a recv-human
    expect_status a recv-human "${status}" 0 || return 1
    "${CHECK[@]}" expect-human "${MEMBER_DIR}/a/recv-human.out" "${TEAM}" "${eventId}" "${HUMAN}" \
        "${HUMAN_TEXT}" || return 1
    status=0
    member_run b recv-human /dev/null "${PINGBUS}" recv || status=$?
    show b recv-human
    expect_status b recv-human "${status}" 0 3 || return 1
    "${CHECK[@]}" expect-absent "${MEMBER_DIR}/b/recv-human.out" "${eventId}" || return 1
    evidence "human message ${eventId} delivered to a only: human-message.json, members/a/recv-human.out, members/b/recv-human.out"
}

# check_timeout — a sends `review` (a commit reference) to b, who never answers; a's wait
# prints exactly one TIMEOUT, for that ping and b, once a's ack deadline passes. The review b
# acked earlier is past the same deadline by then, so no TIMEOUT for it proves the ack counted.
check_timeout() {
    local status=0 pingId
    send_ping a send-unanswered review "${REF_COMMIT}" --to "${B_HANDLE}" || return $?
    pingId="${SENT_ID}"
    printf '==> waiting for a TIMEOUT for %s, due %d s after it was sent\n' "${pingId}" "${ACK_TIMEOUT_S}"
    member_run a wait-timeout /dev/null "${PINGBUS}" wait --timeout 240 || status=$?
    show a wait-timeout
    expect_status a wait-timeout "${status}" 0 || return 1
    "${CHECK[@]}" expect-timeout "${MEMBER_DIR}/a/wait-timeout.out" "${TEAM}" "${pingId}" \
        "${B_HANDLE}" review "${REF_COMMIT}" || return 1
    evidence "TIMEOUT for ${pingId} (b silent), none for the acked review: members/a/wait-timeout.out"
}
