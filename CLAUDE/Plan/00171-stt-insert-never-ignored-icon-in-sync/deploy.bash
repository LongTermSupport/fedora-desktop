#!/usr/bin/env bash
# Plan 00171 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   play-speech-to-text.yml — deploys the recorders (wsi, wsi-stream, wsi-article,
#   wsi-article-window) and the server (wsi-stream-server), which now tag every D-Bus signal
#   with the panel's session token, send nothing after SUCCESS or ERROR, remove the shared
#   PID file only when it names them, and (server mode) announce RECORDING only once the
#   server has accepted START, and ABORT a START an Insert or Escape cancelled while it was
#   in flight; the server reports how much audio the microphone delivered, and a dictation
#   whose microphone fell short says so (always shown, even with notifications off); and
#   the panel extension (extension.js, the new panelSession.js, panelLook.js and the
#   bundled hourglass icon), which follows one recorder by its token, keeps one recording
#   at a time (a press in the stop grace or while the text renders does nothing but flash
#   a no-entry sign; nothing is queued), never shows ready over a live recorder, starts
#   recorders in their own process group, and shows the recording badge red. A changed
#   wsi-stream restarts the warm speech server (the play's handler).
#
# The extension changed, so a LOGOUT is needed before its code runs (Wayland). This script
# therefore does not end with acceptance.bash (R9): log out and back in, play the sequence
# acceptance.bash names, then run it. Do not dictate between the deploy and the logout:
# the old panel with the new recorders is an untested pairing (the new recorders send no
# IDLE after a result, and the old panel does not know WAITING).
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

Runs play-speech-to-text.yml on the HOST. Then log out and back in (the extension
changed), play the Insert sequence acceptance.bash describes, and run acceptance.bash."

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
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf 'Dry run (--check): nothing was deployed, so there is nothing for acceptance.bash to judge.\n'
else
    printf 'Deploy done. The recorders take effect at once; the panel only after a LOGOUT.\n'
    printf 'Do not dictate before the logout (old panel + new recorders is untested).\n'
    printf 'Next:\n'
    printf '  1. Log out and back in.\n'
    printf '  2. Turn on Debug Logging (panel menu), then play the sequence:\n'
    printf '     dictate a sentence, press Insert to stop, press Insert AGAIN during the grace\n'
    printf '     and AGAIN while it transcribes or the instant it turns green (each should\n'
    printf '     flash a red no-entry sign and start nothing); once the icon is ready press\n'
    printf '     Insert, speak, then press Insert once more.\n'
    printf '  3. Run %s/acceptance.bash (meta-deploy round 2).\n' "${PLAN_SCRIPT_DIR}"
fi

plan_finish
