#!/usr/bin/env bash
# triage.bash — Plan 00255 Phase 0: the host facts a sealed cc session and a deploy broker
# depend on.
#
# PURPOSE: gather FACTS, render no verdict (CLAUDE/PlanScriptStandards.md R9). Each leg is one
# mode of probe-host.bash, which documents what it reads:
#   tools, kernel, paths, bus, sudo, escape; and with --active, two harmless bwrap runs.
#
# WHERE TO RUN: on the HOST, as the desktop user, from a terminal in the desktop session (so the
# session bus is the real one).
#
# EFFECT ON THE HOST: none. Read-only; `sudo -n -l` only lists rules and never prompts. --active
# starts bwrap twice, each running one command inside, then exiting.
#
# SECRETS: no file under a folder the seal would hide is opened or listed (existence and type
# only), and gh output keeps only the lines naming its store.
#
# Usage: ./CLAUDE/Plan/00255-cc-session-sandbox-and-deploy-broker/triage.bash [--active] [-h|--help]
#
# EXIT CODES: 0 every leg reached an answer; 1 a leg could not (it names itself); 2 an unknown
# argument.
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

PLAN_USAGE="usage: triage.bash [--active] [-h|--help]

Read-only: the host facts Plan 00255 (sealed cc sessions, deploy broker) depends on. Run on the
HOST as the desktop user, in the desktop session. --active also runs bwrap twice (harmless).
Writes triage-report.txt under untracked/plan-runs/."
plan_mode gather
plan_parse_common_flags "$@"
ACTIVE=0
for arg in "${PLAN_REMAINING_ARGS[@]}"; do
  case "${arg}" in
    --active) ACTIVE=1 ;;
    *) printf 'triage.bash: unknown argument %s (see --help)\n' "${arg}" >&2; exit 2 ;;
  esac
done

plan_require_host "it reads the host's own session bus, sudo rules and home folders"
plan_start_log auto

readonly REPORT="${PLAN_RUN_DIR}/triage-report.txt"
readonly PROBE="${PLAN_SCRIPT_DIR}/probe-host.bash"
readonly CLIENTS="${PLAN_REPO_ROOT}/src-go/internal/matrixclients/clients.json"

printf '# Plan 00255 triage: read "sudo now" and "session bus" first\n\n' >"${REPORT}"
plan_gather_leg "sandbox tools" bash "${PROBE}" tools "${REPORT}" "${CLIENTS}"
plan_gather_leg "kernel settings" bash "${PROBE}" kernel "${REPORT}" "${CLIENTS}"
plan_gather_leg "folders the seal would hide" bash "${PROBE}" paths "${REPORT}" "${CLIENTS}"
plan_gather_leg "session bus and credential stores" bash "${PROBE}" bus "${REPORT}" "${CLIENTS}"
plan_gather_leg "sudo now" bash "${PROBE}" sudo "${REPORT}" "${CLIENTS}"
plan_gather_leg "ways out of a seal" bash "${PROBE}" escape "${REPORT}" "${CLIENTS}"
if [[ "${ACTIVE}" == "1" ]]; then
  plan_gather_leg "bwrap runs" bash "${PROBE}" active "${REPORT}" "${CLIENTS}"
fi
plan_finish
