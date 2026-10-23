#!/usr/bin/env bash
# qa-lxc-attach-streams.bash — the rule lxc-attach-file-stream: no command that reaches lxc-attach
# (by name, through a variable holding it, or through a function that runs it) may have stdin,
# stdout or stderr on a file. lxc-attach hands each such file to the container's root, so the
# caller can no longer read what it captured. How to fix a finding: CLAUDE/QA.md under the
# identifier; the reason: CLAUDE/AgentNotes.md, "lxc-attach chowns any file".
#
# The tool is Go (src-go/qa/lxcattach, command src-go/qa/cmd/qa-lxc-attach-streams), proven
# against .semgrep/lxc-attach-streams.bash on every run; lib/go-tool.bash builds it into a
# private directory under the cache and runs it, so the exit code reaches the caller.
#
# Usage: scripts/qa-lxc-attach-streams.bash
# Exit: 0 clean, 1 a finding or a rule that failed its fixture, 2 the run could not complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-lxc-attach-streams --scripts "$SCRIPT_DIR" "$@"
