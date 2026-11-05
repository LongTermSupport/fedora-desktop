#!/usr/bin/bash
# branch-delete-list — the one way to build a list of origin's branches to delete in bulk.
#
# Reads branch names (one per line) from FILE, or stdin when FILE is absent or "-", and prints one
# `git push origin --delete <name>` line per branch for the owner to read and run. It deletes
# nothing. It REFUSES the whole list, naming each offender, when any name is exempt or parked in
# scripts/branch-policy.toml, is a release line (F[0-9]*), is origin's default branch, or has no
# refs/archive/<name> on origin at the branch's tip (CLAUDE/QA.md, "Branch hygiene").
#
# The tool is Go (src-go/qa/branchdelete, command src-go/qa/cmd/branch-delete-list);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller.
#
# Exit codes: 0 every candidate passes, 1 refused, 2 bad usage or could not judge.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool branch-delete-list --scripts "$SCRIPT_DIR" "$@"
