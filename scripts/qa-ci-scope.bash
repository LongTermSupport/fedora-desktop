#!/usr/bin/env bash
# How much QA a CI run of .github/workflows/qa.yml does (issue #245, CLAUDE/QA.md#push-without-qa-verdict):
# the full qa-all on a tag, a protected branch or a release branch; qa-changed's diff mode against
# the tip of the protected line a work branch is built on, or a pull request's base. Prints
# mode=, line= and base= lines for $GITHUB_OUTPUT. The tool is Go (src-go/qa/ciscope, command
# src-go/qa/cmd/qa-ci-scope).
# Exit codes: 0 decided, 1 the branch contains no single protected line's tip, 2 could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-ci-scope "$@"
