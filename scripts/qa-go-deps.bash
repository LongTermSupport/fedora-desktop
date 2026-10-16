#!/usr/bin/env bash
# qa-go-deps.bash — Plan 00204: src-go's dependencies are vendored, pinned and allowlisted.
#
# Every module src-go/go.mod requires has a row with its reason in src-go/go-deps.allowlist; a
# pseudo-version or a replace directive needs a row of its own; a row go.mod does not need fails;
# go mod verify passes, go.mod and go.sum are tidy, and src-go/vendor is byte for byte what
# go mod vendor writes. The shipped binaries are built from that vendor tree, offline.
#
# The tool is Go (src-go/qa/godeps, command src-go/qa/cmd/qa-go-deps); lib/go-tool.bash builds it
# into a private directory under the cache and runs it, so the exit code reaches the caller.
# Specification: CLAUDE/Plan/00204-go-tui-polish/SPEC-qa-go-deps.md.
#
# Usage: scripts/qa-go-deps.bash
# Exit: 0 clean, 1 a finding, 2 the run could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-go-deps --scripts "$SCRIPT_DIR" "$@"
