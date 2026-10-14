#!/usr/bin/bash
# qa-python.bash — every Python file git would publish must compile (python3) and pass ruff, and
# every Python template must compile once its {{ }} expressions are substituted. Exits 2 if ruff
# or python3 is missing, .qa-versions has no usable ruff pin, no Python file is found, ruff did
# not read a file it was given, or a template holds Jinja statements. A ruff of another version
# than the pin FAILS the gate (exit 1) without running it.
#
# The tool is Go (src-go/qa/python, command src-go/qa/cmd/qa-python); lib/go-tool.bash builds it
# into a private directory under the cache and runs it, so the exit code reaches the caller.
# Specification: CLAUDE/Plan/00192-.../SPEC-qa-python.md.
#
# stdout:  terse — compile errors, the ruff count, then the verdict
# JSON:    ${QA_JSON_OUT:-/tmp/qa-python-results.json}
#
# jq usage:
#   jq '.status'                # "pass" or "fail"
#   jq '.summary'               # {total, passed, failed}
#   jq '.failures[]'            # files with a compile error or a ruff finding
#   jq '.ruff_diagnostics[]'    # ruff issues with filename/location/code/message
#
# Exit codes: 0 pass, 1 findings (or a ruff version other than the pin), 2 the run could not
# complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-python --scripts "$SCRIPT_DIR" "$@"
