#!/usr/bin/bash
# Bash-limits gate — LLM-friendly
# stdout:  one BASH-LIMITS-OK summary line (the payload)
# stderr:  the findings
#
# Enforces "tooling is Go; bash only for very small helpers" (CLAUDE.md, src-go/CLAUDE.md):
# the limits, the exemptions and the measure are in the Go package src-go/qa/bashlimits, and
# scripts/bash-limits.allowlist is the ratchet for the files already over. The command is built
# here into a private directory under the cache (not /tmp, which can be noexec) so its exit
# code reaches the caller (go run flattens it to 1).
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

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if ! command -v go > /dev/null; then
    echo "✗ bash-limits: go not installed (install via playbooks/imports/play-golang.yml)" >&2
    exit 2
fi
CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/fedora-desktop-qa"
mkdir -p "$CACHE"
BIN_DIR="$(mktemp -d -p "$CACHE")"
trap 'rm -rf "$BIN_DIR"' EXIT
go build -C "$REPO_ROOT/src-go/qa" -o "$BIN_DIR/qa-bash-limits" ./cmd/qa-bash-limits
"$BIN_DIR/qa-bash-limits" ${QA_BASH_LIMITS_BASE:+--base "$QA_BASH_LIMITS_BASE"} --root "$REPO_ROOT" "$@"
