#!/usr/bin/env bash
# Plan 00103 — deploy.bash
#
# PURPOSE: run the play that owns Slack so the home:ro Flatpak override lands on the host.
# HOST ONLY (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in the CCY container.
#
# EFFECT ON THE HOST: play-comms.yml enables Flathub, installs or updates the Slack
# Flatpak, and applies the override this plan is about. Gated before anything mutates (R8).
# Slack must be restarted afterwards for a running instance to pick up the new sandbox.
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

Runs playbooks/imports/play-comms.yml on the HOST. That play installs the Slack
Flatpak and grants its sandbox read-only access to the home directory so a
file dragged from Nautilus can be read. --check previews without changing
anything.

Quit and relaunch Slack afterwards, then run acceptance.bash."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's Flatpak installation"
plan_prime_sudo
plan_start_log auto

plan_gate_change "Slack Flatpak override filesystems=home:ro; Flathub remote and Slack install reconciled"

plan_deploy_leg "play-comms.yml" \
    plan_ansible_playbook playbooks/imports/play-comms.yml

printf '\nQuit Slack fully (File > Quit, not just close) and relaunch it so the new sandbox applies.\n\n'

plan_finish
