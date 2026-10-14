#!/usr/bin/env bash
# Black-box test of the fail-fast directive pattern of the qa-ansible tool (Plan 00081 F10).
#
# WHY THIS EXISTS: F10 fixed a regex that accepted `yes` for ignore_errors only and `false`
# (never `no`) for failed_when, so `failed_when: no` earned a green tick on this repo's #1
# rule. A gate whose own correctness nothing checks is the shape that plan is about.
#
# The pattern is Go now (src-go/qa/ansible/failfast.go) and its unit test holds the same cases;
# this suite builds the shipped command once and drives it over one-line fixture trees, so the
# path from the tool to a verdict is exercised as well. A case that should match must make the
# tool exit 1, one that should not must make it exit 0.
#
# Usage: ./scripts/test-qa-ansible-failfast.bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QA_DIR="$SCRIPT_DIR/../src-go/qa"

if ! command -v go > /dev/null; then
    echo "ERROR: go not installed (install via playbooks/imports/play-golang.yml)" >&2
    exit 2
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
go build -C "$QA_DIR" -o "$WORK/qa-ansible" ./cmd/qa-ansible

# A tree the tool judges clean: one valid general-scope playbook.
TREE="$WORK/tree"
mkdir -p "$TREE/playbooks" "$TREE/tasks"
{
    echo "#!/usr/bin/env -S bash -c 'p=\$(realpath \"\$0\") && exec \"\${p%/playbooks/*}/run.bash\" \"\$p\" \"\$@\"'"
    printf -- '---\n- hosts: all\n  vars:\n    scope: general\n  tasks: []\n'
} > "$TREE/playbooks/play.yml"
chmod +x "$TREE/playbooks/play.yml"

PASS=0
FAIL=0

# expect_match <should-match: yes|no> <label> <line>
expect_match() {
    local want="$1" label="$2" line="$3" got="no" status=0
    printf '%s\n' "$line" > "$TREE/tasks/case.yml"
    QA_JSON_OUT="$WORK/out.json" "$WORK/qa-ansible" --root "$TREE" > /dev/null || status=$?
    case "$status" in
        0) got="no" ;;
        1) got="yes" ;;
        *)
            echo "ERROR: the tool could not run (exit $status) on case: $label" >&2
            exit 2
            ;;
    esac
    if [ "$got" = "$want" ]; then
        printf '  ok    %s\n' "$label"
        PASS=$((PASS + 1))
    else
        printf '  FAIL  %s — wanted match=%s, got match=%s\n' "$label" "$want" "$got"
        FAIL=$((FAIL + 1))
    fi
}

echo "== fail-fast directive pattern, through the built qa-ansible =="
echo

echo "### every falsey spelling of failed_when is caught"
expect_match yes "failed_when: false" "      failed_when: false"
expect_match yes "failed_when: no" "      failed_when: no"
expect_match yes "failed_when: off" "      failed_when: off"
expect_match yes "failed_when: NO (case)" "      failed_when: NO"

echo
echo "### every truthy spelling of ignore_errors is caught"
expect_match yes "ignore_errors: true" "      ignore_errors: true"
expect_match yes "ignore_errors: yes" "      ignore_errors: yes"
expect_match yes "ignore_errors: on" "      ignore_errors: on"

echo
echo "### ignore_unreachable is held to the SAME list, which is the F10 asymmetry"
expect_match yes "ignore_unreachable: true" "      ignore_unreachable: true"
expect_match yes "ignore_unreachable: yes" "      ignore_unreachable: yes"
expect_match yes "ignore_unreachable: on" "      ignore_unreachable: on"

echo
echo "### a templated value is unverifiable under EITHER key"
expect_match yes 'ignore_errors: "{{ x }}"' '      ignore_errors: "{{ maybe }}"'
expect_match yes 'ignore_unreachable: "{{ x }}"' '      ignore_unreachable: "{{ maybe }}"'
expect_match yes 'ignore_errors: {{ x }} unquoted' '      ignore_errors: {{ maybe }}'

echo
echo '### negative controls — the over-match the trailing word boundary exists to stop'
expect_match no "failed_when: not foo.stat.exists" "      failed_when: not foo.stat.exists"
expect_match no "failed_when: nothing_matched" "      failed_when: nothing_matched"
expect_match no "failed_when: true (a real condition)" "      failed_when: true"
expect_match no "ignore_errors: false (explicitly off)" "      ignore_errors: false"
expect_match no "an unrelated key" "      changed_when: false"

echo
echo "=============================================================="
printf 'passed: %d   failed: %d\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
    echo "VERDICT: FAIL — the fail-fast pattern does not classify these correctly."
    exit 1
fi
echo "VERDICT: PASS"
echo "=============================================================="
