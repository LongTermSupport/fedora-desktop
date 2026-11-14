#!/usr/bin/env bash
# triage.bash — Plan 00256: gather FACTS for the ccy idle stand-down (Task 1.1). Fact-finding
# only: it renders no verdict (CLAUDE/PlanScriptStandards.md R9).
#
#   1. What the PTY supervisor (.claude/ccy/claude-supervise.py) and the ccy-lifecycle plugin
#      can see of the four idle conditions: an empty prompt, no turn running, no pending bus
#      delivery, no background task or sub-agent. Read from the supervisor's own files
#      (decision.log, supervisor-status.json, supervise/, the context sidecars) and from what
#      sits beside them in the project and in the container (legs "files" and "containers").
#   2. How often on_idle runs on a session left alone (leg "sample", with --sample SECONDS):
#      the plugin's in-hook marker is created and removed in supervise/ on every hook call, so
#      the gaps between that directory's changes bound the cadence.
#   3. Whether `ccy-sessions --list` and `ccy-sessions notify` leave out a session that has a
#      record but no tmux session (leg "listing", against a scratch registry under the run
#      directory that holds one such record; notify runs with --dry-run).
#   4. Whether `<compose> stop` exists, and what each installed compose tool says `stop` and
#      `down` do (leg "compose": versions and --help only). A throwaway compose project would
#      create and stop real containers, and triage changes nothing (CLAUDE/PlanTriage.md); a
#      behaviour test of `stop` belongs in the plan's acceptance.bash.
#
# WHERE TO RUN: on the HOST (plan_require_host, R2), as the user whose ccy sessions are read,
# with at least one ccy session running and left alone for the sample.
#
# EFFECT ON THE HOST: READ-ONLY, no sudo. It reads files, lists tmux sessions, runs podman ps,
# inspect and exec with reading commands (printenv, find, ps), runs ccy-sessions --list and
# `ccy-sessions notify reboot-warning --minutes 1 --dry-run` (which signals nothing), and asks
# the compose tools for version and help. It stops, starts, removes and signals nothing.
#
# OUTPUT: triage-idle-report.txt in the run directory under untracked/plan-runs/ holds the
# ANONYMISED facts (session-N, project-N, container-N; no names, paths, hosts or ids).
# raw-*.txt beside it hold ccy-sessions output with names, and stay there.
#
# Usage: ./CLAUDE/Plan/00256-ccy-session-idle-stand-down/triage.bash [--sample SECONDS] [-h|--help]
# Exit: 0 every leg answered; 1 a leg could not ("COULD NOT ESTABLISH" in the report); 2 bad args.
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

PLAN_USAGE="usage: triage.bash [--sample SECONDS] [-h|--help]

Read-only: what a ccy session's supervisor and lifecycle plugin can see of an idle session,
what ccy-sessions lists and signals for a session with no tmux session, and what the host's
compose tools say 'stop' does (Plan 00256). Run on the HOST.

  --sample SECONDS   also sample each live ccy project for SECONDS (1 to 3600) to measure how
                     often the supervisor's plugin hook runs; leave the sessions alone meanwhile

Writes triage-idle-report.txt (anonymised) under untracked/plan-runs/."
plan_mode gather
plan_parse_common_flags "$@"
SAMPLE=""
set -- "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --sample)
      if [[ "$#" -lt 2 ]] || [[ ! "$2" =~ ^[1-9][0-9]*$ ]] || [[ "$2" -gt 3600 ]]; then
        printf 'triage.bash: --sample needs whole seconds, 1 to 3600 (see --help)\n' >&2
        exit 2
      fi
      SAMPLE="$2"
      shift 2
      ;;
    *)
      printf 'triage.bash: unknown argument %s (see --help)\n' "$1" >&2
      exit 2
      ;;
  esac
done

plan_require_host "it reads the host's ccy session registry, tmux server, containers and compose tools"
plan_start_log auto

readonly REPORT="${PLAN_RUN_DIR}/triage-idle-report.txt"
readonly PROBE="${PLAN_SCRIPT_DIR}/probe-idle.bash"

plan_gather_leg "sessions: registry against live tmux" bash "${PROBE}" sessions "${REPORT}"
plan_gather_leg "files: what the supervisor can read (item 1)" bash "${PROBE}" files "${REPORT}"
plan_gather_leg "containers: bus inbox and process tree (item 1)" bash "${PROBE}" containers "${REPORT}"
plan_gather_leg "listing: --list and notify for a no-tmux record (item 3)" bash "${PROBE}" listing "${REPORT}"
plan_gather_leg "compose: stop and down (item 4)" bash "${PROBE}" compose "${REPORT}"
if [[ -n "${SAMPLE}" ]]; then
  plan_gather_leg "sample: on_idle cadence over ${SAMPLE}s (item 2)" bash "${PROBE}" sample "${SAMPLE}" "${REPORT}"
else
  printf '==> no --sample given: item 2 (on_idle cadence) is not measured in this run\n' | tee -a "${REPORT}"
fi
printf '==> READ FIRST: the "sample" section (item 2), then "files" and "containers" (item 1)\n'
plan_finish
