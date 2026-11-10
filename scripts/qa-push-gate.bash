#!/usr/bin/env bash
# The pre-push gate on qa-all's verdict (rule push-without-qa-verdict, CLAUDE/QA.md): a push of
# commits the remote does not hold yet goes ahead only when the last qa-all run in this checkout
# passed on them. scripts/git-hooks/pre-push runs it with git's arguments (REMOTE URL) and ref
# lines on stdin. The tool is Go (src-go/qa/pushgate, command src-go/qa/cmd/qa-push-gate).
# Exit codes: 0 the push may go, 1 refused, 2 the gate could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-push-gate "$@"
