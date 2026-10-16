#!/usr/bin/env bash
# qa-go-vuln.bash — Plan 00204: src-go's code reaches no known vulnerability.
#
# govulncheck is pinned by the tool directive of src-go/qa/go.mod (its version in go.mod, its
# content in go.sum; nothing installed system-wide) and runs over src-go, whose package load reads
# src-go/vendor. It needs the vulnerability database (vuln.go.dev): a scan that could not fetch
# it is exit 2, never a pass.
#
# The tool is Go (src-go/qa/godeps, command src-go/qa/cmd/qa-go-vuln); lib/go-tool.bash builds it
# into a private directory under the cache and runs it, so the exit code reaches the caller.
# Specification: CLAUDE/Plan/00204-go-tui-polish/SPEC-qa-go-deps.md.
#
# Usage: scripts/qa-go-vuln.bash
# Exit: 0 clean, 1 a vulnerability is reached, 2 the run could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-go-vuln --scripts "$SCRIPT_DIR" "$@"
