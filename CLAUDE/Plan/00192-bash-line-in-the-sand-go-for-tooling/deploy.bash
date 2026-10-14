#!/usr/bin/env bash
# Plan 00192 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   play-claude-yolo.yml — builds and installs the ccy-sessions and ccy-teams commands through
#   the new shared tasks/build-go-command.yml. Same flags, paths and modes as before, so an
#   unchanged source reports ok.
#   play-podfreeze.yml, play-lxcfreeze.yml — import play-golang.yml, then build podfreeze and
#   lxcfreeze from this checkout (Go binaries, src-go/cmd/<tool>), install them at the paths the
#   bash tools had, and remove the retired ~/.local/lib/freeze library. fzf stays installed.
#
# BEFORE running this on a host that still has the bash freeze tools, save what they print
# (ACCEPTANCE-freeze.md lists the commands); this replaces them.
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

Runs play-claude-yolo.yml, play-podfreeze.yml and play-lxcfreeze.yml on the HOST. Then follow
ACCEPTANCE-freeze.md."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible plays that build Go commands into ~/.local/bin and remove ~/.local/lib/freeze"

# Before the run log, so a password prompt is not flooded by the tee (R3).
plan_prime_sudo

plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml
plan_deploy_leg "play-podfreeze.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-podfreeze.yml
plan_deploy_leg "play-lxcfreeze.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-lxcfreeze.yml

printf '\n'
printf 'Deploy done. podfreeze and lxcfreeze are Go binaries now.\n'
printf 'Next: the checks in %s/ACCEPTANCE-freeze.md.\n' "${PLAN_SCRIPT_DIR}"

plan_finish
