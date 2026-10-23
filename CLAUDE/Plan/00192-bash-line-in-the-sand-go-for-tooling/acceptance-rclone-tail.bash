#!/usr/bin/env bash
# Plan 00192 — acceptance-rclone-tail.bash: the Go rclone-tail is installed and reports each of
# this host's rclone mounts without an RC error (SPEC-rclone-tail.md H1).
#
# Run on the HOST, as the desktop user, AFTER play-rclone.yml (deploy-rclone-tail.bash runs the
# play first, then this). Read-only: it runs the installed command, changes nothing, needs no sudo.
#
# CHECKS: the installed ~/.local/bin/rclone-tail is a compiled program; `--help` exits 0;
# `--once` exits 0 and prints the `=== HH:MM:SS ===` heading; every mount line is `idle`, an
# activity line or a mount started without --rc, never `error:`. A host with no rclone mount
# cannot show that: it is NOT ESTABLISHED, not passed.
#
# NOT COVERED (the owner, by hand): H2, a stream during an upload; H3, Ctrl-C ending it with
# exit 0 after the closing snapshot; H4, ftp-camera's closing upload status.
#
# Usage: ./acceptance-rclone-tail.bash [-h|--help]
#
# EXIT CODES: 0 every check passed; 1 at least one failed or could not be established; 64 usage.
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

PLAN_USAGE="usage: acceptance-rclone-tail.bash [-h|--help]

Checks, on the HOST after play-rclone.yml, that ~/.local/bin/rclone-tail is the Go binary and
reports every rclone mount without an RC error. Read-only."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n%s\n' "${PLAN_REMAINING_ARGS[*]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs the deployed command against the host's real rclone mounts"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user: the command and the mounts are its own\n' >&2
    exit 1
fi
plan_start_log auto

readonly TOOL="${HOME}/.local/bin/rclone-tail"

total=0
passed=0
failed=0
notEstablished=()
check() {
    local label="$1" ok="$2"
    total=$((total + 1))
    if [[ "${ok}" == "yes" ]]; then
        passed=$((passed + 1))
        printf '  PASS: %s\n' "${label}"
    else
        failed=$((failed + 1))
        printf '  FAIL: %s\n' "${label}"
    fi
}

# is_elf <file> — the first four bytes are the ELF magic.
is_elf() {
    local magic=""
    [[ -r "$1" ]] || return 1
    LC_ALL=C IFS= read -r -n 4 magic <"$1"
    [[ "${magic}" == $'\x7f'ELF ]]
}

printf '=== what is installed ===\n'
if is_elf "${TOOL}"; then
    check "${TOOL} is a compiled program" yes
else
    check "${TOOL} is a compiled program" no
fi

printf '=== the command ===\n'
helpStatus=0
"${TOOL}" --help >"${PLAN_RUN_DIR}/help.txt" 2>"${PLAN_RUN_DIR}/help.err" || helpStatus=$?
if [[ "${helpStatus}" -eq 0 ]]; then
    check "--help exits 0" yes
else
    check "--help exits 0 (got ${helpStatus}; stderr: ${PLAN_RUN_DIR}/help.err)" no
fi

status=0
"${TOOL}" --once >"${PLAN_RUN_DIR}/once.txt" 2>"${PLAN_RUN_DIR}/once.err" || status=$?
report="${PLAN_RUN_DIR}/once.txt"
if [[ "${status}" -eq 1 ]] && grep -q '^rclone-tail: No rclone mounts found\.$' "${PLAN_RUN_DIR}/once.err"; then
    notEstablished+=("live mount lines: this host has no rclone mount (stderr: ${PLAN_RUN_DIR}/once.err)")
elif [[ "${status}" -ne 0 ]]; then
    check "--once exits 0 (got ${status}; stderr: ${PLAN_RUN_DIR}/once.err)" no
else
    check "--once exits 0" yes
    if grep -qE '^=== [0-9]{2}:[0-9]{2}:[0-9]{2} ===$' "${report}"; then
        check "--once prints the heading" yes
    else
        check "--once prints the heading (output: ${report})" no
    fi
    if grep -q '  error: ' "${report}"; then
        check "every mount's RC answered, no 'error:' line (output: ${report}; reasons: ${PLAN_RUN_DIR}/once.err)" no
    elif grep -qE '  (idle|queued: )' "${report}"; then
        check "the mounts report live activity or idle" yes
    else
        notEstablished+=("live mount lines: no mount was started with --rc (output: ${report})")
    fi
fi

printf '\nCOVERAGE: %d of %d checks passed\n' "${passed}" "${total}"
if [[ "${#notEstablished[@]}" -gt 0 ]]; then
    printf 'NOT ESTABLISHED:\n'
    printf '  - %s\n' "${notEstablished[@]}"
    PLAN_FAILED_LEGS="${PLAN_FAILED_LEGS}${PLAN_FAILED_LEGS:+ }not-established"
fi
if [[ "${failed}" -ne 0 ]]; then
    PLAN_FAILED_LEGS="${PLAN_FAILED_LEGS}${PLAN_FAILED_LEGS:+ }checks"
fi
plan_finish
