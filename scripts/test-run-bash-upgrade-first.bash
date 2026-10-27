#!/usr/bin/env bash
# Unit-test that run.bash upgrades every package before any Ansible process starts (Plan 00229
# Task 2.2). An upgrade of python3 under a running ansible-playbook replaces the interpreter's
# extension modules, and the run dies at the first later import of one ("name 'ssl' is not
# defined"). So the upgrade is a plain dnf run in run.bash's dependency step, finished before
# pipx, ansible-galaxy and the main playbook start; a failed upgrade stops the run there.
#
# The step is inline in the main flow (the developer and the headless path share it), so it is
# cut out with awk (from its title to the next `completed`) and run under the main flow's shell
# options with a stub _sudo. The order against the Ansible steps is checked on the real file.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_BASH="${RUN_BASH_UNDER_TEST:-$REPO_ROOT/run.bash}"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

step="$(awk '/^title "Installing System Dependencies"$/ {s=1; next}
    s==1 && /^completed$/ {exit}
    s==1 {print}' "$RUN_BASH")"
if ! grep -q '_sudo dnf' <<<"$step"; then
    echo "FAIL: could not extract the dependency step from ${RUN_BASH}" >&2
    exit 1
fi
{
    cat <<'EOF'
set -e
set -u
set -o pipefail
IFS=$'\n\t'
info(){ echo "INFO $1"; }
_sudo(){
    local IFS=' '
    echo "$*" >>"$TEST_LOG"
    if [[ "$*" == "dnf -y upgrade"* ]]; then return "${TEST_UPGRADE_RC:-0}"; fi
    return 0
}
EOF
    printf '%s\n' "$step"
    printf 'echo END\n'
} >"$work/step.bash"

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

# run_step [env assignments...]
run_step() {
    : >"$work/sudo.log"
    out="$(env TEST_LOG="$work/sudo.log" "$@" bash "$work/step.bash" 2>&1)"
    rc=$?
    calls="$(tr '\n' ';' <"$work/sudo.log")"
}
reached_end() { if grep -qx END <<<"$out"; then echo yes; else echo no; fi; }

echo "=== the upgrade runs first, then the install ==="
run_step
check "exits 0" "0" "$rc"
check "upgrades every package with fresh metadata, then installs the dependencies" \
    "dnf -y upgrade --refresh;dnf -y install git python3 python3-pip python3-libdnf5 grubby jq openssl pipx golang;" "$calls"
check "carries on" "yes" "$(reached_end)"

echo "=== a failed upgrade stops the run ==="
run_step TEST_UPGRADE_RC=1
check "exits non-zero" "yes" "$(if [ "$rc" -ne 0 ]; then echo yes; else echo no; fi)"
check "installs nothing after it" "dnf -y upgrade --refresh;" "$calls"
check "does not carry on" "no" "$(reached_end)"

echo "=== in run.bash, the upgrade comes before every Ansible step ==="
# first_line <ERE>: the first line of run.bash's top level (unindented) matching it, or 0.
first_line() { awk -v re="$1" '$0 ~ re {print NR; found=1; exit} END {if (!found) print 0}' "$RUN_BASH"; }
upgrade_at="$(first_line '^_sudo dnf -y upgrade --refresh$')"
check "run.bash has the upgrade at its top level" "yes" "$(if [ "$upgrade_at" -gt 0 ]; then echo yes; else echo no; fi)"
for ansible_step in '^  pipx install --include-deps ansible$' \
    '^if ! ansible-galaxy install ' \
    '^  ansible-playbook playbooks/playbook-main.yml '; do
    step_at="$(first_line "$ansible_step")"
    check "found: ${ansible_step}" "yes" "$(if [ "$step_at" -gt 0 ]; then echo yes; else echo no; fi)"
    check "the upgrade is before: ${ansible_step}" "yes" \
        "$(if [ "$upgrade_at" -gt 0 ] && [ "$upgrade_at" -lt "$step_at" ]; then echo yes; else echo no; fi)"
done

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
