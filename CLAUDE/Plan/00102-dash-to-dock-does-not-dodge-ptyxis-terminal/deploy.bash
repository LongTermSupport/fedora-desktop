#!/usr/bin/env bash
# Plan 00102 — deploy.bash
#
# PURPOSE: run the play that owns Dash to Dock so the intellihide-mode key lands on the
# host. HOST ONLY (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in the CCY
# container.
#
# EFFECT ON THE HOST: play-gnome-shell-extensions.yml installs/updates every listed GNOME
# Shell extension and may restart the Shell if an extension download happens, besides
# writing the one dconf key this plan is about. Gated before anything mutates (R8).
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

Runs playbooks/imports/play-gnome-shell-extensions.yml on the HOST. That play
writes the Dash to Dock intellihide-mode key (ALL_WINDOWS) and also reconciles
every GNOME Shell extension it owns, so it can download updates and restart
the Shell. --check previews without changing anything.

Run acceptance.bash afterwards to confirm the key landed."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's GNOME session"
plan_prime_sudo
plan_start_log auto

plan_gate_change "Dash to Dock intellihide-mode -> ALL_WINDOWS; every extension in play-gnome-shell-extensions.yml reconciled (may restart GNOME Shell)"

plan_deploy_leg "play-gnome-shell-extensions.yml" \
    plan_ansible_playbook playbooks/imports/play-gnome-shell-extensions.yml

plan_finish
