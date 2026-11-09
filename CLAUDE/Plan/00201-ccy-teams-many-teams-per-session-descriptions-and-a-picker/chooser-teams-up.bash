#!/usr/bin/env bash
# Plan 00201 — chooser-teams-up.bash: two throwaway teams, left up, for driving the INTERACTIVE
# seat chooser of `ccy --teams` by hand or from a host agent in tmux (ACCEPTANCE-owner.md, Morning
# start steps 5 and 6: "Team <team>: which seat does this session take?", then for a seat that
# already exists its role, Keep as is or Make coordinator). acceptance.bash makes the same teams
# for its headless legs; this script makes them the same way and stops before any launch.
# chooser-teams-down.bash removes them.
#
# RUN ON THE HOST, as the desktop user (not root), after play-agent-bus.yml and
# play-claude-yolo.yml: through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/chooser-teams-up.bash [--bus-address=<ip>]
# The one prompt is sudo's, before the log opens (R3); every root step after it uses `sudo -n`.
#
# WHAT IT CHANGES, and leaves in place for chooser-teams-down.bash: two throwaway teams,
# chooser201a and chooser201b, made by agent-bus-install from this checkout (homeservers, units,
# firewalld rules, state), declared only in a host_vars file in the run directory, which
# ccy-teams and the launches read through CCY_TEAMS_HOST_VARS; and a copy of this checkout's
# Quick Launch record (.claude/ccy/.last-launch.conf), which every launch rewrites and the
# teardown puts back. The run directory is named in chooser-teams.current beside the run logs,
# which is how the teardown finds it. The owner's host_vars and teams are never touched, and the
# run refuses to start if the owner declares a team of either name. A run that fails part-way
# removes what it made, as acceptance.bash does.
#
# EXIT CODES: 0 the teams are up (what to run next is printed); 1 a step failed and names itself
# (what it made is removed); 2 COULD NOT ESTABLISH (an owner step is missing; nothing was
# changed); 64 usage.
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

PLAN_USAGE="usage: chooser-teams-up.bash [--bus-address=<ip>] [-h|--help]

Plan 00201: two throwaway teams (chooser201a, chooser201b) left up for driving the interactive
seat chooser of ccy --teams (ACCEPTANCE-owner.md steps 5 and 6). Prints the command to run;
chooser-teams-down.bash removes the teams.
  --bus-address=<ip>   the teams' listen address; without it, agent_bus_address from the
                       host_vars localhost.yml
Exit 0 up, 1 a step failed (what it made is removed), 2 COULD NOT ESTABLISH, 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"

BUS_ADDRESS=""
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --bus-address=?*) BUS_ADDRESS="${arg#--bus-address=}" ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the teams are real\n' >&2
    exit 64
fi

plan_require_host "it creates homeserver teams and firewalld rules on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

# The names the M7 legs read (_acceptance-m7.inc.bash). SEAT and SEAT_LIST are the seat the
# first launch takes; no launch is made here.
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
readonly CURRENT="${PLAN_RUN_DIR%/*/*}/chooser-teams.current"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-m7.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-m7.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_chooser-teams.inc.bash
source "${PLAN_SCRIPT_DIR}/_chooser-teams.inc.bash"
# A run that stops part-way removes what it made; one that finishes leaves the teams up (the
# last leg disarms it).
plan_on_cleanup m7_teardown_after_stop

printf '# Plan 00201 seat-chooser teams\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

printf '\n==> [prerequisites] ccy, agent-bus, ccy-teams, the hooks daemon and a saved Quick Launch are here; the owner declares neither team\n'
prereq_status=0
m7_prerequisites || prereq_status=$?
case "${prereq_status}" in
    0) ;;
    2)
        printf 'COULD NOT ESTABLISH: an owner step is missing (above); nothing was changed\n' | tee -a "${REPORT}" >&2
        exit 2
        ;;
    *) exit 1 ;;
esac
plan_deploy_leg "the seat-chooser teams are not up already" up_not_already
plan_deploy_leg "remove what an interrupted run left: containers, seats and teams of these names" m7_leftovers
plan_deploy_leg "the bus address" m7_resolve_bus_address
plan_deploy_leg "write the team files and the run's own host_vars" m7_write_teams
plan_deploy_leg "agent-bus-install team ${TEAM_A} and ${TEAM_B}" m7_install_teams
plan_deploy_leg "record each team's firewalld rules, present now" m7_record_rules
plan_deploy_leg "save the Quick Launch record, which every launch rewrites" m7_save_launch_conf
plan_deploy_leg "name this run directory for the teardown" up_record
plan_deploy_leg "leave the teams up" m7_left_up

printf '\nUP: %s and %s are running, declared only in %s.\n' "${TEAM_A}" "${TEAM_B}" "${HOST_VARS_FILE}" | tee -a "${REPORT}"
cat <<EOF | tee -a "${REPORT}"

Drive the seat chooser in this checkout (in tmux, or any terminal):

  cd ${PLAN_REPO_ROOT} && CCY_TEAMS_HOST_VARS=${HOST_VARS_FILE} ${CCY_LAUNCHER} --teams --no-restore

(--no-restore: a session left running across a reboot is not brought back into teams the
teardown has purged.)

1. Both teams cover this checkout, so a list is shown: Tab marks ${TEAM_A} and ${TEAM_B}, Enter
   takes them. For each, "Team <team>: which seat does this session take?": take the default,
   a new seat, which joins as a worker without a role question. End the session.
2. Run the same command again. Each team's seat now exists, so after the seat question its
   role is asked: the first line, Keep as is, or Make coordinator (sudo asks).

When done: ./CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/chooser-teams-down.bash
EOF
plan_finish
