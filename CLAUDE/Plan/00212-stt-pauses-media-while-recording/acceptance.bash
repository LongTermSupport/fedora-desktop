#!/usr/bin/env bash
# Plan 00212 — acceptance.bash
#
# PURPOSE: render the VERDICT on the installed wsi-pause-media (CLAUDE/PlanScriptStandards.md
# R9), the command the speech-to-text panel starts beside each recorder to pause the media
# players that are playing and play them again when the microphone closes. The unit tests fake
# busctl's replies; this runs the real binary and the real busctl against a real bus. HOST ONLY.
#
# Each check runs on a private session bus (dbus-run-session) with a fake MPRIS player
# (fake-mpris-player.py), so no real player is paused or played. It changes nothing on the host
# but the scratch directories it makes and removes. No sudo, no prompts. The panel's side (the
# menu switch, the release when the microphone closes) needs GNOME Shell and a person: that is
# the owner's manual check in PLAN.md.
#
# Usage: ./acceptance.bash [-h|--help]
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

The Plan 00212 acceptance gate. Run play-speech-to-text.yml first (meta-deploy.bash does).
It runs the installed wsi-pause-media against a fake media player on a private session bus;
no real player is touched.

EXIT STATUS
  0  ACCEPTED — every check ran and passed
  1  REJECTED — a check failed, or did not run
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it checks the wsi-pause-media binary the host's play installed"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-media-pause.bash"
plan_gather_leg "a Playing player is paused while held and played again at TERM" bash "${checker}" resumes
plan_gather_leg "a player stopped by hand during the hold is not played again" bash "${checker}" left-stopped
plan_gather_leg "nothing playing: exit 0 at once, no player touched" bash "${checker}" not-playing
plan_gather_leg "the parent ending (GNOME Shell gone) plays the player again" bash "${checker}" parent-gone
plan_finish
