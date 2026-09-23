#!/usr/bin/env bash
# Plan 00134 — deploy.bash
#
# PURPOSE: put the 2026-09-23 fixes onto the HOST in one run. HOST ONLY
# (CLAUDE/PlanScriptStandards.md R2): Ansible never runs in the CCY container.
#
# THE LEGS, IN ORDER, AND WHY THAT ORDER:
#
#   0. clear the ledger's BROKEN sentinel. BEFORE the plays: while it is live the ledger
#      records nothing, so clearing afterwards would leave this deploy's own plays
#      unrecorded. The sentinel on the host was written by an ad-hoc `ansible -m` run,
#      which the ledger now skips (Task 1.1, Task 1.6), so the cause is gone.
#   1. play-claude-yolo.yml — now deploys BOTH launchers (host `cc` and container `ccy`),
#      the lib they share and `ccy-sessions`. This is what makes `cc` start again, and
#      it retires the removed play-claude-code.yml finding (Task 1.5). It also rebuilds
#      the ccy image, which takes a while.
#   2. play-basic-configs.yml — deploys `shutdown-with-update` / `reboot-with-update`,
#      whose session warnings now follow the restore opt-in (Plan 00135 Task 3.7).
#   3. play-host-health-login-report.yml — the producer: the readable ledger finding,
#      the counts-only notification, and `fedora-desktop-health --run-play`
#      (Plan 00109 Task 4.3).
#   4. play-fedora-desktop-panel.yml — the consumer, LAST because it reads what leg 3
#      produces: wrapped findings, "Copy these findings", and the plays section.
#
# PHASE 2 LEGS, independent of each other and of the legs above:
#
#   5. play-hd-audio.yml — the WirePlumber 0.5 SPA-JSON port (Task 2.1). It stops if Lua
#      configuration it did not write is left in main.lua.d/ or bluetooth.lua.d/.
#   6. play-browsers.yml — one [vivaldi] repo file, and the scriptlet stopped from adding
#      the other back (Task 2.2).
#
# Usage: ./deploy.bash [-h|--help] [-y|--yes]
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

PLAN_USAGE="usage: deploy.bash [-h|--help] [-y|--yes]

Deploys the 2026-09-23 fixes on the HOST, fail-fast, in this order:

  clear the play ledger's BROKEN sentinel
  playbooks/imports/play-claude-yolo.yml            (cc + ccy; cc starts again)
  playbooks/imports/play-basic-configs.yml          (shutdown-/reboot-with-update)
  playbooks/imports/optional/common/play-host-health-login-report.yml
  playbooks/imports/optional/common/play-fedora-desktop-panel.yml
  playbooks/imports/optional/common/play-hd-audio.yml          (WirePlumber 0.5 port)
  playbooks/imports/play-browsers.yml                          (one [vivaldi] repo)

-y/--yes is accepted; this script asks nothing, so it is a no-op here.
--check is REFUSED: play-fedora-desktop-panel.yml reads a command task's registered
stdout in a later task, which check mode skips.

AFTERWARDS: log out and log back in. On Wayland that is the only way GNOME Shell loads
the panel's new code, and it is also what fires the login-time health report."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check is not supported by this deploy: play-fedora-desktop-panel.yml\n' >&2
    printf '        reads a command task'"'"'s registered stdout in a later task, and check\n' >&2
    printf '        mode skips command tasks. Run without --check, or one play by hand.\n' >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's launchers, its systemd user manager and its GNOME session"
plan_prime_sudo
plan_start_log auto

# `env --chdir` rather than `cd`: `python3 -m helpers…` resolves the package from the cwd,
# and plan_deploy_leg refuses to be called inside a subshell.
plan_deploy_leg "clear the ledger's BROKEN sentinel" \
    env --chdir="${PLAN_REPO_ROOT}" python3 -m helpers.play_ledger.check_freshness --clear-broken

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

plan_deploy_leg "play-basic-configs.yml" \
    plan_ansible_playbook playbooks/imports/play-basic-configs.yml

plan_deploy_leg "play-host-health-login-report.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-host-health-login-report.yml

plan_deploy_leg "play-fedora-desktop-panel.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-fedora-desktop-panel.yml

plan_deploy_leg "play-hd-audio.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-hd-audio.yml

plan_deploy_leg "play-browsers.yml" \
    plan_ansible_playbook playbooks/imports/play-browsers.yml

printf '\n'
printf '==> NEXT: log out and log back in, then check:\n'
printf '    - cc starts in a project directory\n'
printf '    - the panel icon is not the blue question mark, findings wrap, and\n'
printf '      "Copy these findings" copies them\n'
printf '    - the "Plays run on this machine" section lists plays after that login\n'
printf '\n'

plan_finish
