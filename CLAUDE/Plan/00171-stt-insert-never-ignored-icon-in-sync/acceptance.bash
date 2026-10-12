#!/usr/bin/env bash
# Plan 00171 — acceptance.bash: the pass/fail gate for "Insert is never ignored, the icon is
# always in sync", judged from the HOST's own evidence. Read-only.
#
# Run on the HOST, as the desktop user, AFTER deploy.bash, a logout/login, and the owner
# playing the Insert rules with Debug Logging on:
#
#   dictate a sentence, press Insert to stop, press Insert AGAIN during the grace (orange
#   dots, microphone still open), and AGAIN while it transcribes or the instant it turns
#   green; once the icon is ready press Insert, speak, then press Insert once more.
#
# It checks: every recorder, the server and the extension files are deployed and identical
# to the checkout's; the panel running in the Shell is the new one (its debug log names a
# session token per recorder, which only the new panel writes, so it also proves the
# logout); and, from the panel's debug log, the replay itself (probe-insert-sequence.py):
# the first dictation was stopped once, the presses in its grace and while it rendered did
# nothing but flash BLOCKED, nothing started by itself, the press once ready started a new
# dictation, the next press stopped it, and nothing showed the ready icon while it recorded.
# It prints COVERAGE: n of m and names what it CANNOT establish.
#
# Usage: ./acceptance.bash [-h|--help]
#
# EXIT CODES: 0 every check passed; 1 at least one failed; 2 nothing failed but the replay
# could not be judged (Debug Logging off, or the sequence not played); 64 usage error.
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Checks, on the HOST after deploy.bash, a logout/login and the Insert replay with Debug
Logging on: the recorders, server and extension files are deployed and identical to this
checkout's; the running panel is the new one; and the replay in the panel's debug log.
Prints COVERAGE: n of m and what it cannot establish."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the host's deployed scripts, the extension's settings and the panel's debug log"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: the settings and logs are per user\n' >&2
    exit 1
fi

plan_start_log auto

binDir="${HOME}/.local/bin"
extUuid="speech-to-text@fedora-desktop"
extDeployed="${HOME}/.local/share/gnome-shell/extensions/${extUuid}"
extSource="${repoRoot}/extensions/${extUuid}"
debugLog="${HOME}/.local/share/speech-to-text/debug.log"
REPORT="${PLAN_RUN_DIR}/acceptance-report.md"

total=0
passed=0
failed=0
unset_checks=0

check() {
    local label="${1}" ok="${2}"
    total=$((total + 1))
    if [[ "${ok}" == "yes" ]]; then
        passed=$((passed + 1))
        printf '  PASS: %s\n' "${label}"
    elif [[ "${ok}" == "unset" ]]; then
        unset_checks=$((unset_checks + 1))
        printf '  NOT ESTABLISHED: %s\n' "${label}"
    else
        failed=$((failed + 1))
        printf '  FAIL: %s\n' "${label}"
    fi
    printf -- '- %s: %s\n' "${ok}" "${label}" >> "${REPORT}"
}

printf '# Plan 00171 acceptance\n\n' > "${REPORT}"

printf '=== scripts deployed and identical to the checkout ===\n'
for script in wsi wsi-stream wsi-stream-server wsi-article wsi-article-window; do
    deployed="${binDir}/${script}"
    if [[ ! -x "${deployed}" ]]; then
        check "${script} is deployed and executable" no
    elif cmp -s "${repoRoot}/files/home/.local/bin/${script}" "${deployed}"; then
        check "${script} is deployed and identical to the checkout" yes
    else
        check "${script} is deployed but DIFFERS from the checkout (re-run deploy.bash)" no
    fi
done

printf '=== extension files deployed and identical ===\n'
# Every tracked file of the extension but its schemas (compiled on the host, not copied),
# so a file added to the extension cannot be left out of this check
extFiles=()
while IFS= read -r tracked; do
    extFiles+=("${tracked#extensions/"${extUuid}"/}")
done < <(git -C "${repoRoot}" ls-files -- "extensions/${extUuid}/" ":(exclude)extensions/${extUuid}/schemas/")
if [[ "${#extFiles[@]}" -eq 0 ]]; then
    printf '[FATAL] git ls-files found no files under extensions/%s\n' "${extUuid}" >&2
    exit 1
fi
printf 'Checking %d extension files\n' "${#extFiles[@]}"
for file in "${extFiles[@]}"; do
    if [[ ! -f "${extDeployed}/${file}" ]]; then
        check "${file} is deployed" no
    elif cmp -s "${extSource}/${file}" "${extDeployed}/${file}"; then
        check "${file} is deployed and identical" yes
    else
        check "${file} is deployed but DIFFERS from the checkout" no
    fi
done

printf '=== the panel running in the Shell is the new one ===\n'
# wsi-setting is the reader the recorders use; its exit status is the answer being checked
if ! debugMode="$("${binDir}/wsi-setting" debug-mode 2>&1)"; then
    check "Debug Logging can be read (got: ${debugMode})" no
elif [[ "${debugMode}" != "true" ]]; then
    check "Debug Logging is on, so the panel's log holds the evidence (it is ${debugMode}: turn it on in the panel menu, replay, run again)" unset
elif [[ ! -f "${debugLog}" ]]; then
    check "the panel's debug log exists at ${debugLog}" unset
elif grep -Eq '\[EXT\] Started (dictation|claude|article) recorder [0-9]+ \(session ' "${debugLog}"; then
    check "the panel's log names a session token per recorder (only the new panel does: the logout happened)" yes
else
    check "the panel's log names a session token per recorder (none yet: log out and back in, then dictate)" unset
fi

printf '=== the Insert rules replayed (probe-insert-sequence.py) ===\n'
if [[ "${debugMode}" == "true" && -f "${debugLog}" ]]; then
    probeStatus=0
    python3 "${PLAN_SCRIPT_DIR}/probe-insert-sequence.py" "${debugLog}" 0 "${REPORT}" || probeStatus=$?
    rulesLabel="Insert stopped a dictation once, did nothing in its grace or while it rendered, started nothing by itself, then started and stopped the next one"
    case "${probeStatus}" in
        0) check "${rulesLabel}" yes ;;
        2) check "the whole replay was played (see above for what to press)" unset ;;
        *) check "${rulesLabel} (exit ${probeStatus})" no ;;
    esac
else
    check "the replay can be judged (needs Debug Logging on and its log)" unset
fi

printf '\nCOVERAGE: %d of %d checks passed' "${passed}" "${total}"
printf ' (%d failed, %d not established)\n' "${failed}" "${unset_checks}"
printf 'report: %s\n' "${REPORT}"
printf '\nNOT ESTABLISHABLE by this script (a person must look):\n'
printf '  - the red no-entry flash itself on the icon (the log only says it was drawn)\n'
printf '  - the grace notification no longer offering "Insert: stop now"\n'
printf '  - a START the server refuses: the icon red for two seconds and the reason in the menu\n'
printf '  - Ctrl+Insert / Alt+Insert and Create Article following the same rules\n'
printf '  - Escape during recording or the grace discarding the dictation\n'

if [[ "${failed}" -gt 0 ]]; then
    printf '\n==> %d check(s) FAILED\n' "${failed}" >&2
    exit 1
fi
if [[ "${unset_checks}" -gt 0 ]]; then
    printf '\n==> nothing failed, but %d check(s) had no evidence\n' "${unset_checks}" >&2
    exit 2
fi
printf '\n==> all checks passed\n'
