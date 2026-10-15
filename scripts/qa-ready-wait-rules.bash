#!/usr/bin/env bash
# qa-ready-wait-rules.bash — the rule ready-wait-ignores-child-exit (Plan 00156).
#
# A loop that waits for a process this code started, and never asks whether that process has
# exited, reports a crash as a timeout and hides the process's own error. The reasoning, and how to
# fix a finding, is in CLAUDE/QA.md under the identifier. Python is judged by semgrep
# (.semgrep/ready-wait.yml, proven against .semgrep/ready-wait.py), shell by the rule in the Go
# package src-go/qa/readywait (proven against .semgrep/ready-wait.bash).
#
# The tool is Go (src-go/qa/readywait, command src-go/qa/cmd/qa-ready-wait-rules); lib/go-tool.bash
# builds it into a private directory under the cache and runs it, so the exit code reaches the
# caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-ready-wait-rules.md.
#
# Usage: scripts/qa-ready-wait-rules.bash [PATH...]   (paths: run on those files only)
# Exit: 0 clean, 1 findings or a rule that failed its fixture, 2 a tool missing or broken, or a
# handed file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-ready-wait-rules --scripts "$SCRIPT_DIR" "$@"
