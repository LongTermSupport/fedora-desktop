#!/usr/bin/env bash
# Plan 00192 — deploy-rclone-tail.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   play-rclone.yml --tags rclone-tail — installs the Go toolchain if missing
#   (play-golang.yml), builds src-go/cmd/rclone-tail from this checkout and installs it at
#   ~/.local/bin/rclone-tail in place of the bash script. Only the tagged tasks run: the mounts
#   are not stopped, reconfigured or restarted.
#   Then acceptance-rclone-tail.bash checks the installed command (skipped under --check).
#
# Usage: ./deploy-rclone-tail.bash [--check] [-y|--yes] [-h|--help]
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

PLAN_USAGE="usage: deploy-rclone-tail.bash [--check] [-y|--yes] [-h|--help]

Runs play-rclone.yml --tags rclone-tail on the HOST (the Go rclone-tail built and installed;
the mounts are left alone), then acceptance-rclone-tail.bash."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs an Ansible play that builds a Go command into ~/.local/bin"

# Before the run log, so a password prompt is not flooded by the tee (R3).
plan_prime_sudo

plan_start_log auto

plan_deploy_leg "play-rclone.yml --tags rclone-tail" \
    plan_ansible_playbook playbooks/imports/optional/common/play-rclone.yml --tags rclone-tail

if [[ "${PLAN_CHECK:-0}" == "1" ]]; then
    printf '\n==> --check: acceptance-rclone-tail.bash skipped, nothing was deployed to test\n'
else
    plan_deploy_leg "acceptance-rclone-tail.bash" "${PLAN_SCRIPT_DIR}/acceptance-rclone-tail.bash"
fi

printf '\n'
printf 'Deploy done. rclone-tail is a Go binary now.\n'
printf 'Next, by hand (SPEC-rclone-tail.md section 7): run rclone-tail while a file uploads into a\n'
printf 'mount (H2), end it with Ctrl-C and check that echo $? prints 0 (H3), and see the closing\n'
printf 'upload status of ftp-camera still print (H4).\n'

plan_finish
