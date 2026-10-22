#!/usr/bin/env bash
# Plan 00214 — acceptance.bash
#
# PURPOSE: render the VERDICT (CLAUDE/PlanScriptStandards.md R9) on issue #110 on this host's
# real ledger and checkout, which the unit tests cannot reach. It checks that the health
# report's play-freshness check and run.bash --changed's helper name the same plays (the
# report counted 10 where --changed ran 25), and that fedora-desktop-health and the login
# shell's once-a-day reminder print this checkout's run.bash --changed when a play has to
# run again. HOST ONLY.
#
# It runs no play and needs no sudo. What it changes is what a login already changes: the
# freshness check's git fetch and its fetch stamp, and one collection (login_report
# --no-notify, as host-health.service runs it), which rewrites the status document and the
# handoff file. The once-a-day stamp it uses is a scratch one, so the next login still shows
# the full report. Run it BEFORE a changed-plays entry: with no play to run there is no
# command to look for, and those two checks are NOT JUDGED (exit 2).
#
# Usage: ./acceptance.bash [-h|--help]
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

The Plan 00214 acceptance gate (issue #110). On this host's ledger and checkout: the health
report and run.bash --changed name the same plays, and fedora-desktop-health and the login
reminder print this checkout's run.bash --changed when a play has to run again. Runs no
play.

EXIT STATUS
  0  ACCEPTED — every check ran and passed
  1  REJECTED — a check failed, or did not run
  2  NOT JUDGED — nothing failed, but no play had to run, so the command was not looked for
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads this host's play ledger and the report its login shows"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-changed-plays.bash"
plan_gather_leg "the report and run.bash --changed name the same plays" bash "${checker}" same "${PLAN_REPO_ROOT}" "${PLAN_RUN_DIR}"
plan_gather_leg "fedora-desktop-health prints this checkout's run.bash --changed" bash "${checker}" report "${PLAN_REPO_ROOT}" "${PLAN_RUN_DIR}"
plan_gather_leg "the login reminder names the same command" bash "${checker}" reminder "${PLAN_REPO_ROOT}" "${PLAN_RUN_DIR}"

if [[ -z "${PLAN_FAILED_LEGS}" && -s "${PLAN_RUN_DIR}/not-judged" ]]; then
    # plan_finish knows only failed legs and would exit 0; a check with no evidence is exit 2.
    printf '\nNOT JUDGED: %s (nothing failed; no play had to run)\n' "$(paste -sd ' ' "${PLAN_RUN_DIR}/not-judged")"
    printf '==> run log: %s\n' "${PLAN_RUN_LOG}"
    exit 2
fi
plan_finish
