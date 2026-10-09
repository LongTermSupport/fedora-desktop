#!/usr/bin/env bash
# deploy.bash — deploy plan 00166's lid fix, then run acceptance.bash.
#
# WHERE TO RUN: on the HOST, docked with the lid CLOSED, so acceptance can check the panel.
# Enforced: plan_require_host (R2).
#
# WHAT IT CHANGES (via playbooks/imports/play-suspend-and-lid-policy.yml):
#   - /etc/UPower/UPower.conf   IgnoreLid=true -> IgnoreLid=false, and restarts upower
#   The rest of the play is already applied on this host and should report no change.
#
# No reboot or logout: GNOME picks up UPower's lid state as soon as upower restarts.
#
# Usage: ./CLAUDE/Plan/00166-laptop-panel-stays-on-with-lid-closed-when-docked/deploy.bash [--check] [-h]
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

PLAN_USAGE="usage: deploy.bash [--check] [-h|--help]

Deploys playbooks/imports/play-suspend-and-lid-policy.yml (UPower IgnoreLid=false),
then runs acceptance.bash. Run it docked with the lid closed."

plan_mode deploy
plan_parse_common_flags "$@"

plan_require_host "it deploys UPower configuration to the live machine"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "suspend and lid policy" \
    plan_ansible_playbook playbooks/imports/play-suspend-and-lid-policy.yml

if [[ "${PLAN_CHECK:-0}" == "1" ]]; then
    printf '==> --check: nothing was deployed, so acceptance is not run\n'
else
    plan_deploy_leg "acceptance" bash "${PLAN_SCRIPT_DIR}/acceptance.bash"
fi

plan_finish
