#!/usr/bin/env bash
# triage.bash — Plan 00254 Task 3.1: can Lightning run one instance per team?
#
# PURPOSE: gather FACTS, render no verdict (CLAUDE/PlanScriptStandards.md R9):
#   - whether `flatpak run --env=XDG_*_HOME=...` reaches Lightning's sandbox, and whether the
#     `--command=env` fallback does;
#   - whether two instances on two data roots both stay running, each with its own lock, and a
#     second launch on the same root exits (raising the first) instead of opening a third;
#   - whether the runtime has /etc/ssl/certs/ca-certificates.crt and python3 (Phase 4).
# Not establishable by script, and listed in the report for the owner: that the two windows
# each showed their own login page.
#
# WHERE TO RUN: on the HOST, from a terminal in the desktop session (plan_require_host, R2): it
# starts the host's Lightning Flatpak, which the container does not have.
#
# EFFECT ON THE HOST: up to three Lightning windows open for under a minute, with their data
# under ~/.var/app/org.lightning_matrix.Lightning/teams-probe/, which is removed afterwards. The
# default profile and any Lightning already running are not touched. No sudo.
#
# Usage: ./CLAUDE/Plan/00254-lightning-default-team-client/triage.bash [-h|--help]
#
# EXIT CODES: 0 every leg reached an answer; 1 a leg could not (it names itself); 2 an unknown
# argument.
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

Read-only apart from a probe directory it removes: establishes whether Lightning can run one
instance per team (Plan 00254 Task 3.1). Run on the HOST in the desktop session; opens up to
three Lightning windows for under a minute. Writes triage-instances-report.txt under
untracked/plan-runs/."
plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
  printf 'triage.bash: unknown argument %s (see --help)\n' "${PLAN_REMAINING_ARGS[0]}" >&2
  exit 2
fi

plan_require_host "it starts the host's Lightning Flatpak"
plan_start_log auto

readonly REPORT="${PLAN_RUN_DIR}/triage-instances-report.txt"
readonly PROBE="${PLAN_SCRIPT_DIR}/probe-instances.bash"

plan_gather_leg "XDG paths reach the sandbox" bash "${PROBE}" env "${REPORT}"
plan_gather_leg "runtime CA bundle and python3" bash "${PROBE}" runtime "${REPORT}"
plan_gather_leg "two instances side by side" bash "${PROBE}" two "${REPORT}"
plan_finish
