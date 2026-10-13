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
#: Where systemd keeps a DynamicUser's StateDirectory; /var/lib/<name> is a symlink into it.
readonly PRIVATE_STATE_ROOT="/var/lib/private"
# Whether it was there when this run started; it is only stat'ed here, which needs no root.
if [[ -d "${PRIVATE_STATE_ROOT}" ]]; then
    PRIVATE_STATE_ROOT_EXISTED=1
else
    PRIVATE_STATE_ROOT_EXISTED=0
fi
readonly MEMBER_REPO="acceptance"
readonly MEMBER_HOST="acceptance"
#: Member a's ack deadline, the lowest pingbus accepts (§10), so TIMEOUT falls due in a minute.
readonly ACK_TIMEOUT_S=60
readonly HUMAN_TEXT="M1 acceptance: a message addressed to one member only"
readonly PEER_TEXT="M1 acceptance: a note from member a to member b"
readonly PEER_HUMAN_TEXT="M1 acceptance: a note from member a to the human only"
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
    # hands the open descriptors to the service. --expand-environment=no: systemd-run
    # otherwise expands $NAME in the arguments as ExecStart= does, and a Matrix event ID
    # starts with `$`, so `ack --re <event>` arrived with no event (journal 26-10-07).
    {
        sudo -n systemd-run --quiet --pipe --wait --collect --service-type=exec --expand-environment=no \
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
    # pingbus send names the branch a ccy session's checkout tracks on origin (D64); M2's
    # sessions send without --branch, so the upstream must be on origin for that to happen.
    if [[ "${remote}" != "origin" ]]; then
        printf '[FAIL] branch %s tracks %s, not origin: pingbus send names only an origin branch\n' "${current}" "${remote}" >&2
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
    REF_ORG="${REF_REPO%%/*}"
    printf '==> references: %s and %s (org %s trusted, on branch %s)\n' "${REF_PATH}" "${REF_COMMIT}" \
        "${REF_ORG}" "${REF_BRANCH}"
}

# write_acceptance_team_file — the team on the bus address, on a port free on loopback now. It
# trusts the reference repository's org and lists no repository (D64).
write_acceptance_team_file() {
    local port
    port="$(free_loopback_port)" || return 1
    # FORGE_API "-": pingbus's own GitHub API, where every reference is checked.
    "${CHECK[@]}" team-file "${TEAM}" "${port}" "${BUS_ADDRESS}" "${HUMAN}" - - "${REF_PREFIX}" - \
        "${REF_ORG}" >"${TEAM_FILE}" || return 1
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
    # Not tar -x: under the service's seccomp filter (DynamicUser implies RestrictSUIDSGID)
    # GNU tar's file opens fail with ENOSYS, "Function not implemented" (journal 26-10-07),
    # while Python opens with plain openat. The data filter refuses links, absolute paths and
    # any member outside the target, and does not restore ownership.
    member_run "${member}" place "${dir}/bundle.tar" python3 -I -c '
import sys, tarfile
with tarfile.open(fileobj=sys.stdin.buffer, mode="r|") as bundle:
    bundle.extractall(sys.argv[1], filter="data")
' "/var/lib/$(member_name "${member}")" || status=$?
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

# remove_private_state_root — systemd creates /var/lib/private (0700) for the first
# DynamicUser StateDirectory on a host. When this run found none, remove it again if the
# members' directories were all it held; anything else in it is another service's, and stays.
remove_private_state_root() {
    local empty
    if [[ "${PRIVATE_STATE_ROOT_EXISTED}" -eq 1 || ! -d "${PRIVATE_STATE_ROOT}" ]]; then
        return 0
    fi
    empty="$(sudo -n find "${PRIVATE_STATE_ROOT}" -maxdepth 0 -empty)" || return 1
    if [[ -z "${empty}" ]]; then
        printf '==> %s, created during this run, now holds another service'\''s state: it stays\n' "${PRIVATE_STATE_ROOT}"
        return 0
    fi
    sudo -n rmdir -- "${PRIVATE_STATE_ROOT}" || return 1
    printf '==> removed %s, which this run created\n' "${PRIVATE_STATE_ROOT}"
}

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
        sudo -n rm -rf -- "${PRIVATE_STATE_ROOT}/${name}" "/var/lib/${name}" || return 1
        rm -f -- "${MEMBER_DIR}/${member}/bundle.tar" "${MEMBER_DIR}/${member}/${TEAM}/token" || return 1
    done
    remove_private_state_root || return 1
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
    send_ping a send-review review "${REF_PATH}" --branch "${REF_BRANCH}" --to "${B_HANDLE}" || status=$?
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
    send_ping a send-unanswered review "${REF_COMMIT}" --branch "${REF_BRANCH}" --to "${B_HANDLE}" || return $?
    pingId="${SENT_ID}"
    printf '==> waiting for a TIMEOUT for %s, due %d s after it was sent\n' "${pingId}" "${ACK_TIMEOUT_S}"
    member_run a wait-timeout /dev/null "${PINGBUS}" wait --timeout 240 || status=$?
    show a wait-timeout
    expect_status a wait-timeout "${status}" 0 || return 1
    "${CHECK[@]}" expect-timeout "${MEMBER_DIR}/a/wait-timeout.out" "${TEAM}" "${pingId}" \
        "${B_HANDLE}" review "${REF_COMMIT}" || return 1
    evidence "TIMEOUT for ${pingId} (b silent), none for the acked review: members/a/wait-timeout.out"
}

# say_text <member> <label> <text> <to> — `pingbus say --to <to>` as the member, the text on
# stdin; SENT_ID is the event ID.
say_text() {
    local member="$1" label="$2" input="${MEMBER_DIR}/$1/$2.in" status=0
    printf '%s\n' "$3" >"${input}" || return 1
    member_run "${member}" "${label}" "${input}" "${PINGBUS}" say --to "$4" || status=$?
    show "${member}" "${label}"
    expect_status "${member}" "${label}" "${status}" 0 || return 1
    SENT_ID="$("${CHECK[@]}" sent-event "${MEMBER_DIR}/${member}/${label}.out" "${TEAM}")"
}

# check_peer_text — a says one text to b and one to the human only. b's recv prints exactly
# one PEER line, the text that names it, from a; the text b is not named in reaches it not
# at all (DESIGN.md D63).
check_peer_text() {
    local status=0 peerId humanOnlyId
    say_text a say-peer "${PEER_TEXT}" "${B_HANDLE}" || return 1
    peerId="${SENT_ID}"
    say_text a say-human "${PEER_HUMAN_TEXT}" "${HUMAN}" || return 1
    humanOnlyId="${SENT_ID}"
    member_run b recv-peer /dev/null "${PINGBUS}" recv || status=$?
    show b recv-peer
    expect_status b recv-peer "${status}" 0 || return 1
    "${CHECK[@]}" expect-peer "${MEMBER_DIR}/b/recv-peer.out" "${TEAM}" "${peerId}" "${A_HANDLE}" \
        "${PEER_TEXT}" || return 1
    evidence "agent text ${peerId} from a delivered to b as PEER; ${humanOnlyId}, to the human only, not delivered to b: members/a/say-peer.out, members/a/say-human.out, members/b/recv-peer.out"
}

# ── the D64 checks: the org's branches, forks, outside repositories ──────────────────────
# Each is a `done` from a to b: it needs no ack, so it leaves nothing for M1.3's TIMEOUT.

#: A public repository outside any team's org, not a fork, whose first commit GitHub has
#: served unchanged for over a decade: the outside reference.
readonly EXTERNAL_COMMIT="commit:octocat/hello-world@7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

# d64_delivered <label> <trust> <branch|-> <ref> [send flags...] — a sends `done <ref>` to b;
# b's recv prints exactly that PING, marked <trust>, naming <branch>.
d64_delivered() {
    local label="$1" trust="$2" branch="$3" ref="$4" status=0
    shift 4
    send_ping a "send-${label}" "done" "${ref}" "$@" --to "${B_HANDLE}" || return $?
    member_run b "recv-${label}" /dev/null "${PINGBUS}" recv || status=$?
    show b "recv-${label}"
    expect_status b "recv-${label}" "${status}" 0 || return 1
    "${CHECK[@]}" expect-marked "${MEMBER_DIR}/b/recv-${label}.out" "${TEAM}" "${SENT_ID}" "${A_HANDLE}" \
        "done" "${ref}" "${trust}" "${branch}" || return 1
    evidence "${label}: ${ref} delivered ${trust}, branch ${branch}: members/a/send-${label}.out, members/b/recv-${label}.out"
}

# d64_refused <label> <code> <send argv...> — a's `pingbus send` is refused with <code>, and
# nothing is sent. 2 when GitHub did not answer the reference check.
d64_refused() {
    local label="$1" code="$2" status=0 outcome=0 err="${MEMBER_DIR}/a/send-$1.err"
    shift 2
    member_run a "send-${label}" /dev/null "${PINGBUS}" send "$@" --to "${B_HANDLE}" || status=$?
    show a "send-${label}"
    "${CHECK[@]}" send-outcome "${status}" "${err}" || outcome=$?
    if [[ "${outcome}" -eq 2 ]]; then
        printf '[UNKNOWN] member a send exited %d: GitHub, where the reference is checked, did not answer\n' "${status}" >&2
        return 2
    fi
    "${CHECK[@]}" refused-as "${status}" "${err}" "${code}" || return 1
    evidence "${label}: refused, ${code}: members/a/send-${label}.err"
}

# d64_branches — the reference repository's default branch and its tip on GitHub now
# (D64_DEFAULT, D64_DEFAULT_SHA), and the first other branch whose tip this checkout holds and
# the default branch does not contain (D64_SIDE, D64_SIDE_SHA). 2 when GitHub did not answer.
d64_branches() {
    local head="${PLAN_RUN_DIR}/d64-head.txt" heads="${PLAN_RUN_DIR}/d64-heads.txt" sha ref name status
    if ! git -C "${PLAN_REPO_ROOT}" ls-remote --symref origin HEAD >"${head}" \
        || ! git -C "${PLAN_REPO_ROOT}" ls-remote --heads origin >"${heads}"; then
        printf '[UNKNOWN] git ls-remote origin did not answer: the branches of %s are not known\n' "${REF_REPO}" >&2
        return 2
    fi
    D64_DEFAULT="$("${CHECK[@]}" symref-head <"${head}")" || return 1
    D64_DEFAULT_SHA="" D64_SIDE="" D64_SIDE_SHA=""
    while IFS=$'\t' read -r sha ref; do
        if [[ "${ref}" == "refs/heads/${D64_DEFAULT}" ]]; then
            D64_DEFAULT_SHA="${sha}"
        fi
    done <"${heads}"
    if [[ -z "${D64_DEFAULT_SHA}" ]] || ! git -C "${PLAN_REPO_ROOT}" cat-file -e "${D64_DEFAULT_SHA}^{commit}"; then
        printf '[FAIL] this checkout does not hold the tip of %s on GitHub: git fetch origin\n' "${D64_DEFAULT}" >&2
        return 1
    fi
    while IFS=$'\t' read -r sha ref; do
        name="${ref#refs/heads/}"
        if [[ "${name}" == "${D64_DEFAULT}" ]] || ! git -C "${PLAN_REPO_ROOT}" cat-file -e "${sha}^{commit}"; then
            continue
        fi
        status=0
        git -C "${PLAN_REPO_ROOT}" merge-base --is-ancestor "${sha}" "${D64_DEFAULT_SHA}" || status=$?
        case "${status}" in
            0) continue ;;
            1)
                D64_SIDE="${name}" D64_SIDE_SHA="${sha}"
                break
                ;;
            *) return 1 ;;
        esac
    done <"${heads}"
    if [[ -z "${D64_SIDE}" ]]; then
        printf '[FAIL] no branch of %s but %s holds a commit %s lacks that this checkout has: git fetch origin\n' \
            "${REF_REPO}" "${D64_DEFAULT}" "${D64_DEFAULT}" >&2
        return 1
    fi
    printf '==> %s: default branch %s at %s; branch %s at %s, not on %s\n' "${REF_REPO}" "${D64_DEFAULT}" \
        "${D64_DEFAULT_SHA}" "${D64_SIDE}" "${D64_SIDE_SHA}" "${D64_DEFAULT}"
}

# check_org_trust — D64 at the forge: an unlisted repository of the org is trusted on any of
# its branches, the branch named; with none named, its default branch; a commit claimed on a
# branch that does not contain it is refused (the fork network's commits are served by the
# parent's API, so being found proves nothing). A public repository outside the org arrives
# marked external, and a verb that would act on it is refused.
check_org_trust() {
    local status=0
    d64_branches || return $?
    d64_delivered org-branch trusted "${D64_SIDE}" "commit:${REF_REPO}@${D64_SIDE_SHA}" \
        --branch "${D64_SIDE}" || return $?
    d64_delivered org-default trusted - "commit:${REF_REPO}@${D64_DEFAULT_SHA}" || return $?
    d64_refused org-wrong-branch provenance "done" "commit:${REF_REPO}@${D64_SIDE_SHA}" \
        --branch "${D64_DEFAULT}" || return $?
    d64_delivered external external - "${EXTERNAL_COMMIT}" || return $?
    d64_refused external-run-qa allowlist run-qa "${EXTERNAL_COMMIT}" || status=$?
    return "${status}"
}

# check_org_fork — a fork the org owns is refused, though its owner is the org. SKIPPED-NEEDS-
# OWNER when the org owns no public fork among its first 100 repositories.
check_org_fork() {
    local listing="${PLAN_RUN_DIR}/d64-org-repos.json" fork sha
    if ! curl --silent --show-error --fail-with-body --max-time 30 -H 'Accept: application/vnd.github+json' \
        "https://api.github.com/users/${REF_ORG}/repos?type=owner&per_page=100" >"${listing}"; then
        printf '[UNKNOWN] GitHub did not list the repositories of %s\n' "${REF_ORG}" >&2
        return 2
    fi
    if ! fork="$("${CHECK[@]}" org-fork "${REF_ORG}" <"${listing}")"; then
        needs_owner "a public fork owned by ${REF_ORG} (fork any public repository into it), to show a fork is refused"
        return 3
    fi
    if ! sha="$(git ls-remote "https://github.com/${fork}" HEAD)" || [[ -z "${sha}" ]]; then
        printf '[UNKNOWN] git ls-remote did not answer for %s\n' "${fork}" >&2
        return 2
    fi
    d64_refused org-fork provenance "done" "commit:${fork}@${sha%%$'\t'*}"
}
