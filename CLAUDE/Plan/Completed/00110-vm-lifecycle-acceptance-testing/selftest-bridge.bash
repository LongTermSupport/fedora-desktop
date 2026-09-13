#!/usr/bin/env bash
# Plan 00110 — selftest-bridge.bash (T4.8)
#
# PURPOSE: prove, against the LIVE bridge on this host, that every rejection path the watcher
# has both rejects AND answers, and that a hostile spool is refused rather than answered. A
# bridge that has only ever been seen accepting is not evidence of anything; each case here
# plants a real bad input, watches the response go red in the named way, and cleans up. The
# wedged-bridge report (T4.9) is selftest-liveness.bash; the cases themselves live in
# selftest-bridge.inc.bash, shared by both.
#
# RUN ON THE HOST, after deploy.bash (the units must be enabled):
#   ./CLAUDE/Plan/00110-vm-lifecycle-acceptance-testing/selftest-bridge.bash
#
# EFFECT ON THE HOST: writes requests into the checkout's spool and reads the answers; every
# file it creates is removed on exit. Two cases touch deployed state and restore it on exit:
# the policy file is moved aside for the "missing policy" case, and the in-flight lock is
# planted for the single-flight case. The hostile-spool case deliberately fails the bridge
# service unit (that is the behaviour under test) and resets it with the documented remedy.
# Nothing is booted. The rate-limit case runs last because the limit it trips lasts a minute.
#
# Usage: ./selftest-bridge.bash [-h|--help]
#
# EXIT CODES: 0 every case behaved as designed; 1 at least one did not (the report says which).
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
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
# shellcheck source-path=SCRIPTDIR
# shellcheck source=selftest-bridge.inc.bash
source "${scriptDir}/selftest-bridge.inc.bash"

PLAN_USAGE="usage: selftest-bridge.bash [-h|--help]

Exercises every rejection path of the live bridge and the hostile-spool
refusal. Host-only; boots nothing; cleans up after itself.
Writes a report into <this plan folder>/acceptance-runs/<timestamp>/."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi
plan_require_host "it drives this machine's systemd --user bridge units"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/plan-00110-bridge-selftest-report.md"
SPOOL="${repoRoot}/untracked/vmtest-bridge"
SLUG="$(systemd-escape --path "${repoRoot}")"
CONFIG_DIR="${HOME}/.config/vmtest-bridge/${SLUG}"
STATE_DIR="${HOME}/.local/state/vmtest-bridge/${SLUG}"
PATH_UNIT="vmtest-bridge@${SLUG}.path"
SERVICE_UNIT="vmtest-bridge@${SLUG}.service"
HEARTBEAT_UNIT="vmtest-bridge-heartbeat@${SLUG}.service"
REQUESTER="${repoRoot}/scripts/vmtest-request.bash"
readonly REPORT SPOOL SLUG CONFIG_DIR STATE_DIR PATH_UNIT SERVICE_UNIT HEARTBEAT_UNIT REQUESTER

[[ -d "${SPOOL}/requests" ]] || { printf '[FATAL] no spool at %s; run deploy.bash first\n' "${SPOOL}" >&2; exit 1; }
[[ -r "${CONFIG_DIR}/policy" ]] || { printf '[FATAL] no policy at %s; run deploy.bash first\n' "${CONFIG_DIR}" >&2; exit 1; }
systemctl --user is-active --quiet "${PATH_UNIT}" || { printf '[FATAL] %s is not active; run deploy.bash first\n' "${PATH_UNIT}" >&2; exit 1; }
trap selftest_cleanup EXIT

{
    printf '# Plan 00110 — bridge selftest report\n\n'
    printf 'One line per case. READ THIS FOR: any line marked NOT as designed.\n\n'
} >"${REPORT}"

plan_gather_leg "bad filename rejects and responds" case_bad_filename
plan_gather_leg "denylisted verb rejects and responds" case_denylisted_verb
plan_gather_leg "unknown verb rejects and responds" case_unknown_verb
plan_gather_leg "filename/body verb disagreement rejects and responds" case_verb_mismatch
plan_gather_leg "unknown argument rejects and responds" case_unknown_argument
plan_gather_leg "MODE=deny rejects and responds" case_policy_deny
plan_gather_leg "missing policy file rejects and responds" case_missing_policy
plan_gather_leg "in-flight lock rejects and responds" case_in_flight
plan_gather_leg "symlinked spool directory refuses, does not respond" case_symlinked_spool_refuses
plan_gather_leg "the documented remedy resets the refused service" case_service_reset
plan_gather_leg "watcher rate limit answers every request in a burst" case_rate_limit

if [[ -n "${PLAN_FAILED_LEGS}" ]]; then
    printf '\nVERDICT: FAIL — %s\n' "${PLAN_FAILED_LEGS}" >>"${REPORT}"
else
    printf '\nVERDICT: PASS — every rejection path rejected and responded; the hostile spool was refused\n' >>"${REPORT}"
fi
plan_finish
