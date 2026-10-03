#!/usr/bin/env bash
# Plan 00150 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   1. play-cli-tools.yml — installs ImageMagick and `file`, asserts ImageMagick can write
#      WebP, and deploys ~/.local/bin/imgpaste.
#   2. acceptance.bash — the pass/fail gate, run straight after (skipped under --check).
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

Runs play-cli-tools.yml on the HOST: ImageMagick (with WebP write support asserted), file,
and ~/.local/bin/imgpaste, then runs acceptance.bash (not under --check)."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs an Ansible play that installs packages and writes to ~/.local/bin"

# Before the run log, so a password prompt is not flooded by the tee (R3).
plan_prime_sudo

plan_start_log auto

plan_deploy_leg "play-cli-tools.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-cli-tools.yml

# A --check run deployed nothing, so acceptance would only fail on the undeployed copy.
if [[ "${PLAN_CHECK}" -eq 1 ]]; then
    printf '\n==> --check: acceptance.bash not run, nothing was deployed to test\n'
else
    plan_deploy_leg "acceptance.bash" "${PLAN_SCRIPT_DIR}/acceptance.bash"
fi

plan_finish
