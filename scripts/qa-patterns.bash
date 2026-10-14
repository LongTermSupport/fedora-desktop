#!/usr/bin/bash
# qa-patterns.bash — the semgrep bash-convention rules (.semgrep/bash-conventions.yml) over every
# shell script git would publish, after proving the rules against .semgrep/bash-conventions.bash.
# Scans a temporary mirror so coverage never depends on a file's mode, and exits 2 if semgrep did
# not read, or could not parse, a script it was handed (Plan 00076).
#
# The tool is Go (src-go/qa/patterns, command src-go/qa/cmd/qa-patterns); lib/go-tool.bash builds
# it into a private directory under the cache and runs it, so the exit code reaches the caller.
# Specification: CLAUDE/Plan/00192-.../SPEC-qa-patterns.md.
#
# stdout:  terse — coverage notes, then the verdict and the failing files
# JSON:    ${QA_JSON_OUT:-/tmp/qa-patterns-results.json}
#
# jq usage:
#   jq '.status'           # "pass" or "fail"
#   jq '.summary'          # {total, passed, failed}
#   jq '.failures[]'       # all files with violations
#
# Exit codes: 0 pass, 1 violations found, 2 the run could not complete (semgrep missing, its
# self-test failing, a script not scanned or not parsed, no scripts found).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-patterns --scripts "$SCRIPT_DIR" "$@"
