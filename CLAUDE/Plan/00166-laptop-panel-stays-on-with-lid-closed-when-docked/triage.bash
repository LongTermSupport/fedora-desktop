#!/usr/bin/env bash
# triage.bash — gather grounded FACTS about the laptop panel staying active with the lid
# closed: the lid as the kernel, UPower and logind each see it, every DRM connector, the
# inhibitors, the power profile, the UPower config and its package state, and Mutter's
# monitor list. Fact-finding only: renders no verdict (R9) and changes nothing.
#
# Most useful run docked with the lid CLOSED, which is the broken state. Safe to re-run.
#
# WHERE TO RUN: on the HOST, in the GNOME session. Enforced by plan_require_host (R2).
#
# Usage: ./CLAUDE/Plan/00166-laptop-panel-stays-on-with-lid-closed-when-docked/triage.bash [-h]
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

PLAN_USAGE="usage: triage.bash [-h|--help]

Gathers lid, panel, inhibitor, power-profile and UPower facts from this host.
Read-only. Writes its report under untracked/plan-runs/."

plan_mode gather
plan_parse_common_flags "$@"

plan_require_host "it reads the host's sysfs, UPower, logind and the GNOME session bus"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"      # listed by plan_finish (R10)
plan_gather_leg "lid / panel / power facts" bash "${PLAN_SCRIPT_DIR}/probe-lid.bash" "${REPORT}"

plan_finish
