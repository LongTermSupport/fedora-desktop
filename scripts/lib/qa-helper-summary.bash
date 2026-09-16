#!/usr/bin/env bash
# Read the `helper-tests` stage line out of a captured `python3 -m unittest` run.
# Plan 00125, Task 4.2. Driven by scripts/test-qa-helper-summary.bash.
#
# Sourced, never executed — it defines functions and runs nothing.
#
# This is two lines of text handling that lives in its own file because it has been wrong
# repeatedly: `unittest` counts a SKIPPED test inside testsRun, so `Ran 1456 tests` is
# byte-identical whether a test asserted or skipped itself, and two machines running
# different subsets of the same suite printed the same sentence for weeks. The skip count
# is what distinguishes them, which makes every way of reading it wrongly a way of putting
# the blindness back. The test file records each one.
#
# BOTH FUNCTIONS TAKE UNITTEST'S STDERR, NOT A MERGED CAPTURE. That is a precondition, not a
# detail, and it is what makes "last match wins" sound in both.
#
# Three revisions were spent looking for a match rule that survives a merged stream, and no
# such rule exists. A test's `print` goes to STDOUT, which Python BLOCK-BUFFERS to a pipe: a
# small decoy flushes at process exit and lands AFTER unittest's summary, while a decoy
# followed by more than the 8KB buffer flushes EARLY and lands BEFORE it. Both are
# reachable, both were measured, so first-wins and last-wins each fail one of them — and the
# two readers disagreed with each other on the same capture.
#
# Separating the channels deletes the whole class instead of out-guessing it. unittest writes
# its summary to stderr; `qa-helper-tests.bash` writes its own progress line to stdout. On
# stderr alone, unittest's summary is ALWAYS last: a test writes during the run, and the
# summary is printed after every test has finished.

# helper_test_summary <capture> — `Ran N tests`, or the word `passed` if unittest's count
# line is absent. Degrading to a WORD rather than a number is deliberate: a wrong count
# reads as a measurement, and "passed" cannot be mistaken for one.
#
# Scoped to a LINE unittest itself wrote, for the same reason `helper_skip_count` is, and it
# had the same bug: an unscoped `grep -oE` returns EVERY match, so a capture holding a
# second `Ran N tests` emitted BOTH and the stage line became two lines — the first read as
# the stage and the second lost. Worse than a wrong number, because a wrong number is at
# least still a verdict line.
#
# LAST match wins — sound only because the input is unittest's STDERR (see the header).
helper_test_summary() {
    local capture="$1" line=""
    line=$(printf '%s' "$capture" |
        awk '/^Ran [0-9]+ tests? in /{answer=$0} END{print answer}')
    if [[ "$line" =~ ^(Ran[[:space:]][0-9]+[[:space:]]tests?) ]]; then
        printf '%s' "${BASH_REMATCH[1]}"
    else
        printf 'passed'
    fi
}

# helper_skip_count <capture> — the number of skipped tests, from unittest's own result
# line. Fails (returning non-zero, writing to stderr) when that line cannot be found.
#
# Scoped to the LAST `^(OK|FAILED)` line, symmetrically with `helper_test_summary`. Bash
# `=~` takes the first match anywhere in the subject, so run over the whole capture any
# earlier `skipped=<digits>` wins.
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
    if ! result=$(printf '%s' "$capture" |
        awk '/^(OK|FAILED)( \(|$)/{answer=$0} END{if (answer != "") print answer}' |
        grep -E '^(OK|FAILED)( \(|$)'); then
        printf 'helper_skip_count: no unittest result line (^OK / ^FAILED) in the capture\n' >&2
        return 1
    fi
    if [[ "$result" =~ skipped=([0-9]+) ]]; then
        printf '%s' "${BASH_REMATCH[1]}"
    else
        printf '0'
    fi
}
