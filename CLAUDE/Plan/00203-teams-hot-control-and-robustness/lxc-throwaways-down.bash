#!/usr/bin/env bash
# Plan 00203 — lxc-throwaways-down.bash: removes the two throwaway LXC containers
# lxc-throwaways-up.bash left running for Task 3.23, and checks that neither remains.
#
# RUN ON THE HOST, as the desktop user (not root), once both members have been removed from the
# team (Task 3.23's steps), by hand:
#   ./CLAUDE/Plan/00203-teams-hot-control-and-robustness/lxc-throwaways-down.bash
# The one prompt is sudo's, before the log opens (R3); every root step after it uses `sudo -n`.
#
# WHAT IT CHANGES: t00203-coord and t00203-worker, stopped and destroyed (`lxc-destroy -f`), with
# everything in them (the agent's gh login among it). A name that does not start with t00203- is
# refused, so no other container is ever touched. One that is not there is not an error, so a
# second run finishes what a failed one left.
#
# EXIT CODES: 0 REMOVED (and checked); 1 a step or the check failed and names itself; 64 usage.
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

PLAN_USAGE="usage: lxc-throwaways-down.bash [-h|--help]

Plan 00203 Task 3.23: destroys the throwaway LXC containers lxc-throwaways-up.bash left running
(t00203-coord, t00203-worker), then checks that neither remains. Only names starting t00203- are
ever touched.
Exit 0 removed and checked, 1 a step or the check failed, 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
    exit 64
done
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the containers are real\n' >&2
    exit 64
fi

plan_require_host "it destroys LXC containers in root's LXC on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_lxc-throwaways.inc.bash
source "${PLAN_SCRIPT_DIR}/_lxc-throwaways.inc.bash"
readonly REPORT="${PLAN_RUN_DIR}/lxc-throwaways-report.md"

printf '# Plan 00203 Task 3.23 throwaway LXC containers: teardown\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"
plan_deploy_leg "LXC's tools" down_prerequisites
plan_deploy_leg "destroy ${LT_NAMES[*]}" down_all
plan_deploy_leg "neither container remains" lt_none_left
printf '\nREMOVED: %s are destroyed.\n' "${LT_NAMES[*]}" | tee -a "${REPORT}"
plan_finish
