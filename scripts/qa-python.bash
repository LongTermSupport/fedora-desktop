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

# Assert the ruff VERSION, not just its presence (Plan 00071).
#
# This gate does not enumerate `select` — the enforced ruleset IS ruff's default
# set. That is deliberate (pinning a copy of the list would rot against upstream)
# but it makes the verdict a function of the ruff VERSION: 0.16.0's default
# enables UP, BLE, SIM, DTZ, B and more, far beyond the historical E4/E7/E9/F.
# So an unpinned ruff silently tightens the gate whenever upstream ships, and
# main goes red with no commit behind it — which is exactly what happened.
#
# ruff reaches this repo three ways, and only two are pinnable from here:
#   .claude/ccy/Dockerfile            pipx install ruff==$(cat .ruff-version)  [pinned]
#   .github/workflows/qa.yml          pip install ruff==$(cat .ruff-version)   [pinned]
#   playbooks/imports/play-python.yml dnf: ruff                                [Fedora's]
# The dnf one tracks whatever Fedora ships and CANNOT be pinned from here.
# Asserting the version is what makes that divergence LOUD instead of silent:
# a host whose ruff differs is told so, rather than quietly getting another answer.
RUFF_VERSION_FILE="$REPO_ROOT/.ruff-version"
if [[ ! -f "$RUFF_VERSION_FILE" ]]; then
    echo "✗ python: $RUFF_VERSION_FILE is missing — it is the single source of truth" >&2
    echo "  for the pinned ruff version, and every install site reads it." >&2
    exit 2
fi
RUFF_EXPECTED="$(tr -d '[:space:]' < "$RUFF_VERSION_FILE")"
RUFF_ACTUAL="$(ruff --version | awk '{print $2}')"
if [[ "$RUFF_ACTUAL" != "$RUFF_EXPECTED" ]]; then
    echo "✗ python: ruff version mismatch — this gate's verdict is version-dependent." >&2
    echo "    expected : $RUFF_EXPECTED  (.ruff-version)" >&2
    echo "    found    : $RUFF_ACTUAL  ($(command -v ruff))" >&2
    echo "" >&2
    echo "  The enforced ruleset is ruff's DEFAULT set, so a different version is a" >&2
    echo "  different gate. Either match the pin, or bump .ruff-version deliberately" >&2
    echo "  and triage what the new defaults add (then rebuild the ccy image)." >&2
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
#
# THE TWO PASSES MUST SHARE ONE EXCLUSION LIST (Plan 00071). They did not: the
# shebang pass was missing __pycache__, .venv and venv, so `.claude/untracked/venv/bin/pip`
# — a third-party console script with a `#!...python` line and no `.py` extension —
# was linted as if it were repo-owned code. The extension pass excluded it correctly,
# which is exactly why the gap was invisible: the same directory was half-excluded.
# Keep PY_EXCLUDES as the single source of truth for both.
#
# `.claude/ccy/*` and `.claude/skills/*` are excluded WHOLE, matching qa-bash.bash:48-50.
# This gate previously excluded only ccy/plugins and ccy/file-history, so it linted the
# vendored CCY supervisor that a daemon upgrade overwrites.
PY_EXCLUDES=(
    ! -path "*/.git/*"
    ! -path "$REPO_ROOT/.ansible/roles/*"
    ! -path "$REPO_ROOT/.claude/hooks-daemon/*"
    ! -path "$REPO_ROOT/.claude/ccy/*"
    ! -path "$REPO_ROOT/.claude/skills/*"
    ! -path "*/node_modules/*"
    ! -path "$REPO_ROOT/untracked/*"
    ! -path "*/__pycache__/*"
    ! -path "*/.venv/*"
    ! -path "*/venv/*"
)

PY_FILES=()
while IFS= read -r -d '' file; do
    PY_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f -name "*.py" "${PY_EXCLUDES[@]}" -print0)

while IFS= read -r file; do
    if head -n1 "$file" 2>/dev/null | grep -q "^#!/.*python"; then
        PY_FILES+=("$file")
    fi
done < <(find "$REPO_ROOT" -type f -executable "${PY_EXCLUDES[@]}" ! -name "*.py")

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
