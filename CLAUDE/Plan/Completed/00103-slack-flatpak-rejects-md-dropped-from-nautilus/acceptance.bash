#!/usr/bin/env bash
# Plan 00103 — acceptance.bash
#
# PURPOSE: render the VERDICT that deploy.bash landed the fix (CLAUDE/PlanScriptStandards.md
# R9). Checks the override is recorded AND that the sandbox can now actually open the
# fixture next to this script, which is the exact operation the drop needs. HOST ONLY,
# read-only.
#
# The drag itself cannot be automated from here; the closing banner tells the operator
# what to do.
#
# Usage: ./acceptance.bash [-h|--help]
# Exit 0 = ACCEPTED, non-zero = REJECTED (the failed leg names itself).
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

Checks, on the HOST and without changing anything:
  1. the system Flatpak override for com.slack.Slack records home:ro
  2. a fresh Slack sandbox can stat sample-drop.md next to this script
  3. the sandbox reads the same byte count the host does

Exit 0 = ACCEPTED, non-zero = REJECTED."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the host Flatpak overrides and runs a command inside the Slack sandbox"
plan_start_log auto

readonly APP_ID="com.slack.Slack"
readonly FIXTURE="${PLAN_SCRIPT_DIR}/sample-drop.md"

# Every check is a plain command, so a failed leg is a failed check and plan_finish exits
# non-zero. The sandbox legs spawn a one-off instance with the CURRENT overrides, which
# is what a relaunched Slack gets.
plan_gather_leg "override records home:ro" \
    grep -q 'home:ro' <<<"$(flatpak override --show "${APP_ID}")"
plan_gather_leg "sandbox can stat the fixture" \
    flatpak run --command=stat "${APP_ID}" -c '%s %n' "${FIXTURE}"
plan_gather_leg "sandbox reads the same size as the host" \
    test "$(flatpak run --command=stat "${APP_ID}" -c '%s' "${FIXTURE}")" = "$(stat -c '%s' "${FIXTURE}")"

printf '\nNow the real test: with Slack relaunched, drag sample-drop.md from this folder in\n'
printf 'Nautilus into a message. It must upload. If it does not, the plan is not done.\n\n'

plan_finish
