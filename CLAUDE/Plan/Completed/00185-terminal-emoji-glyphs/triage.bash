#!/usr/bin/env bash
# Plan 00185 — triage.bash (Task 1.1): the facts the wrong-emoji-glyph cause rests on (#60).
#
# PURPOSE: record, on the host, everything that decides which layer swaps the emoji glyphs:
#   - the terminal this run is in (environment, parent chain) and every terminal emulator
#     that is running, with its start time and version, and the VTE library version;
#   - the emoji fonts installed (packages, files, install times, rpm -V integrity) and any
#     font a user directory adds;
#   - the fontconfig fallback chain for emoji (fc-match -s emoji, fc-match and fc-list for
#     each code point of the issue), the fontconfig config files that mention emoji;
#   - the fontconfig cache ages against the emoji font file's mtime;
#   - which emoji font file each running terminal has mapped, and whether that mapping is
#     marked (deleted), meaning the font was replaced under the running process;
#   - the font and renderer settings of each terminal (GPU renderer environment, configs).
# Fact-finding only: it renders no verdict (PlanScriptStandards R9). The probes are in
# triage-probe.bash beside this script.
#
# RUN ON THE HOST, as the desktop user, ideally from the terminal that shows the wrong glyphs:
# through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00185-terminal-emoji-glyphs/triage.bash
# It never prompts and needs no sudo.
#
# EFFECT ON THE HOST: nothing. It reads files, /proc and the rpm database, and runs
# fc-match, fc-list, fc-cache --version and each terminal's --version. It does not refresh
# any cache. Its only output is the run log and report under untracked/plan-runs/.
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

Read-only, on the HOST: the terminal in use and every running terminal emulator with its
version, the VTE version, the installed emoji fonts, the fontconfig fallback chain for the
issue's code points, the fontconfig cache ages, the emoji font each terminal has mapped, and
each terminal's font and renderer settings. The report is written to the run directory."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it inspects the desktop's terminals, fonts and fontconfig caches"
plan_start_log auto

PROBE="${PLAN_SCRIPT_DIR}/triage-probe.bash"
REPORT="${PLAN_RUN_DIR}/triage-report.md"
printf '# Plan 00185 triage\n\n' >"${REPORT}"

plan_gather_leg "this run's terminal" bash "${PROBE}" terminal "${REPORT}"
plan_gather_leg "running terminals and versions" bash "${PROBE}" processes "${REPORT}"
plan_gather_leg "emoji font packages and files" bash "${PROBE}" fonts "${REPORT}"
plan_gather_leg "fontconfig fallback for emoji" bash "${PROBE}" fontconfig "${REPORT}"
plan_gather_leg "fontconfig cache ages" bash "${PROBE}" cache "${REPORT}"
plan_gather_leg "emoji fonts mapped by running terminals" bash "${PROBE}" mapped "${REPORT}"
plan_gather_leg "terminal font and renderer settings" bash "${PROBE}" settings "${REPORT}"

printf '\n==> READ THIS FOR the cause: "emoji fonts mapped by running terminals" (a (deleted)\n'
printf '    mapping or two different emoji files), then the install time in "emoji font packages\n'
printf '    and files" against the terminal start times in "running terminals and versions".\n'
plan_finish
