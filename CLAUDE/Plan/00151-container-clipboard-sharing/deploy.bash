#!/usr/bin/env bash
# Plan 00151 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   play-claude-yolo.yml — deploys the ccy and cc launchers, their lib/, and the shared
#   Dockerfile that carries wl-clipboard and the 5 s wl-paste guard, then the play builds
#   claude-yolo:latest from it (a podman build as the desktop user; the first run after a
#   Dockerfile change takes a few minutes).
#
# The LAST LEG is acceptance.bash, skipped under --check (nothing was deployed to test).
# Project images built from a custom Dockerfile are rebuilt by ccy on its next start there.
#
# Usage: ./deploy.bash [--check] [-y|--yes] [-h|--help]
set -euo pipefail

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

PLAN_USAGE="usage: deploy.bash [--check] [-y|--yes] [-h|--help]

Runs play-claude-yolo.yml on the HOST (it also builds the claude-yolo:latest image), then
acceptance.bash as the last leg (not under --check)."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs an Ansible play that writes /var/local/claude-yolo and /opt/claude-yolo"

# Before the run log, so a password prompt is not flooded by the tee (R3).
plan_prime_sudo

plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

# A --check run deployed nothing, so acceptance would only fail on the undeployed copy.
if [[ "${PLAN_CHECK}" -eq 1 ]]; then
    printf '\n==> --check: acceptance.bash not run, nothing was deployed to test\n'
else
    plan_deploy_leg "acceptance.bash" "${PLAN_SCRIPT_DIR}/acceptance.bash"
fi

plan_finish
