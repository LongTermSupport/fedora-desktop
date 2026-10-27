#!/usr/bin/env bash
# Plan 00192 — acceptance.bash: the Go podfreeze and lxcfreeze keep the bash tools' layout on
# this host's real engines.
#
# Run on the HOST, as the desktop user, AFTER deploy.bash (which runs this as its last leg).
#
# WHAT IT DOES: deploy.bash saved, while the bash tools were still installed, what each printed
# for `list` and for `freeze --all -n` (the dry run) under untracked/freeze-acceptance/before-*.
# This runs the same four commands with the Go tools and checks each exits 0 (the dry run may
# exit 1 with nothing on stdout: nothing running to freeze) and keeps the bash layout
# (freeze-layout.bash): the containers differ from the baseline's on any live machine, so rows
# are not compared, but every other line must be one the bash tool printed. Byte parity was
# shown on the first deploys (PLAN.md Task 3.1). It also checks that both installed tools are
# compiled programs and that ~/.local/lib/freeze is gone. Each command's stderr is kept beside
# the result in this run's directory (lxcfreeze's says why it asks for the sudo password).
#
# A tool with no saved baseline is NOT ESTABLISHED, not passed: the comparison needs the bash
# output, which cannot be made once the bash tool is gone.
#
# NOT COVERED (the host agent, ACCEPTANCE-freeze.md step 2): freezing and thawing a real
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

Checks, on the HOST after deploy.bash, that what the Go podfreeze and lxcfreeze print for
'list' and 'freeze --all -n' keeps the layout the bash tools printed before deploy.bash
replaced them (untracked/freeze-acceptance/before-*; container rows are not compared). Also
checks both tools are compiled programs and ~/.local/lib/freeze is gone."

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
readonly GOLDEN_DIR="${PLAN_REPO_ROOT}/src-go/internal/freeze/golden/testdata/golden"
declare -rA GOLDEN_PREFIX=([podfreeze]=pod [lxcfreeze]=lxc)

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
        gotStatus=0
        run_tool "${tool}" "${label}" "${args[@]}" || gotStatus=$?
        after="${PLAN_RUN_DIR}/after-${tool}-${label}.txt"
        # `list` always exits 0. The dry run exits 1, with nothing on stdout, when nothing is
        # running to freeze ("nothing matched"), which a machine whose containers stopped since
        # the baseline legitimately answers.
        if [[ "${gotStatus}" -eq 0 ]] || [[ "${label}" == "dry" && "${gotStatus}" -eq 1 && ! -s "${after}" ]]; then
            check "${tool} ${args[*]} exits ${gotStatus}" yes
        else
            check "${tool} ${args[*]} exits 0 (got ${gotStatus}; stderr: ${PLAN_RUN_DIR}/after-${tool}-${label}.err)" no
            continue
        fi
        # The containers differ from the baseline's on any live machine, so the rows are not
        # compared; the layout is (freeze-layout.bash). The bash tools' recorded output for an
        # empty machine and a full dry run also vouches for a layout line.
        layoutStatus=0
        "${PLAN_SCRIPT_DIR}/freeze-layout.bash" "${before}" "${after}" \
            "${GOLDEN_DIR}/${GOLDEN_PREFIX[${tool}]}-list-empty/stdout.golden" \
            "${GOLDEN_DIR}/${GOLDEN_PREFIX[${tool}]}-dry-run-all/stdout.golden" \
            >"${PLAN_RUN_DIR}/layout-${tool}-${label}-report.txt" || layoutStatus=$?
        case "${layoutStatus}" in
            0) check "${tool} ${args[*]} keeps the bash tool's layout" yes ;;
            1) check "${tool} ${args[*]} keeps the bash tool's layout (findings: ${PLAN_RUN_DIR}/layout-${tool}-${label}-report.txt)" no ;;
            *)
                printf '[FATAL] freeze-layout.bash failed with status %s\n' "${layoutStatus}" >&2
                exit 1
                ;;
        esac
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
