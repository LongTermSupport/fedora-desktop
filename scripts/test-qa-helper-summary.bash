#!/usr/bin/env bash
# Unit-test scripts/lib/qa-helper-summary.bash — Plan 00125, Task 4.2.
#
# WHY THIS EXISTS. The `helper-tests` stage line has to distinguish a clean run from a
# blind one, because `unittest` counts a SKIPPED test inside testsRun and `Ran 1464 tests`
# is identical either way. Four readers were written against the captured TEXT and all
# four were wrong, each verified by hand and then thrown away:
#
#   1. It printed only `Ran N tests`, with no skip count at all.
#   2. The skip count was matched as `\(skipped=[0-9]+\)`, which misses
#      `OK (skipped=1, expected failures=1)` and silently reports 0.
#   3. The match ran over the WHOLE capture and bash `=~` takes the first hit anywhere, so
#      any earlier `skipped=<digits>` won. `tests/helpers/vmtest/test_transcript.py` holds
#      exactly that text in a fixture.
#   4. "Last match wins" — first over a merged `2>&1` capture, where Python's 8KB stdout
#      block buffering puts a decoy on either side of the summary depending only on how
#      much follows it, then over stderr alone, where an `atexit` handler writes after
#      unittest's summary. That reader answered `Ran 3 tests` / `99` for a run whose truth
#      was `Ran 1 test` / `0`.
#
# All four have the same shape, and it is this plan's subject: **a check whose clean result
# is indistinguishable from a blind one**. That is exactly the property a hand-check cannot
# hold on to, because the next reviser sees the line and not the reasoning.
#
# So the counts are not read from any stream. `helpers/qa_environment/unittest_counts.py`
# takes them from the `TestResult` object and writes them to a file; this library reads
# that file, and these cases drive the REAL function — not a copy of the expression, which
# could not catch a change to the thing it was copying.
#
# What is left to get wrong is narrow, and it is all here: a malformed file must FAIL
# rather than read as a clean zero.
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

if ! declare -F helper_counts_summary >/dev/null; then
    echo "FAIL: helper_counts_summary is not defined after sourcing the library" >&2
    exit 1
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

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

# counts_file <content> — write a counts file and echo its path. Each call gets a fresh
# name so a case cannot read the previous case's file and pass by accident.
counts_seq=0
counts_file() {
    counts_seq=$((counts_seq + 1))
    local path="$WORK_DIR/counts-$counts_seq"
    printf '%s' "$1" >"$path"
    printf '%s' "$path"
}

# The token every well-formed fixture carries, and that the reader is told to expect.
TOKEN="tok-12345"

# summary <file> — the reader, with the expected token supplied.
summary() { helper_counts_summary "$1" "$TOKEN"; }

# good_counts <tests> <skipped> <modules> — a well-formed file, echoing its path. Used
# where the case is about reading a VALID file; the malformed cases build theirs literally
# with counts_file so the defect under test is visible at the call site.
good_counts() {
    counts_file "$(printf 'token=%s\ntests=%s\nskipped=%s\nmodules=%s\n' \
        "$TOKEN" "$1" "$2" "$3")"
}

# refuses <label> <path> — the function must FAIL on this file rather than answer it, AND
# write nothing to stdout while doing so. stderr goes to /dev/null on purpose: the
# assertion is about the payload channel, and a diagnostic on the test's own stderr would
# be indistinguishable from a failure report. Which channel it lands on is asserted
# separately below, where it is the subject rather than the noise.
refuses() {
    local label="$1" path="$2" out="" rc=0
    out="$(helper_counts_summary "$path" "$TOKEN" 2>/dev/null)" || rc=$?
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

echo "=== a well-formed counts file is read exactly ==="

check "every count is reported" \
    "Ran 1464 tests in 64 modules, 1 skipped" \
    "$(summary "$(good_counts 1464 1 64)")"

check "a clean run reports zero skips rather than omitting them" \
    "Ran 1464 tests in 64 modules, 0 skipped" \
    "$(summary "$(good_counts 1464 0 64)")"

check "a single test is not pluralised into a mismatch" \
    "Ran 1 test in 1 module, 0 skipped" \
    "$(summary "$(good_counts 1 0 1)")"

check "a large skip count is not truncated" \
    "Ran 1464 tests in 64 modules, 137 skipped" \
    "$(summary "$(good_counts 1464 137 64)")"

# NOT commensurable: `testsRun` counts test methods, `skipped` counts skip events, and one
# method can register several via subTest. The reader must not "correct" a file that is
# faithful to the run it describes.
check "more skips than tests is reported rather than clamped" \
    "Ran 1 test in 1 module, 3 skipped" \
    "$(summary "$(good_counts 1 3 1)")"

check "the keys are read by name, not by position" \
    "Ran 9 tests in 2 modules, 2 skipped" \
    "$(summary "$(counts_file "token=$TOKEN
modules=2
skipped=2
tests=9
")")"

check "a trailing blank line is not an error" \
    "Ran 3 tests in 1 module, 0 skipped" \
    "$(summary "$(counts_file "token=$TOKEN
tests=3
skipped=0
modules=1

")")"

check "a file with no trailing newline is still read" \
    "Ran 3 tests in 1 module, 1 skipped" \
    "$(summary "$(counts_file "token=$TOKEN
tests=3
skipped=1
modules=1")")"

check "the summary is exactly one line" \
    "1" \
    "$(summary "$(good_counts 3 1 1)" | grep -c '')"

echo "=== an unreadable counts file is refused, never reported as zero ==="

# THE WHOLE POINT. Answering `0 skipped` for "the file could not be read" would put a
# blind machine and a clean one back on the same line — the defect four readers in a row
# reintroduced, in a fifth disguise.
refuses "a missing file is refused" "$WORK_DIR/does-not-exist"

# Reachable, not theoretical: a test calling `os._exit(0)` skips the runner's write while
# the process still exits 0, leaving exactly the zero-byte file `mktemp` created. Existence
# is not generation.
refuses "an empty file is refused" "$(counts_file '')"

refuses "a file with no tests= is refused" "$(counts_file "token=$TOKEN
skipped=0
modules=1
")"
refuses "a file with no skipped= is refused" "$(counts_file "token=$TOKEN
tests=3
modules=1
")"
refuses "a file with no modules= is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=0
")"
refuses "an empty tests value is refused" "$(counts_file "token=$TOKEN
tests=
skipped=0
modules=1
")"
refuses "an empty skipped value is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=
modules=1
")"
refuses "a non-numeric skip count is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=none
modules=1
")"
refuses "a negative count is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=-1
modules=1
")"
refuses "a count with trailing text is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=1 skipped
modules=1
")"
refuses "an unknown key is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=1
modules=1
errors=2
")"
refuses "a duplicated key is refused" "$(counts_file "token=$TOKEN
tests=3
tests=4
skipped=1
modules=1
")"

# Both occurrences non-empty is the easy half. A duplicate whose FIRST value is empty
# defeats any check written as "have I got a value yet?" rather than "have I seen this key
# yet?" — the header promises the latter, so the code must do the latter.
refuses "a duplicate whose first value is empty is refused" "$(counts_file "token=$TOKEN
tests=
tests=5
skipped=0
modules=1
")"
refuses "a duplicate token whose first value is empty is refused" "$(counts_file "token=
token=$TOKEN
tests=5
skipped=0
modules=1
")"

# THE CLOBBER CASE. The counts path travels in argv, so a test can find and overwrite the
# file — and this suite's own tests drive the runner. A file that is perfectly well formed
# but was not written by the run that asked for it must FAIL, not be believed.
refuses "a well-formed file carrying the wrong token is refused" \
    "$(counts_file "token=someone-elses
tests=3
skipped=0
modules=1
")"
refuses "a well-formed file carrying no token at all is refused" \
    "$(counts_file 'tests=3
skipped=0
modules=1
')"

# Unittest's own summary text, fed in as if it were the counts file. A reader that fell
# back to scraping — or that ignored what it could not parse — would answer here.
refuses "unittest's human-readable output is not a counts file" \
    "$(counts_file 'Ran 1464 tests in 6.153s

OK (skipped=1)
')"

# A test's forged line, written into the file's position. Refused because `OK (...)` is not
# a key this reader knows, not because of where it appeared.
refuses "a forged result line is refused rather than believed" \
    "$(counts_file 'OK (skipped=99)
')"

echo "=== the refusal is a diagnostic, and goes to stderr ==="

# Asserted rather than assumed: a diagnostic on stdout would be captured by `$(...)` at the
# call site and printed as if it were the stage line. The non-zero exit is expected here
# and is what the `refuses` cases above already assert, so it is consumed explicitly rather
# than hidden.
diag_rc=0
diag_stderr="$(summary "$WORK_DIR/absent" 2>&1 1>/dev/null)" || diag_rc=$?
if [ "$diag_rc" -eq 0 ]; then
    failed=$((failed + 1))
    echo "  FAIL  the refusal diagnostic goes to stderr (it did not refuse at all)" >&2
elif [[ "$diag_stderr" == *"no counts file at"* ]]; then
    passed=$((passed + 1))
    echo "  PASS  the refusal diagnostic goes to stderr"
else
    failed=$((failed + 1))
    printf '  FAIL  the refusal diagnostic goes to stderr\n        got: %s\n' \
        "$diag_stderr" >&2
fi

# The diagnostic must name WHICH key is wrong. "Something was unreadable" sends the next
# reader back to the runner with nothing to go on.
diag_rc=0
diag_stderr="$(summary "$(counts_file "token=$TOKEN
tests=3
modules=1
")" 2>&1 1>/dev/null)" || diag_rc=$?
if [ "$diag_rc" -ne 0 ] && [[ "$diag_stderr" == *"skipped="* ]]; then
    passed=$((passed + 1))
    echo "  PASS  the diagnostic names the key that could not be read"
else
    failed=$((failed + 1))
    printf '  FAIL  the diagnostic names the key that could not be read\n        got: %s\n' \
        "$diag_stderr" >&2
fi

# An EMPTY file must say "nothing wrote this", not "something else wrote this". It is the
# one malformed case the header calls reachable (`os._exit(0)`), and reporting it as a
# token mismatch sends the next reader hunting a culprit that does not exist.
diag_rc=0
diag_stderr="$(summary "$(counts_file '')" 2>&1 1>/dev/null)" || diag_rc=$?
if [ "$diag_rc" -ne 0 ] && [[ "$diag_stderr" == *"is empty"* ]]; then
    passed=$((passed + 1))
    echo "  PASS  an empty file is diagnosed as unwritten, not as a foreign file"
else
    failed=$((failed + 1))
    printf '  FAIL  an empty file is diagnosed as unwritten, not as a foreign file\n        got: %s\n' \
        "$diag_stderr" >&2
fi

# A wrong token must say so in those words, rather than reporting a missing key. The two
# have different remedies: one means the runner is broken, the other that something else
# wrote the file.
diag_rc=0
diag_stderr="$(summary "$(counts_file "token=not-ours
tests=3
skipped=0
modules=1
")" 2>&1 1>/dev/null)" || diag_rc=$?
if [ "$diag_rc" -ne 0 ] && [[ "$diag_stderr" == *"token"* ]]; then
    passed=$((passed + 1))
    echo "  PASS  the diagnostic distinguishes a foreign file from a malformed one"
else
    failed=$((failed + 1))
    printf '  FAIL  the diagnostic distinguishes a foreign file from a malformed one\n        got: %s\n' \
        "$diag_stderr" >&2
fi

echo "=== end to end: the runner's real output is what this reader consumes ==="

# The two halves are developed separately and could drift apart — a fake cannot catch a
# rename in the thing it is faking. So this case runs the REAL runner over a real (tiny)
# test module and reads the REAL file it writes. It is the only case here that would
# survive the format being changed on one side only.
e2e_dir="$WORK_DIR/e2e"
mkdir -p "$e2e_dir"
cat >"$e2e_dir/t_e2e.py" <<'PY'
import unittest


class T(unittest.TestCase):
    def test_asserts(self):
        self.assertTrue(True)

    @unittest.skip("proves the skip count survives the round trip")
    def test_skips(self):
        pass
PY
e2e_counts="$WORK_DIR/e2e-counts"
e2e_log="$WORK_DIR/e2e-log"
e2e_rc=0
(
    cd "$REPO_ROOT" &&
        PYTHONPATH="$REPO_ROOT:$e2e_dir" python3 -m helpers.qa_environment.unittest_counts \
            --counts-file "$e2e_counts" --counts-token "$TOKEN" t_e2e
) >"$e2e_log" 2>&1 || e2e_rc=$?
if [ "$e2e_rc" -ne 0 ]; then
    failed=$((failed + 1))
    printf '  FAIL  the real runner writes a file this reader understands\n' >&2
    printf '        runner exited %s; its output follows\n' "$e2e_rc" >&2
    cat "$e2e_log" >&2
else
    check "the real runner writes a file this reader understands" \
        "Ran 2 tests in 1 module, 1 skipped" "$(summary "$e2e_counts")"
fi

echo
echo "passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
