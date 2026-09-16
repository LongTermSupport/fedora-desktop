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
#
# Scoped to a LINE unittest itself wrote, for the same reason `helper_skip_count` is, and it
# had the same bug: an unscoped `grep -oE` returns EVERY match, so a capture holding a
# second `Ran N tests` emitted BOTH and the stage line became two lines — the first read as
# the stage and the second lost. Worse than a wrong number, because a wrong number is at
# least still a verdict line.
#
# Neither the first match nor the last is right, and the reason is buffering rather than
# formatting. unittest writes its summary to STDERR; a test's `print` goes to STDOUT, which
# Python BLOCK-BUFFERS to a pipe — and this capture is taken through a pipe. So a decoy is
# flushed at process exit and appears AFTER the result line, never before it: "last wins"
# picks the decoy in the only ordering that actually occurs. A decoy on stderr would arrive
# before, so "first wins" is no better.
#
# What holds is unittest's own structure: the count line is the one most recently seen WHEN
# the result line arrives. That anchors on the same `^(OK|FAILED)` line `helper_skip_count`
# trusts, so both readers depend on one fact about the format instead of two.
helper_test_summary() {
    local capture="$1" line=""
    line=$(printf '%s' "$capture" |
        awk '/^Ran [0-9]+ tests? in /{seen=$0}
             /^(OK|FAILED)( \(|$)/{if (seen != "") {answer=seen}}
             END{print answer}')
    if [[ "$line" =~ ^(Ran[[:space:]][0-9]+[[:space:]]tests?) ]]; then
        printf '%s' "${BASH_REMATCH[1]}"
    else
        printf 'passed'
    fi
}

# helper_skip_count <capture> — the number of skipped tests, from unittest's own result
# line. Fails (returning non-zero, writing to stderr) when that line cannot be found.
#
# Scoped to `^(OK|FAILED)` FIRST, and that scoping is the load-bearing part. Bash `=~` takes
# the first match anywhere in the subject, so run over the whole capture any earlier
# `skipped=<digits>` wins — and a test writing to stderr is unbuffered, so printing one of
# this repo's own transcript fixtures is enough to do it. The first match is the right one
# here for the mirror-image reason: a STDOUT decoy is block-buffered to a pipe and lands
# after the result line, so it can never precede it.
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
