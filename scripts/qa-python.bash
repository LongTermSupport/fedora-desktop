#!/usr/bin/bash
# Python QA validation - LLM-friendly
# stdout:  terse — errors + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-python-results.json}
#
# jq usage:
#   jq '.status'                # "pass" or "fail"
#   jq '.summary'               # {total, passed, failed}
#   jq '.failures[]'            # syntax errors
#   jq '.ruff_diagnostics[]'    # ruff issues with file/line/code/message

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-python-results.json}"
TMP_RESULTS=$(mktemp)
trap 'rm -f "$TMP_RESULTS"' EXIT
ERRORS=0

# Fail fast: check required dependencies
if ! command -v ruff &>/dev/null; then
    echo "✗ python: ruff not installed (sudo dnf install ruff)"
    exit 1
fi

# Discover files
PY_FILES=()
while IFS= read -r -d '' file; do
    PY_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f -name "*.py" \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/.claude/hooks-daemon/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    ! -path "*/__pycache__/*" \
    ! -path "*/.venv/*" \
    ! -path "*/venv/*" \
    -print0)

while IFS= read -r file; do
    if head -n1 "$file" 2>/dev/null | grep -q "^#!/.*python"; then
        PY_FILES+=("$file")
    fi
done < <(find "$REPO_ROOT" -type f -executable \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/.claude/hooks-daemon/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    ! -name "*.py")

TOTAL=${#PY_FILES[@]}

# Syntax check each file
for file in "${PY_FILES[@]}"; do
    rel_path="${file#$REPO_ROOT/}"
    if err=$(python3 -m py_compile "$file" 2>&1); then
        jq -nc --arg f "$rel_path" '{"file":$f,"type":"python","status":"pass"}' >> "$TMP_RESULTS"
    else
        echo "✗ python: $rel_path: $err"
        jq -nc --arg f "$rel_path" --arg e "$err" \
            '{"file":$f,"type":"python","status":"fail","error":$e}' >> "$TMP_RESULTS"
        ((ERRORS++)) || true
    fi
done

# Ruff: auto-fix then capture remaining diagnostics as JSON
RUFF_JSON="[]"
if [[ $TOTAL -gt 0 ]]; then
    ruff check --fix "${PY_FILES[@]}" >/dev/null 2>&1 || true
    ruff_raw=$(ruff check --output-format json "${PY_FILES[@]}" 2>/dev/null) || true
    RUFF_JSON="${ruff_raw:-[]}"
    ruff_count=$(printf '%s' "$RUFF_JSON" | jq 'length')
    if [[ "$ruff_count" -gt 0 ]]; then
        echo "✗ python: ruff: $ruff_count issues (see $JSON_OUT .ruff_diagnostics)"
        ((ERRORS++)) || true
    fi
fi

# Write JSON
STATUS="pass"
[[ $ERRORS -gt 0 ]] && STATUS="fail"

jq -s \
    --arg status "$STATUS" \
    --argjson ruff "$RUFF_JSON" \
    '{
        "type": "python",
        "status": $status,
        "summary": {
            "total": length,
            "passed": ([.[] | select(.status == "pass")] | length),
            "failed": ([.[] | select(.status == "fail")] | length)
        },
        "results": .,
        "failures": [.[] | select(.status == "fail")],
        "ruff_diagnostics": $ruff
    }' "$TMP_RESULTS" > "$JSON_OUT"

# Terse summary
if [[ $ERRORS -eq 0 ]]; then
    echo "✓ python: $TOTAL files OK"
    exit 0
else
    echo "✗ python: failed → $JSON_OUT"
    exit 1
fi
