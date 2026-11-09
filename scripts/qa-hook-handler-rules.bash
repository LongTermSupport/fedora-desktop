#!/usr/bin/env bash
# qa-hook-handler-rules.bash — the rules in .semgrep/hook-handlers.yml (Plan 00191).
#
#   hook-path-resolved-against-cwd       a hook handler resolving a path against its cwd
#
# Code the hooks daemon loads (the project's handlers in .claude/hooks/handlers/) runs in the
# daemon's process, whose working directory is not the agent's project. Each rule's reasoning,
# and how to fix a finding, is in CLAUDE/QA.md under its identifier.
#
# The tool is Go (src-go/qa/sttrules run with its HookHandlers spec, command
# src-go/qa/cmd/qa-hook-handler-rules); lib/go-tool.bash builds it into a private directory under
# the cache and runs it, so the exit code reaches the caller. It proves each rule against
# .semgrep/hook-handlers.py on every run, then judges every Python file git would publish, by name.
#
# Usage: scripts/qa-hook-handler-rules.bash [PATH...]   (paths: run on those files only)
# Exit: 0 clean, 1 findings or a rule that failed its fixture, 2 semgrep missing or broken, or
# a handed file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-hook-handler-rules --scripts "$SCRIPT_DIR" "$@"
