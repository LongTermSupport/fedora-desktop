#!/usr/bin/bash
# qa-bash.bash — every shell script git would publish must parse (bash -n) and have no shellcheck
# finding of level error or warning (info and style stay advisory). Exits 2 if shellcheck or bash
# is missing, discovery finds no shell file or misses a tracked one, or an engine did not answer.
#
# The tool is Go (src-go/qa/bash, command src-go/qa/cmd/qa-bash); lib/go-tool.bash builds it into
# a private directory under the cache and runs it, so the exit code reaches the caller.
# Specification: CLAUDE/Plan/00192-.../SPEC-qa-bash.md.
#
# stdout:  terse — syntax errors, gating findings, then the verdict
# JSON:    ${QA_JSON_OUT:-/tmp/qa-bash-results.json}
#
# jq usage:
#   jq '.status'                   # "pass" or "fail"
#   jq '.summary'                  # {total, passed, failed}
#   jq '.failures[]'               # files that do not parse or have an error/warning finding
#   jq '.shellcheck_diagnostics[]' # every shellcheck finding, advisory levels included
#
# Exit codes: 0 pass, 1 findings, 2 the run could not complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-bash --scripts "$SCRIPT_DIR" "$@"
