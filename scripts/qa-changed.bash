#!/usr/bin/bash
# Diff-scoped QA for use while iterating: runs only the checks the CHANGED files need, so the
# full ./scripts/qa-all.bash (every gate, every suite) is needed only before a push.
#
#   ./scripts/qa-changed.bash                 files changed vs HEAD, plus untracked (not ignored)
#   ./scripts/qa-changed.bash --base <ref>    files changed vs <ref> (e.g. origin/F44)
#   ./scripts/qa-changed.bash --plan          print the chosen checks, run nothing
#   ./scripts/qa-changed.bash --files F...    use exactly these repo-relative files (no git)
#   ./scripts/qa-changed.bash --help          the full usage
#
# The tool is Go (src-go/qa/changed, command src-go/qa/cmd/qa-changed); lib/go-tool.bash builds it
# into a private directory under the cache and runs it, so the exit code reaches the caller. What each kind of file gets is data: scripts/qa-routing.tsv, the
# excluded trees scripts/qa-trees.tsv, the Go command suites scripts/go-binary-suites.tsv.
#
# stdout is the report; failure detail goes to stderr.
# Exit codes: 0 pass (or nothing changed), 1 a check failed, 2 missing tool / bad usage / a
# check that could not run (a build failure of the tool itself is 2 too).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-changed --scripts "$SCRIPT_DIR" "$@"
