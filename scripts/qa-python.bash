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
TMP_RUFF_ERR=$(mktemp)
trap 'rm -f "$TMP_RESULTS" "$TMP_RUFF_ERR"' EXIT
ERRORS=0

# Fail fast: check required dependencies
if ! command -v ruff &>/dev/null; then
    echo "✗ python: ruff not installed (sudo dnf install ruff)" >&2
    exit 2
fi

# Discover files
#
# ANCHORING (Plan 00067): the repo-root-relative exclusions are anchored to "$REPO_ROOT".
# `find -path` matches the WHOLE path it prints, so an unanchored `*/untracked/*` excludes an
# entire checkout that merely LIVES under a directory called untracked — e.g. lts-infra vendors
# this repo at untracked/repos/fedora-desktop. Here that defect was MASKED by the `ruff not
# installed` exit 2 rather than avoided; qa-bash.bash had the same bug and reported a clean pass
# over 0 of 86 files. `.git`, `node_modules`, `__pycache__`, `.venv` and `venv` stay UNANCHORED
# on purpose — all of them legitimately occur at any depth.
PY_FILES=()
while IFS= read -r -d '' file; do
    PY_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f -name "*.py" \
    ! -path "*/.git/*" \
    ! -path "$REPO_ROOT/.ansible/roles/*" \
    ! -path "$REPO_ROOT/.claude/hooks-daemon/*" \
    ! -path "$REPO_ROOT/.claude/ccy/plugins/*" \
    ! -path "$REPO_ROOT/.claude/ccy/file-history/*" \
    ! -path "*/node_modules/*" \
    ! -path "$REPO_ROOT/untracked/*" \
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
    ! -path "$REPO_ROOT/.ansible/roles/*" \
    ! -path "$REPO_ROOT/.claude/hooks-daemon/*" \
    ! -path "$REPO_ROOT/.claude/ccy/plugins/*" \
    ! -path "$REPO_ROOT/.claude/ccy/file-history/*" \
    ! -path "*/node_modules/*" \
    ! -path "$REPO_ROOT/untracked/*" \
    ! -name "*.py")

TOTAL=${#PY_FILES[@]}

# A gate that scanned NOTHING must not report a pass (Plan 00067, Decision 2). This repo always
# contains Python (helpers/, scripts/), so zero means discovery is broken.
if [[ "$TOTAL" -eq 0 ]]; then
    echo "✗ python: found 0 files to check under $REPO_ROOT — refusing to report a pass."
    echo "  A zero-file scan means discovery is broken, not that the code is clean."
    echo "  Likely cause: an exclusion pattern matching the whole checkout (they are anchored"
    echo "  to \$REPO_ROOT precisely to prevent that), or REPO_ROOT resolving unexpectedly."
    exit 2
fi

# Syntax check each file
for file in "${PY_FILES[@]}"; do
    rel_path="${file#"$REPO_ROOT"/}"
    if err=$(python3 -m py_compile "$file" 2>&1); then
        jq -nc --arg f "$rel_path" '{"file":$f,"type":"python","status":"pass"}' >> "$TMP_RESULTS"
    else
        echo "✗ python: $rel_path: $err"
        jq -nc --arg f "$rel_path" --arg e "$err" \
            '{"file":$f,"type":"python","status":"fail","error":$e}' >> "$TMP_RESULTS"
        ERRORS=$((ERRORS + 1))
    fi
done

# Ruff: capture diagnostics as JSON (read-only check — never mutate the tree).
#
# Crash handling (probe-then-fail): ruff rc 0 (clean) or 1 (lint findings) is
# normal data; rc>=2 is an invocation error (bad config, internal failure).
# A crash must surface as exit 2, not degrade to an empty "[]" pass.
# stderr is kept in a temp file so the error message is preserved, not hidden.
RUFF_JSON="[]"
if [[ $TOTAL -gt 0 ]]; then
    ruff_rc=0
    ruff_raw=$(ruff check --output-format json "${PY_FILES[@]}" 2>"$TMP_RUFF_ERR") || ruff_rc=$?
    if [[ $ruff_rc -ge 2 ]]; then
        echo "✗ python: ruff invocation failed (rc=$ruff_rc):" >&2
        cat "$TMP_RUFF_ERR" >&2
        exit 2
    fi
    RUFF_JSON="${ruff_raw:-[]}"
    ruff_count=$(printf '%s' "$RUFF_JSON" | jq 'length')
    if [[ "$ruff_count" -gt 0 ]]; then
        echo "✗ python: ruff: $ruff_count issues (see $JSON_OUT .ruff_diagnostics)"
        ERRORS=$((ERRORS + 1))
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
