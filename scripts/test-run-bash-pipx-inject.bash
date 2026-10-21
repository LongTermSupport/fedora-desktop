#!/usr/bin/env bash
# Unit-test run.bash's pipx inject step (Plan 00188, #54): a re-run with every Ansible
# dependency already injected injects nothing and prints one line; a missing one is injected
# alone; a failed inject, and a `pipx list --json` that cannot be read, stop the run.
#
# The step is inline in main(), so it is cut out with awk (from the line after the
# `pipx install` block's `fi` to the ansible-lint symlink) and run under main()'s own shell
# options. pipx is a stub; jq is the real one.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_BASH="${RUN_BASH_UNDER_TEST:-$REPO_ROOT/run.bash}"

if ! command -v jq >/dev/null; then
    echo "FAIL: jq is required by the code under test and is not installed" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

step="$(awk '/pipx install --include-deps ansible/ {s=1; next}
    s==1 && /^fi$/ {s=2; next}
    s==2 && /ln -sf .*ansible-lint/ {exit}
    s==2 {print}' "$RUN_BASH")"
if ! grep -q 'pipx' <<<"$step"; then
    echo "FAIL: could not extract the pipx inject step from ${RUN_BASH}" >&2
    exit 1
fi
{
    cat <<'EOF'
set -e
set -u
set -o pipefail
IFS=$'\n\t'
success(){ echo "SUCCESS $1"; }
info(){ echo "INFO $1"; }
EOF
    printf '%s\n' "$step"
    printf 'echo END\n'
} >"$work/step.bash"

mkdir -p "$work/bin"
cat >"$work/bin/pipx" <<'EOF'
#!/usr/bin/env bash
case "$1" in
    list)
        [ "$*" = "list --json" ] || { echo "stub pipx: unexpected: $*" >&2; exit 97; }
        cat "$TEST_LIST_OUT"
        exit "${TEST_LIST_RC:-0}"
        ;;
    inject)
        echo "$*" >>"$TEST_LOG"
        exit "${TEST_INJECT_RC:-0}"
        ;;
esac
echo "stub pipx: unexpected: $*" >&2
exit 97
EOF
chmod 755 "$work/bin/pipx"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %q\n        got:  %q\n' "$label" "$want" "$got" >&2
    fi
}

# venv_json <injected package names...>: pipx list --json with an ansible venv
venv_json() {
    local injected='{}' p
    for p in "$@"; do
        injected="$(jq -c --arg p "$p" '. + {($p): {package: $p}}' <<<"$injected")"
    done
    jq -n --argjson inj "$injected" \
        '{pipx_spec_version: "0.1", venvs: {ansible: {metadata: {main_package: {package: "ansible"}, injected_packages: $inj}}}}'
}

# run_step <list output> [env assignments...]
run_step() {
    printf '%s' "$1" >"$work/list.out"
    shift
    : >"$work/inject.log"
    out="$(env PATH="$work/bin:/usr/bin:/bin" TEST_LIST_OUT="$work/list.out" TEST_LOG="$work/inject.log" "$@" \
        bash "$work/step.bash" 2>&1)"
    rc=$?
    injects="$(tr '\n' ';' <"$work/inject.log")"
}
reached_end() { if grep -qx END <<<"$out"; then echo yes; else echo no; fi; }

echo "=== every dependency already injected ==="
run_step "$(venv_json jmespath passlib ansible-lint)"
check "exits 0" "0" "$rc"
check "injects nothing" "" "$injects"
check "prints exactly one line before carrying on" "1" "$(grep -cv '^END$' <<<"$out")"
check "that line is a success line" "yes" "$(if grep -q '^SUCCESS ' <<<"$out"; then echo yes; else echo no; fi)"
check "carries on" "yes" "$(reached_end)"

echo "=== one dependency missing ==="
run_step "$(venv_json jmespath ansible-lint)"
check "exits 0" "0" "$rc"
check "injects only the missing one" "inject ansible passlib;" "$injects"
check "carries on" "yes" "$(reached_end)"

echo "=== nothing injected yet ==="
run_step "$(venv_json)"
check "injects all three, one at a time" "inject ansible jmespath;inject ansible passlib;inject ansible ansible-lint;" "$injects"
check "carries on" "yes" "$(reached_end)"

echo "=== a failed inject stops the run ==="
run_step "$(venv_json jmespath)" TEST_INJECT_RC=3
check "exits non-zero" "yes" "$(if [ "$rc" -ne 0 ]; then echo yes; else echo no; fi)"
check "stops at the first failure" "inject ansible passlib;" "$injects"
check "does not carry on" "no" "$(reached_end)"

for case in "not JSON|garbage" "empty output|" "no ansible venv|{\"venvs\": {}}"; do
    echo "=== unreadable pipx list output: ${case%%|*} ==="
    run_step "${case#*|}"
    check "exits non-zero" "yes" "$(if [ "$rc" -ne 0 ]; then echo yes; else echo no; fi)"
    check "injects nothing, never assumes present or missing" "" "$injects"
    check "does not carry on" "no" "$(reached_end)"
done

echo "=== pipx list itself fails ==="
run_step "$(venv_json jmespath passlib ansible-lint)" TEST_LIST_RC=1
check "exits non-zero" "yes" "$(if [ "$rc" -ne 0 ]; then echo yes; else echo no; fi)"
check "does not carry on" "no" "$(reached_end)"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
