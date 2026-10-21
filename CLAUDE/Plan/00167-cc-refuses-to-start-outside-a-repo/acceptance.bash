#!/usr/bin/env bash
# Plan 00167 — acceptance.bash
#
# PURPOSE: render the VERDICT on the installed cc-project-dir (CLAUDE/PlanScriptStandards.md
# R9), the directory host cc starts its session in, through the real path the unit tests'
# fake runner never exercises: the binary play-claude-yolo.yml built with the checkout baked
# in, and the real git. It checks the no-terminal path (stdin </dev/null, and
# CCY_SESSION_RESTORE=1 at a terminal, each refuse with exit 1 and offer nothing) and the
# git-top path (a subfolder of a scratch repository gives the repository's top). HOST ONLY.
#
# It never runs cc, so no Claude session and no tmux session is started. It changes nothing
# but the scratch directories it makes with mktemp -d and removes. No prompts. The offer
# itself needs a person at a terminal: that is the owner's check (type cc in ~, accept once
# and decline once).
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

The Plan 00167 acceptance gate. Run play-claude-yolo.yml first (meta-deploy.bash does).
It runs the installed cc-project-dir with no terminal, as a restore, and in a scratch
repository's subfolder. It never starts Claude.

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

plan_require_host "it checks the cc-project-dir binary the host's play installed"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-cc-project-dir.bash"
plan_gather_leg "stdin </dev/null outside a repository refuses, exit 1, no offer" bash "${checker}" no-stdin
plan_gather_leg "CCY_SESSION_RESTORE=1 outside a repository refuses, exit 1, no offer" bash "${checker}" restore
plan_gather_leg "a scratch repository's subfolder gives the repository's top" bash "${checker}" git-top
plan_finish
