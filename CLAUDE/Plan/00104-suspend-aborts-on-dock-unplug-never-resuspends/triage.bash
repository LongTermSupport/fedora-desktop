#!/usr/bin/env bash
# triage.bash — gather grounded FACTS about the suspend abort and the failure to
# re-suspend. Fact-finding only: renders no verdict (R9) and changes nothing. Read-only and
# safe to re-run on a live system, mid-incident.
#
# WHERE TO RUN: on the HOST, in a terminal. Enforced by plan_require_host (R2) — the CCY
# container has no host journal, no /sys/power and no logind, so a result obtained there is
# not evidence about the laptop.
#
# The run report is RAW host state and lands in a gitignored triage-runs/ directory.
# The sanitised, committed transcription is TRIAGE-EVIDENCE.md — that is the file to read
# from the container.
#
# Usage: ./CLAUDE/Plan/00104-suspend-aborts-on-dock-unplug-never-resuspends/triage.bash [-h]
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

Gathers suspend/resume, lid, wakeup-source and power-policy facts from this host.
Read-only. Writes a raw report into a gitignored triage-runs/<timestamp>/ directory."

plan_mode gather
plan_parse_common_flags "$@"

plan_require_host "it reads the host journal, /sys/power, logind and the GNOME session bus"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"      # listed by plan_finish (R10)
plan_gather_leg "suspend / lid / wakeup facts" \
    bash "${PLAN_SCRIPT_DIR}/probe-suspend.bash" "${REPORT}"

plan_finish
