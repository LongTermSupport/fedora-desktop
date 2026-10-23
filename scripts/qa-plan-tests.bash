#!/usr/bin/bash
# qa-plan-tests.bash — every active plan folder's own Python tests (CLAUDE/Plan/<NNNNN>-*/**/test_*.py)
# pass. The helper-tests gate reads only tests/helpers, so the tests of the plans' acceptance and
# triage checkers had no gate. Completed/ is frozen history and is not run.
#
# Each directory holding tests runs in a process of its own, from that directory (as
# `cd DIR && python3 -m unittest <modules>` would), under the stdlib unittest runner that writes
# its counts as data (helpers/qa_environment/unittest_counts.py --chdir); the walk is checked
# against git's index, and a run whose counts disagree with the request cannot pass.
#
# The tool is Go (src-go/qa/plantests, command src-go/qa/cmd/qa-plan-tests; its package doc holds
# the expectations P1-P9); lib/go-tool.bash builds it under the cache and runs it.
#
# stdout: PLAN-TESTS-OK <n> test(s) in <m> module(s) in <d> plan folder(s), <s> skipped; the rest stderr.
# Exit: 0 every test passed, 1 a test failed, 2 the run could not complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-plan-tests --scripts "$SCRIPT_DIR" "$@"
