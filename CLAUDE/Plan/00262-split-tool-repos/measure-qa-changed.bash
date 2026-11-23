#!/usr/bin/env bash
# measure-qa-changed.bash — wall time of a local qa-changed run before and after the lean change
# (subagent-reports/qa-changed-lean-report.md). Gathers numbers only; renders no verdict (R9).
#
# It adds two throwaway detached worktrees under untracked/qa-measure/ (BEFORE and AFTER),
# appends one comment line to src-go/internal/teams/app.go in each (never committed), and in each
# runs:
#   one-package   qa-changed --base HEAD over that change: the Go gate in diff mode, the semgrep
#                 gates and the ccy-teams suites, with an empty suite-pass store;
#   no-op repeat  the same run again with nothing new: what re-checking an unchanged binary costs.
# The worktrees are removed at the start (a leftover of an interrupted run) and at the end.
# Nothing else in the checkout changes.
#
# RUN ON THE HOST, with the machine otherwise idle: it runs qa-changed four times, at the QA
# lock's nice 19 / ionice idle (QA_FULL_PRIORITY=1 times at normal priority).
#
# Usage: ./CLAUDE/Plan/00262-split-tool-repos/measure-qa-changed.bash [-h|--help]
#   AFTER   the lean commit (default origin/qa-changed-lean)
#   BEFORE  the commit to compare with (default the merge base of origin/F44 and AFTER)
#
# EXIT CODES: 0 every leg ran; 1 a leg failed (it names itself; its time is no measurement);
#  64 usage error
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
# Copy this block verbatim into every plan script. See CLAUDE/PlanScriptStandards.md R1.
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

PLAN_USAGE="usage: measure-qa-changed.bash [-h|--help]   (env: AFTER, BEFORE)

Times qa-changed in a BEFORE and an AFTER worktree over a one-package change,
then again with nothing new. Host-only; writes measure-report.md in the run
directory and names it on completion."

plan_mode gather
plan_parse_common_flags "$@"

# Reject unknown arguments rather than ignoring them (R12/R10 structure).
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64 # EX_USAGE — never confusable with a real probe result
fi

# Refuse the container BEFORE opening a log, so a wrong-environment run fails immediately and
# leaves no half-written report to be mistaken for evidence.
plan_require_host "it times QA on the owner's machine; a container's numbers are not the owner's"

plan_start_log auto

after="${AFTER:-origin/qa-changed-lean}"
before="${BEFORE:-$(git -C "${PLAN_REPO_ROOT}" merge-base origin/F44 "${after}")}"
wtRoot="${PLAN_REPO_ROOT}/untracked/qa-measure"
one="${PLAN_SCRIPT_DIR}/measure-qa-changed-one.bash"
edited="src-go/internal/teams/app.go"
REPORT="${PLAN_RUN_DIR}/measure-report.md"
readonly REPORT

{
    printf '# qa-changed wall time, before (%s) and after (%s)\n\n' "${before}" "${after}"
    printf 'Change: one comment line appended to %s, diffed against HEAD.\n\n' "${edited}"
    printf '| run | seconds | exit | suites skipped | log |\n|---|---|---|---|---|\n'
} >"${REPORT}"

plan_gather_leg "remove leftover worktrees" "${one}" --remove "${wtRoot}/before" "${wtRoot}/after"
for side in before after; do
    ref="${before}"
    [[ "${side}" == after ]] && ref="${after}"
    plan_gather_leg "${side}: worktree at ${ref}" git -C "${PLAN_REPO_ROOT}" worktree add --detach "${wtRoot}/${side}" "${ref}"
    plan_gather_leg "${side}: the one-package change" "${one}" --edit "${wtRoot}/${side}/${edited}"
    passes="${PLAN_RUN_DIR}/${side}-suite-passes"
    plan_gather_leg "${side}: one-package" "${one}" "${wtRoot}/${side}" "${side}-one-package" "${REPORT}" "${passes}"
    plan_gather_leg "${side}: no-op repeat" "${one}" "${wtRoot}/${side}" "${side}-no-op-repeat" "${REPORT}" "${passes}"
done
plan_gather_leg "remove the worktrees" "${one}" --remove "${wtRoot}/before" "${wtRoot}/after"

plan_finish
