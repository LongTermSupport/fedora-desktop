#!/usr/bin/env bash
#
# acceptance-signal.bash — establish the ONE thing no container run can: that a warning
# raised on the host is actually RENDERED to a live session.
#
# `triage-signal.bash` records this as A5, explicitly NOT established, because it needs a real
# ccy session with a real supervisor attached. This is that run. It exercises the DEPLOYED
# `ccy-sessions` and the DEPLOYED supervisor, not the repo copies — the whole point is the
# half that lives outside this repository.
#
# WHERE TO RUN: on the HOST, in a terminal. Enforced by plan_require_host (R2). In a container
# there is no tmux server and no supervisor, so every check would go vacuously green.
#
# IT WARNS A REAL SESSION, and then retracts. That is not a side effect to apologise for, it
# is the thing under test: a signal nobody receives is not a signal. `reboot-cancelled` is
# raised at the end — including on failure, via an EXIT trap — so no session is left believing
# a reboot is coming. NOTHING IS EVER REBOOTED by this script.
#
# RUN IT AGAINST AN IDLE SESSION. The supervisor reads a signal only while the session is in
# its MONITOR state and otherwise doing nothing; mid-turn, the signal legitimately waits. A
# timeout here therefore means "not observed within the window", which this reports as such
# rather than as a failure of the channel.
#
# Usage: acceptance-signal.bash [--minutes N] [--wait S] [--help]
# Exit 0 = ACCEPTED, 1 = REJECTED, 2 = could not be run (no session to test against).
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

WARN_MINUTES=99
WAIT_SECONDS=90
while [[ $# -gt 0 ]]; do
    case "$1" in
    --minutes)
        if [[ $# -lt 2 ]]; then
            printf 'acceptance-signal.bash: --minutes needs a number.\n' >&2
            exit 64
        fi
        shift
        WARN_MINUTES="$1"
        ;;
    --wait)
        if [[ $# -lt 2 ]]; then
            printf 'acceptance-signal.bash: --wait needs a number of seconds.\n' >&2
            exit 64
        fi
        shift
        WAIT_SECONDS="$1"
        ;;
    -h | --help)
        awk 'NR == 1 { next } /^[^#]/ { exit } { sub(/^# ?/, ""); print }' "${BASH_SOURCE[0]}"
        exit 0
        ;;
    *)
        printf 'acceptance-signal.bash: unknown argument %s\n' "$1" >&2
        exit 64
        ;;
    esac
    shift
done
if [[ ! "${WARN_MINUTES}" =~ ^[1-9][0-9]*$ ]]; then
    printf 'acceptance-signal.bash: --minutes wants a whole number above zero.\n' >&2
    exit 64
fi
if [[ ! "${WAIT_SECONDS}" =~ ^[1-9][0-9]*$ ]]; then
    printf 'acceptance-signal.bash: --wait wants a whole number of seconds above zero.\n' >&2
    exit 64
fi

plan_mode gather
plan_require_host 'a live ccy session and its supervisor are the subject; in a container there is neither, so every check below would pass having exercised nothing'
plan_start_log auto

# `plan_finish` is deliberately NOT used to close this script. It owns the exit status, and an
# acceptance script's exit status IS its verdict (R9) — so the verdict block at the foot prints
# the report path itself rather than handing that job to a function that would also decide
# whether the run passed.
REPORT="${PLAN_RUN_DIR}/acceptance-signal-report.md"

SESSIONS_CMD="${CCY_SESSIONS_CMD:-$HOME/.local/bin/ccy-sessions}"

# Reads a status message's rendered text, or prints nothing. A malformed or half-written file
# is "no text yet" rather than an error: this is polled while another process writes it, so a
# partial read is an expected transient, not a failure to report.
STATUS_TEXT_READER='
import json, sys
try:
    doc = json.load(open(sys.argv[1]))
except (OSError, ValueError):
    print("")
else:
    print(doc.get("text", "") if isinstance(doc, dict) else "")
'

# COVERAGE, for the reason Plan 00099's own acceptance gate had to learn it: a verdict built
# from a bare pass count cannot distinguish "every check ran and passed" from "some checks
# stopped running". Each check registers as it starts, and an incomplete run is REJECTED even
# with no failures.
EXPECTED_CHECKS=(0 1 2 3 4 5)
RAN_CHECKS=()
PASS=0
FAIL=0

check() {
    RAN_CHECKS+=("$1")
    printf '\n[%s] %s\n' "$1" "$2"
    printf '\n### [%s] %s\n\n' "$1" "$2" >> "${REPORT}"
}
ok() {
    printf '  PASS  %s\n' "$1"
    printf -- '- PASS %s\n' "$1" >> "${REPORT}"
    PASS=$((PASS + 1))
}
bad() {
    printf '  FAIL  %s\n' "$1" >&2
    if [[ $# -gt 1 ]]; then
        printf '        %s\n' "$2" >&2
    fi
    printf -- '- FAIL %s\n' "$1" >> "${REPORT}"
    FAIL=$((FAIL + 1))
}

{
    printf '# Plan 00123 — host acceptance: is a warning actually rendered?\n\n'
    printf 'Establishes A5 from triage-signal.bash, which a container cannot.\n'
} > "${REPORT}"

# Retract on the way out, whatever happened. A session left holding a warning for a reboot
# this script was never going to perform is the one outcome worse than a failed test.
RETRACTED=0
retract() {
    if [[ "${RETRACTED}" -eq 1 ]]; then
        return 0
    fi
    RETRACTED=1
    printf '\nRetracting the warning (reboot-cancelled)...\n'
    if "${SESSIONS_CMD}" notify reboot-cancelled; then
        printf '  retracted.\n'
    else
        printf '  WARNING: the retraction FAILED. Run this yourself:\n' >&2
        printf '    %s notify reboot-cancelled\n' "${SESSIONS_CMD}" >&2
    fi
}
trap retract EXIT

if [[ ! -x "${SESSIONS_CMD}" ]]; then
    printf '[ABORT] ccy-sessions is not deployed at %s\n' "${SESSIONS_CMD}" >&2
    printf '        Deploy it, then re-run. Nothing was signalled.\n' >&2
    RETRACTED=1
    exit 2
fi

# --- 0. precondition: there is a live session to warn ------------------------------------
# Without this the run is worthless and would LOOK clean: notify exits 0 with "nobody to
# signal", and every later check would have nothing to contradict it. Refuse to render a
# verdict instead — the exact trap Plan 00099's check [0] exists for.
check 0 "precondition: a live ccy session whose project can be warned"
audit_out=""
if ! audit_out="$("${SESSIONS_CMD}" reboot --dry-run 2>&1)"; then
    printf '  ABORT  the pre-reboot audit refused:\n' >&2
    printf '%s\n' "${audit_out}" >&2
    printf '         Fix that first — this gate cannot warn what it cannot enumerate.\n' >&2
    RETRACTED=1
    exit 2
fi
if printf '%s' "${audit_out}" | grep -q 'no sessions are running'; then
    printf '  ABORT  no ccy session is running, so there is nothing to warn.\n' >&2
    printf '         Start one (ccy in a project), leave it IDLE, and re-run.\n' >&2
    RETRACTED=1
    exit 2
fi
ok "the audit enumerated at least one warnable session"

# Where the deployed supervisor keeps its per-project state. Derived from the project the
# audit just named, not guessed: the signal and the status message are project-scoped, so a
# fixed path would be the wrong path on every project but one.
project_dir="$(printf '%s' "${audit_out}" | awk '/^ +[^ ]+ +ready +/ { print $3; exit }')"
if [[ -z "${project_dir}" ]]; then
    printf '  ABORT  could not read a ready project out of the audit table.\n' >&2
    printf '%s\n' "${audit_out}" >&2
    RETRACTED=1
    exit 2
fi
project_dir="${project_dir/#\~/$HOME}"
DAEMON_UNTRACKED="${project_dir}/.claude/hooks-daemon/untracked"
SIDECAR_DIR="${DAEMON_UNTRACKED}/context-sidecar"
STATUS_FILE="${DAEMON_UNTRACKED}/status-message.json"
printf '  project under test: %s\n' "${project_dir}"
printf -- '- project under test: %s\n' "${project_dir}" >> "${REPORT}"

# --- 1. the warning is raised ------------------------------------------------------------
check 1 "ccy-sessions notify reboot-warning is accepted"
notify_out=""
if notify_out="$("${SESSIONS_CMD}" notify reboot-warning --minutes "${WARN_MINUTES}" 2>&1)"; then
    ok "notify exited 0"
else
    bad "notify refused" "${notify_out}"
fi

# --- 2. the artefact carries what was asked for ------------------------------------------
# The writer's half, re-checked on the host: the container established the CONTRACT, this
# establishes that the deployed client passes the right values through it.
check 2 "the signal artefact carries the kind and the number"
signal_file=""
if [[ -d "${SIDECAR_DIR}" ]]; then
    signal_file="$(find "${SIDECAR_DIR}" -maxdepth 1 -type f -name '*.operator-signal' -print -quit)"
fi
if [[ -z "${signal_file}" ]]; then
    bad "no .operator-signal file under ${SIDECAR_DIR}" \
        "either notify did not reach this project, or the supervisor consumed it before this check ran"
else
    signal_kind="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("kind",""))' "${signal_file}")"
    signal_minutes="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("minutes",""))' "${signal_file}")"
    if [[ "${signal_kind}" == "reboot-warning" ]]; then
        ok "kind is reboot-warning"
    else
        bad "kind is '${signal_kind}', not reboot-warning"
    fi
    if [[ "${signal_minutes}" == "${WARN_MINUTES}" ]]; then
        ok "minutes is ${WARN_MINUTES}, as asked"
    else
        bad "minutes is '${signal_minutes}', not ${WARN_MINUTES}"
    fi
fi

# --- 3. THE ONE A CONTAINER CANNOT DO: the supervisor RENDERED it -------------------------
# The status message is written by the supervisor from its own fixed template. Its presence is
# proof the signal was read AND rendered — strictly more than "the file was consumed", because
# a file can be deleted without a word reaching anyone.
#
# Polled with a CAP. An unbounded wait on a session that is simply busy would hang for ever.
check 3 "the supervisor rendered the warning to the session"
deadline=$(($(date +%s) + WAIT_SECONDS))
rendered=""
while [[ "$(date +%s)" -lt "${deadline}" ]]; do
    if [[ -r "${STATUS_FILE}" ]]; then
        rendered="$(python3 -c "${STATUS_TEXT_READER}" "${STATUS_FILE}")"
        if printf '%s' "${rendered}" | grep -q 'will reboot in'; then
            break
        fi
    fi
    sleep 2
done
if printf '%s' "${rendered}" | grep -q 'will reboot in'; then
    ok "the session was shown: ${rendered}"
    if printf '%s' "${rendered}" | grep -q "${WARN_MINUTES} minute"; then
        ok "and the sentence carries the number that was signalled"
    else
        bad "the rendered sentence does not carry ${WARN_MINUTES} minutes" "${rendered}"
    fi
else
    bad "no rendered warning observed within ${WAIT_SECONDS}s" \
        "NOT necessarily a channel failure: the supervisor reads a signal only while the session is idle and in MONITOR state. Re-run against an idle session, or raise --wait."
fi

# --- 4. the retraction is rendered too ---------------------------------------------------
# A cancel that is written but never rendered leaves the session counting down to a reboot
# that was called off, which is the failure this check exists for.
check 4 "the retraction is rendered as a cancellation"
retract
deadline=$(($(date +%s) + WAIT_SECONDS))
cancelled=""
while [[ "$(date +%s)" -lt "${deadline}" ]]; do
    if [[ -r "${STATUS_FILE}" ]]; then
        cancelled="$(python3 -c "${STATUS_TEXT_READER}" "${STATUS_FILE}")"
        if printf '%s' "${cancelled}" | grep -q 'cancelled'; then
            break
        fi
    fi
    sleep 2
done
if printf '%s' "${cancelled}" | grep -q 'cancelled'; then
    ok "the session was shown the cancellation: ${cancelled}"
else
    bad "no rendered cancellation observed within ${WAIT_SECONDS}s" "${cancelled:-<no status message>}"
fi

# --- 5. nothing rebooted -----------------------------------------------------------------
# Stated as a check rather than assumed, because it is the property an operator most needs
# this script to have kept. Uptime spanning the run is the evidence.
check 5 "the host did not reboot during this run"
uptime_s="$(awk '{ printf "%d", $1 }' /proc/uptime)"
if [[ "${uptime_s}" -gt "${WAIT_SECONDS}" ]]; then
    ok "uptime is ${uptime_s}s, which spans this run — no reboot happened"
else
    bad "uptime is only ${uptime_s}s" "something restarted this machine during the run"
fi

# --- verdict ------------------------------------------------------------------------------
missing=()
for expected in "${EXPECTED_CHECKS[@]}"; do
    case " ${RAN_CHECKS[*]} " in
    *" ${expected} "*) ;;
    *) missing+=("${expected}") ;;
    esac
done

printf '\n==============================================================\n'
printf 'COVERAGE: %s of %s checks executed (%s assertion(s) passed, %s failed)\n' \
    "${#RAN_CHECKS[@]}" "${#EXPECTED_CHECKS[@]}" "${PASS}" "${FAIL}"
if [[ "${#missing[@]}" -ne 0 ]]; then
    printf '  NOT RUN: %s\n' "${missing[*]}" >&2
fi
printf 'report: %s\n' "${REPORT}"

if [[ "${FAIL}" -eq 0 ]] && [[ "${#missing[@]}" -eq 0 ]]; then
    printf 'ACCEPTED — a host-raised warning reaches a live session, and its retraction does too.\n'
    printf '==============================================================\n'
    exit 0
fi
if [[ "${FAIL}" -eq 0 ]]; then
    printf 'REJECTED — no assertion failed, but %s declared check(s) never ran.\n' "${#missing[@]}" >&2
else
    printf 'REJECTED — %s assertion(s) failed, %s passed.\n' "${FAIL}" "${PASS}" >&2
fi
printf '==============================================================\n'
exit 1
