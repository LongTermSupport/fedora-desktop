#!/usr/bin/env bash
set -euo pipefail
# triage.bash — FACT-FINDING for Plan 00125. Renders no verdict (PlanScriptStandards R9).
#
# Answers one question: does `./scripts/qa-all.bash` reach the same verdict HERE as it did
# on the last CI run of this branch, stage by stage? Not pass/fail — the two divergences
# this plan chased hardest were both invisible to a pass/fail comparison. The `js` stage
# reported "files OK" on both machines over a DIFFERENT number of files for weeks, and a
# gate that cannot pass in CI stops every gate declared after it from running at all, so a
# stage can be ABSENT from one side rather than merely failing there. Both show up here.
#
# The probe body is in probe-qa-verdicts.bash beside this file. It changes nothing on this
# machine and runs no playbook.
# Its stdout IS the payload (CLAUDE/StderrHygiene.md's report-command exception).
#
#   CLAUDE/Plan/00125-ci-qa-gate-red-and-machine-dependent/triage.bash
#
# DELIBERATELY NEITHER plan_require_host NOR plan_require_container (R2). R2 asks for one
# of the two and notes that a script accepting either is "rare and worth re-examining" —
# examined: this script's finding is what THIS machine answers against what CI answered,
# so it is meant to be run in the container AND on the host, and the two runs compared.
# Pinning it to one location would delete the comparison the plan exists to make. Where it
# ran is therefore the first fact the probe reports, not an assumption it makes.

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

PLAN_USAGE="usage: triage.bash [--branch NAME] [-h|--help]

Compares this machine's ./scripts/qa-all.bash verdicts against the most recent COMPLETED
QA workflow run for a branch, stage by stage, and names every stage they disagree on.

  --branch NAME   which branch's CI run to compare against (default: the checked-out one)

Needs the gh CLI, authenticated, and takes as long as qa-all.bash does. Run it in the CCY
container AND on the host: the two reports are meant to be read next to each other."

# Parsed before anything resolves an environment, so --help still works on the machine
# that needs diagnosing (CLAUDE/PlanTriage.md).
plan_parse_common_flags "$@"

TRIAGE_BRANCH=""
expecting_branch=0
for arg in ${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}; do
    if [[ "${expecting_branch}" -eq 1 ]]; then
        TRIAGE_BRANCH="${arg}"
        expecting_branch=0
        continue
    fi
    case "${arg}" in
        --branch) expecting_branch=1 ;;
        *)
            printf '[FATAL] unknown argument: %s\n\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 1
            ;;
    esac
done
if [[ "${expecting_branch}" -eq 1 ]]; then
    printf '[FATAL] --branch needs a value\n\n%s\n' "${PLAN_USAGE}" >&2
    exit 1
fi

plan_mode gather
plan_start_log auto

printf '════════════════════════════════════════════════════════════\n'
printf ' Plan 00125 — does qa-all.bash answer the same here as in CI?\n'
printf '════════════════════════════════════════════════════════════\n\n'

plan_gather_leg 'qa-all.bash here, against the last CI run' \
    bash "${PLAN_SCRIPT_DIR}/probe-qa-verdicts.bash" \
    "${PLAN_RUN_DIR}" "${PLAN_REPO_ROOT}" "${TRIAGE_BRANCH}"

printf '\n'
printf 'READ THE "differs", "only-here" AND "only-there" ROWS FIRST.\n'
printf '  differs      the same stage reached a different verdict on the two machines.\n'
printf '  only-there   this machine did not run that stage AT ALL. qa-all.bash exits at\n'
printf '               the first failing hard gate, so this is usually an earlier gate\n'
printf '               failing here and hiding every gate declared after it.\n'
printf '  only-here    the mirror of that, on the CI side.\n'
printf '\n'
printf 'A differing row is either an environment dependency this repo has already\n'
printf 'declared or a new one. CLAUDE/QA.md, "The same command does not reach the same\n'
printf 'verdict everywhere", lists the declared ones — check there before assuming a bug.\n'
printf 'This script renders no verdict on which it is.\n'

plan_finish
