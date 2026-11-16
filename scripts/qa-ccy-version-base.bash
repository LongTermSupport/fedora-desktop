#!/usr/bin/env bash
# The commit the pre-commit hook judges a ccy version bump against (issue #298): HEAD outside a
# merge; in a merge, the parent on a protected line, or an empty line when neither parent is on
# one. A merge whose staged CCY_VERSION or container LABEL is below a parent's is refused. The
# tool is Go (src-go/qa/ccyversionbase, command src-go/qa/cmd/qa-ccy-version-base).
# Exit codes: 0 named, 1 refused, 2 could not judge, 64 usage.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-ccy-version-base "$@"
