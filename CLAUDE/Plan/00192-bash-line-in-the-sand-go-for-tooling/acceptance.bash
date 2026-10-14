#!/usr/bin/env bash
# Plan 00192 — acceptance.bash: the Go podfreeze and lxcfreeze print what the bash tools printed
# on this host's real engines.
#
# Run on the HOST, as the desktop user, AFTER deploy.bash (which runs this as its last leg).
#
# WHAT IT DOES: deploy.bash saved, while the bash tools were still installed, what each printed
# for `list` and for `freeze --all -n` (the dry run) under untracked/freeze-acceptance/before-*.
# This runs the same four commands with the Go tools and compares stdout byte for byte. It also
# checks that both installed tools are compiled programs and that ~/.local/lib/freeze is gone.
# stdout is the payload compared; each command's stderr is kept beside the result in this run's
# directory (lxcfreeze's says why it asks for the sudo password).
#
# A tool with no saved baseline is NOT ESTABLISHED, not passed: the comparison needs the bash
# output, which cannot be made once the bash tool is gone.
#
# NOT COVERED (the owner, ACCEPTANCE-freeze.md steps 3 and 4): freezing and thawing a real
# container and the menu.
#
# Usage: ./acceptance.bash [-h|--help]
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Compares, on the HOST after deploy.bash, what the Go podfreeze and lxcfreeze print for 'list'
and 'freeze --all -n' with what the bash tools printed before deploy.bash replaced them
(untracked/freeze-acceptance/before-*). Also checks both tools are compiled programs and
~/.local/lib/freeze is gone."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n%s\n' "${PLAN_REMAINING_ARGS[*]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs the deployed tools against the host's real Podman and LXC"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user: the tools are installed in its ~/.local/bin\n' >&2
    exit 1
fi
plan_prime_sudo
plan_start_log auto

readonly BIN_DIR="${HOME}/.local/bin"
readonly BASELINE_DIR="${PLAN_REPO_ROOT}/untracked/freeze-acceptance"

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
yes_if() { if "$@"; then echo yes; else echo no; fi; }

# is_elf <file> — the first four bytes are the ELF magic.
is_elf() {
    local magic=""
    LC_ALL=C IFS= read -r -n 4 magic <"$1"
    [[ "${magic}" == $'\x7f'ELF ]]
}

# run_tool <tool> <label> <verb-and-args...> — the Go tool's stdout to after-<tool>-<label>.txt
# and its stderr to after-<tool>-<label>.err in this run's directory; non-zero is its status.
run_tool() {
    local tool="$1" label="$2"
    shift 2
    "${BIN_DIR}/${tool}" "$@" >"${PLAN_RUN_DIR}/after-${tool}-${label}.txt" 2>"${PLAN_RUN_DIR}/after-${tool}-${label}.err"
}

printf '=== what is installed ===\n'
for tool in podfreeze lxcfreeze; do
    if is_elf "${BIN_DIR}/${tool}"; then
        check "${BIN_DIR}/${tool} is a compiled program" yes
    else
        check "${BIN_DIR}/${tool} is a compiled program" no
    fi
done
check "${HOME}/.local/lib/freeze is gone" "$(yes_if test ! -e "${HOME}/.local/lib/freeze")"

printf '=== the Go tools against the bash baseline ===\n'
for tool in podfreeze lxcfreeze; do
    for label in list dry; do
        before="${BASELINE_DIR}/before-${tool}-${label}.txt"
        if [[ ! -f "${before}" ]]; then
            notEstablished+=("${tool} ${label}: no bash baseline at ${before} (deploy.bash saves it while the bash tool is still installed)")
            continue
        fi
        if [[ "${label}" == "list" ]]; then
            args=(list)
        else
            args=(freeze --all -n)
        fi
        # The bash tool's exit status is part of its answer (no containers: "nothing matched",
        # exit 1), so the Go tool must give the same status, not necessarily 0. A baseline
        # saved before statuses were recorded has no .rc file and is held to exit 0.
        wantStatus=0
        if [[ -f "${BASELINE_DIR}/before-${tool}-${label}.rc" ]]; then
            read -r wantStatus <"${BASELINE_DIR}/before-${tool}-${label}.rc"
        fi
        gotStatus=0
        run_tool "${tool}" "${label}" "${args[@]}" || gotStatus=$?
        if [[ "${gotStatus}" -eq "${wantStatus}" ]]; then
            check "${tool} ${args[*]} exits ${wantStatus}, as the bash tool did" yes
        else
            check "${tool} ${args[*]} exits ${wantStatus}, as the bash tool did (got ${gotStatus}; stderr: ${PLAN_RUN_DIR}/after-${tool}-${label}.err)" no
            continue
        fi
        after="${PLAN_RUN_DIR}/after-${tool}-${label}.txt"
        if cmp -s "${before}" "${after}"; then
            check "${tool} ${args[*]} prints what the bash tool printed" yes
        else
            check "${tool} ${args[*]} prints what the bash tool printed (diff: ${PLAN_RUN_DIR}/diff-${tool}-${label}-report.txt)" no
            # diff exits 1 on a difference, which is the finding here, so its status is read.
            diffStatus=0
            diff -u "${before}" "${after}" >"${PLAN_RUN_DIR}/diff-${tool}-${label}-report.txt" || diffStatus=$?
            if [[ "${diffStatus}" -gt 1 ]]; then
                printf '[FATAL] diff failed with status %s\n' "${diffStatus}" >&2
                exit 1
            fi
        fi
    done
done

printf '\nCOVERAGE: %d of %d checks passed\n' "${passed}" "${total}"
if [[ "${#notEstablished[@]}" -gt 0 ]]; then
    printf 'NOT ESTABLISHED:\n'
    printf '  - %s\n' "${notEstablished[@]}"
    PLAN_FAILED_LEGS="${PLAN_FAILED_LEGS}${PLAN_FAILED_LEGS:+ }baseline"
fi
if [[ "${failed}" -ne 0 ]]; then
    PLAN_FAILED_LEGS="${PLAN_FAILED_LEGS}${PLAN_FAILED_LEGS:+ }checks"
fi
plan_finish
