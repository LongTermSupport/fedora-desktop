#!/usr/bin/env bash
# qa-private-names.bash — install-specific names must not reach this public repository
# (Plan 00245 Phase 3). Learns the names from the agent bus and the machine's names, keeps them in
# <git dir>/private-terms (never tracked) and scans: `staged` (pre-commit), `push REMOTE URL`
# (pre-push, ref lines on stdin), `range ARG...`, `tree REV` (the QA gate), `refresh`.
# Findings name a place and a source, never the name. The tool is Go (src-go/qa/privatenames).
#
# In a ccy container a list that cannot be built is exit 2, never a skip.
# Exit codes: 0 clean, 1 findings, 2 bad usage or the guard could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-private-names "$@"
