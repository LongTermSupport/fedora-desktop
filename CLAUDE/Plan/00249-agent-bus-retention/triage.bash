#!/usr/bin/env bash
# triage.bash — Plan 00249: measure what the agent team bus homeservers keep on disk.
#
# PURPOSE: gather FACTS about each team's homeserver storage: the RocksDB database, Tuwunel's
# own database backups, the installer's state tars, media, the free space of the filesystem
# that holds them, and the number of SST files and backups. Fact-finding only: it renders no
# verdict (CLAUDE/PlanScriptStandards.md R9). Re-run it after each phase of the plan to see the
# effect of retention.
#
# Both systems are measured where they exist: v1 (agent-bus, /var/lib/agent-bus/<team>/ and
# /var/lib/agent-bus-install/<team>/) and v2 (teamchat, /var/lib/private/teamchat/<team>/ and
# /var/lib/teamchat-admin/<team>/). A system that is not installed is reported as absent.
#
# WHERE TO RUN: on the HOST (plan_require_host, R2). The state directories are 0700 and owned
# by the service user or root, so the probes read them with `sudo -n` after plan_prime_sudo.
#
# EFFECT ON THE HOST: READ-ONLY. It runs du, df, find and stat; it does not talk to any
# homeserver, read any secret, stop any unit or take a backup. It never reads a file's content
# under secrets/: only sizes of directories are measured.
#
# Usage: ./CLAUDE/Plan/00249-agent-bus-retention/triage.bash [-h|--help]
#
# EXIT CODES: 0 every leg reached an answer; 1 a leg could not (the fact-finding is
# incomplete, the failing leg names itself); 2 an unknown argument.
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

PLAN_USAGE="usage: triage.bash [-h|--help]

Read-only: measures every agent team bus homeserver's database, backups, state tars and
media, and the free space under them (Plan 00249). Run on the HOST; needs sudo for reading
only. Writes triage-storage-report.txt under untracked/plan-runs/."
plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
  printf 'triage.bash: unknown argument %s (see --help)\n' "${PLAN_REMAINING_ARGS[0]}" >&2
  exit 2
fi

plan_require_host "it measures the homeserver state directories of the host"
plan_prime_sudo
plan_start_log auto

readonly REPORT="${PLAN_RUN_DIR}/triage-storage-report.txt"
readonly PROBE="${PLAN_SCRIPT_DIR}/probe-storage.bash"

plan_gather_leg "v1 (agent-bus) per-team storage" bash "${PROBE}" v1 "${REPORT}"
plan_gather_leg "v2 (teamchat) per-team storage" bash "${PROBE}" v2 "${REPORT}"
plan_gather_leg "free space of the filesystems holding them" bash "${PROBE}" df "${REPORT}"
plan_finish
