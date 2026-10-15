#!/usr/bin/env bash
# qa-plan-script-logging.bash — Plan 00130: no active plan script sends its own output to a process
# substitution (exec > >(tee "$LOG") 2>&1), and no active plan folder keeps a logs/ directory.
#
# The shell cannot wait on a process substitution, so the run log loses its last chunk, which is
# the part naming what failed; a plan-local logs/ directory hides unscrubbed run logs from every
# git status. Run logs belong under untracked/plan-runs/ (plan_start_log in the plan library).
# Scope is CLAUDE/Plan/NNNNN-*/ only: Completed/ is frozen history.
#
# The tool is Go (src-go/qa/planscriptlogging, command src-go/qa/cmd/qa-plan-script-logging);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit code
# reaches the caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-plan-script-logging.md.
#
# Usage: scripts/qa-plan-script-logging.bash
# Exit: 0 clean, 1 an offence, 2 the run could not complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-plan-script-logging --scripts "$SCRIPT_DIR" "$@"
