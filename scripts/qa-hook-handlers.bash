#!/usr/bin/bash
# qa-hook-handlers.bash — the project-owned hooks-daemon handlers (.claude/hooks/handlers) pass
# their own tests (Plan 00191 Task 4.6).
#
# The handlers import claude_code_hooks_daemon, which only the daemon's venv has, and that venv has
# no pytest. The tool runs the tests under the daemon's interpreter (found by the daemon's own
# resolver, never a guessed venv name; a linked worktree reads the main checkout's daemon, which
# is gitignored) with the project's pytest, the one the interpreter of the same Python version
# carries, appended to its path. A missing daemon, venv or pytest is exit 2 naming the fix.
# CI's checkout has no daemon: .github/workflows/qa.yml exempts it there, by name and reason.
#
# The tool is Go (src-go/qa/hookhandlers, command src-go/qa/cmd/qa-hook-handlers; its package doc
# holds the expectations K1-K8); lib/go-tool.bash builds it under the cache and runs it.
#
# stdout: HOOK-HANDLERS-OK <n> test(s) ..., or HOOK-HANDLERS-EXEMPT <reason> in CI; the rest stderr.
# Exit: 0 passed (or exempt in CI), 1 a test failed, 2 the run could not complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-hook-handlers --scripts "$SCRIPT_DIR" "$@"
