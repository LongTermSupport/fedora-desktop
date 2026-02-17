#!/usr/bin/bash
# Run all QA checks - LLM-friendly
# stdout:  terse — errors + final summary only
# JSON:    /tmp/qa-results.json
#
# jq usage:
#   jq '.status'               # "pass" or "fail"
#   jq '.summary'              # {total, passed, failed}
#   jq '.failures[]'           # all failures across bash + python
#   jq '.checks.bash'          # bash-specific results
#   jq '.checks.python'        # python-specific results
#   jq '.checks.python.ruff_diagnostics[]'  # ruff issues

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
JSON_OUT="/tmp/qa-results.json"
TMP_BASH=$(mktemp)
TMP_PYTHON=$(mktemp)
trap 'rm -f "$TMP_BASH" "$TMP_PYTHON"' EXIT
FAILED=0

# Run sub-checks (each writes JSON to temp file, outputs terse to stdout)
if ! QA_JSON_OUT="$TMP_BASH" "$SCRIPT_DIR/qa-bash.bash"; then
    ((FAILED++)) || true
fi

if ! QA_JSON_OUT="$TMP_PYTHON" "$SCRIPT_DIR/qa-python.bash"; then
    ((FAILED++)) || true
fi

# Merge JSON from both checks
STATUS="pass"
[[ $FAILED -gt 0 ]] && STATUS="fail"

jq -s \
    --arg status "$STATUS" \
    '{
        "status": $status,
        "summary": {
            "total":  ([.[].summary.total]  | add // 0),
            "passed": ([.[].summary.passed] | add // 0),
            "failed": ([.[].summary.failed] | add // 0)
        },
        "failures": [.[].failures[]],
        "checks": {
            "bash":   .[0],
            "python": .[1]
        }
    }' "$TMP_BASH" "$TMP_PYTHON" > "$JSON_OUT"

# Final terse summary
TOTAL=$(jq '.summary.total' "$JSON_OUT")
if [[ $FAILED -eq 0 ]]; then
    echo "✓ QA passed: $TOTAL files checked"
    exit 0
else
    NERRORS=$(jq '.summary.failed' "$JSON_OUT")
    echo "✗ QA FAILED: $NERRORS errors in $TOTAL files"
    echo "  Details: jq '.failures[]' $JSON_OUT"
    exit 1
fi
