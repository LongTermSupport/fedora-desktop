#!/usr/bin/env bash
# Plan 00112 — deploy.bash
#
# PURPOSE: run the play this plan changed, so the declared enabled list lands on the HOST.
# HOST ONLY (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in the CCY container.
#
# EFFECT ON THE HOST: play-gnome-shell-extensions.yml installs the extension installer and
# its DNF packages, fetches the seven extensions.gnome.org extensions (skipping any already
# current), compiles their GSettings schemas, copies the custom extension, and then — the
# part this plan added — writes every deployed UUID into org.gnome.shell enabled-extensions,
# ADDITIVELY: nothing already in the list is removed. It then verifies every deployed UUID's
# live state and writes the Space Bar and Dash to Dock dconf keys.
#
# Run triage.bash BEFORE and AFTER this, and diff the two reports: that is how Task 2.1's
# "the host's own list is unchanged (nothing removed)" is evidenced rather than asserted.
# Run this twice to evidence idempotency — the second run must report no change for the
# 'Declare Deployed Extensions Enabled' task.
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

Runs playbooks/imports/play-gnome-shell-extensions.yml on the HOST. The change
this plan makes is the 'Declare Deployed Extensions Enabled' task: every
deployed UUID is added to org.gnome.shell enabled-extensions, additively.
--check previews without changing anything.

Run triage.bash before and after, and diff the reports."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's GNOME session and dconf database"
plan_prime_sudo
plan_start_log auto

plan_gate_change "extension packages installed where absent, extensions fetched and their schemas compiled, the custom extension copied, and every deployed UUID ADDED to org.gnome.shell enabled-extensions (nothing removed), then Space Bar and Dash to Dock dconf keys written"

plan_deploy_leg "play-gnome-shell-extensions.yml" \
    plan_ansible_playbook playbooks/imports/play-gnome-shell-extensions.yml

plan_finish
