#!/usr/bin/env bash
# deploy.bash — deploy plan 00166's lid fix. acceptance.bash is run separately (see the end).
#
# WHERE TO RUN: on the HOST, docked with the lid CLOSED, so acceptance can check the panel.
# Enforced: plan_require_host (R2).
#
# WHAT IT CHANGES (via playbooks/imports/play-suspend-and-lid-policy.yml):
#   - /etc/UPower/UPower.conf   IgnoreLid=true -> IgnoreLid=false, and restarts upower
#   The rest of the play is already applied on this host and should report no change.
#
# No reboot or logout: GNOME picked up UPower's lid state as soon as upower restarted (Plan
# 00166's first deploy, accepted 4 of 4 straight after).
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
# shellcheck source=../../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: deploy.bash [--check] [-h|--help]

Deploys playbooks/imports/play-suspend-and-lid-policy.yml (UPower IgnoreLid=false).
Run acceptance.bash afterwards, docked with the lid closed."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it deploys UPower configuration to the live machine"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "suspend and lid policy" \
    plan_ansible_playbook playbooks/imports/play-suspend-and-lid-policy.yml

# STANDARD-EXCEPTION(R9): no acceptance.bash leg here, where R9 puts it. meta-deploy.bash
# runs acceptance.bash after this script and the second triage, so a leg here would run it
# twice, and an undocked run's COULD NOT ESTABLISH (exit 2) would report the deploy FAILED.
printf '==> now run acceptance.bash, docked with the lid closed\n'
plan_finish
