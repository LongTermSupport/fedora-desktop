#!/usr/bin/bash
# qa-js.bash — every JavaScript file git would publish must parse (`node --check`), and the
# GNOME-extension project must pass ESLint under the repository's own config only (no config
# lookup, no inline directives). Exits 2 if node or the extensions/ ESLint tooling is missing,
# no JavaScript file is found, or ESLint did not read a file it was given.
#
# The tool is Go (src-go/qa/js, command src-go/qa/cmd/qa-js); lib/go-tool.bash builds it
# into a private directory under the cache and runs it, so the exit code reaches the caller.
# Specification: CLAUDE/Plan/00192-.../SPEC-qa-js.md.
#
# stdout:  terse — errors, then the verdict
# JSON:    ${QA_JSON_OUT:-/tmp/qa-js-results.json}
#
# jq usage:
#   jq '.status'         # "pass" or "fail"
#   jq '.summary'        # {total, passed, failed}
#   jq '.failures[]'     # files with a syntax or lint error
#   jq '.eslint'         # eslint sub-result ({ran, status, error})
#
# Exit codes: 0 pass, 1 findings, 2 the run could not complete (a missing tool, a failed
# discovery, an ESLint that did not read a file).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-js --scripts "$SCRIPT_DIR" "$@"
