#!/usr/bin/env bash
# The three readers that produce `qa-all.bash`'s stage lines: the `helper-tests` counts out
# of the file `helpers/qa_environment/unittest_counts.py` writes, and the case count and the
# summary detail out of the other gates' captured output. Plan 00125, Tasks 4.4 and 4.5.
# Driven by scripts/test-qa-helper-summary.bash.
#
# Sourced, never executed — it defines three functions and runs nothing.
#
# THE HELPER-TESTS COUNTS ARE NOT PARSED FROM ANY OUTPUT STREAM, and that is the point of
# `helper_counts_summary`. The other two functions here DO parse output, because the gates
# they read have no equivalent of a `TestResult` object to take numbers from — so they are
# built to fail visibly rather than plausibly instead.
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
# The helper-tests counts are also read by ONE function rather than two. The previous pair
# drifted into using opposite match rules and disagreed with each other about the same run;
# a single reader cannot.

# helper_counts_summary <counts-file> <expected-token> — prints
# `Ran 1464 tests in 64 modules (64 tracked), 1 skipped`.
#
# Fails, loudly and on stderr, if the file is missing or empty, is short a key, repeats a
# key, holds anything but digits, or comes back with a token that is not the one the caller
# asked for. Every one of those is a bug rather than a state a run can legitimately reach,
# and answering `0 skipped` for any of them would make an unreadable file look exactly like
# a clean machine — the defect this whole line exists to remove, restored by the back door.
#
# WHAT THE TOKEN DETECTS, stated no wider than it is true: a counts file written by
# something that never read this run's `argv` — a stale file, a concurrent run, a hardcoded
# path. It is NOT a lock against a test: the token travels in the same `argv` as the path,
# so anything that finds the file BY READING ARGV already has the token. Something that
# reaches the file another way — a hardcoded path, a stale file from a previous run — does
# not, and that is the whole of what this check buys.
#
# What defeats a clobber from inside the suite is WRITE ORDERING, not this check: the runner
# writes after every test has finished, so a forgery landing mid-run is overwritten. And
# what nothing here detects is a write landing AFTER the runner's — from `atexit`, or a
# thread outliving the run — which reproduces a valid token and a wrong number.
#
# That last paragraph is the one worth keeping. Four revisions of this file failed by
# claiming a guarantee BROADER than they had; a narrower claim than the truth costs nothing.
#
# An EMPTY file is refused with its own message rather than as a token mismatch, because the
# two have different remedies and the empty case is reachable: a test calling `os._exit(0)`
# skips the write entirely while the runner still exits 0, leaving the zero-byte file
# `mktemp` created. Existence is not generation.
#
# `tracked <= modules` is deliberately NOT checked here, and the reason is placement rather
# than reachability — the guards above refuse plenty of states the runner cannot produce.
# It is already enforced UPSTREAM by `qa-helper-tests.bash`'s exit-2 cross-check against
# `git ls-files`, which fires earlier and names the missing files, so a check here would be
# a second opinion on strictly less information. That makes this reader's input domain
# dependent on that check: relax it and this assumption widens silently.
helper_counts_summary() {
    local path="$1" expected_token="$2"
    local tests="" skipped="" modules="" tracked="" token="" key="" value=""
    local seen_tests="" seen_skipped="" seen_modules="" seen_tracked="" seen_token=""
    local test_noun="" module_noun=""

    if [[ ! -f "$path" ]]; then
        printf 'helper_counts_summary: no counts file at %s\n' "$path" >&2
        return 1
    fi

    # Before the token check, because "nothing wrote this" and "something else wrote this"
    # have different remedies, and reporting the first as the second sends the next reader
    # looking for a culprit that does not exist.
    if [[ ! -s "$path" ]]; then
        printf 'helper_counts_summary: %s is empty — the runner did not write it\n' \
            "$path" >&2
        return 1
    fi

    # `|| [[ -n "$key" ]]` keeps the last line when the file does not end in a newline:
    # `read` returns non-zero there but has already assigned. Without it the final key is
    # silently dropped, and a reader that ignores part of its input is the shape this
    # whole library exists to avoid.
    #
    # Duplicates are tracked with SEEN flags rather than by testing the value for
    # emptiness: `tests=` followed by `tests=5` is a repeated key, and a non-empty test
    # would have accepted it because the first occurrence left the variable empty.
    while IFS='=' read -r key value || [[ -n "$key" ]]; do
        case "$key" in
            tests)
                if [[ -n "$seen_tests" ]]; then
                    printf 'helper_counts_summary: duplicate tests= in %s\n' "$path" >&2
                    return 1
                fi
                seen_tests=1
                tests="$value"
                ;;
            skipped)
                if [[ -n "$seen_skipped" ]]; then
                    printf 'helper_counts_summary: duplicate skipped= in %s\n' "$path" >&2
                    return 1
                fi
                seen_skipped=1
                skipped="$value"
                ;;
            modules)
                if [[ -n "$seen_modules" ]]; then
                    printf 'helper_counts_summary: duplicate modules= in %s\n' "$path" >&2
                    return 1
                fi
                seen_modules=1
                modules="$value"
                ;;
            tracked)
                if [[ -n "$seen_tracked" ]]; then
                    printf 'helper_counts_summary: duplicate tracked= in %s\n' "$path" >&2
                    return 1
                fi
                seen_tracked=1
                tracked="$value"
                ;;
            token)
                if [[ -n "$seen_token" ]]; then
                    printf 'helper_counts_summary: duplicate token= in %s\n' "$path" >&2
                    return 1
                fi
                seen_token=1
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

    # Checked before the digits: if the file is not the one this caller's run produced, its
    # numbers describe something else and validating them would only lend them credibility.
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
    if [[ ! "$tracked" =~ ^[0-9]+$ ]]; then
        printf 'helper_counts_summary: tracked= is missing or not a number in %s\n' "$path" >&2
        return 1
    fi

    # The module count rides along because two machines COLLECTING different sets is the
    # same defect one level up, and the test count on its own cannot show it. The TRACKED
    # total rides along for the other half: `find` discovers untracked files too, so a test
    # nobody committed runs here and nowhere else, and the run's counts stop being
    # comparable. `modules` above `tracked` is exactly that case, visible in the stage line
    # — which is where it has to be, because this line is what two machines are diffed on.
    test_noun="tests"
    if [[ "$tests" -eq 1 ]]; then
        test_noun="test"
    fi
    module_noun="modules"
    if [[ "$modules" -eq 1 ]]; then
        module_noun="module"
    fi
    printf 'Ran %s %s in %s %s (%s tracked), %s skipped' \
        "$tests" "$test_noun" "$modules" "$module_noun" "$tracked" "$skipped"
}

# qa_gate_case_count <capture> — `passed: N` for a `scripts/test-*.bash` gate, or the word
# `passed` when the capture carries no count.
#
# The second reader behind `qa-all.bash`'s stage lines, and it lives here for the same reason
# the first does: 21 hard gates used to inline `grep -oE 'passed: [0-9]+'`, and `-o` prints
# EVERY match, so any earlier `passed: <digits>` in a child's output was emitted alongside the
# real one and the stage line became TWO lines — which `verdicts.py` half-drops, reading the
# first as the stage and losing the rest. That is the defect round 4 found in the helper-tests
# reader; it simply lived in 21 more copies, none of them tested.
#
# SCOPED TO THE LAST MATCHING LINE, then to the LAST match within it. A line rather than a
# match because the 21 gates do not agree on a format. Measured across them: `passed: N
# failed: M` with one, two and three spaces; `passed: N` with no `failed:` at all; and two
# that prefix the line with the gate's own name (`ccy selinux-verdict: passed: 14`).
#
# WHY LAST IS SOUND HERE, stated as what was measured rather than as a law. An earlier draft
# said "a gate prints its summary last, which is true because it is a summary" — circular,
# and false for 8 of the 21, which print `OK` or `VERDICT: PASS` after it. The property that
# actually holds is narrower: **no gate emits a second `passed: <digits>` line**, and every
# count-bearing line in all 21 comes from the single parent bash process on stdout, so the
# `2>&1` capture cannot reorder them — no child writes one, no gate backgrounds anything, and
# every `EXIT` trap in them is an `rm`. Taking the last is then a tie-break that never fires
# rather than a bet on ordering.
#
# Degrading to a WORD rather than a number is deliberate: a wrong count reads as a
# measurement, `passed` cannot. That path is DEFENSIVE — all 21 live callers emit a count,
# and it is reached only by this library's own tests. (An earlier comment justified it with
# `planlib-tests`, which prints `PASSED (library version 1.2.0)`; that gate is not a caller,
# it has its own reader below.)
#
# This does NOT hard-fail on an unreadable capture, unlike `helper_counts_summary` — which
# refuses outright rather than degrading. The difference is what the number means: the
# helper-tests counts distinguish two machines and a blind read there is the defect the plan
# exists to remove, whereas this is a case count whose gate has already reported pass or fail
# through its exit status.
qa_gate_case_count() {
    local capture="$1" line="" match=""
    line=$(printf '%s' "$capture" |
        awk '/passed:[[:space:]]+[0-9]+/{answer=$0} END{print answer}')
    # LAST match within the line too, so the two rules agree. Taking the first would answer
    # `passed: 3` for `suite passed: 3 of them; passed: 29 failed: 0` — a wrong number, which
    # is the one thing this function must never emit. Unreachable across the current 21, and
    # pinned by a case so it stays that way.
    match=$(printf '%s' "$line" | grep -oE 'passed:[[:space:]]+[0-9]+' | awk 'END{print}')
    if [[ "$match" =~ ([0-9]+)$ ]]; then
        printf 'passed: %s' "${BASH_REMATCH[1]}"
    else
        printf 'passed'
    fi
}

# qa_gate_detail <capture> <extended-regex> — the matched text from the LAST line that
# matches, or the literal `summary unreadable` when nothing does.
#
# For the gates whose stage line is not a case count — 6 captures, 8 patterns, because
# `vmtest-manifest` reports three separate measurements and joining them beats choosing one.
# Same scoping as `qa_gate_case_count`, for the same reason, and it exists because the
# alternative was
# measured and had failed silently for the whole life of one of them: `nokill-containerwatch`
# read `[0-9]+ call site[s]? checked` from a gate that has only ever printed
# `N container-watch file(s) clean`, so the pattern matched ZERO times, the `||` fallback
# substituted the prose `no forbidden kill call sites` on every run, and the coverage number
# never reached the stage line. A blind reader whose blind output is indistinguishable from a
# real answer — this plan's subject, inside `qa-all.bash`, six lines under a comment about
# failing to generalise a fix.
#
# Hence the fallback is `summary unreadable` and not a plausible sentence. A fallback that
# ASSERTS something is worse than no fallback: it is a claim nothing verified, and it reads
# exactly like a measurement. This one cannot be mistaken for one.
#
# The fallback is a last resort, not the guard. What actually stops a pattern drifting from
# its gate is `test-qa-helper-summary.bash`, which reads every pattern below OUT of
# `qa-all.bash` and runs it against the real gate — a call site whose capture variable is not
# registered there fails, so the next one cannot be added uncoupled.
qa_gate_detail() {
    local capture="$1" pattern="$2" line=""
    line=$(printf '%s' "$capture" | awk -v re="$pattern" '$0 ~ re {answer=$0} END{print answer}')
    if [[ -n "$line" && "$line" =~ $pattern ]]; then
        printf '%s' "${BASH_REMATCH[0]}"
    else
        printf 'summary unreadable'
    fi
}
