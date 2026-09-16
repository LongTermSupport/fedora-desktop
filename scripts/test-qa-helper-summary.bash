#!/usr/bin/env bash
# Unit-test scripts/lib/qa-helper-summary.bash — Plan 00125, Tasks 4.4 and 4.5.
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

for fn in helper_counts_summary qa_gate_case_count qa_gate_detail; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: ${fn} is not defined after sourcing the library" >&2
        exit 1
    fi
done

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
    counts_file "$(printf 'token=%s\ntests=%s\nskipped=%s\nmodules=%s\ntracked=%s\n' \
        "$TOKEN" "$1" "$2" "$3" "${4:-$3}")"
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
    "Ran 1464 tests in 64 modules (64 tracked), 1 skipped" \
    "$(summary "$(good_counts 1464 1 64)")"

check "a clean run reports zero skips rather than omitting them" \
    "Ran 1464 tests in 64 modules (64 tracked), 0 skipped" \
    "$(summary "$(good_counts 1464 0 64)")"

check "a single test is not pluralised into a mismatch" \
    "Ran 1 test in 1 module (1 tracked), 0 skipped" \
    "$(summary "$(good_counts 1 0 1)")"

check "a large skip count is not truncated" \
    "Ran 1464 tests in 64 modules (64 tracked), 137 skipped" \
    "$(summary "$(good_counts 1464 137 64)")"

# NOT commensurable: `testsRun` counts test methods, `skipped` counts skip events, and one
# method can register several via subTest. The reader must not "correct" a file that is
# faithful to the run it describes.
check "more skips than tests is reported rather than clamped" \
    "Ran 1 test in 1 module (1 tracked), 3 skipped" \
    "$(summary "$(good_counts 1 3 1)")"

check "the keys are read by name, not by position" \
    "Ran 9 tests in 2 modules (2 tracked), 2 skipped" \
    "$(summary "$(counts_file "token=$TOKEN
modules=2
tracked=2
skipped=2
tests=9
")")"

# THE CASE THE TRACKED COUNT EXISTS FOR. `find` discovers untracked files too, so a test
# nobody committed is run and counted while no tracked file is missing — the gate passes and
# the number moves. That number is what two machines are diffed on, so the divergence has to
# be in the stage line itself; reporting it only to the stderr `qa-all.bash` discards on
# success would be producing it without delivering it.
check "an untracked module shows as modules above tracked" \
    "Ran 1483 tests in 66 modules (65 tracked), 1 skipped" \
    "$(summary "$(good_counts 1483 1 66 65)")"

check "a trailing blank line is not an error" \
    "Ran 3 tests in 1 module (1 tracked), 0 skipped" \
    "$(summary "$(counts_file "token=$TOKEN
tests=3
skipped=0
modules=1
tracked=1

")")"

check "a file with no trailing newline is still read" \
    "Ran 3 tests in 1 module (1 tracked), 1 skipped" \
    "$(summary "$(counts_file "token=$TOKEN
tests=3
skipped=1
modules=1
tracked=1")")"

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
tracked=1
")"
refuses "a file with no skipped= is refused" "$(counts_file "token=$TOKEN
tests=3
modules=1
tracked=1
")"
refuses "a file with no modules= is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=0
tracked=1
")"
refuses "a file with no tracked= is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=0
modules=1
")"
refuses "an empty tests value is refused" "$(counts_file "token=$TOKEN
tests=
skipped=0
modules=1
tracked=1
")"
refuses "an empty skipped value is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=
modules=1
tracked=1
")"
refuses "a non-numeric skip count is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=none
modules=1
tracked=1
")"
refuses "a negative count is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=-1
modules=1
tracked=1
")"
refuses "a count with trailing text is refused" "$(counts_file "token=$TOKEN
tests=3
skipped=1 skipped
modules=1
tracked=1
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
tracked=1
")"

# Both occurrences non-empty is the easy half. A duplicate whose FIRST value is empty
# defeats any check written as "have I got a value yet?" rather than "have I seen this key
# yet?" — the header promises the latter, so the code must do the latter.
refuses "a duplicate whose first value is empty is refused" "$(counts_file "token=$TOKEN
tests=
tests=5
skipped=0
modules=1
tracked=1
")"
refuses "a duplicate token whose first value is empty is refused" "$(counts_file "token=
token=$TOKEN
tests=5
skipped=0
modules=1
tracked=1
")"

# THE CLOBBER CASE. The counts path travels in argv, so a test can find and overwrite the
# file — and this suite's own tests drive the runner. A file that is perfectly well formed
# but was not written by the run that asked for it must FAIL, not be believed.
refuses "a well-formed file carrying the wrong token is refused" \
    "$(counts_file "token=someone-elses
tests=3
skipped=0
modules=1
tracked=1
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
tracked=1
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
tracked=1
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
            --counts-file "$e2e_counts" --counts-token "$TOKEN" --tracked-modules 1 t_e2e
) >"$e2e_log" 2>&1 || e2e_rc=$?
if [ "$e2e_rc" -ne 0 ]; then
    failed=$((failed + 1))
    printf '  FAIL  the real runner writes a file this reader understands\n' >&2
    printf '        runner exited %s; its output follows\n' "$e2e_rc" >&2
    cat "$e2e_log" >&2
else
    check "the real runner writes a file this reader understands" \
        "Ran 2 tests in 1 module (1 tracked), 1 skipped" "$(summary "$e2e_counts")"
fi

echo "=== qa_gate_case_count: the OTHER stage-line reader, shared by 21 gates ==="

# Each of those gates ran `grep -oE 'passed: [0-9]+'` over its child's whole capture. `-o`
# prints EVERY match, so a second occurrence made the stage line TWO lines, and `verdicts.py`
# reads the first as the stage and loses the rest. Same defect round 4 found in the
# helper-tests reader; it just lived in 21 more places. Scoping to the LAST matching LINE is
# what fixes it, and it has to be a line rather than a match because the 21 do not agree on a
# format — see the cases below.

check "a bare summary line is read" \
    "passed: 29" "$(qa_gate_case_count 'passed: 29 failed: 0')"

# The real spread across the 21, measured from the scripts rather than assumed.
check "two spaces before failed: is read" \
    "passed: 15" "$(qa_gate_case_count 'passed: 15  failed: 0')"
check "three spaces before failed: is read" \
    "passed: 187" "$(qa_gate_case_count 'passed: 187   failed: 0')"
check "a count with no failed: at all is read" \
    "passed: 20" "$(qa_gate_case_count 'passed: 20')"
check "a line prefixed with the gate's own name is read" \
    "passed: 14" "$(qa_gate_case_count 'ccy selinux-verdict: passed: 14  failed: 0')"

# THE DEFECT, stated as a test. An earlier `passed: N` anywhere in the capture used to be
# emitted alongside the real one, making the stage line two lines.
check "an earlier count in the capture does not join the summary" \
    "passed: 29" "$(qa_gate_case_count "$(printf '  PASS  a case mentioning passed: 3 in its label\npassed: 29 failed: 0\n')")"

check "the answer is exactly one line" \
    "1" "$(qa_gate_case_count "$(printf 'passed: 3 failed: 0\npassed: 29 failed: 0\n')" | grep -c '')"

# A gate whose output has no count at all must degrade to a WORD, never to a number: a wrong
# count reads as a measurement, `passed` cannot be mistaken for one. (planlib-tests is real:
# it prints `PASSED (library version 1.2.0)`.)
check "a capture with no count degrades to a word" \
    "passed" "$(qa_gate_case_count 'PASSED (library version 1.2.0)')"
check "an empty capture degrades to a word" \
    "passed" "$(qa_gate_case_count '')"

# 8 of the 21 print something AFTER their summary (`OK`, `VERDICT: PASS`, an explanatory
# echo), so "the summary is the last line" is false. What holds is that no gate emits a
# second count — the rule has to survive trailing noise, not depend on its absence.
check "a count line followed by other output is still found" \
    "passed: 18" "$(qa_gate_case_count "$(printf 'passed: 18 failed: 0\nOK\nVERDICT: PASS\n')")"

# Within a line the rule is LAST too, so both halves agree. Taking the first would answer
# `passed: 3` here — a wrong NUMBER, which is the one output this function must never emit.
check "an earlier count on the same line does not win" \
    "passed: 29" "$(qa_gate_case_count 'suite passed: 3 of them; passed: 29 failed: 0')"

# The two patterns used to disagree on whitespace: awk required exactly one space, the bash
# match allowed several, so `passed:  29` silently degraded to a word.
check "extra whitespace after the colon is read, not degraded" \
    "passed: 29" "$(qa_gate_case_count 'passed:  29 failed: 0')"

echo "=== qa_gate_detail: the gates whose stage line is not a case count ==="

# `nokill-containerwatch` read `[0-9]+ call site[s]? checked` from a gate that has only ever
# printed `N container-watch file(s) clean`. Zero matches for its entire life, hidden by a
# `||` fallback that asserted `no forbidden kill call sites` — a claim nothing verified,
# printed as though it had been measured.
check "the nokill gate's real wording is read" \
    "3 container-watch file(s) clean" \
    "$(qa_gate_detail '✓ no-kill gate: 3 container-watch file(s) clean — reporting-only confirmed' \
        '[0-9]+ container-watch file[(]s[)] clean')"

check "the planlib gate's real wording is read" \
    "PASSED (library version 1.2.0)" \
    "$(qa_gate_detail "$(printf 'PASS: a case\ntest-planlib: PASSED (library version 1.2.0)\n')" \
        'PASSED [(]library version [0-9.]+[)]')"

# THE POINT OF THE FALLBACK'S WORDING. A pattern that stops matching must produce something
# that cannot be mistaken for an answer — the previous fallback was a sentence asserting a
# fact, which is why nobody noticed the reader was blind.
check "a pattern that does not match says so instead of asserting something" \
    "summary unreadable" \
    "$(qa_gate_detail '✓ no-kill gate: 3 container-watch file(s) clean' \
        '[0-9]+ call site[s]? checked')"

check "an empty capture says so too" \
    "summary unreadable" "$(qa_gate_detail '' 'anything')"

check "the last matching line wins here as well" \
    "2 files clean" \
    "$(qa_gate_detail "$(printf '1 files clean\n2 files clean\n')" '[0-9]+ files clean')"

echo
echo "=== every qa_gate_detail pattern is read from qa-all.bash and run against its real gate ==="

# THE DEFECT THIS CLOSES. `nokill-containerwatch` was blind for its entire life because its
# pattern and the gate's wording drifted apart and nothing compared them. The cases above
# would not have caught it: they assert against a HARDCODED COPY of the gate's output, and a
# copy cannot notice the thing it is copying changing. The end-to-end counts case one screen
# up already states the principle — "the only case here that would survive the format being
# changed on one side only" — and it was not applied to the two patterns whose drift was the
# defect being repaired.
#
# So this reads each pattern out of the REAL qa-all.bash and runs the REAL gate that
# produces the capture it is applied to. Neither side is a fixture.
#
# It is also self-extending, which is the property that matters more than the eight cases:
# a new qa_gate_detail call site whose capture variable is not registered below FAILS here,
# so the next pattern cannot be added without being coupled to its gate.

QA_ALL="$REPO_ROOT/scripts/qa-all.bash"

# capture-variable -> the command that produces it, run from the repo root. These are the
# gates' own invocations, copied from qa-all.bash's call sites, not re-derived.
gate_command_for() {
    case "$1" in
        nokill_out) echo "bash scripts/qa-nokill-containerwatch.bash" ;;
        planlib_out) echo "bash scripts/test-planlib.bash" ;;
        compat_out) echo "python3 -m helpers.gnome.check_extension_compat" ;;
        panel_contract_out) echo "python3 -m helpers.gnome.check_panel_contract ." ;;
        manifest_out) echo "bash scripts/qa-vmtest-manifest.bash" ;;
        pins_out) echo "bash scripts/qa-version-pins.bash" ;;
        *) return 1 ;;
    esac
}

# Each gate runs once however many patterns are applied to its capture.
declare -A gate_capture=()
capture_for() {
    local var="$1" command
    if [ -n "${gate_capture[$var]+set}" ]; then
        printf '%s' "${gate_capture[$var]}"
        return 0
    fi
    command="$(gate_command_for "$var")" || return 1
    gate_capture[$var]="$(cd "$REPO_ROOT" && eval "$command" 2>&1)"
    printf '%s' "${gate_capture[$var]}"
}

# THE EXTRACTION MUST ACCEPT EVERY SPELLING, or a call site is silently not checked and the
# coupling this whole section exists for does not apply to it. The first version keyed on
# `"$lower_snake"` with a single-quoted pattern, and four spellings went unmatched: `${braces}`,
# any uppercase or digit in the name, and a double-quoted pattern. `link_check.py`'s
# `_QA_SCRIPT_GATE` had already been widened for exactly this reason, 250 lines away in the
# same commit — "keying on one spelling would exempt the other two without saying so".
#
# `grep -o` prints every match, which is wanted here: one line per call site.
detail_re='qa_gate_detail "\$\{?[A-Za-z_][A-Za-z0-9_]*\}?" ('\''[^'\'']+'\''|"[^"]+")'
mapfile -t detail_sites < <(grep -oE "$detail_re" "$QA_ALL")

# GUARDING ZERO IS NOT ENOUGH — the partial case is the one that reads as clean. An
# under-matching regex extracts SOME call sites, checks those, and reports a pass; nothing
# distinguishes that from having checked them all. So the denominator is counted
# independently, from a deliberately loose pattern that cannot miss what the strict one
# catches, and the two must agree.
#
# The denominator has to be loose enough that nothing the strict one catches can escape it,
# and the first attempt was not: `[^_]qa_gate_detail ` requires a character BEFORE the name,
# so a call at column 1 was missed by the denominator AND by the extraction — the two would
# have agreed while both under-counting, which is the failure this check exists to prevent,
# wearing the check's own uniform. `(^|[^_[:alnum:]])` covers the line start; the class keeps
# `_qa_gate_detail` and similar from counting. Comment lines are dropped so a prose mention
# of the function cannot over-count and fail the suite for the wrong reason.
#
# `grep -c` exits 1 when it counts zero. That is a RESULT here, not an error — the branch
# below reports it — so the status is consumed explicitly rather than discarded.
detail_calls=0
if ! detail_calls=$(grep -vE '^[[:space:]]*#' "$QA_ALL" |
    grep -cE '(^|[^_[:alnum:]])qa_gate_detail[[:space:]]'); then
    detail_calls=0
fi
if [ "${#detail_sites[@]}" -ne "$detail_calls" ]; then
    failed=$((failed + 1))
    printf '  FAIL  %s\n        %s\n' \
        "extracted ${#detail_sites[@]} qa_gate_detail call site(s) from qa-all.bash but it has $detail_calls" \
        "the extraction regex does not cover every spelling — the difference is unchecked, not absent" >&2
elif [ "$detail_calls" -eq 0 ]; then
    failed=$((failed + 1))
    printf '  FAIL  %s\n' "no qa_gate_detail call sites found in qa-all.bash — the extraction broke, not the gates" >&2
else
    passed=$((passed + 1))
    printf '  PASS  COVERAGE: %s of %s qa_gate_detail call site(s) extracted\n' \
        "${#detail_sites[@]}" "$detail_calls"
fi

for site in "${detail_sites[@]}"; do
    site_var="${site#qa_gate_detail \"\$}"
    site_var="${site_var%%\"*}"
    site_pattern="${site#*\'}"
    site_pattern="${site_pattern%\'}"

    if ! site_capture="$(capture_for "$site_var")"; then
        failed=$((failed + 1))
        printf '  FAIL  %s\n        %s\n' \
            "qa-all.bash reads \$$site_var with qa_gate_detail, but no gate command is registered for it" \
            "register it in gate_command_for so the pattern is checked against what the gate prints" >&2
        continue
    fi

    site_answer="$(qa_gate_detail "$site_capture" "$site_pattern")"
    if [ "$site_answer" = "summary unreadable" ]; then
        failed=$((failed + 1))
        printf '  FAIL  %s\n        pattern: %s\n        the gate printed: %s\n' \
            "the pattern qa-all.bash applies to \$$site_var no longer matches that gate's output" \
            "$site_pattern" "$site_capture" >&2
    else
        passed=$((passed + 1))
        printf '  PASS  %s -> %s\n' "\$$site_var" "$site_answer"
    fi
done

echo
echo "passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
