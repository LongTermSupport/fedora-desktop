#!/usr/bin/env bash
# Plan 00164 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   play-speech-to-text.yml — deploys the recorders wsi and wsi-stream: after the panel gives
#   the window pinned at Insert focus back, they paste only once it has kept focus for half
#   a second, and wait longer before the Enter. A changed wsi-stream restarts the warm
#   speech server (the play's handler), so its model loads again.
#
# The extension is unchanged, so no logout is needed: the recorders take effect at once.
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

Runs play-speech-to-text.yml on the HOST. Then dictate into a window, move focus away
before you stop, and see the text pasted AND sent; then run triage.bash."

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
printf 'Deploy done. The recorders take effect at once (no logout: the extension is unchanged).\n'
printf 'Next (Task 3.1): turn on Debug Logging, start a dictation, click another window before\n'
printf 'you stop, and check the text is pasted into the first window AND sent. Then run\n'
printf '%s/triage.bash.\n' "${PLAN_SCRIPT_DIR}"

plan_finish
