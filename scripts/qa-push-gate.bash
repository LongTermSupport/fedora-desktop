#!/usr/bin/env bash
# The pre-push gate on CI's QA verdict (rule push-without-qa-verdict, CLAUDE/QA.md): a protected
# branch (exempt in scripts/branch-policy.toml) moves only to a commit GitHub CI's QA workflow
# passed on a push; any other ref is not judged. scripts/git-hooks/pre-push runs it with git's
# arguments (REMOTE URL) and ref lines on stdin. The tool is Go (src-go/qa/pushgate, command src-go/qa/cmd/qa-push-gate).
# Exit codes: 0 the push may go, 1 refused, 2 the gate could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-push-gate "$@"
