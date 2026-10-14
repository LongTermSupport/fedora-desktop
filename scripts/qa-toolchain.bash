#!/usr/bin/env bash
# qa-toolchain.bash — the QA tools on this machine are the versions .qa-versions pins.
#
# A gate's verdict belongs to the binary that produced it (two shellchecks four minor releases
# apart reported 141 and 173 issues on one tree), so .qa-versions is the single source of truth
# for ruff, semgrep, shellcheck and golangci-lint, and the go directive of src-go/go.mod is the
# floor for go. Reasoning and how to fix a mismatch: CLAUDE/QA.md, "toolchain".
#
# The tool is Go (src-go/qa/toolchain, command src-go/qa/cmd/qa-toolchain); lib/go-tool.bash
# builds it into a private directory under the cache and runs it, so the exit code reaches the
# caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-toolchain.md.
#
# stdout: one `TOOLCHAIN-OK N tool(s) match .qa-versions` line; diagnostics on stderr.
# Exit: 0 every tool matches, 1 a tool is absent, unreadable or drifted (never 2: exit 2 aborts
# the whole qa-all run), 2 the pins could not be read or the tool could not run.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-toolchain --scripts "$SCRIPT_DIR" "$@"
