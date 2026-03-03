#!/usr/bin/bash
# Pattern-based QA using Semgrep - LLM-friendly
# Runs .semgrep/bash-conventions.yml rules against all bash files in the repo.
# stdout:  terse — violations + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-patterns-results.json}
#
# jq usage:
#   jq '.status'           # "pass" or "fail"
#   jq '.summary'          # {total, passed, failed}
#   jq '.failures[]'       # all files with violations
#
# Exit codes:
#   0  pass
#   1  fail (violations found)
#   2  missing required tool (semgrep not installed)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-patterns-results.json}"
TMP_SEMGREP=$(mktemp)
TMP_SEMGREP_ERR=$(mktemp)
trap 'rm -f "$TMP_SEMGREP" "$TMP_SEMGREP_ERR"' EXIT

# Require semgrep
if ! command -v semgrep >/dev/null 2>/dev/null; then
    echo "ERROR: semgrep not found. Install with: pipx install semgrep" >&2
    exit 2
fi

# Run semgrep
# --json-output: write findings JSON to file (separate from progress output)
# --quiet:       suppress progress/banner output
# --metrics=off: no telemetry
rc=0
semgrep \
    --config "$REPO_ROOT/.semgrep/bash-conventions.yml" \
    --json-output "$TMP_SEMGREP" \
    --metrics=off \
    --quiet \
    --exclude '.git' \
    --exclude 'node_modules' \
    --exclude 'untracked' \
    --exclude '.ansible/roles' \
    --exclude '.claude/ccy/plugins' \
    --exclude '.claude/ccy/file-history' \
    "$REPO_ROOT" 2>"$TMP_SEMGREP_ERR" || rc=$?

# rc >= 2 means semgrep itself failed (not just "found something")
if [[ $rc -ge 2 ]]; then
    echo "ERROR: semgrep failed (exit $rc)" >&2
    cat "$TMP_SEMGREP_ERR" >&2
    exit 2
fi

# Validate JSON output was produced
if [[ ! -s "$TMP_SEMGREP" ]]; then
    echo "ERROR: semgrep produced no JSON output" >&2
    cat "$TMP_SEMGREP_ERR" >&2
    exit 2
fi

# Build QA JSON from semgrep output
# Semgrep JSON fields used:
#   .paths.scanned[]          - files that were scanned
#   .results[].path           - file with a finding
#   .results[].check_id       - rule that matched
#   .results[].start.line     - line number
#   .results[].extra.message  - human-readable message
jq \
    '{
        "type": "patterns",
        "status": (if (.results | length) > 0 then "fail" else "pass" end),
        "summary": {
            "total":  (.paths.scanned | length),
            "passed": ((.paths.scanned | length) - (.results | map(.path) | unique | length)),
            "failed": (.results | map(.path) | unique | length)
        },
        "failures": [
            .results | group_by(.path)[] | {
                "file":   .[0].path,
                "type":   "patterns",
                "status": "fail",
                "error":  (map(.check_id + ":" + (.start.line | tostring) + " " + .extra.message) | join("; "))
            }
        ]
    }' "$TMP_SEMGREP" > "$JSON_OUT"

# Terse summary
TOTAL=$(jq '.summary.total' "$JSON_OUT")
ERRORS=$(jq '.summary.failed' "$JSON_OUT")

if [[ $ERRORS -eq 0 ]]; then
    echo "✓ patterns: $TOTAL files OK"
    exit 0
else
    echo "✗ patterns: $ERRORS/$TOTAL files failed → $JSON_OUT"
    jq -r '.failures[] | "  ✗ \(.file)\n    \(.error)"' "$JSON_OUT"
    exit 1
fi
