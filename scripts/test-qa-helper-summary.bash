#!/usr/bin/env bash
# Unit-test scripts/lib/qa-helper-summary.bash — Plan 00125, Task 4.2.
#
# WHY THIS EXISTS. The `helper-tests` stage line has been wrong twice and revised three
# times, and every revision was verified by hand and then thrown away:
#
#   1. It printed only `Ran N tests`, which `unittest` produces identically whether a test
#      asserted or skipped itself — testsRun COUNTS skips. Two machines running different
#      subsets of the same suite emitted the same sentence.
#   2. The skip count was matched as `\(skipped=[0-9]+\)`, which misses
#      `OK (skipped=1, expected failures=1)` and silently reports 0.
#   3. The match ran over the WHOLE capture, and bash `=~` takes the first hit anywhere, so
#      any earlier `skipped=<digits>` won. `tests/helpers/vmtest/test_transcript.py` holds
#      exactly that text in a fixture, and a test writing to stderr reaches the capture
#      ahead of the summary.
#
# All three have the same shape and it is this plan's subject: **a check whose clean result
# is indistinguishable from a blind one**. That is precisely the property a hand-check
# cannot hold on to, because the next reviser sees the line and not the reasoning. Hence a
# committed test that drives the REAL function — not a copy of the expression, which could
# not catch a change to the thing it was copying.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB="$REPO_ROOT/scripts/lib/qa-helper-summary.bash"

if [ ! -f "$LIB" ]; then
    echo "FAIL: qa-helper-summary.bash not found at $LIB" >&2
    exit 1
fi

# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/qa-helper-summary.bash
source "$LIB"

for fn in helper_test_summary helper_skip_count; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: ${fn} is not defined after sourcing the library" >&2
        exit 1
    fi
done

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# refuses <label> <capture> — the function must FAIL on this input rather than answer it,
# AND write nothing to stdout while doing so. stderr is sent to /dev/null on purpose: the
# assertion is about the payload channel, and a diagnostic printed to the test's own stderr
# would be indistinguishable from a failure report. Which channel it lands on is asserted
# separately below, where it is the subject rather than the noise.
refuses() {
    local label="$1" input="$2" out="" rc=0
    out="$(helper_skip_count "$input" 2>/dev/null)" || rc=$?
    if [ "$rc" -eq 0 ]; then
        failed=$((failed + 1))
        printf '  FAIL  %s\n        answered %s instead of failing\n' "$label" "$out" >&2
        return
    fi
    if [ -n "$out" ]; then
        failed=$((failed + 1))
        printf '  FAIL  %s\n        wrote %s to stdout while failing\n' "$label" "$out" >&2
        return
    fi
    passed=$((passed + 1))
    printf '  PASS  %s\n' "$label"
}

# A capture in the shape qa-helper-tests.bash really produces: unittest's progress output,
# a blank line, then the result line last.
capture() {
    printf '%s\n\n%s\n' "${2:-Ran 5 tests in 0.006s}" "$1"
}

echo "=== helper_skip_count: every shape unittest's result line can take ==="

check "a clean run reports no skips" \
    "0" "$(helper_skip_count "$(capture 'OK')")"

check "a skip is reported" \
    "1" "$(helper_skip_count "$(capture 'OK (skipped=1)')")"

# The round-2 defect. unittest appends `expected failures=` and `unexpected successes=`
# AFTER the skip count inside the SAME bracket, so a pattern anchored on `skipped=N)`
# silently reports 0 — restoring exactly the blindness the line exists to remove.
check "an expected failure after the skip count does not hide it" \
    "1" "$(helper_skip_count "$(capture 'OK (skipped=1, expected failures=1)')")"

check "an unexpected success after the skip count does not hide it" \
    "2" "$(helper_skip_count "$(capture 'OK (skipped=2, unexpected successes=1)')")"

check "a skip count that follows a failure count is found" \
    "3" "$(helper_skip_count "$(capture 'FAILED (failures=1, skipped=3)')")"

check "an error count before the skip count does not hide it" \
    "4" "$(helper_skip_count "$(capture 'FAILED (errors=2, skipped=4)')")"

check "a two-digit count is not truncated" \
    "12" "$(helper_skip_count "$(capture 'OK (skipped=12)')")"

echo "=== helper_skip_count: the count comes from the RESULT line, not the capture ==="

# The round-3 defect, and reachable rather than theoretical: a test writing to STDERR is
# unbuffered, so printing one of the repo's own transcript fixtures puts this text into the
# capture ahead of the summary. (A test writing to STDOUT lands after it instead — both
# directions are covered, because the count must survive either.)
check "an earlier line mentioning skipped= does not win the match" \
    "2" "$(helper_skip_count "$(capture 'OK (skipped=2)' \
        'VMTEST-CHECKS-DONE passed=7 skipped=41
Ran 5 tests in 0.006s')")"

# The quiet direction of the same bug, and the worse one: a decoy reading zero puts two
# machines with different skip counts back on an identical line.
check "an earlier skipped=0 does not silently zero the count" \
    "1" "$(helper_skip_count "$(capture 'OK (skipped=1)' \
        'VMTEST-CHECKS-DONE passed=7 skipped=0
Ran 5 tests in 0.006s')")"

check "a skip REASON containing the text does not win the match" \
    "1" "$(helper_skip_count "$(capture 'OK (skipped=1)' \
        "test_x (m.T) ... skipped 'no DP connector; skipped=42'
Ran 5 tests in 0.006s")")"

# The other direction, which the three cases above cannot reach. A STDOUT decoy is
# block-buffered to a pipe and flushed at exit, so it arrives AFTER the result line — and a
# decoy that is itself shaped like a result line would be a second `^OK` match.
check "a result-shaped decoy flushed after the real one does not win" \
    "1" "$(helper_skip_count "$(printf 'Ran 5 tests in 0.006s\n\nOK (skipped=1)\nOK (skipped=99)\n')")"

echo "=== helper_skip_count: an unreadable capture is refused, not reported as zero ==="

# The whole point of this line is that a clean result must not resemble a blind one.
# Answering 0 for "there is no result line to read" would be that same defect a fourth
# time, so the function fails and the caller stops.
refuses "a capture with no result line is refused" "Ran 5 tests in 0.006s"
refuses "an empty capture is refused" ""
refuses "prose merely containing the word OK is refused" "everything looks OK to me"

# The channel, asserted rather than assumed: a diagnostic on stdout would be captured by
# `$(...)` at the call site and printed as if it were the count. The non-zero exit is
# expected here and is what the `refuses` cases above already assert, so it is consumed
# explicitly rather than hidden.
diag_rc=0
diag_stderr="$(helper_skip_count "no result line here" 2>&1 1>/dev/null)" || diag_rc=$?
if [ "$diag_rc" -eq 0 ]; then
    failed=$((failed + 1))
    echo "  FAIL  the refusal diagnostic goes to stderr (it did not refuse at all)" >&2
elif [[ "$diag_stderr" == *"no unittest result line"* ]]; then
    passed=$((passed + 1))
    echo "  PASS  the refusal diagnostic goes to stderr"
else
    failed=$((failed + 1))
    printf '  FAIL  the refusal diagnostic goes to stderr\n        got: %s\n' \
        "$diag_stderr" >&2
fi

echo "=== helper_test_summary ==="

check "the test count is taken from unittest's own line" \
    "Ran 1456 tests" "$(helper_test_summary "$(capture 'OK (skipped=1)' 'Ran 1456 tests in 0.42s')")"

check "a single test is not pluralised into a mismatch" \
    "Ran 1 test" "$(helper_test_summary "$(capture 'OK' 'Ran 1 test in 0.01s')")"

check "a capture with no Ran line degrades to a word rather than a wrong number" \
    "passed" "$(helper_test_summary 'OK')"

# Round 4 found this one function away from the scoping fix above, with the same cause:
# `grep -o` returns EVERY match, so an unscoped search over the whole capture emitted both.
# The consequence is worse than a wrong number — the stage line became TWO lines, and the
# verdict parser reads the first as the stage and loses the second.
check "a decoy Ran line mid-capture does not join the summary" \
    "Ran 1464 tests" "$(helper_test_summary "$(printf 'VMTEST-CHECKS-DONE Ran 3 tests in a scenario\nRan 1464 tests in 0.42s\n\nOK (skipped=1)\n')")"

check "the summary is exactly one line" \
    "1" "$(helper_test_summary "$(printf 'Ran 3 tests in a scenario\nRan 1464 tests in 0.42s\n\nOK\n')" | grep -c '')"

# THE ORDERING THAT ACTUALLY OCCURS, and the one the fixtures above cannot produce.
# unittest writes its summary to STDERR; a test's `print` goes to STDOUT, which Python
# BLOCK-BUFFERS when it is a pipe — and `qa-all.bash` captures through a pipe. So a decoy is
# flushed at process exit and lands AFTER `OK`, never before it. Measured, not reasoned:
#
#   3: Ran 1 test in 0.000s
#   5: OK
#   6: Ran 3 tests in a scenario     <- the decoy, after everything
#
# A "last match wins" rule therefore picks the decoy in the only ordering that is real. The
# rule that holds is the `Ran` line most recently seen WHEN the result line arrives, which
# anchors on the same `^(OK|FAILED)` line `helper_skip_count` already trusts.
check "a decoy flushed AFTER the result line does not win" \
    "Ran 1 test" "$(helper_test_summary "$(printf '.\n---\nRan 1 test in 0.000s\n\nOK\nRan 3 tests in a scenario\n')")"

check "a decoy on stderr BEFORE unittest's own line does not win either" \
    "Ran 1464 tests" "$(helper_test_summary "$(printf 'Ran 3 tests in a scenario\nRan 1464 tests in 0.42s\n\nOK\n')")"

check "a decoy after a FAILED result line does not win" \
    "Ran 9 tests" "$(helper_test_summary "$(printf 'Ran 9 tests in 0.1s\n\nFAILED (failures=1)\nRan 3 tests in a scenario\n')")"

echo
echo "passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
