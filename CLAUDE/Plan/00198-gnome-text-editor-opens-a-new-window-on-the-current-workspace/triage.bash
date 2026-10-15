#!/usr/bin/env bash
# Plan 00198 — triage.bash (Task 1.1): the facts the new-window fix rests on.
#
# PURPOSE: record, on the host, how GNOME Text Editor is installed and launched:
#   - the RPM or the Flatpak (or both);
#   - its command-line options, to find the new-window or standalone flag;
#   - every org.gnome.TextEditor.desktop on the XDG data path, with its Exec and
#     DBusActivatable lines (D-Bus activation is what makes Nautilus reuse the open window);
#   - the default handler for text/plain and text/markdown;
#   - the editor's own GSettings and mutter's workspace settings.
# Fact-finding only: it renders no verdict (PlanScriptStandards R9). The probes are in
# triage-probe.bash beside this script.
#
# RUN ON THE HOST, as the desktop user: through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00198-gnome-text-editor-opens-a-new-window-on-the-current-workspace/triage.bash
# It never prompts and needs no sudo.
#
# EFFECT ON THE HOST: nothing. The editor is run only with --help-all, which opens no window.
#
# EXIT CODES: 0 every leg established its facts; 1 at least one did not (the leg names
# itself); 64 usage.
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

PLAN_USAGE="usage: triage.bash [-h|--help]

Read-only, on the HOST: how GNOME Text Editor is installed, its command-line options, its
desktop files, the default handler for text files, and the editor's and mutter's settings.
The report is written to the run directory."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it inspects the desktop's GNOME Text Editor install and settings"
plan_start_log auto

PROBE="${PLAN_SCRIPT_DIR}/triage-probe.bash"
REPORT="${PLAN_RUN_DIR}/triage-report.md"
printf '# Plan 00198 triage\n\n' >"${REPORT}"

plan_gather_leg "install: RPM or Flatpak" bash "${PROBE}" install "${REPORT}"
plan_gather_leg "command-line options" bash "${PROBE}" help "${REPORT}"
plan_gather_leg "desktop files" bash "${PROBE}" desktop "${REPORT}"
plan_gather_leg "default handler for text files" bash "${PROBE}" mime "${REPORT}"
plan_gather_leg "editor and workspace settings" bash "${PROBE}" settings "${REPORT}"

plan_finish
