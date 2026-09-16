#!/usr/bin/env bash
# Read the `helper-tests` stage line out of the counts file written by
# `helpers/qa_environment/unittest_counts.py`. Plan 00125, Task 4.2.
# Driven by scripts/test-qa-helper-summary.bash.
#
# Sourced, never executed — it defines one function and runs nothing.
#
# THIS FILE PARSES NO OUTPUT STREAM, and that is the point of it.
#
# `unittest` counts a SKIPPED test inside testsRun, so `Ran 1464 tests` is byte-identical
# whether a test asserted or skipped itself: two machines running different subsets of the
# same suite print the same sentence. The skip count is what separates them, which makes
# every wrong way of reading it a way of putting the blindness back.
#
# Scraping that count out of the captured TEXT is unsound, and it takes four defeated
# readers to see the whole shape of why:
#
#   1. `\(skipped=[0-9]+\)` missed `OK (skipped=1, expected failures=1)` — reported 0.
#   2. The match ran over the whole capture, and bash `=~` takes the FIRST hit anywhere,
#      so an earlier `skipped=<digits>` won. This repo's own fixtures contain that text.
#   3. "Last match wins" over a merged `2>&1` capture. Python BLOCK-BUFFERS stdout to a
#      pipe, so a small decoy flushes at exit and lands AFTER unittest's summary while a
#      decoy padded past 8KB flushes early and lands BEFORE it — both reachable, both
#      measured, so no first-or-last rule over a merged stream can be right.
#   4. "Last match wins over STDERR alone", on the claim that unittest's summary is always
#      last there. A test that registers an `atexit` handler printing to stderr disproves
#      it; the readers then answered `Ran 3 tests` / `99` where the truth was
#      `Ran 1 test` / `0`.
#
# There is no fifth match rule here. A test can write anything to either stream in any
# order, so the text is not the source of truth — the `TestResult` object is, and the
# runner writes those two numbers to a file whose path the caller chooses. Nothing a test
# prints shares a channel with the payload.
#
# It is also ONE function rather than two. The previous pair drifted into using opposite
# match rules and disagreed with each other about the same run; a single reader cannot.

# helper_counts_summary <counts-file> <expected-token> — prints
# `Ran 1464 tests in 64 modules, 1 skipped`.
#
# Fails, loudly and on stderr, if the file is missing, is short a key, carries a key twice,
# holds anything but digits, or comes back with a token that is not the one the caller
# asked for. Every one of those is a bug rather than a state a run can legitimately reach,
# and answering `0 skipped` for any of them would make an unreadable file look exactly like
# a clean machine — the defect this whole line exists to remove, restored by the back door.
#
# THE TOKEN IS A CLOBBER DETECTOR, NOT A LOCK. The counts path travels in `argv`, so a test
# can read it; the token means the caller notices, because a file written by anything other
# than the run it asked for carries the wrong value or none. It stops an accident, not an
# attempt — which is the honest claim, and the previous four revisions each failed by
# stating a narrower guarantee than they had.
#
# An EMPTY file is refused by the same path, and that case is reachable: a test calling
# `os._exit(0)` skips the write entirely while the runner still exits 0, leaving the
# zero-byte file `mktemp` created. Existence is not generation.
helper_counts_summary() {
    local path="$1" expected_token="$2"
    local tests="" skipped="" modules="" token="" key="" value="" noun=""

    if [[ ! -f "$path" ]]; then
        printf 'helper_counts_summary: no counts file at %s\n' "$path" >&2
        return 1
    fi

    # `|| [[ -n "$key" ]]` keeps the last line when the file does not end in a newline:
    # `read` returns non-zero there but has already assigned. Without it the final key is
    # silently dropped, and a reader that ignores part of its input is the shape this
    # whole library exists to avoid.
    while IFS='=' read -r key value || [[ -n "$key" ]]; do
        case "$key" in
            tests)
                if [[ -n "$tests" ]]; then
                    printf 'helper_counts_summary: duplicate tests= in %s\n' "$path" >&2
                    return 1
                fi
                tests="$value"
                ;;
            skipped)
                if [[ -n "$skipped" ]]; then
                    printf 'helper_counts_summary: duplicate skipped= in %s\n' "$path" >&2
                    return 1
                fi
                skipped="$value"
                ;;
            modules)
                if [[ -n "$modules" ]]; then
                    printf 'helper_counts_summary: duplicate modules= in %s\n' "$path" >&2
                    return 1
                fi
                modules="$value"
                ;;
            token)
                if [[ -n "$token" ]]; then
                    printf 'helper_counts_summary: duplicate token= in %s\n' "$path" >&2
                    return 1
                fi
                token="$value"
                ;;
            '')
                continue
                ;;
            *)
                printf 'helper_counts_summary: unexpected key %q in %s\n' "$key" "$path" >&2
                return 1
                ;;
        esac
    done <"$path"

    # Checked FIRST: if the file is not the one this caller's run produced, its numbers
    # describe something else and validating them would only lend them credibility.
    if [[ "$token" != "$expected_token" ]]; then
        printf 'helper_counts_summary: %s carries token %q, expected %q — the file was\n' \
            "$path" "$token" "$expected_token" >&2
        printf '  written by something other than the run that asked for it\n' >&2
        return 1
    fi

    # An absent key and a zero must not look alike: one is a clean run, the other is a
    # reader gone blind. `^[0-9]+$` also rejects an empty value, so both are covered here.
    if [[ ! "$tests" =~ ^[0-9]+$ ]]; then
        printf 'helper_counts_summary: tests= is missing or not a number in %s\n' "$path" >&2
        return 1
    fi
    if [[ ! "$skipped" =~ ^[0-9]+$ ]]; then
        printf 'helper_counts_summary: skipped= is missing or not a number in %s\n' "$path" >&2
        return 1
    fi
    if [[ ! "$modules" =~ ^[0-9]+$ ]]; then
        printf 'helper_counts_summary: modules= is missing or not a number in %s\n' "$path" >&2
        return 1
    fi

    # The module count rides along because two machines COLLECTING different sets is the
    # same defect one level up, and the test count on its own cannot show it.
    noun="tests"
    if [[ "$tests" -eq 1 ]]; then
        noun="test"
    fi
    printf 'Ran %s %s in %s modules, %s skipped' "$tests" "$noun" "$modules" "$skipped"
}
