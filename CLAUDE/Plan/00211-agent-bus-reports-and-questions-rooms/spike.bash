#!/usr/bin/env bash
# spike.bash — Plan 00211 Task 0.1 (SPEC §11.2): does the installed Tuwunel do what the side
# rooms need? It starts the pinned Tuwunel binary as YOU (not the agent-bus user), on a free
# loopback port, with a config, database and throwaway accounts in a scratch directory under
# this run's directory, runs spike_probe.py's probes against it, and removes the scratch
# directory and the process on exit, however the run ends.
#
# WHAT IT CHANGES: nothing outside its own run directory. No unit, no team file, no play, no
# real team's homeserver. It is `deploy` mode only because it starts a process (R7).
#
# WHAT IT RECORDS (spike-report.md in the run directory, listed at the end):
#   R1 a restricted join into reports by main-room membership (room version 12);
#   R2 a power-0 human refused m.room.message in reports; R3 a power-50 human refused
#   m.reaction in questions; R4 a pending invite harmless across filtered syncs.
# A fact that does not hold fails the run: SPEC §3.1, §4.4 and §5.3 are then amended
# before Phase 1 is deployed.
#
# WHERE TO RUN: on the HOST, through meta-deploy (plan_require_host). Needs the agent-bus play
# to have installed /usr/local/lib/agent-bus/tuwunel.
#
# Usage: ./CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/spike.bash [-h|--help]
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

PLAN_USAGE="usage: spike.bash [-h|--help]"
plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 0 ]]; then
  printf '[FATAL] spike.bash takes no arguments: %s\n' "${PLAN_USAGE}" >&2
  exit 1
fi
if [[ "${PLAN_CHECK:-0}" == "1" ]]; then
  printf '[FATAL] spike.bash has no --check: it runs no play, and its only change is a scratch process\n' >&2
  exit 1
fi

plan_require_host "it runs the Tuwunel binary the agent-bus play installed on this host"
plan_start_log auto

SPIKE_WORK="${PLAN_RUN_DIR}/homeserver"
spike_cleanup() {
  local pid=""
  if [[ -f "${SPIKE_WORK}/tuwunel.pid" ]]; then
    pid="$(<"${SPIKE_WORK}/tuwunel.pid")"
    if [[ -d "/proc/${pid}" ]]; then
      printf '==> stopping the spike homeserver (pid %s)\n' "${pid}"
      kill -TERM "${pid}"
    fi
  fi
  if [[ -d "${SPIKE_WORK}" ]]; then
    rm -rf -- "${SPIKE_WORK}"
  fi
}
plan_on_cleanup spike_cleanup

plan_deploy_leg "probe a throwaway Tuwunel" \
  env PYTHONPATH="${PLAN_REPO_ROOT}" python3 "${PLAN_SCRIPT_DIR}/spike_probe.py" \
  "${SPIKE_WORK}" "${PLAN_RUN_DIR}/spike-report.md"
spike_cleanup # the same call an interrupted run makes through plan_on_cleanup
plan_finish
