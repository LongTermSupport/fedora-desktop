#!/usr/bin/env bash
# qa-untracked-audit.bash — work kept only where git does not carry it (Plan 00245).
#
# Fails on a file in untracked/ older than the age floor that is not scratch (any .md, .patch or
# .diff, and any file not a scratch shape), on a local branch with commits no remote holds, on a
# linked work tree with old uncommitted changes, and on any stash. The data (stores, scratch
# shapes, floor, branches being worked) is scripts/untracked-policy.yml; the tool is Go
# (src-go/qa/untrackedaudit, command src-go/qa/cmd/qa-untracked-audit).
#
# LOCAL ONLY: a clean checkout (CI) has no untracked/ and no work in progress, so CI cannot run it.
# It is not yet in scripts/qa-gates.tsv: the existing pile makes it red until swept (Plan 00245).
#
# Exit codes: 0 clean, 1 findings, 2 bad usage or the audit could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-untracked-audit --scripts "$SCRIPT_DIR" "$@"
