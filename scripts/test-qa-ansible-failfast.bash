#!/usr/bin/env bash
# Unit-test the fail-fast directive pattern in scripts/qa-ansible.bash (Plan 00081 F10).
#
# WHY THIS EXISTS: F10 fixed a regex that accepted `yes` for ignore_errors only and
# `false` (never `no`) for failed_when, so `failed_when: no` earned a green tick on
# this repo's #1 rule. The fix shipped with NO test — reverting FF_FALSEY/FF_TRUTHY
# to the old single-spelling regex turned nothing red anywhere in the repo. A gate
# whose own correctness nothing checks is the shape this plan is about.
#
# It reads the three FF_* definitions OUT of qa-ansible.bash rather than restating
# them. A copy would pass while the shipped gate was broken, which is precisely the
# defect the 00079 review found in that plan's unit test.
#
# Usage: ./scripts/test-qa-ansible-failfast.bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="$SCRIPT_DIR/qa-ansible.bash"

if [ ! -f "$GATE" ]; then
    echo "ERROR: $GATE not found — nothing to test." >&2
    exit 2
fi

# Pull the definitions from the real file. A missing definition is a HARD failure:
# evaluating nothing would leave FF_PATTERN unset and every case would "pass".
FF_DEFS="$(grep -E '^FF_(FALSEY|TRUTHY|TEMPLATED|PATTERN)=' "$GATE")" || FF_DEFS=""
if [ -z "$FF_DEFS" ]; then
    echo "ERROR: no FF_* definitions found in $GATE." >&2
    echo "  They were renamed or removed; this test cannot vouch for a pattern it" >&2
    echo "  could not read. Update the test rather than deleting it." >&2
    exit 2
fi
DEF_COUNT="$(printf '%s\n' "$FF_DEFS" | wc -l)"
if [ "$DEF_COUNT" -lt 4 ]; then
    echo "ERROR: expected 4 FF_* definitions in $GATE, read $DEF_COUNT." >&2
    exit 2
fi
eval "$FF_DEFS"

PASS=0
FAIL=0

# expect_match <should-match: yes|no> <label> <line>
expect_match() {
    local want="$1" label="$2" line="$3" got="no"
    if printf '%s\n' "$line" | grep -qiE -- "$FF_PATTERN"; then
        got="yes"
    fi
    if [ "$got" = "$want" ]; then
        printf '  ok    %s\n' "$label"
        PASS=$((PASS + 1))
    else
        printf '  FAIL  %s — wanted match=%s, got match=%s\n' "$label" "$want" "$got"
        FAIL=$((FAIL + 1))
    fi
}

echo "== fail-fast directive pattern, read from qa-ansible.bash =="
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
# `no` inside `not` is the real regression: without \b this reported 10 violations
# against legitimate probes, and an unsatisfiable gate gets bypassed.
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
