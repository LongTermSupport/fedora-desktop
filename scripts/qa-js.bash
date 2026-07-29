#!/usr/bin/bash
# JavaScript QA validation - LLM-friendly
# 1. `node --check` over every repo-owned .js file (parse/syntax validation).
# 2. ESLint over the extensions/ project when its node_modules is present.
# stdout:  terse — errors + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-js-results.json}
#
# jq usage:
#   jq '.status'         # "pass" or "fail"
#   jq '.summary'        # {total, passed, failed}
#   jq '.failures[]'     # all files with syntax/lint errors
#   jq '.eslint'         # eslint sub-result ({ran, status, error})
#
# Exit codes:
#   0  pass
#   1  fail (node --check or eslint reported errors)
#   2  missing required tool (node absent, or extensions/node_modules absent
#      when an eslint run is required)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-js-results.json}"
TMP_RESULTS=$(mktemp)
TMP_ERR=$(mktemp)
TMP_ESLINT=$(mktemp)
trap 'rm -f "$TMP_RESULTS" "$TMP_ERR" "$TMP_ESLINT"' EXIT
ERRORS=0

# Fail fast: require node.
# `command -v` prints the resolved path to stdout (discarded) and nothing to
# stderr — no error output is suppressed by dropping stdout here.
if ! command -v node >/dev/null; then
    echo "✗ js: node not installed (install via play-nvm-install.yml)" >&2
    exit 2
fi

# Discover repo-owned .js files. Exclude vendored/upstream/runtime trees.
#
# ANCHORING (Plan 00067): the repo-root-relative exclusions are anchored to "$REPO_ROOT".
# `find -path` matches the WHOLE path, so an unanchored `*/untracked/*` excludes an entire
# checkout that merely LIVES under a directory called untracked — which made this gate scan 0
# files and still print "0 files OK". `.git` and `node_modules` stay unanchored on purpose:
# they legitimately occur at any depth.
JS_FILES=()
while IFS= read -r -d '' file; do
    JS_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f -name "*.js" \
    ! -path "*/.git/*" \
    ! -path "*/node_modules/*" \
    ! -path "$REPO_ROOT/.ansible/roles/*" \
    ! -path "$REPO_ROOT/roles/vendor/*" \
    ! -path "$REPO_ROOT/.claude/hooks-daemon/*" \
    ! -path "$REPO_ROOT/.claude/ccy/*" \
    ! -path "$REPO_ROOT/untracked/*" \
    -print0)

TOTAL=${#JS_FILES[@]}

# A gate that scanned NOTHING must not report a pass (Plan 00067, Decision 2). This repo always
# ships JavaScript (the GNOME Shell extensions), so zero means discovery is broken.
if [[ "$TOTAL" -eq 0 ]]; then
    echo "✗ js: found 0 files to check under $REPO_ROOT — refusing to report a pass."
    echo "  A zero-file scan means discovery is broken, not that the code is clean."
    echo "  Likely cause: an exclusion pattern matching the whole checkout (they are anchored"
    echo "  to \$REPO_ROOT precisely to prevent that), or REPO_ROOT resolving unexpectedly."
    exit 2
fi

# node --check each file. stderr is captured to a temp file so genuine parse
# errors are surfaced (never hidden) in stdout and the JSON output.
for file in "${JS_FILES[@]}"; do
    rel_path="${file#"$REPO_ROOT"/}"
    if node --check "$file" >/dev/null 2>"$TMP_ERR"; then
        jq -nc --arg f "$rel_path" '{"file":$f,"type":"js","status":"pass"}' >> "$TMP_RESULTS"
    else
        err=$(cat "$TMP_ERR")
        echo "✗ js: $rel_path"
        echo "$err"
        jq -nc --arg f "$rel_path" --arg e "$err" \
            '{"file":$f,"type":"js","status":"fail","error":$e}' >> "$TMP_RESULTS"
        ERRORS=$((ERRORS + 1))
    fi
done

# ESLint over the extensions/ project. An eslint run IS required whenever the
# extensions project carries lintable JS; refuse to skip silently if its
# node_modules (and the eslint binary) are absent — that is a missing dependency.
ESLINT_BIN="$REPO_ROOT/extensions/node_modules/.bin/eslint"
ESLINT_RAN=false
ESLINT_STATUS="skipped"
ESLINT_ERR=""

if [[ -d "$REPO_ROOT/extensions" ]]; then
    if [[ ! -x "$ESLINT_BIN" ]]; then
        # Dev-only tooling: the extensions/ eslint deps are NOT installed by any
        # playbook (a plain repo install / fresh CCY container does not carry
        # them, and most users never edit the GNOME-extension JS). Fail loudly
        # with clear setup guidance rather than silently skipping or
        # auto-installing — the latter would force dev tooling on every install.
        {
            echo "✗ js: eslint dev tooling for extensions/ is not set up"
            echo ""
            echo "  The GNOME-extension lint check needs node deps that no playbook"
            echo "  installs — they are dev-only and only required if you are editing"
            echo "  the extensions/ JavaScript."
            echo ""
            echo "  Set the tooling up once (reproducible from package-lock.json):"
            echo "      cd extensions && npm ci"
            echo ""
            echo "  Then re-run ./scripts/qa-all.bash."
        } >&2
        # Missing-dependency rule: do not skip. Surface as missing-tool exit 2.
        exit 2
    fi

    ESLINT_RAN=true
    eslint_rc=0
    ( cd "$REPO_ROOT/extensions" && "$ESLINT_BIN" . ) >"$TMP_ESLINT" 2>&1 || eslint_rc=$?
    if [[ $eslint_rc -eq 0 ]]; then
        ESLINT_STATUS="pass"
    else
        ESLINT_STATUS="fail"
        ESLINT_ERR=$(cat "$TMP_ESLINT")
        echo "✗ js: eslint reported errors"
        echo "$ESLINT_ERR"
        ERRORS=$((ERRORS + 1))
    fi
fi

# Write JSON
STATUS="pass"
[[ $ERRORS -gt 0 ]] && STATUS="fail"

jq -s \
    --arg status "$STATUS" \
    --argjson eslint_ran "$ESLINT_RAN" \
    --arg eslint_status "$ESLINT_STATUS" \
    --arg eslint_err "$ESLINT_ERR" \
    '{
        "type": "js",
        "status": $status,
        "summary": {
            "total": length,
            "passed": ([.[] | select(.status == "pass")] | length),
            "failed": ([.[] | select(.status == "fail")] | length)
        },
        "results": .,
        "failures": [.[] | select(.status == "fail")],
        "eslint": {
            "ran": $eslint_ran,
            "status": $eslint_status,
            "error": $eslint_err
        }
    }' "$TMP_RESULTS" > "$JSON_OUT"

# Terse summary
if [[ $ERRORS -eq 0 ]]; then
    echo "✓ js: $TOTAL files OK"
    exit 0
else
    echo "✗ js: $ERRORS error(s) in $TOTAL files → $JSON_OUT"
    exit 1
fi
