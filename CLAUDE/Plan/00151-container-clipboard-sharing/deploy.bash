#!/usr/bin/env bash
# Plan 00151 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   play-claude-yolo.yml — deploys the ccy and cc launchers, their lib/, and the shared
#   Dockerfile that now carries wl-clipboard and the 5 s wl-paste guard. The container image
#   is NOT rebuilt here: the launcher rebuilds claude-yolo:latest on its next start, when the
#   Dockerfile's version label differs from the image's.
#
# The LAST LEG is acceptance.bash. Until a ccy has been started once after this run, the image
# is still the old one and acceptance reports that as PENDING (exit 3), which this script
# passes on as a note, not a failure.
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

Runs play-claude-yolo.yml on the HOST, then acceptance.bash as the last leg. If acceptance
says the container image is still the old one, start ccy once (it rebuilds) and run
acceptance.bash again."

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

acceptanceStatus=0
"${PLAN_SCRIPT_DIR}/acceptance.bash" || acceptanceStatus=$?
case "${acceptanceStatus}" in
    0) printf '\nDeploy and acceptance passed.\n' ;;
    3)
        printf '\nDeploy done. The container image is still the old one.\n'
        printf 'Start ccy once in any project (it rebuilds the image), then run %s/acceptance.bash.\n' \
            "${PLAN_SCRIPT_DIR}"
        ;;
    *)
        printf '[FATAL] acceptance.bash failed (exit %d); see its output above\n' "${acceptanceStatus}" >&2
        exit "${acceptanceStatus}"
        ;;
esac

plan_finish
