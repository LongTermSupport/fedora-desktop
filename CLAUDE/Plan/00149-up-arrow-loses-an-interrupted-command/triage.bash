#!/usr/bin/env bash
# triage.bash — Plan 00149: gather FACTS about why up-arrow does not bring back a command
# that was interrupted with Ctrl+C. Fact-finding only: renders no verdict (R9).
#
# WHERE TO RUN: on the HOST, in a terminal, as the desktop user (not root: the Ctrl+R
# history search, and the per-terminal up-arrow it sets up, is skipped for root).
#
# WHAT IT CHANGES: nothing is configured. The up-arrow probe runs a few short commands in
# real interactive shells, marked `uparrow-probe`, and those reach your history file as any
# command would.
#
# Two runs of the same three cases (a finished command; a running command stopped with
# Ctrl+C; a typed line abandoned with Ctrl+C), so the cause can be placed:
#   1. the full interactive startup, exactly as a terminal gets it;
#   2. only this repo's two history files, with nothing else from the startup.
# If 2 is right and 1 is wrong, something else in the startup is the cause.
#
# Usage: ./CLAUDE/Plan/00149-up-arrow-loses-an-interrupted-command/triage.bash [-h|--help]
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

PLAN_USAGE="usage: triage.bash [-h|--help]"
plan_mode gather
plan_parse_common_flags "$@"

plan_require_host "it probes the host's own interactive bash and its startup files"
if [[ "${EUID}" -eq 0 ]]; then
  printf '[FATAL] run this as the desktop user, not root: root has no per-terminal up-arrow\n' >&2
  exit 1
fi
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"
MINIMAL_RC="${PLAN_RUN_DIR}/history-only.rc"
cat >"${MINIMAL_RC}" <<'RC'
# Only this repo's two history files, as a terminal would load them, and a plain prompt.
source /etc/profile.d/zz_lts-fedora-desktop.bash
source "${HOME}/.bashrc-includes/history-search.bash"
PS1='uparrow-probe> '
RC

plan_gather_leg "facts" bash "${PLAN_SCRIPT_DIR}/probe-facts.bash" "${REPORT}" "${PLAN_REPO_ROOT}"
plan_gather_leg "up-arrow, full startup" python3 "${PLAN_SCRIPT_DIR}/probe-uparrow.py" "${REPORT}"
plan_gather_leg "up-arrow, history files only" python3 "${PLAN_SCRIPT_DIR}/probe-uparrow.py" "${REPORT}" "${MINIMAL_RC}"
plan_finish
