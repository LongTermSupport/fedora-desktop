#!/usr/bin/bash
# QA: verify ccy-ctrl-z-patch.js correctly patches @anthropic-ai/claude-code
#
# Tests that the ctrl+z/SIGSTOP patch successfully extends the Ink platform
# guard to also check CCY_DISABLE_SUSPEND, against the latest installed
# Claude Code package.
#
# Usage:
#   ./scripts/qa-ctrl-z-patch.bash           # test (auto-install on first run)
#   ./scripts/qa-ctrl-z-patch.bash --update  # pull latest Claude Code, then test
#
# Claude Code is cached in scripts/qa-ccy/node_modules/ (gitignored).
# On first run the package is installed automatically.
# Use --update to explicitly refresh to the latest published version.
#
# jq usage:
#   jq '.status'                 # "pass" or "fail"
#   jq '.claude_code_version'   # Claude Code version tested against
#   jq '.patch_result'          # "applied-known", "applied-dynamic", or "not-applied"
#   jq '.failures[]'            # failure details if any
#
# JSON: ${QA_JSON_OUT:-/tmp/qa-ctrl-z-patch-results.json}

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
QA_DIR="$SCRIPT_DIR/qa-ccy"
NODE_MODULES="$QA_DIR/node_modules"
CC_DIR="$NODE_MODULES/@anthropic-ai/claude-code"
PATCH_SCRIPT="$REPO_ROOT/files/var/local/claude-yolo/ccy-ctrl-z-patch.js"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-ctrl-z-patch-results.json}"
SUSPEND_GUARD='&&!process.env.CCY_DISABLE_SUSPEND'

UPDATE=0
[[ "${1:-}" == "--update" ]] && UPDATE=1

# Write failure JSON and exit 1
_fail() {
    local msg="$1"
    local details="${2:-}"
    jq -n --arg m "$msg" --arg d "$details" '{
        "type": "ccy-ctrl-z-patch",
        "status": "fail",
        "summary": {"total": 1, "passed": 0, "failed": 1},
        "results": [{"test": "ctrl-z-patch", "status": "fail", "error": $m, "details": $d}],
        "failures": [{"test": "ctrl-z-patch", "error": $m, "details": $d}]
    }' > "$JSON_OUT"
    echo "✗ ccy-ctrl-z-patch: $msg"
    [[ -n "$details" ]] && echo "  $details"
    exit 1
}

# Install / update Claude Code package
if [[ $UPDATE -eq 1 ]] || [[ ! -d "$NODE_MODULES" ]]; then
    action="Installing"
    [[ $UPDATE -eq 1 ]] && action="Updating"
    echo "  $action @anthropic-ai/claude-code (this may take a moment)..."
    npm install --prefix "$QA_DIR" > /tmp/qa-ccy-npm.log 2>&1 \
        || _fail "npm install failed" "See /tmp/qa-ccy-npm.log for details"
fi

# Sanity-check the package is present
if [[ ! -d "$CC_DIR" ]]; then
    _fail "claude-code package not found after install" "Try: $0 --update"
fi

# CCY-03: detect which artifact npm produced and exercise the matching patch
# path. Claude Code 2.1.x+ ships as a native SEA binary (bin/claude.exe); older
# releases ship cli.js. Hardcoding cli.js meant the QA either hard-failed on
# "cli.js not found" or validated an artifact production no longer uses.
if [[ -f "$CC_DIR/cli.js" ]]; then
    ARTIFACT="cli.js"
elif [[ -f "$CC_DIR/bin/claude.exe" ]]; then
    ARTIFACT="native"
else
    _fail "Neither cli.js nor bin/claude.exe present in installed package" \
        "Claude Code packaging changed again — update qa-ctrl-z-patch.bash and ccy-ctrl-z-patch.js"
fi

# Get installed version for reporting
CLAUDE_VERSION=$(node -e \
    "process.stdout.write(require('$CC_DIR/package.json').version)")

# Build an isolated package dir copy so we never mutate the real install. The
# patch script resolves both artifacts from CCY_PKG_DIR, and writes its soft-fail
# sentinel to CCY_PATCH_STATUS_PATH (CCY-07).
TMP_PKG=$(mktemp -d)
TMP_STATUS=$(mktemp)
trap 'rm -rf "$TMP_PKG" "$TMP_STATUS"' EXIT

PATCH_RESULT="not-applied"
STATUS="fail"
PATCH_RC=0

if [[ "$ARTIFACT" == "cli.js" ]]; then
    cp "$CC_DIR/cli.js" "$TMP_PKG/cli.js"
    PATCH_OUTPUT=$(CCY_PKG_DIR="$TMP_PKG" CCY_PATCH_STATUS_PATH="$TMP_STATUS" \
        node "$PATCH_SCRIPT" 2>&1) || PATCH_RC=$?
    # Success is verified by inspecting the file, not the exit code (soft-fail exits 0).
    if grep -qF "$SUSPEND_GUARD" "$TMP_PKG/cli.js" 2>/dev/null; then
        STATUS="pass"
        if echo "$PATCH_OUTPUT" | grep -q "known pattern"; then
            PATCH_RESULT="applied-known"
        else
            PATCH_RESULT="applied-dynamic"
        fi
    fi
else
    mkdir -p "$TMP_PKG/bin"
    cp "$CC_DIR/bin/claude.exe" "$TMP_PKG/bin/claude.exe"
    PATCH_OUTPUT=$(CCY_PKG_DIR="$TMP_PKG" CCY_PATCH_STATUS_PATH="$TMP_STATUS" \
        node "$PATCH_SCRIPT" 2>&1) || PATCH_RC=$?
    # Native binary is patched by a same-length byte flip, so we cannot grep for
    # the cli.js guard string. Success = the patch reported applying (or already
    # applied) AND no soft-fail sentinel was written.
    sentinel="$(cat "$TMP_STATUS" 2>/dev/null)" || sentinel=""
    if [[ "$sentinel" != "failed" ]] && \
       echo "$PATCH_OUTPUT" | grep -qE "applied to native binary|already applied"; then
        STATUS="pass"
        PATCH_RESULT="applied-native"
    fi
fi

if [[ "$PATCH_RC" -ne 0 ]]; then
    echo "  note: patch script exited $PATCH_RC; success is verified via inspection above" >&2
fi

# Terse output
if [[ "$STATUS" == "pass" ]]; then
    echo "✓ ccy-ctrl-z-patch: patch applied to Claude Code $CLAUDE_VERSION ($PATCH_RESULT)"
    echo "  $PATCH_OUTPUT"
else
    echo "✗ ccy-ctrl-z-patch: patch NOT applied to Claude Code $CLAUDE_VERSION"
    echo "  Patch output: $PATCH_OUTPUT"
    echo "  Action needed: update knownPatterns in ccy-ctrl-z-patch.js"
fi

# Write JSON result
jq -n \
    --arg status "$STATUS" \
    --arg version "$CLAUDE_VERSION" \
    --arg result "$PATCH_RESULT" \
    --arg output "$PATCH_OUTPUT" \
    '{
        "type": "ccy-ctrl-z-patch",
        "status": $status,
        "claude_code_version": $version,
        "patch_result": $result,
        "summary": {
            "total": 1,
            "passed": (if $status == "pass" then 1 else 0 end),
            "failed": (if $status == "fail" then 1 else 0 end)
        },
        "results": [{"test": "ctrl-z-patch", "status": $status, "patch_result": $result, "output": $output}],
        "failures": (if $status == "fail" then [{"test": "ctrl-z-patch", "error": ("patch not applied to Claude Code " + $version), "output": $output}] else [] end)
    }' > "$JSON_OUT"

[[ "$STATUS" == "pass" ]] && exit 0 || exit 1
