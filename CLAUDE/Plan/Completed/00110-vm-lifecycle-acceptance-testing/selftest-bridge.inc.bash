# shellcheck shell=bash
#
# selftest-bridge.inc.bash — the cases and primitives shared by selftest-bridge.bash (T4.8)
# and selftest-liveness.bash (T4.9). Sourced, never executed. The scripts set the globals
# below after their R1 bootstrap and before any case runs:
#
#   SPOOL STATE_DIR CONFIG_DIR PATH_UNIT SERVICE_UNIT HEARTBEAT_UNIT REQUESTER REPORT
#
# Every case writes exactly one line to REPORT and returns 0 (as designed) or 1 (not).
# Cleanup state is kept in the PLANTED array and the *_PLANTED / *_ASIDE flags so that the
# scripts' EXIT traps can undo everything a case did, including an interrupted one.

RESPONSE_WAIT_SECONDS=20
PLANTED=()
POLICY_ASIDE=""
LOCK_PLANTED=0
SYMLINK_PLANTED=0
OUTSIDE_DIR=""
REQUEST_NAME=""

out() { printf '%s\n' "$*" >>"${REPORT}"; }
stamp() { date -u +%Y%m%dT%H%M%SZ; }
nonce() { head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \n'; }
request_count() { find "${SPOOL}/requests" -mindepth 1 -maxdepth 1 | wc -l; }

# bridge_remedy — the exact command the heartbeat hands a human (verdict.remedy_for): clear
# the failed units and make sure the path unit is listening again.
bridge_remedy() {
    systemctl --user reset-failed "${PATH_UNIT}" "${SERVICE_UNIT}"
    systemctl --user start "${PATH_UNIT}"
}

# selftest_cleanup — undo everything any case may have left: restore the policy, drop the
# planted lock, put requests/ back, remove every planted file, and run the remedy (the
# hostile-spool cases stop the path unit and fail the service unit BY DESIGN).
selftest_cleanup() {
    local name
    if [[ -n "${POLICY_ASIDE}" ]] && [[ -e "${POLICY_ASIDE}" ]]; then
        mv -f "${POLICY_ASIDE}" "${CONFIG_DIR}/policy"
        POLICY_ASIDE=""
    fi
    if [[ "${LOCK_PLANTED}" -eq 1 ]]; then
        rm -f "${STATE_DIR}/in-flight"
        LOCK_PLANTED=0
    fi
    if [[ "${SYMLINK_PLANTED}" -eq 1 ]]; then
        rm -f "${SPOOL}/requests"
        mkdir -p "${SPOOL}/requests"
        SYMLINK_PLANTED=0
    fi
    if [[ -n "${OUTSIDE_DIR}" ]] && [[ -d "${OUTSIDE_DIR}" ]]; then
        rm -rf "${OUTSIDE_DIR}"
    fi
    if [[ -n "${REQUEST_NAME}" ]]; then
        PLANTED+=("${REQUEST_NAME}")
    fi
    for name in "${PLANTED[@]+"${PLANTED[@]}"}"; do
        rm -f "${SPOOL}/requests/${name}" "${SPOOL}/processing/${name}" "${SPOOL}/quarantine/${name}" \
            "${SPOOL}/responses/${name}.response.json" "${SPOOL}/tmp/${name}"
    done
    bridge_remedy
}

# plant <name> <body> — write a request the way the container side does: tmp/, then rename.
plant() {
    local name="$1" body="$2"
    PLANTED+=("${name}")
    printf '%s' "${body}" >"${SPOOL}/tmp/${name}"
    mv "${SPOOL}/tmp/${name}" "${SPOOL}/requests/${name}"
}

# await_response <name> — wait for the watcher's answer; echo its path or fail.
await_response() {
    local name="$1" path="${SPOOL}/responses/$1.response.json" i
    for ((i = 0; i < RESPONSE_WAIT_SECONDS * 2; i++)); do
        if [[ -s "${path}" ]]; then
            printf '%s\n' "${path}"
            return 0
        fi
        sleep 0.5
    done
    return 1
}

# response_summary <path> — "<state> <verdict|-> <failure reason|->" from the JSON.
response_summary() {
    python3 -c 'import json, sys
d = json.load(open(sys.argv[1]))
print(d.get("state"), d.get("verdict") or "-", (d.get("failure") or {}).get("reason", "-"))' "$1"
}

# expect_rejected <label> <name> <code> — the request must be answered `rejected` with the
# named code, and the file must have gone to quarantine/ (not processing/, not left behind).
expect_rejected() {
    local label="$1" name="$2" code="$3" path summary
    if ! path="$(await_response "${name}")"; then
        out "- ${label}: **NOT as designed** — no response within ${RESPONSE_WAIT_SECONDS}s (silence is never an outcome)"
        return 1
    fi
    summary="$(response_summary "${path}")"
    if [[ "${summary}" != "rejected - ${code}: "* ]]; then
        out "- ${label}: **NOT as designed** — expected rejected/${code}, got: ${summary}"
        return 1
    fi
    if [[ ! -e "${SPOOL}/quarantine/${name}" ]] || [[ -e "${SPOOL}/requests/${name}" ]] || [[ -e "${SPOOL}/processing/${name}" ]]; then
        out "- ${label}: **NOT as designed** — rejected/${code} answered, but the request was not quarantined"
        return 1
    fi
    out "- ${label}: **as designed** — rejected/${code}, quarantined, answered"
}

# ── T4.8: every rejection path rejects AND responds ──────────────────────────────────────
case_bad_filename() {
    local name="not-a-request.txt"
    plant "${name}" '{}'
    expect_rejected "bad filename" "${name}" "bad-filename"
}

case_denylisted_verb() {
    local n name
    n="$(nonce)"
    name="$(stamp)-exec-${n}.json"
    plant "${name}" "{\"verb\": \"exec\", \"argument\": \"ls\", \"nonce\": \"${n}\"}"
    expect_rejected "denylisted verb" "${name}" "denylisted-verb"
}

case_unknown_verb() {
    local n name
    n="$(nonce)"
    name="$(stamp)-frobnicate-${n}.json"
    plant "${name}" "{\"verb\": \"frobnicate\", \"argument\": null, \"nonce\": \"${n}\"}"
    expect_rejected "unknown verb" "${name}" "unknown-verb"
}

case_verb_mismatch() {
    local n name
    n="$(nonce)"
    name="$(stamp)-list-scenarios-${n}.json"
    plant "${name}" "{\"verb\": \"lab-status\", \"argument\": null, \"nonce\": \"${n}\"}"
    expect_rejected "filename/body verb disagreement" "${name}" "verb-mismatch"
}

case_unknown_argument() {
    local n name
    n="$(nonce)"
    name="$(stamp)-run-scenario-${n}.json"
    plant "${name}" "{\"verb\": \"run-scenario\", \"argument\": \"no-such-scenario\", \"nonce\": \"${n}\"}"
    expect_rejected "argument not in the deployed allowlist" "${name}" "unknown-argument"
}

case_policy_deny() {
    # refresh-base ships denied (§6.4): the request is well-formed and still refused.
    local n name
    n="$(nonce)"
    name="$(stamp)-refresh-base-${n}.json"
    plant "${name}" "{\"verb\": \"refresh-base\", \"argument\": \"server\", \"nonce\": \"${n}\"}"
    expect_rejected "MODE_refresh-base=deny" "${name}" "policy-deny"
}

case_missing_policy() {
    local n name rc=0
    POLICY_ASIDE="${CONFIG_DIR}/policy.selftest-aside"
    mv "${CONFIG_DIR}/policy" "${POLICY_ASIDE}"
    # list-scenarios is the verb the shipped policy allows, so a refusal here can only be
    # the missing file, not the policy's own deny.
    n="$(nonce)"
    name="$(stamp)-list-scenarios-${n}.json"
    plant "${name}" "{\"verb\": \"list-scenarios\", \"argument\": null, \"nonce\": \"${n}\"}"
    expect_rejected "missing policy file (deny by default)" "${name}" "policy-deny" || rc=$?
    mv -f "${POLICY_ASIDE}" "${CONFIG_DIR}/policy"
    POLICY_ASIDE=""
    return "${rc}"
}

case_in_flight() {
    local n name rc=0
    printf 'selftest-fake-run\n' >"${STATE_DIR}/in-flight"
    LOCK_PLANTED=1
    n="$(nonce)"
    name="$(stamp)-list-scenarios-${n}.json"
    plant "${name}" "{\"verb\": \"list-scenarios\", \"argument\": null, \"nonce\": \"${n}\"}"
    expect_rejected "single-flight lock held" "${name}" "in-flight" || rc=$?
    rm -f "${STATE_DIR}/in-flight"
    LOCK_PLANTED=0
    return "${rc}"
}

# wedge_by_hostile_spool — requests/ replaced by a symlink to a directory outside the spool,
# and the drain activated by hand. The path unit is STOPPED first: removing requests/ fires
# its watch, and the first run of this selftest showed the resulting activation racing the
# hand-started one (a run that saw the real directory returned 0) and then, once requests/
# was back, the next trigger re-running the watcher successfully — the failed state is
# self-healing by design, so to observe it the trigger has to be off. Sets WEDGE_RC to the
# hand activation's exit status and restores requests/; the remedy restarts the path unit.
wedge_by_hostile_spool() {
    local i state
    WEDGE_RC=0
    systemctl --user stop "${PATH_UNIT}"
    # An activation from the previous case may still be in its debounce sleep; `start` on a
    # unit that is already running joins that run (exit 0, no new drain) instead of starting
    # one against the symlink. Wait for the service to be idle first. `is-active` exits
    # non-zero for every state but active, so the state text is read, not the status.
    for ((i = 0; i < 40; i++)); do
        state="$(systemctl --user show -p ActiveState --value "${SERVICE_UNIT}")"
        if [[ "${state}" != "activating" ]] && [[ "${state}" != "active" ]]; then
            break
        fi
        sleep 0.25
    done
    OUTSIDE_DIR="$(mktemp -d "${HOME}/.cache/vmtest-selftest-outside.XXXXXX")"
    rmdir "${SPOOL}/requests"
    ln -s "${OUTSIDE_DIR}" "${SPOOL}/requests"
    SYMLINK_PLANTED=1
    if systemctl --user start "${SERVICE_UNIT}" 2>>"${REPORT}.stderr"; then
        WEDGE_RC=0
    else
        WEDGE_RC=$?
    fi
    rm "${SPOOL}/requests"
    mkdir "${SPOOL}/requests"
    SYMLINK_PLANTED=0
}

# The hostile spool: the watcher must REFUSE — exit non-zero, log off the mount, write nothing
# anywhere — which is a different outcome from `rejected`, and the distinction is the assertion.
case_symlinked_spool_refuses() {
    local before after
    before="$(grep -c '' "${STATE_DIR}/service.log")"
    wedge_by_hostile_spool
    after="$(grep -c '' "${STATE_DIR}/service.log")"
    if [[ "${WEDGE_RC}" -eq 0 ]]; then
        out "- symlinked spool directory: **NOT as designed** — the watcher exited 0 against a symlinked requests/"
        return 1
    fi
    if [[ -n "$(find "${OUTSIDE_DIR}" -mindepth 1)" ]]; then
        out "- symlinked spool directory: **NOT as designed** — the watcher wrote INTO the symlink target"
        return 1
    fi
    if [[ "${after}" -le "${before}" ]] || ! tail -n "$((after - before))" "${STATE_DIR}/service.log" | grep -q ' refused '; then
        out "- symlinked spool directory: **NOT as designed** — no 'refused' line reached the off-mount audit log"
        return 1
    fi
    if ! systemctl --user is-failed --quiet "${SERVICE_UNIT}"; then
        out "- symlinked spool directory: **NOT as designed** — refused, but ${SERVICE_UNIT} is not in failed (the heartbeat would not see it)"
        return 1
    fi
    out "- symlinked spool directory: **as designed** — refused (exit ${WEDGE_RC}), nothing written to the target, audit log has 'refused', ${SERVICE_UNIT} is failed"
}

case_service_reset() {
    bridge_remedy
    if ! systemctl --user is-active --quiet "${PATH_UNIT}" || systemctl --user is-failed --quiet "${SERVICE_UNIT}"; then
        out "- reset after the refusal: **NOT as designed** — ${PATH_UNIT} is not active, or ${SERVICE_UNIT} still failed, after the remedy"
        return 1
    fi
    out "- reset after the refusal: **as designed** — the documented remedy restored ${PATH_UNIT}"
}

# The watcher-side rate limit (§6.4 step 8) is consulted AFTER grammar, deny list, verb and
# argument checks — a malformed flood is answered by those and never reaches it — so the burst
# is 12 WELL-FORMED requests that fail at a later step (refresh-base, denied by policy at step
# 9). Every one must be answered, at least one `rate-limited`, and the path unit must still be
# active afterwards (a systemd limit, not the watcher's, would leave some unanswered and the
# unit failed).
case_rate_limit() {
    local i n name names=() path summary limited=0 denied=0 unanswered=0
    for ((i = 0; i < 12; i++)); do
        n="$(nonce)"
        name="$(stamp)-refresh-base-${n}.json"
        names+=("${name}")
        plant "${name}" "{\"verb\": \"refresh-base\", \"argument\": \"server\", \"nonce\": \"${n}\"}"
    done
    for name in "${names[@]}"; do
        if path="$(await_response "${name}")"; then
            summary="$(response_summary "${path}")"
            if [[ "${summary}" == "rejected - rate-limited: "* ]]; then
                limited=$((limited + 1))
            elif [[ "${summary}" == "rejected - policy-deny: "* ]]; then
                denied=$((denied + 1))
            fi
        else
            unanswered=$((unanswered + 1))
        fi
    done
    if [[ "${unanswered}" -gt 0 ]]; then
        out "- watcher rate limit: **NOT as designed** — ${unanswered} of 12 burst requests got NO response"
        return 1
    fi
    if [[ "${limited}" -lt 1 ]]; then
        out "- watcher rate limit: **NOT as designed** — 12 requests in one burst and none was rejected rate-limited"
        return 1
    fi
    if ! systemctl --user is-active --quiet "${PATH_UNIT}"; then
        out "- watcher rate limit: **NOT as designed** — the path unit is no longer active after the burst"
        return 1
    fi
    out "- watcher rate limit: **as designed** — every burst request answered (${denied} policy-deny, ${limited} rate-limited), path unit still active"
}

# ── T4.9: a wedged bridge is reported as wedged, with the remedy ─────────────────────────
case_wedge() {
    wedge_by_hostile_spool
    if [[ "${WEDGE_RC}" -eq 0 ]] || ! systemctl --user is-failed --quiet "${SERVICE_UNIT}"; then
        out "- wedging: **NOT as designed** — the watcher did not refuse the symlinked spool (exit ${WEDGE_RC}) or the service unit is not failed"
        return 1
    fi
    out "- wedging: **as designed** — the hostile-spool refusal left ${SERVICE_UNIT} failed (exit ${WEDGE_RC})"
}

# The reader must say "wedged", print the remedy, and write nothing.
case_wedged_is_reported() {
    local rc=0 outText before after
    systemctl --user start "${HEARTBEAT_UNIT}"
    before="$(request_count)"
    outText="$(bash "${REQUESTER}" list-scenarios --accept-timeout 5 --timeout 5 2>&1)" || rc=$?
    after="$(request_count)"
    if [[ "${rc}" -ne 6 ]]; then
        out "- wedged bridge: **NOT as designed** — requester exited ${rc}, expected 6 (wedged); output: ${outText//$'\n'/ | }"
        return 1
    fi
    if [[ "${outText}" != *"systemctl --user reset-failed vmtest-bridge@"* ]] || [[ "${outText}" != *wedged* ]]; then
        out "- wedged bridge: **NOT as designed** — exit 6 but no remedy line: ${outText//$'\n'/ | }"
        return 1
    fi
    if [[ "${after}" -ne "${before}" ]]; then
        out "- wedged bridge: **NOT as designed** — the requester wrote a request into a wedged bridge"
        return 1
    fi
    out "- wedged bridge: **as designed** — reported wedged (exit 6) with the reset-failed remedy, no request written, not a timeout, not a fail"
}

case_remedy_restores_service() {
    local rc=0 outText
    bridge_remedy
    systemctl --user start "${HEARTBEAT_UNIT}"
    outText="$(bash "${REQUESTER}" list-scenarios --accept-timeout 30 --timeout 60 --poll-seconds 1 2>&1)" || rc=$?
    REQUEST_NAME="$(printf '%s\n' "${outText}" | grep -oE 'VMTEST-REQUEST [^ ]+' | cut -d' ' -f2)" || REQUEST_NAME=""
    if [[ "${rc}" -ne 0 ]] || [[ "${outText}" != *"state=finished verdict=pass"* ]]; then
        out "- after the remedy: **NOT as designed** — list-scenarios did not pass through the reset bridge (exit ${rc}): ${outText//$'\n'/ | }"
        return 1
    fi
    out "- after the remedy: **as designed** — reset-failed restored service; list-scenarios finished pass"
}
