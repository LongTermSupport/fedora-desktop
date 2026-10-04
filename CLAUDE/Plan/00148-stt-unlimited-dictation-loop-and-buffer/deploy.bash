#!/usr/bin/env bash
# Plan 00148 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   play-speech-to-text.yml — deploys every recorder and helper this plan changed (wsi,
#   wsi-stream, wsi-stream-server, wsi-article, wsi-article-window, wsi-stop-grace,
#   wsi-setting, wsi-resolve-model), the extension and its compiled settings schema, and the
#   start-at-login unit, then restarts the warm server so it runs the new code.
#
# It ends by telling the operator to LOG OUT AND BACK IN. On Wayland that is the only way the
# new extension JavaScript (the panel's elapsed time, the STOPPING icon) loads; the scripts
# take effect at once.
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

Runs play-speech-to-text.yml on the HOST, then tells you to log out and back in (the only
way the new extension code loads on Wayland). Run acceptance.bash after logging back in."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs an Ansible play that writes to ~/.local/bin, the extensions directory and the user systemd manager"

# Before the run log, so a password prompt is not flooded by the tee (R3).
plan_prime_sudo

plan_start_log auto

plan_deploy_leg "play-speech-to-text.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-speech-to-text.yml

printf '\n'
printf 'Deploy done. NOW LOG OUT AND LOG BACK IN.\n'
printf 'On Wayland that is the only way the new extension code loads; Alt+F2 r is X11-only.\n'
printf 'Next: %s/acceptance.bash, then the dictation pass (Task 6.2).\n' "${PLAN_SCRIPT_DIR}"

plan_finish
