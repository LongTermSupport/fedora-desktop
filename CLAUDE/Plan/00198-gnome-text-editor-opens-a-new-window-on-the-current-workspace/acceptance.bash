#!/usr/bin/env bash
# Plan 00198 acceptance.bash: the per-user GNOME Text Editor override is deployed and in force.
# Read-only; run on the host as the desktop user (through meta-deploy.bash, after
# playbooks/imports/play-gsettings.yml). It cannot prove the workspace behaviour, which is the
# owner's check in Task 2.2: open files from Nautilus on two workspaces.
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

Read-only, on the HOST: the per-user GNOME Text Editor desktop file opens each file in a new
window without D-Bus activation, and the editor is still the default for text/plain."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it inspects the desktop user's own desktop files and default handlers"
plan_start_log auto

override="${XDG_DATA_HOME:-${HOME}/.local/share}/applications/org.gnome.TextEditor.desktop"

plan_gather_leg "override file exists" test -f "${override}"
plan_gather_leg "override has D-Bus activation off" grep -qx 'DBusActivatable=false' "${override}"
plan_gather_leg "override opens a new window" grep -qx 'Exec=gnome-text-editor --new-window %U' "${override}"
plan_gather_leg "editor is the default for text/plain" bash -c \
    'gio mime text/plain | grep -q "Default application for.*org.gnome.TextEditor.desktop"'

plan_finish
