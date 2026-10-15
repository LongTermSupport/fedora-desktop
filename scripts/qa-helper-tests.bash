#!/usr/bin/bash
# qa-helper-tests.bash — run every helpers/ unit test (tests/helpers/**/test_*.py, stdlib unittest, no
# pytest or venv — see helpers/CLAUDE.md) and prove which tests ran.
#
# The helper packages are namespace packages with no __init__.py, so `python3 -m unittest discover`
# collects nothing and says "Ran 0 tests". The tool names each test module explicitly, checks the
# list against git's index (a tracked test the walk lost is exit 2; an untracked one runs and is
# named), and reads back the counts file the runner writes, so a run that did not happen cannot pass.
#
# The tool is Go (src-go/qa/helpertests, command src-go/qa/cmd/qa-helper-tests); lib/go-tool.bash
# builds it into a private directory under the cache and runs it, so the exit code reaches the
# caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-helper-tests.md.
#
# Usage: scripts/qa-helper-tests.bash [--counts-file PATH [--counts-token VALUE]]
#   --counts-file asks for the run's size and skip count as DATA (qa-all reads it; unittest counts a
#   skipped test inside testsRun, so the output cannot tell two machines apart).
# Everything a human reads goes to stderr; stdout stays empty.
# Exit: 0 every test passed, 1 a test failed, 2 the run could not complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-helper-tests --scripts "$SCRIPT_DIR" "$@"
