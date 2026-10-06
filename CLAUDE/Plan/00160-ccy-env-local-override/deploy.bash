#!/usr/bin/env bash
# Plan 00160 — deploy.bash
#
# PURPOSE: ship ccy's ccy.env.local support. HOST ONLY (CLAUDE/PlanScriptStandards.md R2):
# Ansible never runs in the CCY container.
#
# THE ONE LEG:
#   play-claude-yolo.yml — installs the CCY 3.83.0 launcher, which writes and keeps
#   .claude/ccy/ccy.env.local.dist. The image is unchanged (container 2.44).
#
# There is no acceptance.bash: the check is a ccy session in a project that has a
# .claude/ccy/ccy.env.local, done by hand and described below.
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

Runs, on the HOST:

  playbooks/imports/play-claude-yolo.yml   (launcher 3.83.0, container 2.44)

--check previews without changing anything."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible to install the ccy launcher and its image files"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

printf '\n==> NEXT (by hand; there is no acceptance.bash):\n'
printf '    1. Start ccy in a project. The launch says it wrote\n'
printf '       .claude/ccy/ccy.env.local.dist (version 1); git status lists it, to commit.\n'
printf '    2. Where a project has a .claude/ccy/ccy.env.local, the start-up output says\n'
printf '       "Sourcing project ccy env: /workspace/.claude/ccy/ccy.env.local", git status\n'
printf '       does not list it, and with no "# based on ccy.env.local.dist version 1" line\n'
printf '       the launch warns.\n\n'

plan_finish
