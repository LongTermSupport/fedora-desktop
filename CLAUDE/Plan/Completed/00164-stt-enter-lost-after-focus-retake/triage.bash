#!/usr/bin/env bash
# triage.bash — Plan 00164 Task 1.1: gather FACTS on dictations whose window lost focus
# and was given it back, against ones whose window kept it. Fact-finding only: renders
# no verdict (R9).
#
# WHERE TO RUN: on the HOST, in a terminal, as the desktop user.
#
# WHAT IT CHANGES: nothing. It reads the speech-to-text debug log (and its rotated .old)
# and the extension's settings, and compares the deployed recorders with the checkout.
# Legs:
#   1. settings: the extension's keys that decide whether, how and when a paste and its
#      Enter happen (auto-paste, auto-enter, streaming, continuous dictation, debug-mode);
#   2. deployed recorders: whether ~/.local/bin/wsi and wsi-stream are this checkout's;
#   3. dictations: for the most recent dictations where the panel logged "lost focus",
#      the timeline of focus, paste and Enter lines, and the same for the most recent
#      dictations that pasted with no focus loss, for comparison.
#
# The panel's lines ([EXT]) are UTC with milliseconds; the recorders' ([WSI], [STREAM])
# are local time to the second, and are written only with Debug Logging on (wsi also on
# an error). Dictated text is never copied into the report: only the lines named below.
#
# Usage: ./CLAUDE/Plan/00164-stt-enter-lost-after-focus-retake/triage.bash
#            [--count N] [-h|--help]
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

PLAN_USAGE="usage: triage.bash [--count N] [-h|--help]

Prints, from the speech-to-text debug log, the focus / paste / Enter timeline of the last
N (default 5) dictations whose window lost focus, and of the last 2 that did not."
plan_mode gather
plan_parse_common_flags "$@"

count=5
set -- "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --count)
            [[ $# -ge 2 && "$2" =~ ^[1-9][0-9]*$ ]] || {
                printf '[FATAL] --count needs a positive whole number\n%s\n' "${PLAN_USAGE}" >&2
                exit 2
            }
            count="$2"
            shift 2
            ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "$1" "${PLAN_USAGE}" >&2
            exit 2
            ;;
    esac
done

plan_require_host "it reads the desktop user's speech-to-text log and settings"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: the log and settings are per user\n' >&2
    exit 1
fi
plan_start_log auto

PROBE="${PLAN_SCRIPT_DIR}/probe.bash"
plan_gather_leg "settings" bash "${PROBE}" settings
plan_gather_leg "deployed recorders" bash "${PROBE}" deployed "${PLAN_REPO_ROOT}"
plan_gather_leg "dictations: focus, paste and Enter" bash "${PROBE}" dictations "${count}"

printf '\nRead first: "THE LAST %s WITH A FOCUS LOSS" above. With debug-mode false the\n' "${count}"
printf 'recorders write nothing, so only the panel lines appear: turn on Debug Logging, reproduce\n'
printf 'a dictation whose window loses focus, and run this again.\n'
plan_finish
