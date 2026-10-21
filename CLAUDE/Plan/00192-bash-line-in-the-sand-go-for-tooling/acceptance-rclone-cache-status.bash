#!/usr/bin/env bash
# Plan 00192 — acceptance-rclone-cache-status.bash: the Go rclone-cache-status is installed and
# reports live cache figures on this host's rclone mounts (SPEC-rclone-cache-status.md H1).
#
# Run on the HOST, as the desktop user, AFTER play-rclone.yml (meta-deploy.list runs the play
# first, then this). Read-only: it runs the installed command, changes nothing, needs no sudo.
#
# CHECKS: the installed ~/.local/bin/rclone-cache-status is a compiled program; `--help` exits
# 0; `--no-dir` exits 0 and prints the heading; every mount line is a cache figure or a mount
# started without --rc, never `error:`. A host with no rclone mount cannot show the figures:
# that is NOT ESTABLISHED, not passed.
#
# NOT COVERED (the owner, by hand): H2, the directory analysis run from inside a mount; H3,
# rclone-cache-warm and rclone-tail bracketing their runs with it.
#
# Usage: ./acceptance-rclone-cache-status.bash [-h|--help]
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

PLAN_USAGE="usage: acceptance-rclone-cache-status.bash [-h|--help]

Checks, on the HOST after play-rclone.yml, that ~/.local/bin/rclone-cache-status is the Go
binary and reports live cache figures for the rclone mounts. Read-only."

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

readonly TOOL="${HOME}/.local/bin/rclone-cache-status"

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
"${TOOL}" --no-dir >"${PLAN_RUN_DIR}/no-dir.txt" 2>"${PLAN_RUN_DIR}/no-dir.err" || status=$?
report="${PLAN_RUN_DIR}/no-dir.txt"
if [[ "${status}" -eq 0 ]]; then
    check "--no-dir exits 0" yes
else
    check "--no-dir exits 0 (got ${status}; stderr: ${PLAN_RUN_DIR}/no-dir.err)" no
fi
if grep -q '^=== rclone cache status ===$' "${report}"; then
    check "--no-dir prints the heading" yes
else
    check "--no-dir prints the heading (output: ${report})" no
fi
if grep -q '^  No rclone mounts found\.$' "${report}"; then
    notEstablished+=("live cache figures: this host has no rclone mount (output: ${report})")
elif grep -q '  error: ' "${report}"; then
    check "every mount's RC answered, no 'error:' line (output: ${report}; reasons: ${PLAN_RUN_DIR}/no-dir.err)" no
elif grep -q '  cache: ' "${report}"; then
    check "the mounts report live cache figures" yes
else
    notEstablished+=("live cache figures: no mount was started with --rc (output: ${report})")
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
