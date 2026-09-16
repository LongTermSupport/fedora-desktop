#!/usr/bin/env bash
# Run the panel's decision tests against the shipped extension (Plan 00109 Task 4.2).
#
#   ./scripts/test-panel-sections.bash
#
# WHY THIS EXISTS. The panel is the PRIMARY surface for this plan's findings, and nothing
# exercised its behaviour: `qa-js.bash` parses it, ESLint lints it, and
# `check_panel_contract.py` proves it shares a vocabulary with the producer. None of the
# three can tell a demoted finding from a current one, and neither can a screenshot.
#
# The tests import `statusDocument.js`, `sections/health.js` and `extension.js`
# themselves, with `tests/extensions/gjs-loader.mjs` answering the `gi://` and
# `resource:///` imports a GNOME Shell process would provide. What runs is the shipped
# file.
#
# This does NOT replace a Wayland session. Whether St renders the lines legibly, and
# whether the icon is the right one to look at, are still things only a human in a live
# shell can say. What moves earlier is every decision made before a widget is touched.
#
# Exit codes: 0 pass; 1 a test failed, or none ran; 2 node is absent.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# Every suite is named, and each is checked for readability below rather than trusted to
# the runner: `node --test` exits 0 when a named file declares no tests, so a suite that
# stopped being found would be a gate reporting a pass for a run that judged less.
TEST_FILES=(
    tests/extensions/test-panel-sections.mjs
    tests/extensions/test-panel-indicator.mjs
)

if ! command -v node >/dev/null; then
    echo "✗ panel-sections: node not installed (install via play-nvm-install.yml)" >&2
    exit 2
fi

for test_file in "${TEST_FILES[@]}"; do
    if [[ ! -r "$test_file" ]]; then
        echo "✗ panel-sections: $test_file is missing; nothing would be judged" >&2
        exit 2
    fi
done

# Named explicitly rather than by directory. Node's test runner treats a directory
# argument as a module to load, and a glob would sweep in the loader and the stubs — which
# declare no tests, so a run that quietly found none would still exit 0.
output=""
if ! output="$(node --test "${TEST_FILES[@]}" 2>&1)"; then
    printf '%s\n' "$output" >&2
    echo "✗ QA FAILED: panel section unit tests" >&2
    exit 1
fi

# A gate that ran NOTHING must not report a pass. Both summary spellings are accepted —
# `ℹ pass N` from the spec reporter, `# pass N` from TAP — because which one appears
# depends on the Node in use, and a runner that silently stopped recognising the line
# would be a gate that reports a pass for every run.
count="$(printf '%s\n' "$output" |
    awk '{gsub(/\033\[[0-9;]*m/,"")} /^(ℹ|#) pass [0-9]+$/ {print $3}')"
if [[ ! "$count" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "$output" >&2
    echo "✗ panel-sections: no passing-test count in the run's summary; discovery is broken" >&2
    exit 1
fi

# THE FLOOR IS DERIVED FROM THE SUITES, and `count -gt 0` is not enough on its own.
# Measured: `node --test` on a file declaring no tests reports `tests 1 / pass 1`, scoring
# the FILE as the passing test — so two emptied suites would report `passed: 2` and exit 0,
# and the check above would have been satisfied by a run that judged nothing at all.
#
# So each suite must declare at least one test of its own, and the run must have passed at
# least as many as the files declare. Both numbers come from the files, so neither goes
# stale when a test is added.
expected=0
for test_file in "${TEST_FILES[@]}"; do
    declared=0
    # grep -c exits 1 for zero matches — a RESULT — and 2 for a real error such as an
    # unreadable file. Collapsing the two makes a broken read look like an empty suite.
    grep_status=0
    if declared="$(grep -c '^test(' "$test_file")"; then
        grep_status=0
    else
        grep_status=$?
    fi
    if [[ "$grep_status" -gt 1 ]]; then
        echo "✗ panel-sections: could not read $test_file (grep exit $grep_status)" >&2
        exit 2
    fi
    if [[ "$declared" -eq 0 ]]; then
        echo "✗ panel-sections: $test_file declares no test(); node scores an empty file as" >&2
        echo "  one passing test, so this would otherwise have been reported as a pass" >&2
        exit 1
    fi
    expected=$((expected + declared))
done

if [[ "$count" -lt "$expected" ]]; then
    printf '%s\n' "$output" >&2
    echo "✗ panel-sections: $count passed but the suites declare $expected test(s);" >&2
    echo "  some declared test did not run" >&2
    exit 1
fi

printf 'passed: %s\n' "$count"
exit 0
