#!/usr/bin/env bash
# Plan 00232 — deploy.bash
#
# PURPOSE: deploy the ccy network policy. HOST ONLY (CLAUDE/PlanScriptStandards.md R2) —
# Ansible never runs in the CCY container.
#
# EFFECT ON THE HOST: play-claude-yolo.yml installs the nftables package, builds
# ccy-network-policy into ~/.local/bin, and installs the launcher at CCY 3.101.0, whose every
# later session runs under the policy: the private and LAN ranges and the host refused, the
# public internet, DNS, the agent bus and the session's project networks open. Sessions
# already running keep running unprotected until they are relaunched. Nothing else changes.
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

Plan 00232, the deploy half: runs play-claude-yolo.yml (nftables, the ccy-network-policy
command, and the launcher that sends every session through it) on the HOST.
  --check   the play as a dry run
Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it installs nftables, ccy-network-policy and the ccy launcher on the host"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml (nftables, ccy-network-policy, CCY 3.101.0)" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml
# STANDARD-EXCEPTION(R9): no acceptance.bash leg here, where R9 puts it. meta-deploy.bash
# runs acceptance.bash after this script (and after triage.bash's second run).
plan_finish
