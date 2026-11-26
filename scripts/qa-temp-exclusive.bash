#!/usr/bin/env bash
# qa-temp-exclusive.bash — the temp-not-exclusive rules (.semgrep/temp-exclusive.yml; Plan 00265 R36).
#
# A temporary named by the process id (<file>.tmp.<pid>) and created without an exclusive create
# follows a symlink planted at that name before it is renamed into place. This gate finds every
# such temporary in the shell and Python of every file git would publish. The rules' reasoning,
# and how to fix a finding, are in CLAUDE/QA.md under "temp-not-exclusive"; Go's half is the
# analyzer project_temp_not_exclusive.
#
# The rules are proven against .semgrep/temp-exclusive.fixture (shell) and
# .semgrep/temp-exclusive.py (Python) on every run. qa-changed runs this gate whole (check
# temp-exclusive) for a change to a script or the gate; scripts/qa-routing.tsv has the rows.
#
# The tool is Go (src-go/qa/cmd/qa-temp-exclusive, the engine of src-go/qa/ghscopes run with its
# TempExclusive spec); semgrep stays the engine. lib/go-tool.bash builds it into a private
# directory under the cache and runs it, so the exit code reaches the caller.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or a handed
# file that was not scanned.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-temp-exclusive --scripts "$SCRIPT_DIR" "$@"
