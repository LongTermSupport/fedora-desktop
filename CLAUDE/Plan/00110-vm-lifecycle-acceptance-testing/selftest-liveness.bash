#!/usr/bin/env bash
# Plan 00110 — selftest-liveness.bash (T4.9)
#
# PURPOSE: prove that a wedged bridge is reported as "wedged", with the remedy command, by the
# container-side reader — NOT as a timeout and NOT as a fail — and that the documented remedy
# restores service. This is the test for defect B1 (JOURNAL 12:30): a failed path/service unit
# is silent in exactly the way an uninstalled one is silent, and without this proof the §6.5
# heartbeat is a claim, not a fix. The cases live in selftest-bridge.inc.bash.
#
# HOW IT WEDGES: the same hostile-spool refusal selftest-bridge.bash asserts — requests/
# replaced by a symlink — activated by hand, which the watcher refuses with exit 2 and so
# fails vmtest-bridge@<slug>.service. That is a REAL failure of a REAL unit, produced by the
# bridge's own defence, not a simulated flag.
#
# RUN ON THE HOST, after deploy.bash (the units must be enabled):
#   ./CLAUDE/Plan/00110-vm-lifecycle-acceptance-testing/selftest-liveness.bash
#
# EFFECT ON THE HOST: fails and then resets the bridge service unit; writes one list-scenarios
# request at the end to prove service is back; removes what it created on exit. Boots nothing.
#
# Usage: ./selftest-liveness.bash [-h|--help]
#
# EXIT CODES: 0 wedged was reported as wedged and the remedy restored service; 1 otherwise.
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

PLAN_USAGE="usage: selftest-liveness.bash [-h|--help]

Wedges the live bridge by making its watcher refuse a hostile spool, asserts
the container-side reader reports 'wedged' with the remedy (not a timeout,
not a fail), resets the unit, and asserts a request passes again. Host-only."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi
plan_require_host "it fails and resets this machine's systemd --user bridge units"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/plan-00110-liveness-selftest-report.md"
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
systemctl --user is-active --quiet "${PATH_UNIT}" || { printf '[FATAL] %s is not active; run deploy.bash first\n' "${PATH_UNIT}" >&2; exit 1; }
trap selftest_cleanup EXIT

{
    printf '# Plan 00110 — liveness selftest report\n\n'
    printf 'One line per case. READ THIS FOR: any line marked NOT as designed.\n\n'
} >"${REPORT}"

plan_gather_leg "the hostile-spool refusal wedges the service unit" case_wedge
plan_gather_leg "wedged bridge is reported wedged with the remedy" case_wedged_is_reported
plan_gather_leg "the remedy restores service" case_remedy_restores_service

if [[ -n "${PLAN_FAILED_LEGS}" ]]; then
    printf '\nVERDICT: FAIL — %s\n' "${PLAN_FAILED_LEGS}" >>"${REPORT}"
else
    printf '\nVERDICT: PASS — wedged was reported as wedged with the remedy; the remedy restored service\n' >>"${REPORT}"
fi
plan_finish
