#!/usr/bin/env bash
# Plan 00109 — deploy.bash
#
# PURPOSE: put this plan's four plays onto the HOST, so every task it holds open on
# "run the play here" can be answered. HOST ONLY (CLAUDE/PlanScriptStandards.md R2) —
# Ansible never runs in the CCY container, and a container answer about systemd units,
# a GNOME session or /dev/kvm is a confident wrong answer rather than a missing one.
#
# Run `./acceptance.bash` afterwards. This script establishes that the plays RAN; that
# is a different claim from the units being wanted, the panel having loaded, or the
# drift axes having compared anything, and those are what the plan is actually waiting
# on. Nothing here renders a verdict (R9).
#
# THE LEGS, IN ORDER, AND WHY THAT ORDER:
#
#   1. play-host-health-login-report.yml — the producer. Installs python3-pyyaml for
#      the SYSTEM interpreter, deploys and ENABLES the login-time health unit against
#      graphical-session.target (desktop profile), or the collection timer plus the
#      ~/.bashrc-includes snippet (server profile), and removes the other profile's
#      artefacts. Nothing else can be judged until a status document exists.
#   2. play-fedora-desktop-panel.yml — the consumer. Copies the extension into the
#      user's extensions tree and MERGES its uuid into org.gnome.shell
#      enabled-extensions. Second, because the panel reads what leg 1 produces.
#   3. play-vm-test-lab.yml — carries the `server-host-health-kernel-change` scenario
#      into the deployed allowlist and the two new guest scripts into the lab tree.
#      The bridge refuses a scenario id that exists only in the tracked manifest, so
#      until this runs the VM leg of Task 3.2 cannot be requested at all.
#   4. play-displaylink.yml — LAST, and deliberately. It is the only leg that installs
#      a vendor RPM, runs a DKMS autoinstall and can require a MOK enrolment and a
#      reboot, so it is also the only one with a real chance of aborting the run. Every
#      leg above it has already applied by the time it starts. It is here because
#      Task 5.4's background-recovery action, its dock udev rule and its suspend
#      service are deployed by that play and by no other, and so is Task 5.4a's
#      dock-recovery-on-unlock extension with its polkit rule.
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

Runs Plan 00109's four plays on the HOST, fail-fast, in this order:

  playbooks/imports/optional/common/play-host-health-login-report.yml
  playbooks/imports/optional/common/play-fedora-desktop-panel.yml
  playbooks/imports/optional/common/play-vm-test-lab.yml
  playbooks/imports/optional/hardware-specific/play-displaylink.yml

-y/--yes is accepted and skips any plan_confirm prompt. This script asks nothing, so
it is a no-op here.

--check is REFUSED — see the refusal message for the reason.

FAIL-FAST, AND THAT HAS A CONSEQUENCE WORTH KNOWING: the run stops at the first
failing play, so the ones after it never run. play-vm-test-lab.yml is third and
needs /dev/kvm; on a host without it, play-displaylink.yml is NOT deployed — and
acceptance.bash will then fail its DisplayLink check for a reason that has nothing
to do with DisplayLink. Deploy that play on its own if you hit this:
  ansible-playbook playbooks/imports/optional/hardware-specific/play-displaylink.yml

AFTERWARDS, in this order:
  1. log out and log back in. On Wayland that is the only way GNOME Shell loads
     the panel's new code, and it is also what fires the login-time health unit.
  2. ./acceptance.bash — the verdict. deploy.bash establishes only that the
     plays ran."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

# Refused rather than passed through, because two of these plays CANNOT run under it and
# the failure they produce names nothing useful. `ansible.builtin.command` does not
# support check mode, so Ansible skips those tasks — and both plays then evaluate a later
# task against the skipped task's registered result, which has no `stdout` key. The
# operator gets an undefined-attribute error several minutes in, about a variable they did
# not name, on a play that is fine. A dry run that only half-runs is worse than no dry run,
# so this says so at the point the flag is typed.
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check is not supported by this deploy.\n' >&2
    printf '        play-fedora-desktop-panel.yml and play-vm-test-lab.yml each read a\n' >&2
    printf '        command task'"'"'s registered stdout in a LATER task. Check mode skips\n' >&2
    printf '        command tasks, so that stdout does not exist and the later task fails\n' >&2
    printf '        with an undefined-attribute error that is about check mode, not about\n' >&2
    printf '        the host. Run without --check, or run a single play by hand.\n' >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's systemd user manager, its GNOME session, its DKMS state and /dev/kvm"
plan_prime_sudo
plan_start_log auto

# BEFORE the plays, not after: the ledger records each play as it runs, and while a
# BROKEN sentinel is live it records nothing. Clearing afterwards would leave this very
# deploy's four plays unrecorded — the hole would be closed and immediately re-opened for
# the run that closed it.
#
# The sentinel this clears is dated 2026-09-15 and names the ansible-core 2.19 `ansible_pos`
# removal, which `plugin_support.py` has since been fixed for. By design the sentinel never
# clears itself, so it needs saying once that the CAUSE is gone; the hole was real and the
# plays that ran during it are not recovered by this.
# `env --chdir` rather than a bare `cd`: `python3 -m helpers…` resolves the package from
# the cwd, and plan_deploy_leg refuses to be called inside a subshell, so the cd has to
# belong to the command rather than to the script around it.
plan_deploy_leg "clear the ledger's BROKEN sentinel" \
    env --chdir="${PLAN_REPO_ROOT}" python3 -m helpers.play_ledger.check_freshness --clear-broken

plan_deploy_leg "play-host-health-login-report.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-host-health-login-report.yml

plan_deploy_leg "play-fedora-desktop-panel.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-fedora-desktop-panel.yml

plan_deploy_leg "play-vm-test-lab.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-vm-test-lab.yml

plan_deploy_leg "play-displaylink.yml" \
    plan_ansible_playbook playbooks/imports/optional/hardware-specific/play-displaylink.yml

printf '\n'
printf '==> NEXT, and in this order:\n'
printf '    1. log out and log back in — on Wayland nothing else loads the panel'"'"'s new\n'
printf '       code, and the login is also what fires the health report unit.\n'
printf '    2. ./acceptance.bash — the verdict. This run established only that the\n'
printf '       four plays ran.\n'
printf '\n'

plan_finish
