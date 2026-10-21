#!/usr/bin/env bash
# Plan 00183 — acceptance.bash
#
# PURPOSE: render the VERDICT on the installed git-fetch-age (CLAUDE/PlanScriptStandards.md
# R9), the prompt's stale-fetch mark, through what the unit tests cannot reach: the binary
# play-basic-configs.yml built and installed, the real git, and the deployed prompt file. It
# checks the binary and the prompt are installed, the marks in scratch repositories, that a
# real fetch which cannot reach its remote leaves an empty FETCH_HEAD that is marked rather
# than read as fresh (the cause of #52), and that the deployed PS1 shows the mark. HOST ONLY.
#
# It never reaches a remote: the one fetch it runs goes through ssh replaced by false. It
# changes nothing but the scratch directories it makes with mktemp -d and removes. No
# prompts. The yellow mark in a real terminal is the owner's look.
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

The Plan 00183 acceptance gate. Run play-basic-configs.yml first (meta-deploy.bash does).
It runs the installed git-fetch-age in scratch repositories, after a real fetch that cannot
reach its remote, and through the deployed prompt. It never reaches a remote.

EXIT STATUS
  0  ACCEPTED — every check ran and passed
  1  REJECTED — a check failed, or did not run
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it checks the git-fetch-age binary and the prompt the host's play installed"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-fetch-age.bash"
plan_gather_leg "git-fetch-age installed root-owned, and the prompt calls it" bash "${checker}" installed
plan_gather_leg "marks: fresh none, 3 days old, none recorded, empty FETCH_HEAD" bash "${checker}" marks
plan_gather_leg "a real fetch that cannot reach its remote is marked, not read as fresh" bash "${checker}" failed
plan_gather_leg "the deployed prompt shows the mark" bash "${checker}" prompt
plan_finish
