#!/usr/bin/env bash
# triage-hs.bash — gather grounded FACTS about why team homeservers (Tuwunel, run as
# agent-bus-hs@<team>.service) have stopped, over the whole journal this host keeps.
# Fact-finding only: renders no verdict (R9) and changes nothing. Read-only and safe to re-run.
#
# WHERE TO RUN: on the HOST, in a terminal or through meta-deploy.bash. Enforced by
# plan_require_host (R2): a container has neither the host journal nor the units.
#
# NEEDS SUDO, read-only: the whole system journal (every boot, the kernel's OOM lines, sudo's
# own records), the team directories (/var/lib/agent-bus/<team>, 0700 agent-bus) for the
# config and the database size, the health records, and coredumpctl for the agent-bus user's
# dumps. Every root read is `sudo -n`, primed before the log opens (R3); an unprimed run records
# the refusal and the leg fails, it never prompts into the log.
#
# What it gathers, for every team homeserver and its health and backup units:
#   - every start and stop with systemd's own record of it (exit code or signal, Result=, the
#     restart counter, the watchdog, the OOM killer, a core dump) and who asked for it: a play
#     or deploy (ansible, agent-bus-install), a sudo command, a reboot, the health check's
#     restart, an acceptance run, or nobody (an unrequested stop); hs_journal.py does this on
#     the journal exports and correlates every stop with the plan runs and meta-deploy runs
#     under untracked/plan-runs/ and with the boots (journalctl --list-boots)
#   - for each stop, the unit's own log lines just before it and any kernel OOM line
#   - the health check's restarts with the probe failure it logged
#   - the memory peaks systemd recorded, the current peak and every limit on the units
#   - the Tuwunel version, each team's tuwunel.toml with every secret-named string value
#     substituted, the database size, its filesystem and mount options (Tuwunel #653: a start
#     that hangs after an unclean kill, on btrfs) and its free space and inodes (#649: writes
#     panic on a full disk)
#   - the coredumps of tuwunel
#   - each running ccy container's watcher log (/tmp/pingbus-home/watch.log, read with a
#     rootless `podman exec cat`): its exits, supervisor restarts and dropped teams
#
# The probes are in _triage-hs.inc.bash; the stop analysis is hs_journal.py (its tests:
# test_hs_journal.py).
#
# Reports (UNSCRUBBED host state, never committed) go in this run's directory under
# untracked/plan-runs/; stdout carries a short per-team summary.
#
# Usage: ./CLAUDE/Plan/00203-teams-hot-control-and-robustness/triage-hs.bash [-h|--help]
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

PLAN_USAGE="usage: triage-hs.bash [-h|--help]

Read-only facts about every stop of the agent team bus homeservers (Plan 00203). Run on the
HOST; needs sudo for reading only. Writes its reports under untracked/plan-runs/ and prints a
short per-team summary."
plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf 'triage-hs.bash: unknown argument %s (see --help)\n' "${PLAN_REMAINING_ARGS[0]}" >&2
    exit 2
fi

plan_require_host "it reads the host's system journal, the homeserver units and their state directories"
plan_prime_sudo
plan_start_log auto

readonly STATE_ROOT=/var/lib/agent-bus
readonly HEALTH_ROOT=/var/lib/agent-bus-applied/health
readonly TUWUNEL=/usr/local/lib/agent-bus/tuwunel
readonly RUNS_ROOT="${PLAN_REPO_ROOT}/untracked/plan-runs"
readonly FACTS="${PLAN_RUN_DIR}/triage-hs-facts-report.txt"
readonly STOPS="${PLAN_RUN_DIR}/triage-hs-stops-report.txt"
readonly SUMMARY="${PLAN_RUN_DIR}/summary.txt"
readonly EXPORTS="${PLAN_RUN_DIR}/journal"
mkdir -p "${EXPORTS}"
: >"${FACTS}"

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_triage-hs.inc.bash
source "${PLAN_SCRIPT_DIR}/_triage-hs.inc.bash"

TEAMS=()
teamsOut="$(installed_teams)"
if [[ -n "${teamsOut}" ]]; then
    mapfile -t TEAMS <<<"${teamsOut}"
fi
printf '==> installed teams: %s\n' "${#TEAMS[@]}"

plan_gather_leg "journal export (whole journal, as root)" export_journal
plan_gather_leg "every homeserver stop, its cause and who asked (hs_journal.py)" analyse_stops
plan_gather_leg "units, limits, version, config, database, coredumps" gather_facts

printf '\n==================== SUMMARY ====================\n'
if [[ -s "${SUMMARY}" ]]; then
    cat "${SUMMARY}"
else
    printf 'no stop analysis: see the failed leg above\n'
fi
printf 'tuwunel: %s\n' "$(awk '/^### tuwunel --version/{getline; print; exit}' "${FACTS}")"
printf 'coredumps: %s\n' "$(awk '/^### coredumpctl list tuwunel/{print $NF; getline; print; exit}' "${FACTS}" | tr '\n' ' ')"
printf 'READ FIRST: %s (each stop with its cause), then %s\n' "${STOPS}" "${FACTS}"
plan_finish
