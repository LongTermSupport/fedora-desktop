#!/usr/bin/env bash
# Plan 00158 — deploy.bash
#
# PURPOSE: ship ccy-sessions' Ctrl-T take-over. HOST ONLY (CLAUDE/PlanScriptStandards.md R2):
# Ansible never runs in the CCY container. Run it on every machine that uses ccy-sessions.
#
# THE ONE LEG:
#   play-claude-yolo.yml — installs the CCY 3.79.0 launcher, its lib/ (tmux-session.bash
#   carries ccy_tmux_take_over) and ~/.local/bin/ccy-sessions (the Ctrl-T key). No image
#   content changed (container 2.42), so no rebuild is triggered by this plan.
#
# There is no acceptance.bash: the verdict needs a session held by another terminal and a
# person at this one, so it is the plan's HOST task, done by hand and described below.
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

  playbooks/imports/play-claude-yolo.yml   (launcher 3.79.0, its lib/, ccy-sessions)

--check previews without changing anything. Then try Ctrl-T in ccy-sessions on a session
that is open in another terminal."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible to install the ccy launcher, its lib/ and ccy-sessions"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

printf '\n==> NEXT (by hand; there is no acceptance.bash):\n'
printf '    1. Have a session open in one terminal (or the one a dropped SSH left holding it).\n'
printf '    2. In another terminal: ccy-sessions, choose that "open elsewhere" row, press Ctrl-T.\n'
printf '    3. The question names the other terminal and its idle time; choose Yes.\n'
printf '    4. This terminal shows the session; the other one is detached, and nothing restarted.\n\n'

plan_finish
