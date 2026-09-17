#!/usr/bin/env bash
# Plan 00111 — deploy.bash
#
# PURPOSE: run the play that owns the CCY launcher so the tmux insulation library and the
# bumped launcher land in /var/local/claude-yolo. HOST ONLY (CLAUDE/PlanScriptStandards.md
# R2) — Ansible never runs in the CCY container.
#
# EFFECT ON THE HOST: play-claude-yolo.yml reconciles the whole CCY install — launcher,
# lib/, image build context, skills, bashrc includes — not only the one new library. It can
# trigger a container image rebuild if the image is behind. The host cc wrapper is
# redeployed alongside it by play-claude-code.yml. tmux itself is already installed by play-tmux-sessions.yml (Plan 00105) and is not
# touched here.
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
# shellcheck source=../../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check]

Runs playbooks/imports/play-claude-yolo.yml on the HOST. That play deploys the
CCY launcher and its lib/ directory, which now includes tmux-session.bash, and
reconciles the rest of the CCY install with it. --check previews without
changing anything.

Run acceptance.bash afterwards to prove the insulation works."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's CCY install"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml
# The host cc wrapper sources the library the play above installs, and is deployed by the
# play that owns Claude Code on the host.
plan_deploy_leg "play-claude-code.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-code.yml
# The window title that says "you are in tmux" is in the system-wide tmux.conf that Plan
# 00105's play owns; the ccy server reads that file, so the play is part of this deploy.
plan_deploy_leg "play-tmux-sessions.yml" \
    plan_ansible_playbook playbooks/imports/play-tmux-sessions.yml

plan_finish
