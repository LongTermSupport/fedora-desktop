#!/usr/bin/bash
# qa-deployed-drift — repo-owned files that a play deploys must match their deployed copies.
#
# Plan 00094 fixed files/home/.local/bin/ftp-camera in the repo and never ran the play that
# deploys it; every mechanical check read the repo, which was correct. This gate compares the
# repo with the host: files/home/.local/bin, the VM acceptance lab and the agent bus files. A
# file is compared only when its deployed copy exists. The .j2 templates are checked for a
# playbook dest: (a source-tree fact, verified everywhere) but never byte-compared.
#
# The host half is skipped, with a printed reason and a warning mark, in the CCY container, in a
# linked git worktree and where ~/.local/bin does not exist. Those skips are listed in
# CLAUDE/QA.md; a new one must be added there in the same commit.
#
# The tool is Go (src-go/qa/deployeddrift, command src-go/qa/cmd/qa-deployed-drift);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-deployed-drift.md.
#
# stdout: the stage line (a pass or a skip) and the template lines; drift goes to stderr.
# Exit codes: 0 in sync or skipped, 1 drift, 2 bad usage or the run could not complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-deployed-drift --scripts "$SCRIPT_DIR" "$@"
