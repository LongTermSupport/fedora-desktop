#!/usr/bin/env bash
# Plan 00210 — acceptance.bash
#
# PURPOSE: render the VERDICT on the installed convert command (CLAUDE/PlanScriptStandards.md
# R9), through the real path the unit tests' fake runner never exercises: the binary
# play-cli-tools.yml installed at ~/.local/bin/convert, and the real pandoc. It converts
# fixture.md (headings, bullet and numbered lists, a table, bold, italic, a code block) and
# reads the DOCX back with pandoc to confirm each element survived; it also checks the
# listing and the refusal of an unsupported format (issue #102's acceptance). HOST ONLY.
#
# It changes nothing outside its own run directory, where the fixture copy and the DOCX
# are written. No prompts.
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

The Plan 00210 acceptance gate. Run play-cli-tools.yml first (meta-deploy.bash does).
It runs the installed convert on a fixture Markdown file and checks the DOCX with pandoc.

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

plan_require_host "it checks the convert binary and pandoc the host's play installed"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-convert.bash"
fixture="${PLAN_SCRIPT_DIR}/fixture.md"
plan_gather_leg "convert lists docx for a Markdown file" bash "${checker}" list "${fixture}" "${PLAN_RUN_DIR}/list"
plan_gather_leg "convert refuses --to pdf with exit 64, naming docx" bash "${checker}" refuse "${fixture}" "${PLAN_RUN_DIR}/refuse"
plan_gather_leg "convert writes a DOCX that keeps every fixture element" bash "${checker}" docx "${fixture}" "${PLAN_RUN_DIR}/docx"
plan_finish
