# shellcheck shell=bash
#
# Paths for Plan 00099's falsification harnesses — SOURCED, never executed.
#
# These harnesses lived under untracked/scratch/ and carried the container's absolute path
# as a literal. Two problems: `untracked/` is gitignored wholesale, so the evidence this
# plan's every claim rests on was in no diff, on no other clone, and would not travel into
# Completed/ with the plan it belongs to — and a checkout path is exactly what must not
# appear in a public repo. Both are fixed by living here and resolving from here.
#
# Resolution is script-relative and bounded, the same walk PlanScriptStandards R1 requires
# of plan scripts: `git rev-parse --show-toplevel` answers about the CWD, not the script, so
# a harness run by path from another repo resolves to that repo and tests its files.

_falsify_script_dir="$(cd "$(dirname "${BASH_SOURCE[1]}")" && pwd -P)"
REPO_ROOT="${_falsify_script_dir}"
while [[ "${REPO_ROOT}" != "/" ]] && [[ ! -e "${REPO_ROOT}/ansible.cfg" ]]; do
    if [[ -e "${REPO_ROOT}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' \
            "${_falsify_script_dir}" "${REPO_ROOT}" >&2
        exit 1
    fi
    REPO_ROOT="$(dirname "${REPO_ROOT}")"
done
if [[ ! -e "${REPO_ROOT}/ansible.cfg" ]]; then
    printf '[FATAL] no ansible.cfg above %s\n' "${_falsify_script_dir}" >&2
    exit 1
fi

PLAN_DIR="$(dirname "${_falsify_script_dir}")"
# Exported because the consumer is the SOURCING script, not this file — without it every
# one of these reads as unused here, and a suppression is not available (R11).
export GATE TRIAGE RC_LIB_SRC
GATE="${PLAN_DIR}/acceptance.bash"
TRIAGE="${PLAN_DIR}/triage.bash"
BIN_SRC="${REPO_ROOT}/files/home/.local/bin"
RC_LIB_SRC="${BIN_SRC}/rclone-rc-auth.bash"

# Mutants and scratch trees are written HERE, under untracked/, and never beside the plan's
# tracked files: a mutant is a deliberately broken copy of a gate, and one left behind in a
# plan folder is indistinguishable from the real thing to anyone reading later.
FALSIFY_SCRATCH="${REPO_ROOT}/untracked/scratch"
mkdir -p "${FALSIFY_SCRATCH}"
