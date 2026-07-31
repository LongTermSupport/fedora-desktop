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
TMP_PATTERNS=$(mktemp)
TMP_ANSIBLE=$(mktemp)
TMP_ANSIBLE_SYNTAX=$(mktemp)
TMP_JS=$(mktemp)
TMP_DOCS=$(mktemp)
trap 'rm -f "$TMP_BASH" "$TMP_PYTHON" "$TMP_PATTERNS" "$TMP_ANSIBLE" "$TMP_ANSIBLE_SYNTAX" "$TMP_JS" "$TMP_DOCS"' EXIT
FAILED=0

# Run sub-checks (each writes JSON to temp file, outputs terse to stdout)
# Exit code 2 = missing required tool — refuse to run entirely
rc=0
QA_JSON_OUT="$TMP_BASH" "$SCRIPT_DIR/qa-bash.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools. Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

rc=0
QA_JSON_OUT="$TMP_PYTHON" "$SCRIPT_DIR/qa-python.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools. Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

rc=0
QA_JSON_OUT="$TMP_PATTERNS" "$SCRIPT_DIR/qa-patterns.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools (semgrep). Install with: pipx install semgrep" >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

# Ansible checks (fail-fast patterns + playbook hygiene)
rc=0
QA_JSON_OUT="$TMP_ANSIBLE" "$SCRIPT_DIR/qa-ansible.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools for ansible check. Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

rc=0
QA_JSON_OUT="$TMP_ANSIBLE_SYNTAX" "$SCRIPT_DIR/qa-ansible-syntax.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools (ansible-playbook). Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

rc=0
QA_JSON_OUT="$TMP_JS" "$SCRIPT_DIR/qa-js.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools (node / extensions node_modules). Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

# Documentation integrity (Plan 00070): link/anchor resolution, playbook
# catalogue completeness, topic-file index. Scoped to CORE docs — the plan tree
# is excluded so archiving a plan can never change this gate's verdict.
rc=0
QA_JSON_OUT="$TMP_DOCS" "$SCRIPT_DIR/qa-docs.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: docs gate could not produce a result (zero-file scan or checker crash)." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

# L0 no-kill safety gate (Plan 00055): the container-watch watchdog is
# reporting-only and must never gain a process-termination call site. This is a
# minimal, non-structural HARD gate — deliberately NOT a 7th jq-merged stage, so
# it cannot corrupt the positional .[0]..[5] JSON merge below. It fails the whole
# run immediately if a forbidden kill call site is introduced.
nokill_out=""
if ! nokill_out="$(bash "$SCRIPT_DIR/qa-nokill-containerwatch.bash" 2>&1)"; then
    echo "$nokill_out" >&2
    echo "✗ QA FAILED: no-kill safety gate (container-watch) rejected a process-termination call site" >&2
    exit 1
fi

# Merge JSON from all checks
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
            "bash":            .[0],
            "python":          .[1],
            "patterns":        .[2],
            "ansible":         .[3],
            "ansible_syntax":  .[4],
            "js":              .[5],
            "docs":            .[6]
        }
    }' "$TMP_BASH" "$TMP_PYTHON" "$TMP_PATTERNS" "$TMP_ANSIBLE" "$TMP_ANSIBLE_SYNTAX" "$TMP_JS" "$TMP_DOCS" > "$JSON_OUT"

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
