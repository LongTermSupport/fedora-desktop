#!/usr/bin/env bash
# triage.bash — gather grounded FACTS about what a missing dev dependency costs the QA
# suite. Fact-finding only: renders no verdict (R9) and changes nothing outside a temp dir.
# Read-only and safe to re-run.
#
# WHAT IT ANSWERS: how many of `qa-all.bash`'s gates report a verdict on a checkout WITHOUT
# `extensions/node_modules`, against a checkout that has it — and which ones go missing.
# PLAN.md and FINDINGS.md quote 7 against 38. This is what re-derives those numbers, so
# after Phase 3 lands nobody has to trust a figure written down in a document.
#
# WHERE TO RUN: in the CCY container. Enforced by plan_require_container (R2), and the
# reason is the measurement rather than convenience:
#
#   The finding is a DIFFERENTIAL — two runs of the same suite on the same machine, so the
#   machine's tool inventory cancels out. What does NOT cancel is a gate that behaves
#   differently by location. `qa-deployed-drift.bash` compares the repo against real
#   deployed copies under ~/.local/bin: on the host it does genuine work and can fail for
#   reasons that have nothing to do with this plan, moving one leg's count independently of
#   the other. In the container it self-skips, identically in both legs. So the container
#   gives the more stable comparison, not merely the more convenient one.
#
# Usage: ./CLAUDE/Plan/00128-qa-tool-abort-silences-thirty-gates/triage.bash [-h|--help]
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
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: triage.bash [-h|--help]"
plan_mode gather
plan_parse_common_flags "$@"

plan_require_container "the gate census must compare two runs whose location-dependent gates behave identically, which is true in the container and not on the host"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"
plan_gather_leg "gate census with and without the dev dependency" \
    bash "${PLAN_SCRIPT_DIR}/probe-gate-census.bash" "${REPORT}"
plan_finish
