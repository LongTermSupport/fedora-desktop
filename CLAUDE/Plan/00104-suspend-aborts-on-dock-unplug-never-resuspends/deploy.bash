#!/usr/bin/env bash
# deploy.bash — deploy plan 00104's suspend and lid policy.
#
# WHERE TO RUN: on the HOST. Enforced by plan_require_host (R2) — the CCY container must
# never run Ansible (CLAUDE/ContainerRules.md).
#
# WHAT IT CHANGES:
#   - /etc/systemd/logind.conf.d/laptop-lid.conf        (lid behaviour)
#   - /etc/UPower/UPower.conf                           (IgnoreLid=true)
#   - /etc/udev/rules.d/99-suspend-wakeup-policy.rules  (disarm power-delivery wakeups)
#   - /usr/lib/systemd/system-sleep/resuspend-aborted-suspend  (re-issue aborted suspends)
#   - gsettings sleep-inactive-battery-type=suspend     (backstop)
#
# REBOOT: only if the logind lid drop-in actually CHANGES. logind does not reload its config
# without a restart, and restarting it kills the session — so the play notifies
# `warn-reboot-required` and prints instructions, but only when that file changed. If the
# drop-in already matches (the common case on a machine where it was applied before), no
# reboot is needed at all.
#
# The udev wakeup policy does NOT need a reboot: the play runs `udevadm trigger --settle` and
# then verifies the result with `helpers.suspend_wakeup.cli`, which enumerates the
# power_supply devices the rule targets and prints a `COVERAGE: n of m` line. It does not
# assume a device count — a host with none passes and says so.
#
# Usage: ./CLAUDE/Plan/00104-.../deploy.bash [--check] [-y|--yes] [-h|--help]
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

PLAN_USAGE="usage: deploy.bash [--check] [-y|--yes] [-h|--help]

Deploys playbooks/imports/play-suspend-and-lid-policy.yml.
Changes suspend, lid and wakeup behaviour on THIS machine.

A reboot is required ONLY if the logind lid drop-in changes — the play says so when
it does. The udev wakeup policy applies during the run and prints a COVERAGE line."

plan_mode deploy
plan_parse_common_flags "$@"

plan_require_host "it deploys systemd, udev and logind configuration to the live machine"
plan_prime_sudo
plan_start_log auto

plan_gate_change "suspend/lid/wakeup policy on THIS machine: logind lid config, UPower
IgnoreLid, a udev rule disarming AC and USB-C power-delivery wakeup sources, a system-sleep
hook that re-issues an aborted suspend, and GNOME battery idle-suspend"

plan_deploy_leg "suspend and lid policy" \
    plan_ansible_playbook playbooks/imports/play-suspend-and-lid-policy.yml

plan_finish
