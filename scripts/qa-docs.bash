#!/usr/bin/bash
# qa-docs.bash — documentation integrity QA: every relative link target exists and is tracked,
# every #anchor matches a heading, every playbook playbook-main.yml imports is named in
# docs/playbooks.md and docs/architecture.md, every CLAUDE/ topic file is in CLAUDE.md, and every
# gate qa-gates.tsv runs has a row in CLAUDE/QA.md (and the reverse). Scope is the core
# documents; CLAUDE/Plan/** is excluded on principle (a core gate must not depend on plan content).
#
# The tool is Go (src-go/qa/docs, command src-go/qa/cmd/qa-docs); lib/go-tool.bash builds it into
# a private directory under the cache and runs it, so the exit code reaches the caller.
# Specification: CLAUDE/Plan/00192-.../SPEC-qa-docs.md.
#
# stdout:  terse — the ⚠ block, then the findings and the verdict
# JSON:    ${QA_JSON_OUT:-/tmp/qa-docs-results.json}
#
# jq usage:
#   jq '.status'        # "pass" or "fail"
#   jq '.failures[]'    # every finding, with file and what is wrong
#
# Exit codes: 0 pass, 1 at least one finding, 2 the run could not complete (an empty scope, a file
# that could not be read or parsed, bad usage): never a clean result.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-docs --scripts "$SCRIPT_DIR" "$@"
