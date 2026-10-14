#!/usr/bin/bash
# qa-ansible-syntax.bash — `ansible-playbook --syntax-check` on every playbook in the repository:
# a YAML file git would publish with a top-level `- hosts:` or `- import_playbook:` line. Parse
# only: SAFE in the CCY container, it executes no task. Exits 2 when ansible-playbook is not
# installed, ansible.cfg is missing, a password file the config names is absent or empty (a clean
# checkout and a bare `git worktree add` lack it; the gate never reads it), no playbook is found,
# or a tracked playbook is not in the checked population.
#
# The tool is Go (src-go/qa/ansiblesyntax, command src-go/qa/cmd/qa-ansible-syntax);
# lib/go-tool.bash builds it into a private directory under the cache and runs it, so the exit
# code reaches the caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-ansible-syntax.md.
#
# stdout:  terse — each failing playbook with its error, then the verdict
# JSON:    ${QA_JSON_OUT:-/tmp/qa-ansible-syntax-results.json}
#
# jq usage:
#   jq '.status'         # "pass" or "fail"
#   jq '.summary'        # {total, passed, failed}
#   jq '.failures[]'     # all playbooks with syntax errors (file + error)
#
# Exit codes: 0 pass, 1 one or more playbooks failed --syntax-check, 2 the run could not complete.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-ansible-syntax --scripts "$SCRIPT_DIR" "$@"
