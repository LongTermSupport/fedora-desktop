#!/usr/bin/env bash
# Plan 00201 — chooser-teams-down.bash: removes the two throwaway teams chooser-teams-up.bash left
# up for driving the interactive seat chooser, and checks that nothing of them remains.
#
# RUN ON THE HOST, as the desktop user (not root), once the seat chooser has been driven:
# through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/chooser-teams-down.bash
# The one prompt is sudo's, before the log opens (R3); every root step after it uses `sudo -n`.
#
# WHAT IT CHANGES: every ccy container holding a seat of chooser201a or chooser201b, removed; the
# seats parked (`agent-bus seat remove`) and their directories in this checkout deleted; this
# checkout's Quick Launch record put back as chooser-teams-up.bash saved it; both teams purged by
# agent-bus-install (homeservers, units, firewalld rules, state); then chooser-teams.current,
# which names the up run, removed. The records of the up run (team files, recorded firewalld
# rules, host_vars, saved Quick Launch record) are read from its directory.
#
# HOWEVER IT ENDS: a run that stops part-way tries the rest of the teardown, then checks that
# each recorded firewalld rule (permanent and runtime) is gone, no unit is active and no state is
# left, and keeps chooser-teams.current so a second run finishes the job. The owner's host_vars
# and teams are never touched.
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

PLAN_USAGE="usage: chooser-teams-down.bash [-h|--help]

Plan 00201: removes the seat-chooser teams chooser-teams-up.bash left up (chooser201a,
chooser201b): their sessions, seats and the teams, the Quick Launch record put back, then checks
that no firewalld rule, unit or state of them remains, however the run ends.
Exit 0 removed and checked, 1 a step or the check failed, 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
    exit 64
done
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the teams are real\n' >&2
    exit 64
fi

plan_require_host "it purges homeserver teams and firewalld rules on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

# The names the M7 legs read (_acceptance-m7.inc.bash), as chooser-teams-up.bash set them. The
# run directory is this run's; down_adopt copies the up run's records into it.
readonly TEAM_A="chooser201a"
readonly TEAM_B="chooser201b"
readonly DESC_A="Plan 00201 acceptance (seat chooser) team A: a throwaway team, removed by chooser-teams-down.bash"
readonly DESC_B="Plan 00201 acceptance (seat chooser) team B"
readonly SEAT="1"
readonly SEAT_LIST="${SEAT}@${TEAM_A},${SEAT}@${TEAM_B}"
readonly HUMAN="tester"
readonly INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
readonly AGENT_BUS="/usr/local/bin/agent-bus"
readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly CHECK=(python3 -I "${PLAN_SCRIPT_DIR}/m7_check.py")
readonly REPORT="${PLAN_RUN_DIR}/chooser-teams-report.md"
readonly HOST_VARS_FILE="${PLAN_RUN_DIR}/host_vars.yml"
readonly OWNER_HOST_VARS="${PLAN_REPO_ROOT}/environment/localhost/host_vars/localhost.yml"
# The up script's runs sit beside this one's: <plan>/chooser-teams-up/<stamp>.
readonly CURRENT="${PLAN_RUN_DIR%/*/*}/chooser-teams.current"
BUS_ADDRESS=""
DOWN_FROM=""
DOWN_DONE=0
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-m7.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-m7.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_chooser-teams.inc.bash
source "${PLAN_SCRIPT_DIR}/_chooser-teams.inc.bash"
plan_on_cleanup down_after_stop

printf '# Plan 00201 seat-chooser teams: teardown\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

plan_deploy_leg "the teardown's tools; the owner declares neither team" down_prerequisites
plan_deploy_leg "adopt the records chooser-teams-up.bash left" down_adopt
plan_deploy_leg "end the sessions; park and delete the seats of ${TEAM_A} and ${TEAM_B}" down_sessions
plan_deploy_leg "put the Quick Launch record back" m7_restore_launch_conf
plan_deploy_leg "purge ${TEAM_A} and ${TEAM_B}" m7_purge_teams
plan_deploy_leg "nothing of the teams remains: firewalld rules, units, state, seats; the Quick Launch record as it was" m7_check_gone
plan_deploy_leg "forget the up run" down_forget
printf '\nREMOVED: %s and %s are purged, their firewalld rules gone, the Quick Launch record as it was (up run %s)\n' \
    "${TEAM_A}" "${TEAM_B}" "${DOWN_FROM}" | tee -a "${REPORT}"
plan_finish
