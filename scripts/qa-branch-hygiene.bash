#!/usr/bin/bash
# qa-branch-hygiene — the repository's remote branches against scripts/branch-policy.toml.
#
# At most `cap` open work branches, no branch merged into the default branch left undeleted, no
# work branch dormant past the limits unless parked with a reason and a review date. It judges
# the repository's global state, so it is NOT a gate of qa-all or qa-changed: only the
# branch-hygiene workflow runs it (CLAUDE/QA.md, "Branch hygiene"), and a person may, after
# `git fetch --prune origin`.
#
# The tool is Go (src-go/qa/branchhygiene, command src-go/qa/cmd/qa-branch-hygiene);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller.
#
# Exit codes: 0 clean, 1 findings, 2 bad usage or the repository could not be judged.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-branch-hygiene --scripts "$SCRIPT_DIR" "$@"
