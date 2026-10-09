#!/usr/bin/env bash
# Unit-test the entrypoint's git preflight: the container's git must read /workspace.
#
# WHY THIS TEST EXISTS. A host git can write a repository extension an older container git
# refuses, and sessions then started with no git and no hooks daemon, silently (Plan 00169).
# The test cuts the step out of the real entrypoint (between its GIT-PREFLIGHT markers),
# points it at a throwaway directory, and runs it against a readable repository, one with an
# extension no git knows, and a directory that is not a repository. Any git refuses an
# unknown extension under core.repositoryformatversion=1, so the refusal case does not
# depend on which git runs the test.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT="$SCRIPT_DIR/../files/var/local/claude-yolo/entrypoint.sh"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}
yes_if() { if "$@"; then echo yes; else echo no; fi; }

step="$work/step.bash"
awk -v dir="$work/ws" '
    /^# >>> GIT-PREFLIGHT/ { on = 1; next }
    /^# <<< GIT-PREFLIGHT/ { on = 0 }
    on { gsub("/workspace", dir); print }
' "$ENTRYPOINT" >"$step"
if [ ! -s "$step" ]; then
    echo "FAIL: no GIT-PREFLIGHT block found in $ENTRYPOINT"
    exit 1
fi

# The runner sources the step under set -e, as the entrypoint runs it, then marks that the
# entrypoint would carry on.
runner="$work/runner.bash"
printf 'set -e\n. %q\necho after\n' "$step" >"$runner"

# run_step: run the step with a clean git environment so the caller's repository cannot
# leak in. Writes stdout, stderr and the status to files.
run_step() {
    env -u GIT_DIR -u GIT_WORK_TREE GIT_CEILING_DIRECTORIES="$work" \
        bash "$runner" >"$work/out" 2>"$work/err"
    echo $? >"$work/status"
}

echo "=== a repository the container git reads: the session goes on ==="
git init -q "$work/ws"
run_step
check "exit status 0" "0" "$(cat "$work/status")"
check "the entrypoint carries on past the step" "yes" "$(yes_if grep -qx after "$work/out")"
check "it names the git version that read it" "yes" "$(yes_if grep -q '^✓ git version .* reads ' "$work/out")"
check "nothing on stderr" "" "$(cat "$work/err")"

echo "=== a repository with an extension this git does not know: the session stops ==="
git -C "$work/ws" config core.repositoryformatversion 1
git -C "$work/ws" config extensions.ccyPreflightTestUnknown true
run_step
check "exit status 1" "1" "$(cat "$work/status")"
check "the entrypoint does not carry on" "no" "$(yes_if grep -qx after "$work/out")"
check "stderr names the container's git version" "yes" \
    "$(yes_if grep -q "^ERROR: the container's git (git version .*) cannot read " "$work/err")"
check "stderr carries git's own message verbatim" "yes" \
    "$(yes_if grep -q '^fatal: unknown repository extension' "$work/err")"
check "git's message names the extension" "yes" \
    "$(yes_if grep -qi 'ccypreflighttestunknown' "$work/err")"
check "nothing on stdout" "" "$(cat "$work/out")"

echo "=== a directory that is not a repository: the session stops ==="
rm -rf "$work/ws"
mkdir "$work/ws"
run_step
check "exit status 1" "1" "$(cat "$work/status")"
check "stderr carries git's not-a-repository message" "yes" \
    "$(yes_if grep -qi 'not a git repository' "$work/err")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
