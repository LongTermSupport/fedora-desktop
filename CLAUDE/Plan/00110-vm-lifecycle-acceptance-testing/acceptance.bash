#!/usr/bin/env bash
# Plan 00110 — acceptance.bash
#
# PURPOSE: the pass/fail gate for the server fast path (DESIGN.md §9 T3.2 and T3.4). It runs
# the positive scenario and the three negative scenarios through the DEPLOYED `vmtest` and
# asserts each verdict is exactly the one the design requires:
#
#   server-fast-provision           pass
#   server-main-playbook-fails      fail  @ provision   (a failed main play propagates)
#   server-optional-playbook-fails  fail  @ provision   (a failed optional play propagates)
#   server-optional-play-missing    fail  @ provision   (argument validation refuses first)
#
# The negatives are the falsifiability proof: a harness that could not go red would prove
# nothing, so each is required to go red in the right way — `fail`, never `error` (which
# would mean the harness, not the product, broke), never `pass`, AND with the transcript
# carrying the message of the specific failure the scenario exists to provoke. Red for a
# different reason is not proof; the first run of this script showed all three negatives
# going red on an unrelated run.bash defect.
#
# RUN ON THE HOST, after deploy.bash and after `vmtest build-base server-fast`:
#   ./CLAUDE/Plan/00110-vm-lifecycle-acceptance-testing/acceptance.bash
#
# EFFECT ON THE HOST: boots four throwaway guests in turn on qemu:///session, each on a
# copy-on-write overlay of the read-only base; every overlay of a passing run is removed,
# a non-passing run keeps its overlay and console under ~/.local/share/vmtest/runs/ for
# diagnosis. Nothing outside the lab tree changes. It is slow: the positive run provisions
# a whole server profile.
#
# Usage: ./acceptance.bash [-h|--help]
#
# EXIT CODES: 0 every scenario produced its expected verdict; 1 at least one did not.
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Runs server-fast-provision and the three negative scenarios through the
deployed vmtest CLI and asserts each verdict. Host-only; boots four throwaway
guests in turn; slow. Writes a report into
<this plan folder>/acceptance-runs/<timestamp>/ and names it on completion."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it boots guests on this machine's hypervisor"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/plan-00110-acceptance-report.md"
readonly REPORT
{
    printf '# Plan 00110 — acceptance report (server fast path)\n\n'
    printf 'Each line is one scenario run through the deployed vmtest, with the verdict the\n'
    printf 'design requires. READ THIS FOR: any line marked NOT as expected or UNANSWERED.\n\n'
} >"${REPORT}"

plan_gather_leg "server-fast-provision must pass" \
    bash "${PLAN_SCRIPT_DIR}/run-scenario-leg.bash" server-fast-provision pass - - "${REPORT}"
plan_gather_leg "server-main-playbook-fails must fail at provision on the profile assertion" \
    bash "${PLAN_SCRIPT_DIR}/run-scenario-leg.bash" server-main-playbook-fails fail provision \
    'provisioning_profile=not-a-profile is not recognised' "${REPORT}"
plan_gather_leg "server-optional-playbook-fails must fail at provision on play-nvidia" \
    bash "${PLAN_SCRIPT_DIR}/run-scenario-leg.bash" server-optional-playbook-fails fail provision \
    'play-nvidia.yml FAILED' "${REPORT}"
plan_gather_leg "server-optional-play-missing must fail at provision on name resolution" \
    bash "${PLAN_SCRIPT_DIR}/run-scenario-leg.bash" server-optional-play-missing fail provision \
    "not found under playbooks/imports/optional/" "${REPORT}"
# Phase 3b: the same checks on the Anaconda-installed Server base. The transcript header
# and evidence.base carry the base's kind and name, so this pass is never the fast one's.
plan_gather_leg "server-full-provision must pass on the Anaconda-installed base" \
    bash "${PLAN_SCRIPT_DIR}/run-scenario-leg.bash" server-full-provision pass - \
    "on base server-full-[0-9]+ \(full, server\)" "${REPORT}"

if [[ -n "${PLAN_FAILED_LEGS}" ]]; then
    printf '\nVERDICT: FAIL — %s\n' "${PLAN_FAILED_LEGS}" >>"${REPORT}"
else
    printf '\nVERDICT: PASS — every scenario produced its expected verdict\n' >>"${REPORT}"
fi
plan_finish
