#!/usr/bin/env bash
# Tests for CLAUDE/Plan/meta-deploy.bash reading its queue from CLAUDE/Plan/meta-deploy.list.
#
# Each case copies the script into a throwaway repo tree (ansible.cfg and .git markers, one
# executable plan folder) beside a list file written for the case, and runs only --list, so
# nothing is deployed. Safe in the CCY container.
#
#   scripts/test-meta-deploy.bash
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "${HERE}/.." && pwd -P)"
SCRATCH="$(mktemp -d)"
trap 'rm -rf "${SCRATCH}"' EXIT
FAILS=0
PASSED=0

mkdir -p "${SCRATCH}/.git" "${SCRATCH}/CLAUDE/Plan/00001-demo"
: >"${SCRATCH}/ansible.cfg"
cp "${REPO_ROOT}/CLAUDE/Plan/meta-deploy.bash" "${SCRATCH}/CLAUDE/Plan/"
printf '#!/usr/bin/env bash\n' >"${SCRATCH}/CLAUDE/Plan/00001-demo/deploy.bash"
chmod +x "${SCRATCH}/CLAUDE/Plan/00001-demo/deploy.bash"

# check <name> <expected exit> <expected output fragment> <actual exit> <actual output>
check() {
    if [[ "$4" == "$2" && "$5" == *"$3"* ]]; then
        printf 'PASS  %s\n' "$1"
        PASSED=$((PASSED + 1))
    else
        printf 'FAIL  %s (exit %s, wanted %s, wanted output containing: %s)\n%s\n' "$1" "$4" "$2" "$3" "$5"
        FAILS=$((FAILS + 1))
    fi
}

# run_case <name> <expected exit> <expected output fragment> <list content>
run_case() {
    local out status
    printf '%s' "$4" >"${SCRATCH}/CLAUDE/Plan/meta-deploy.list"
    out="$("${SCRATCH}/CLAUDE/Plan/meta-deploy.bash" --list 2>&1)"
    status=$?
    check "$1" "$2" "$3" "${status}" "${out}"
}

run_case "comments and blank lines are ignored" 0 "1 plan(s):" $'# why\n\n00001-demo\n# more\n'
run_case "words after a plan folder are shown" 0 "00001-demo --flag" $'00001-demo --flag\n'
run_case "empty queue is reported plainly" 0 "queue is empty" $'# nothing pending\n\n'
run_case "missing plan folder is fatal" 1 "nothing runnable" $'00001-demo\n00002-gone\n'
run_case "missing plan script is fatal" 1 "nothing runnable" $'00001-demo/nope.bash\n'
run_case "missing playbook is fatal" 1 "nothing runnable" $'playbooks/imports/nope.yml\n'
run_case "absolute path is malformed, names the line" 1 "line 3" $'# c\n00001-demo\n/etc/passwd\n'
run_case "parent traversal is malformed, names the line" 1 "line 2" $'00001-demo\n../x\n'
run_case "trailing comment is malformed, names the line" 1 "line 1" $'00001-demo # why\n'
run_case "words after a script entry are malformed" 1 "line 1" $'00001-demo/deploy.bash extra\n'

# The run path, on harmless fixture scripts: a failing unit must fail the batch and be named.
printf '#!/usr/bin/env bash\nexit 3\n' >"${SCRATCH}/CLAUDE/Plan/00001-demo/acceptance.bash"
chmod +x "${SCRATCH}/CLAUDE/Plan/00001-demo/acceptance.bash"
printf '00001-demo/acceptance.bash\n' >"${SCRATCH}/CLAUDE/Plan/meta-deploy.list"
out="$("${SCRATCH}/CLAUDE/Plan/meta-deploy.bash" 2>&1)"
check "a failing unit fails the batch and is named" 1 "FAIL  00001-demo-acceptance.bash (exit 3)" "$?" "${out}"
printf '00001-demo\n' >"${SCRATCH}/CLAUDE/Plan/meta-deploy.list"
out="$("${SCRATCH}/CLAUDE/Plan/meta-deploy.bash" 2>&1)"
check "a passing deploy then a failing acceptance is named" 1 "FAIL  00001-demo/acceptance.bash (exit 3)" "$?" "${out}"

rm -f "${SCRATCH}/CLAUDE/Plan/meta-deploy.list"
out="$("${SCRATCH}/CLAUDE/Plan/meta-deploy.bash" --list 2>&1)"
check "missing list file is fatal" 1 "meta-deploy.list" "$?" "${out}"

: >"${SCRATCH}/CLAUDE/Plan/meta-deploy.list"
out="$("${SCRATCH}/CLAUDE/Plan/meta-deploy.bash" --list 2>&1)"
check "an empty list says nothing to run" 0 "Nothing to run" "$?" "${out}"

# The real queue is valid in either state. An entry present must show the --list summary; an
# empty queue must say it has nothing to run. The state is read with the script's own rule:
# a line that is not blank and not a whole-line comment is an entry.
REAL_LIST="${REPO_ROOT}/CLAUDE/Plan/meta-deploy.list"
if grep -qvE '^[[:space:]]*(#.*)?$' "${REAL_LIST}"; then
    REAL_WANT="--list changed nothing"
else
    REAL_WANT="Nothing to run"
fi
out="$("${REPO_ROOT}/CLAUDE/Plan/meta-deploy.bash" --list 2>&1)"
check "the real list validates" 0 "${REAL_WANT}" "$?" "${out}"

if [[ "${FAILS}" -gt 0 ]]; then
    printf '%d case(s) failed\n' "${FAILS}" >&2
    exit 1
fi
printf 'passed: %d\n' "${PASSED}"
