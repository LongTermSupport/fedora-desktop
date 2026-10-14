#!/usr/bin/bash
# Bash-limits gate — LLM-friendly
# stdout:  one BASH-LIMITS-OK summary line (the payload)
# stderr:  the findings
#
# Enforces "tooling is Go; bash only for very small helpers" (CLAUDE.md, src-go/CLAUDE.md):
# the limits, the exemptions and the measure are in the Go package src-go/qa/bashlimits, and
# scripts/bash-limits.allowlist is the ratchet for the files already over. The command is built
# and run by lib/go-tool.bash so its exit code reaches the caller.
#
# The allowlist is compared with a base: QA_BASH_LIMITS_BASE if set (CI sets it), else the
# merge-base of HEAD and origin's default branch, so commits not pushed yet are compared too.
# Other flags (--update, --remeasure, --seed, --base) pass through.
#
# Exit codes:
#   0  pass
#   1  findings
#   2  could not run (missing go, git failure, missing or malformed allowlist, no base)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-bash-limits ${QA_BASH_LIMITS_BASE:+--base "$QA_BASH_LIMITS_BASE"} --root "$REPO_ROOT" "$@"
