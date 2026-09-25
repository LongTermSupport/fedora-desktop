#!/usr/bin/env bash
# Plan 00140 — deploy.bash
#
# PURPOSE: ship the browser session cap. HOST ONLY (CLAUDE/PlanScriptStandards.md R2):
# Ansible never runs in the CCY container.
#
# THE ONE LEG:
#   play-claude-yolo.yml — stages agent-browser-session-guard and the updated Dockerfile,
#   browsing skill and CCY-GUIDE into /opt/claude-yolo, installs the CCY 3.69.0 launcher,
#   and rebuilds the image (container 2.38). That rebuild is what puts the guard in front
#   of agent-browser-headed, agent-browser-headless and agent-browser-lite-headless.
#
# ccy sessions already running keep the image they started from, so they are not capped
# until restarted. The verdict is acceptance.bash, run INSIDE a new ccy session.
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

  playbooks/imports/play-claude-yolo.yml   (guard staged, launcher 3.69.0, image rebuilt)

--check previews without changing anything. Then start a NEW ccy session in this repo
and run ./acceptance.bash inside it."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible to install the ccy launcher and rebuild its image"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

printf '\n==> NEXT:\n'
printf '    1. Start a NEW ccy session in this repo; a running one keeps the old image.\n'
printf '    2. Inside it: ./CLAUDE/Plan/00140-agent-browser-sessions-leak/acceptance.bash\n\n'

plan_finish
