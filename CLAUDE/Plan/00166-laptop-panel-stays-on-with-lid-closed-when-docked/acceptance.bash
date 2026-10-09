#!/usr/bin/env bash
# acceptance.bash — render the VERDICT that the lid fix landed (CLAUDE/PlanScriptStandards.md
# R9). Read-only. Checks the deployed state, not the repo: UPower.conf, what upowerd reports
# over D-Bus, and — docked with the lid closed — that the built-in panel is switched off.
#
# Run it DOCKED WITH THE LID CLOSED. Anywhere else the panel check cannot be made, the run
# is incomplete, and it is rejected rather than passed.
#
# The suspend behaviour (undocked on AC, on battery, dock unplugged mid-suspend) needs a
# person at the machine; the closing banner lists it.
#
# WHERE TO RUN: on the HOST, in the GNOME session. Enforced by plan_require_host (R2).
#
# Usage: ./acceptance.bash [-h|--help]
# Exit 0 = ACCEPTED, non-zero = REJECTED.
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Checks, without changing anything: IgnoreLid=false in UPower.conf, UPower reports the lid,
and — docked with the lid closed — the built-in panel is disabled. Prints a COVERAGE line
and rejects an incomplete run.

Exit 0 = ACCEPTED, non-zero = REJECTED."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the host's UPower, sysfs and lid state"
plan_start_log auto

plan_gather_leg "lid fix in effect" bash "${PLAN_SCRIPT_DIR}/check-lid.bash"

printf '\nBy hand, then record the results in the plan:\n'
printf '  1. Docked, on AC, lid closed: Settings shows only the external monitors; no suspend.\n'
printf '  2. Undocked, on AC, lid closed: no suspend.\n'
printf '  3. On battery, lid closed: suspends.\n'
printf '  4. Suspend, then unplug the dock: it still suspends (Plan 00104).\n\n'

plan_finish
