#!/usr/bin/env bash
# triage.bash — gather grounded FACTS about the HOST side of Plan 00092. Fact-finding only:
# renders no verdict (PlanScriptStandards R9) and changes nothing. Safe to re-run.
#
# WHERE TO RUN: on the HOST, in a terminal. Enforced by plan_require_host (R2) — every check
# below asks about the host's build context, deployed launcher and built image, and a nested
# container answer would be a confident wrong one.
#
# WHY IT EXISTS: CLAUDE/PlanTriage.md — every diagnostic probe belongs in a script, never in
# a command handed to the operator in chat. Run this before deploy.bash to see what is stale,
# and again after, to see that it landed.
#
# Usage: ./CLAUDE/Plan/00092-ccy-child-claude-spawn-mode/triage.bash [-h|--help]
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
set -euo pipefail
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: triage.bash [-h|--help]

Gathers host-side facts for Plan 00092: is the opt-in tree staged in the image
build context, do the coupled version values agree, is the deployed launcher
current, and is a container rebuild pending. Read-only."

plan_mode gather
plan_parse_common_flags "$@"

plan_require_host "it probes the host build context, the deployed launcher and the built image"
plan_start_log auto

PROBE="${PLAN_SCRIPT_DIR}/probe-host.bash"

# Gather legs: a failing leg records itself and the run continues, so one missing artefact
# does not hide the state of everything else. The run still exits non-zero (R9).
plan_gather_leg "H1 opt-in tree staged in the build context" bash "${PROBE}" H1
plan_gather_leg "H2 coupled version values agree" bash "${PROBE}" H2
plan_gather_leg "H3 deployed launcher matches the checkout" bash "${PROBE}" H3
plan_gather_leg "H4 built image carries the required version" bash "${PROBE}" H4

plan_finish
