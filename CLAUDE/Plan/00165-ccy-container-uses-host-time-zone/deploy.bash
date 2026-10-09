#!/usr/bin/env bash
# Plan 00165 — deploy.bash
#
# PURPOSE: ccy containers run in the host's time zone. HOST ONLY
# (CLAUDE/PlanScriptStandards.md R2): Ansible never runs in the CCY container.
#
# THE ONE LEG:
#   play-claude-yolo.yml — installs the CCY 3.89.0 launcher, which passes the host's zone
#   to every container as TZ, and the container 2.49 Dockerfile (tzdata by name). The
#   image rebuilds once, on the next ccy launch.
#
# There is no acceptance.bash: the check is a fresh ccy session, described below.
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

  playbooks/imports/play-claude-yolo.yml   (launcher 3.89.0, container 2.49)

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
printf '    1. Exit any running ccy session and start a fresh one; the image rebuilds once\n'
printf '       (container 2.49). Running sessions keep UTC until they are restarted.\n'
printf '    2. Inside it, run: date — it shows the same time and zone as the desktop clock,\n'
printf '       and so does the status line.\n\n'

plan_finish
