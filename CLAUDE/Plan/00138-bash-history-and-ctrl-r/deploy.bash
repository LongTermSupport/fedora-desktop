#!/usr/bin/env bash
# Plan 00138 — deploy.bash
#
# PURPOSE: run the play that owns the Bash Tweaks, so durable history and the ranked Ctrl+R
# search land on the host. HOST ONLY (CLAUDE/PlanScriptStandards.md R2) — Ansible never
# runs in the CCY container.
#
# EFFECT ON THE HOST: play-basic-configs.yml, which also re-applies everything else it owns
# (basic packages, sudoers block, vim colours, ssh helper scripts, yq, dnf, grub, fwupd,
# ABRT policy). What this plan adds:
#   - installs fzf and gawk
#   - /etc/profile.d/zz_lts-fedora-desktop.bash: history written at every prompt,
#     timestamped, unlimited, in ~/.local/state/bash/history
#   - /var/local/ps1-prompt: appends its prompt hook instead of overwriting another
#   - removes the Bash Tweaks block from user and root ~/.bash_profile (after asserting
#     each sources ~/.bashrc), so login shells load the tweaks once
#   - creates ~/.local/state/bash (0700) for the user and root, and seeds history (0600)
#     there once from ~/.bash_history
#   - deploys ~/.bashrc-includes/history-search.bash and ~/.local/bin/bash-history-rank
#     for the desktop user only
#
# Terminals already open keep their old settings until they are closed: whatever they hold
# is written to ~/.bash_history when they exit, not to the new file.
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

Runs playbooks/imports/play-basic-configs.yml on the HOST: durable, timestamped,
unlimited bash history in ~/.local/state/bash, and Ctrl+R ranked for the current
directory and git repository. The play also re-applies everything else it owns.
--check previews without changing anything.

Then open a NEW terminal and run acceptance.bash."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's shell configuration"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-basic-configs.yml" \
    plan_ansible_playbook playbooks/imports/play-basic-configs.yml

printf '\nOpen a NEW terminal before running acceptance.bash. Terminals opened before this\n'
printf 'deploy keep the old settings until closed, and write to ~/.bash_history on exit.\n\n'

plan_finish
