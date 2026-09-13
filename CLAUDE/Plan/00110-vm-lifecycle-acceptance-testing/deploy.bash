#!/usr/bin/env bash
# Plan 00110 — deploy.bash
#
# PURPOSE: run the play that owns the VM acceptance lab so the rootless libvirt/QEMU stack,
# the lab tree and the rendered scenario manifest land on the HOST. HOST ONLY
# (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in the CCY container.
#
# EFFECT ON THE HOST: play-vm-test-lab.yml installs packages (virt-install, guestfs-tools,
# cloud-utils, xorriso, lorax, and the libvirt/QEMU stack where absent), enables systemd
# --user lingering for the user, creates ~/.local/share/vmtest/, renders scenarios.json and
# the scenario allowlist. It does not build a base or boot anything. Gated before anything
# mutates (R8).
#
# Usage: ./deploy.bash [-h|--help] [--check]
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

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check]

Runs playbooks/imports/optional/common/play-vm-test-lab.yml on the HOST: the
rootless libvirt/QEMU stack, systemd --user lingering, the lab tree under
~/.local/share/vmtest, and the rendered scenario manifest and allowlist.
--check previews without changing anything.

Run triage.bash first if you want the host facts on record."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's hypervisor stack and user session"
plan_prime_sudo
plan_start_log auto

plan_gate_change "virt packages installed, systemd --user linger enabled, ~/.local/share/vmtest created, scenario manifest and allowlist rendered"

plan_deploy_leg "play-vm-test-lab.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-vm-test-lab.yml

plan_finish
