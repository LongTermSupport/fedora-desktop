#!/usr/bin/env bash
# Plan 00235 — acceptance.bash
#
# PURPOSE: judge the daily prune on the HOST, after deploy.bash (Task 7.1's timer parts).
# Read-only: it asks systemd and podman and removes nothing.
#
# Checks: the timer is enabled with a next run; the prune service has run and its last run
# succeeded, with its removal log present; no dangling image over a day old is left that no
# container uses; every container still has its image; claude-yolo:base and :latest exist; the
# free space on / and /home now. The free space before the deploy is in the triage report of
# the same meta-deploy run (triage.bash runs before deploy.bash and again after it).
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

The Plan 00235 acceptance gate for the daily prune. Run deploy.bash first (meta-deploy.bash
does).

EXIT STATUS
  0  ACCEPTED — every check ran and passed
  1  REJECTED — a check failed, or did not run
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n%s\n' "${PLAN_REMAINING_ARGS[*]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it checks the host user's prune timer and podman store"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-prune.bash"
plan_gather_leg "the prune timer is enabled with a next run" bash "${checker}" timer
plan_gather_leg "the prune has run, its last run succeeded, its log exists" bash "${checker}" last-run
plan_gather_leg "no unused dangling image over a day old is left" bash "${checker}" no-old-unused-dangling
plan_gather_leg "every container still has its image" bash "${checker}" containers-keep-images
plan_gather_leg "claude-yolo:base and :latest exist" bash "${checker}" ccy-images
plan_gather_leg "free space on / and /home after the prune" bash "${checker}" free-space
plan_finish
