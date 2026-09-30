#!/usr/bin/env bash
# Plan 00144 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   1. play-container-watch.yml — the watchdog backend only; it no longer deploys or enables
#      an extension.
#   2. play-fedora-desktop-panel.yml — deploys the panel with its container section, removes
#      container-watch@fedora-desktop from enabled-extensions, then deletes its files.
# The order matters: the backend is current before the panel that reads its report lands.
#
# It ends by telling the operator to LOG OUT AND BACK IN. On Wayland that is the only way the
# new panel JavaScript loads; until then the old code keeps running.
#
# Usage: ./deploy.bash [--check] [-y|--yes] [-h|--help]
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
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: deploy.bash [--check] [-y|--yes] [-h|--help]

Runs play-container-watch.yml then play-fedora-desktop-panel.yml on the HOST, in that
order, then tells you to log out and back in (the only way the new panel code loads on
Wayland). Run acceptance.bash after logging back in."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible plays that write to the user's extensions directory and dconf"

# Before the run log, so a password prompt is not flooded by the tee (R3).
plan_prime_sudo

plan_start_log auto

plan_deploy_leg "play-container-watch.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-container-watch.yml
plan_deploy_leg "play-fedora-desktop-panel.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-fedora-desktop-panel.yml

printf '\n'
printf 'Deploy done. NOW LOG OUT AND LOG BACK IN.\n'
printf 'On Wayland that is the only way the new panel code loads; Alt+F2 r is X11-only.\n'
printf 'Next: %s/acceptance.bash, then the visual pass (Task 7.3).\n' "${PLAN_SCRIPT_DIR}"

plan_finish
