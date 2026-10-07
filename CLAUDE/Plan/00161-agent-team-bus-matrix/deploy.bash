#!/usr/bin/env bash
# Plan 00161 — deploy.bash (units U16 and U22, host run): agent-bus-install for real on this
# desktop, then play-agent-bus.yml with a throwaway team, then triage.bash's H1 and H2
# against the bus address (DESIGN.md sections 12, 5.3 and 13).
#
# RUN ON THE HOST, as the desktop user (not root): through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00161-agent-team-bus-matrix/deploy.bash [--bus-address=<ip>]
# The bus address is install-specific, so it is not in this repository: --bus-address=, or
# else agent_bus_address in the untracked host_vars localhost.yml (see localhost.yml.dist).
# The one prompt is sudo's, before the log opens (R3); every root step after it uses
# `sudo -n`, so a lapsed timestamp fails that step by name instead of prompting into the log.
# It does not end by running acceptance.bash: see the STANDARD-EXCEPTION(R9) marker after
# the last leg.
#
# IN ORDER, stopping at the first failure (plan_mode deploy):
#   1. refuse the bus address, changing nothing, if an interface other than agentbus0 holds
#      it or a route other than a default route covers it (deploy_check.py);
#   2. remove a throwaway team an interrupted earlier run left (a no-op otherwise);
#   3. `agent-bus-install software --source <this checkout> --bus-address <ip>`, run from
#      this checkout, since the installed copy does not exist before the first run;
#   4. `team` for the throwaway team zz-deploy-check, from a team file of example values in
#      the run directory: the bus address, a free port, allow_from 192.0.2.0/24, human owner;
#   5. `software` and `team` again: any CHANGED line fails the run;
#   6. `check --team`, printed: any FAIL fact fails the run;
#   7. `remove --team --purge`; after a failure once the team is in, the same removal runs on
#      the way out (plan_on_cleanup);
#   8. U22: play-agent-bus.yml with extra vars declaring only zz-deploy-check (the same team
#      file) present: the recap must count a change; again: it must count none; then the
#      team absent with purge: it must count a change. The same plan_on_cleanup applies;
#   9. `docker pull` of the busybox image triage H2 runs in (_h2-docker-image.inc.bash, pinned
#      by digest; it stays), when docker is installed, since the triage itself pulls nothing;
#  10. `triage.bash --reach-only --bus-address=<ip>`, whose report stays in its run directory;
#  11. U19: play-claude-yolo.yml, which installs the ccy launcher and rebuilds the image with
#      the pingbus zipapp and the agent-bus kit (CCY 3.85.0, container 2.45).
#
# WHAT STAYS, deliberately (every desktop carries the homeserver software): the packages of
# DESIGN.md section 3.2, the agent-bus user, /var/lib/agent-bus{,-install}, the pinned
# Tuwunel, the zipapps, /usr/local/bin/agent-bus and pingbus, /usr/local/sbin/agent-bus-install,
# the member kit, the three template units (no instance enabled), and the NetworkManager dummy
# connection agentbus0 holding <ip>, across reboots; and the busybox image in docker. Nothing of zz-deploy-check stays. The
# log, the team file, the play vars and each installer and play run's stdout stay in the
# untracked run directory.
#
# EXIT CODES: 0 every step succeeded; 1 a step failed (it names itself); 64 usage (an unknown
# argument, --check, no bus address, or one that is not a concrete, canonical IP literal).
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

# Cannot collide with a real team: teams are named for their purpose, never zz-anything.
readonly TEAM="zz-deploy-check"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_bus-address.inc.bash
source "${PLAN_SCRIPT_DIR}/_bus-address.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_h2-docker-image.inc.bash
source "${PLAN_SCRIPT_DIR}/_h2-docker-image.inc.bash"

PLAN_USAGE="usage: deploy.bash [--bus-address=<ip>] [-h|--help]

Units U16 and U22's host run for Plan 00161 (agent team bus): installs the agent-bus
homeserver software and the agentbus0 bus address (both stay), installs, re-runs, checks and
removes a throwaway team, does the same through play-agent-bus.yml, then runs triage.bash's
H1 and H2 against the bus address, and rebuilds the ccy image (play-claude-yolo.yml).
  --bus-address=<ip>   the address for agentbus0; without it, ${BUS_ADDRESS_VAR} from the
                       host_vars localhost.yml
Prompts for sudo once, before the run log opens."

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
    printf '[FATAL] --check has nothing to rehearse here: agent-bus-install has no dry run, so the play skips every step under --check too\n' >&2
    exit 64
fi

plan_require_host "it installs the homeserver software, a NetworkManager interface and systemd units on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for the installer, and its triage legs use rootless podman\n' >&2
    exit 1
fi

resolve_bus_address || exit $?
checker=(python3 -I "${PLAN_SCRIPT_DIR}/deploy_check.py")

plan_prime_sudo
plan_start_log auto

INSTALLER="${PLAN_REPO_ROOT}/files/usr/local/sbin/agent-bus-install"
TEAM_FILE="${PLAN_RUN_DIR}/${TEAM}.team.json"
PLAY_VARS_PRESENT="${PLAN_RUN_DIR}/play-vars-present.json"
PLAY_VARS_ABSENT="${PLAN_RUN_DIR}/play-vars-absent.json"
TEAM_PRESENT=0
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_deploy-steps.inc.bash
source "${PLAN_SCRIPT_DIR}/_deploy-steps.inc.bash"
plan_on_cleanup remove_team_after_failure

plan_deploy_leg "bus address ${BUS_ADDRESS} is free for agentbus0" "${checker[@]}" free "${BUS_ADDRESS}"
plan_deploy_leg "remove a ${TEAM} left by an interrupted run" remove_team remove-leftover
plan_deploy_leg "agent-bus-install software (stays installed)" install_software software
plan_deploy_leg "write the ${TEAM} team file" write_team_file
plan_deploy_leg "agent-bus-install team ${TEAM}" install_team team
plan_deploy_leg "software and team again: nothing may change" second_run
plan_deploy_leg "agent-bus-install check --team ${TEAM}" check_team
plan_deploy_leg "agent-bus-install remove --team ${TEAM} --purge" remove_team remove
plan_deploy_leg "write the play vars for ${TEAM} present and absent" write_play_vars
plan_deploy_leg "play-agent-bus.yml with ${TEAM} present: a change" play_team_present
plan_deploy_leg "play-agent-bus.yml again: no change" play_team_present_again
plan_deploy_leg "play-agent-bus.yml with ${TEAM} absent, purged: a change" play_team_absent
plan_deploy_leg "pull triage H2's docker image (busybox, pinned)" pull_h2_docker_image
plan_deploy_leg "triage H1 and H2 against ${BUS_ADDRESS}" \
    "${PLAN_SCRIPT_DIR}/triage.bash" --reach-only "--bus-address=${BUS_ADDRESS}"
plan_deploy_leg "play-claude-yolo.yml (U19: the pingbus kit in the ccy image)" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml
# STANDARD-EXCEPTION(R9): no acceptance.bash leg here, where R9 puts it. meta-deploy.bash
# runs acceptance.bash after this script and the second triage, so a leg here would run it twice.
printf '==> the homeserver software and agentbus0 (%s) stay installed; %s is gone\n' "${BUS_ADDRESS}" "${TEAM}"
plan_finish
