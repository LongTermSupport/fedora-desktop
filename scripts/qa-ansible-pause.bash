#!/usr/bin/env bash
# qa-ansible-pause.bash — the rule pause-without-register: an ansible.builtin.pause task under
# playbooks/ or tasks/ must `register:` its answer or carry a `# PAUSE-OK: <reason>` comment.
# Reasoning and how to fix a finding: CLAUDE/QA.md, "pause-without-register".
#
# The tool is Go (src-go/qa/ansiblepause, command src-go/qa/cmd/qa-ansible-pause);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-ansible-pause.md. The rule
# is proven against tests/fixtures/ansible-pause by the tool's Go tests, not on every run.
#
# Usage: scripts/qa-ansible-pause.bash [PATH...]   (paths: scan those files only)
# stdout: `passed: N failed: M`; findings on stderr.
# Exit: 0 clean, 1 findings, 2 the run could not complete.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-ansible-pause --scripts "$SCRIPT_DIR" "$@"
