#!/usr/bin/bash
# Bash/Shell QA validation - LLM-friendly
# stdout:  terse — errors + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-bash-results.json}
#
# jq usage:
#   jq '.status'                   # "pass" or "fail"
#   jq '.summary'                  # {total, passed, failed}
#   jq '.failures[]'               # all failed files with errors
#   jq '.shellcheck_diagnostics[]' # shellcheck issues (if installed)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-bash-results.json}"
TMP_RESULTS=$(mktemp)
trap 'rm -f "$TMP_RESULTS"' EXIT
ERRORS=0

# Discover files
BASH_FILES=()
while IFS= read -r -d '' file; do
    BASH_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f \( -name "*.sh" -o -name "*.bash" \) \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    -print0)

while IFS= read -r file; do
    first_line=$(head -n1 "$file" 2>/dev/null)
    if [[ "$first_line" =~ ^#!/.*bash ]] || [[ "$first_line" == "#!/bin/sh" ]] || [[ "$first_line" == "#!/usr/bin/sh" ]]; then
        BASH_FILES+=("$file")
    fi
done < <(find "$REPO_ROOT" -type f -executable \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    ! -name "*.sh" \
    ! -name "*.bash")

TOTAL=${#BASH_FILES[@]}

# Syntax check each file
for file in "${BASH_FILES[@]}"; do
    rel_path="${file#$REPO_ROOT/}"
    if err=$(bash -n "$file" 2>&1); then
        jq -nc --arg f "$rel_path" '{"file":$f,"type":"bash","status":"pass"}' >> "$TMP_RESULTS"
    else
        echo "✗ bash: $rel_path: $err"
        jq -nc --arg f "$rel_path" --arg e "$err" \
            '{"file":$f,"type":"bash","status":"fail","error":$e}' >> "$TMP_RESULTS"
        ((ERRORS++)) || true
    fi
done

# Shellcheck (optional, captures JSON if available)
SHELLCHECK_JSON="[]"
if command -v shellcheck &>/dev/null && [[ $TOTAL -gt 0 ]]; then
    sc_raw=$(shellcheck --format json "${BASH_FILES[@]}" 2>/dev/null) || true
    SHELLCHECK_JSON="${sc_raw:-[]}"
    sc_count=$(printf '%s' "$SHELLCHECK_JSON" | jq 'length')
    [[ "$sc_count" -gt 0 ]] && echo "⚠ shellcheck: $sc_count issues (see $JSON_OUT .shellcheck_diagnostics)"
fi

# Write JSON
STATUS="pass"
[[ $ERRORS -gt 0 ]] && STATUS="fail"

jq -s \
    --arg status "$STATUS" \
    --argjson sc "$SHELLCHECK_JSON" \
    '{
        "type": "bash",
        "status": $status,
        "summary": {
            "total": length,
            "passed": ([.[] | select(.status == "pass")] | length),
            "failed": ([.[] | select(.status == "fail")] | length)
        },
        "results": .,
        "failures": [.[] | select(.status == "fail")],
        "shellcheck_diagnostics": $sc
    }' "$TMP_RESULTS" > "$JSON_OUT"

# Terse summary
if [[ $ERRORS -eq 0 ]]; then
    echo "✓ bash: $TOTAL files OK"
    exit 0
else
    echo "✗ bash: $ERRORS/$TOTAL files failed → $JSON_OUT"
    exit 1
fi
