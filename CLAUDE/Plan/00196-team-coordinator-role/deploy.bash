#!/usr/bin/env bash
# Plan 00196 — deploy.bash
#
# PURPOSE: put the coordinator role on the host. HOST ONLY (CLAUDE/PlanScriptStandards.md
# R2) — Ansible never runs in the CCY container.
#
# EFFECT ON THE HOST, in this order:
#   1. play-agent-bus.yml    the agent-bus software (set-role refuses a second coordinator,
#                            the registry and team record read orchestrator as coordinator);
#                            a team whose software is out of date is rebuilt and restarted.
#   2. play-claude-yolo.yml  the launcher (CCY 3.97.0: ccy --teams offers the seat's role,
#                            Quick Launch shows it), the ccy-teams binary (the coordinator on
#                            the board and in a seat's menu, choose-seat, describe-seats) and
#                            the pingbus plugin and skill the containers get.
# A session already running keeps the pingbus it started with; a new session has the role
# guidance and --to-coordinator. Every member of a team takes the new pingbus before the
# team's record names a coordinator (docs/agent-bus-protocol.md section 1).
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

Runs playbooks/imports/play-agent-bus.yml, then playbooks/imports/play-claude-yolo.yml, on
the HOST. --check previews without changing anything.

Afterwards: ccy-teams shows the coordinator and offers Make coordinator on a seat's menu,
and a bare ccy --teams asks the seat's role."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's agent bus and ccy install"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-agent-bus.yml" \
    plan_ansible_playbook playbooks/imports/play-agent-bus.yml
plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

plan_finish
