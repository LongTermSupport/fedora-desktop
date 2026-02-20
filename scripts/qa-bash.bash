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
TMP_SC=$(mktemp)
trap 'rm -f "$TMP_RESULTS" "$TMP_SC"' EXIT
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
    rel_path="${file#"$REPO_ROOT"/}"
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
# Use xargs -0 to batch files — avoids ARG_MAX when there are many files.
# Each xargs batch outputs a JSON array; jq 'add // []' flattens them all into one.
if command -v shellcheck &>/dev/null && [[ $TOTAL -gt 0 ]]; then
    printf '%s\0' "${BASH_FILES[@]}" \
        | xargs -0 shellcheck --format json 2>/dev/null \
        | jq -s 'add // []' > "$TMP_SC" || true
    sc_count=$(jq 'length' "$TMP_SC")
    [[ "$sc_count" -gt 0 ]] && echo "⚠ shellcheck: $sc_count issues (see $JSON_OUT .shellcheck_diagnostics)"
else
    printf '[]' > "$TMP_SC"
fi

# Write JSON
# Use --slurpfile for shellcheck data — avoids ARG_MAX when JSON is large (754+ issues).
STATUS="pass"
[[ $ERRORS -gt 0 ]] && STATUS="fail"

jq -s \
    --arg status "$STATUS" \
    --slurpfile sc "$TMP_SC" \
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
        "shellcheck_diagnostics": ($sc | first)
    }' "$TMP_RESULTS" > "$JSON_OUT"

# Terse summary
if [[ $ERRORS -eq 0 ]]; then
    echo "✓ bash: $TOTAL files OK"
    exit 0
else
    echo "✗ bash: $ERRORS/$TOTAL files failed → $JSON_OUT"
    exit 1
fi
