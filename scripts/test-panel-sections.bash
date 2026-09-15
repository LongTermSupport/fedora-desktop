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
# The tests import `statusDocument.js` and `sections/health.js` themselves, with
# `tests/extensions/gjs-loader.mjs` answering the `gi://` and `resource:///` imports a
# GNOME Shell process would provide. What runs is the shipped file.
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

TEST_FILE="tests/extensions/test-panel-sections.mjs"

if ! command -v node >/dev/null; then
    echo "✗ panel-sections: node not installed (install via play-nvm-install.yml)" >&2
    exit 2
fi

if [[ ! -r "$TEST_FILE" ]]; then
    echo "✗ panel-sections: $TEST_FILE is missing; nothing would be judged" >&2
    exit 2
fi

# Named explicitly rather than by directory. Node's test runner treats a directory
# argument as a module to load, and a glob would sweep in the loader and the stubs — which
# declare no tests, so a run that quietly found none would still exit 0.
output=""
if ! output="$(node --test "$TEST_FILE" 2>&1)"; then
    printf '%s\n' "$output" >&2
    echo "✗ QA FAILED: panel section unit tests" >&2
    exit 1
fi

# A gate that ran NOTHING must not report a pass. `node --test` exits 0 on a file
# declaring no tests, so the count is read from the summary rather than assumed. Both
# summary spellings are accepted — `ℹ pass N` from the spec reporter, `# pass N` from TAP
# — because which one appears depends on the Node in use, and a runner that silently
# stopped recognising the line would be a gate that reports a pass for every run.
count="$(printf '%s\n' "$output" |
    awk '{gsub(/\033\[[0-9;]*m/,"")} /^(ℹ|#) pass [0-9]+$/ {print $3}')"
if [[ ! "$count" =~ ^[0-9]+$ ]] || [[ "$count" -eq 0 ]]; then
    printf '%s\n' "$output" >&2
    echo "✗ panel-sections: no passing-test count in the run's summary; discovery is broken" >&2
    exit 1
fi

printf 'passed: %s\n' "$count"
exit 0
