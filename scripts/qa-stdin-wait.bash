#!/usr/bin/env bash
# qa-stdin-wait.bash — the stdin-read-unbounded rule (.semgrep/stdin-wait.yml; issue #260).
#
# A read of stdin returns at end of input, and an open pipe that nothing writes to or closes never
# ends: the command waits for ever and says nothing. This gate finds every Python file git would
# publish that reads stdin directly instead of through helpers/stdin_text, which says when it is
# waiting and takes a bound. The rule's reasoning, and how to fix a finding, are in CLAUDE/QA.md
# under "stdin-read-unbounded".
#
# The rule is proven against .semgrep/stdin-wait.py on every run. qa-changed runs this gate over
# each changed Python file (check stdin-wait-files), and whole (check stdin-wait) when the rule,
# the fixture, this wrapper or the tool change; scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/sttrules run with its StdinWait spec, command
# src-go/qa/cmd/qa-stdin-wait); lib/go-tool.bash builds it into a private directory under the
# cache and runs it, so the exit code reaches the caller.
#
# Usage: scripts/qa-stdin-wait.bash [PATH...]   (paths: run on those files only)
# Exit: 0 clean, 1 findings or a rule that failed its fixture, 2 semgrep missing or broken, or a
# handed file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-stdin-wait --scripts "$SCRIPT_DIR" "$@"
