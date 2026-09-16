#!/usr/bin/env bash
# Read the `helper-tests` stage line out of a captured `python3 -m unittest` run.
# Plan 00125, Task 4.2. Driven by scripts/test-qa-helper-summary.bash.
#
# Sourced, never executed — it defines functions and runs nothing.
#
# This is two lines of text handling that lives in its own file because it has been wrong
# twice: `unittest` counts a SKIPPED test inside testsRun, so `Ran 1456 tests` is
# byte-identical whether a test asserted or skipped itself, and two machines running
# different subsets of the same suite printed the same sentence for weeks. The skip count
# is what distinguishes them, which makes every way of reading it wrongly a way of putting
# the blindness back. The test file records each one.

# helper_test_summary <capture> — `Ran N tests`, or the word `passed` if unittest's count
# line is absent. Degrading to a WORD rather than a number is deliberate: a wrong count
# reads as a measurement, and "passed" cannot be mistaken for one.
helper_test_summary() {
    local capture="$1" summary=""
    summary=$(printf '%s' "$capture" | grep -oE 'Ran [0-9]+ tests?') || summary="passed"
    printf '%s' "$summary"
}

# helper_skip_count <capture> — the number of skipped tests, from unittest's own result
# line. Fails (returning non-zero, writing to stderr) when that line cannot be found.
#
# Scoped to `^(OK|FAILED)` FIRST, and that scoping is the load-bearing part. Bash `=~` takes
# the first match anywhere in the subject, so run over the whole capture any earlier
# `skipped=<digits>` wins — and unittest does not buffer stdout, so a test printing one of
# this repo's own transcript fixtures is enough to do it.
#
# The count is then matched WITHOUT a closing paren, because unittest appends
# `expected failures=` and `unexpected successes=` after it inside the same bracket:
# `OK (skipped=1, expected failures=1)` does not end at `skipped=1)`.
#
# A missing result line is a FAILURE rather than a zero. Answering 0 would make an
# unreadable capture indistinguishable from a clean run, which is the exact defect this
# whole line exists to remove.
helper_skip_count() {
    local capture="$1" result=""
    if ! result=$(printf '%s' "$capture" | grep -E '^(OK|FAILED)( \(|$)'); then
        printf 'helper_skip_count: no unittest result line (^OK / ^FAILED) in the capture\n' >&2
        return 1
    fi
    if [[ "$result" =~ skipped=([0-9]+) ]]; then
        printf '%s' "${BASH_REMATCH[1]}"
    else
        printf '0'
    fi
}
