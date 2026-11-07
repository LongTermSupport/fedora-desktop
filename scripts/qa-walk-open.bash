#!/usr/bin/env bash
# qa-walk-open.bash — the walk-opens-non-regular-file rule (.semgrep/walk-open.yml; issue #191).
#
# A directory walk lists named pipes, sockets and devices as well as files, and opening a named
# pipe for reading blocks for ever. This gate finds every Python file git would publish that
# opens an entry a walk found without checking it is a regular file. The rule's reasoning, and
# how to fix a finding, are in CLAUDE/QA.md under "walk-opens-non-regular-file".
#
# The rule is proven against .semgrep/walk-open.py on every run. qa-changed runs this gate whole
# (check walk-open) when the rule, the fixture, this wrapper or the tool change;
# scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/sttrules run with its WalkOpen spec, command src-go/qa/cmd/qa-walk-open);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller.
#
# Usage: scripts/qa-walk-open.bash [PATH...]   (paths: run on those files only)
# Exit: 0 clean, 1 findings or a rule that failed its fixture, 2 semgrep missing or broken, or a
# handed file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-walk-open --scripts "$SCRIPT_DIR" "$@"
