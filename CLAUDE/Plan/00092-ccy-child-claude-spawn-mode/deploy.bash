#!/usr/bin/env bash
# deploy.bash — stage Plan 00092's artefacts into the image build context, on the HOST.
#
# WHERE TO RUN: on the HOST, in a terminal. Enforced by plan_require_host (R2). Never in the
# CCY container: CLAUDE/ContainerRules.md is edit-here, deploy-there, and an Ansible run
# inside the container would target the container.
#
# WHAT IT CHANGES: it runs playbooks/imports/play-claude-yolo.yml, which writes the launcher,
# entrypoint, Dockerfile and skills into /opt/claude-yolo and /var/local/claude-yolo. It is
# idempotent. It does NOT rebuild the container image — see the closing note.
#
# Usage: ./CLAUDE/Plan/00092-ccy-child-claude-spawn-mode/deploy.bash [--check] [-y|--yes] [-h|--help]
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
set -euo pipefail
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: deploy.bash [--check] [-y|--yes] [-h|--help]

Runs play-claude-yolo.yml on the HOST so the child-claude wrapper and skill reach
the image build context. Does not rebuild the image; run triage.bash after, which
says whether a rebuild is pending."

plan_mode deploy
plan_parse_common_flags "$@"

plan_require_host "it runs an Ansible play that writes to /opt/claude-yolo and /var/local/claude-yolo"

# Before the run log, so a password prompt is not flooded by the tee (R3).
plan_prime_sudo

plan_start_log auto

plan_gate_change "install the CCY launcher, entrypoint, Dockerfile and skills into /opt/claude-yolo and /var/local/claude-yolo (idempotent)"

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

# Deliberately NOT rebuilding the image here.
#
# The image version moved 2.28 -> 2.29, so a rebuild IS required before the feature reaches a
# session. But `ccy --rebuild` is a long, interactive, resource-heavy operation the operator
# should start knowingly, and burying it inside a deploy wrapper would make this script mean
# something much bigger than its name. triage.bash's H4 leg reports whether one is pending,
# so the need is surfaced rather than assumed.
printf '\n'
printf 'Staging done. The image still needs rebuilding: the container version moved to 2.29.\n'
printf 'Next, in this order:\n'
printf '  1. ccy --rebuild\n'
printf '  2. %s/triage.bash          # confirms the rebuild landed\n' "${PLAN_SCRIPT_DIR}"
printf '  3. enable the mode in a project ccy.env, start a session, and run\n'
printf '     %s/acceptance.bash      # INSIDE that container\n' "${PLAN_SCRIPT_DIR}"
printf '  4. remove the flag, start a later session, and run acceptance.bash again\n'
printf '     Step 4 is the one that matters: it proves the mode can be turned OFF.\n'

plan_finish
