#!/usr/bin/env bash
# Plan 00212 — acceptance.bash
#
# PURPOSE: render the VERDICT on the installed wsi-lower-media-volume
# (CLAUDE/PlanScriptStandards.md R9), the command the speech-to-text panel starts beside each
# recorder to lower the media that is playing to a third of its volume and restore it when the
# microphone closes (or pause a player whose sound it cannot find). The unit tests fake busctl's
# and pactl's replies; this runs the real binary and the real busctl against a real bus. HOST
# ONLY.
#
# Each check runs on a private session bus (dbus-run-session) with a fake MPRIS player
# (fake-mpris-player.py) and a fake pactl (fake-pactl.py, first on PATH) holding its one
# stream, so no real player is paused and no real volume is changed. It changes nothing on the
# host but the scratch directories it makes and removes. No sudo, no prompts. The panel's side
# (the menu switch, the release when the microphone closes) and the real pactl against the real
# sound server need GNOME Shell and a person: that is the owner's manual check in PLAN.md.
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
It runs the installed wsi-lower-media-volume against a fake media player on a private
session bus and a fake pactl; no real player or volume is touched.

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

plan_require_host "it checks the wsi-lower-media-volume binary the host's play installed"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-media-volume.bash"
plan_gather_leg "a playing stream is lowered to a third while held and restored exactly at TERM" bash "${checker}" lowers
plan_gather_leg "a volume changed by hand during the hold is left as set" bash "${checker}" changed-by-hand
plan_gather_leg "a stream that ends during the hold is skipped, exit 0" bash "${checker}" stream-ended
plan_gather_leg "a player with no stream found is paused while held and played again at TERM" bash "${checker}" no-stream
plan_gather_leg "a paused player stopped by hand during the hold is not played again" bash "${checker}" left-stopped
plan_gather_leg "nothing playing: exit 0 at once, nothing touched" bash "${checker}" not-playing
plan_gather_leg "the parent ending (GNOME Shell gone) restores the volume" bash "${checker}" parent-gone
plan_finish
