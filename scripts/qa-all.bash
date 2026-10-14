#!/usr/bin/bash
# The full QA run, the gate before every push: every gate of scripts/qa-gates.tsv in order, one
# stage line each, never stopping at the first failure, one verdict at the end.
#
#   ./scripts/qa-all.bash          run every gate
#   ./scripts/qa-all.bash --help   the full usage
#
# The machine report is /tmp/qa-results.json (QA_RESULTS_JSON overrides the path):
#   jq '.status'                   # "pass" or "fail"
#   jq '.summary'                  # {total, passed, failed}
#   jq '.failures[]'               # every failing file across the seven report stages
#   jq '.checks.bash'              # one stage's whole report (also python, patterns, ansible,
#                                  # ansible_syntax, js, docs)
#
# The tool is Go (src-go/qa/all, command src-go/qa/cmd/qa-all); lib/go-tool.bash builds it into a
# private directory under the cache and runs it, so the exit code reaches the caller. The gate list
# is data, scripts/qa-gates.tsv: adding a gate is adding a row. QA_ALL_TIMEOUT is the per-gate
# limit as a Go duration (30m default, 0 disables).
#
# stdout is the stage lines and the verdict; failure detail goes to stderr.
# Exit codes: 0 every gate passed, 1 a gate failed, 2 bad usage or the run could not complete (a
# stage reporting a missing tool, an unreadable gate table, a build failure of the tool itself).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-all --scripts "$SCRIPT_DIR" "$@"
