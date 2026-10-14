#!/usr/bin/bash
# Bash-limits gate — LLM-friendly
# stdout:  one BASH-LIMITS-OK summary line (the payload)
# stderr:  the findings
#
# Enforces "tooling is Go; bash only for very small helpers" (CLAUDE.md, src-go/CLAUDE.md): at
# most 60 code lines, 4 functions, no associative arrays, no interactive UI per bash file, with
# a ratchet allowlist (scripts/bash-limits.allowlist) for the files already over. The logic is
# the Go command src-go/cmd/qa-bash-limits, built here into a temp file so its exit code reaches
# the caller (go run flattens it to 1).
#
# --base compares the allowlist with that ref's copy and fails on any added entry or raised
# record: the default is HEAD, which catches a hand-edited allowlist before it is committed;
# CI sets QA_BASH_LIMITS_BASE to the push's or pull request's base commit, so a commit that
# sneaks an entry in is caught there. Other flags (--update, --seed) pass through.
#
# Exit codes:
#   0  pass
#   1  findings
#   2  could not run (missing go, git failure, missing or malformed allowlist)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if ! command -v go > /dev/null; then
    echo "✗ bash-limits: go not installed (install via playbooks/imports/play-golang.yml)" >&2
    exit 2
fi
BIN="$(mktemp)"
trap 'rm -f "$BIN"' EXIT
(cd "$REPO_ROOT/src-go" && go build -o "$BIN" ./cmd/qa-bash-limits)
"$BIN" --base "${QA_BASH_LIMITS_BASE:-HEAD}" --root "$REPO_ROOT" "$@"
