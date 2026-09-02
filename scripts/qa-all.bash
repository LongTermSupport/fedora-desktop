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

# Deployed-drift gate (Plan 00072): a repo-owned user script that was changed
# but never deployed means the host is running different code from the one QA
# just passed. Like the no-kill gate above, deliberately NOT a jq-merged stage
# — it inspects the HOST, not the source tree, and self-skips where there is no
# host to inspect (CCY container, clean CI checkout).
drift_out=""
if ! drift_out="$(bash "$SCRIPT_DIR/qa-deployed-drift.bash" 2>&1)"; then
    echo "$drift_out" >&2
    echo "✗ QA FAILED: repo-owned scripts differ from their deployed copies" >&2
    exit 1
fi
# Print the pass line too. A gate whose only visible output is a failure is
# indistinguishable from a gate that is not running — and "a check that silently
# does nothing" is precisely the defect Plan 00072 exists to fix.
echo "$drift_out"

# Helper unit tests + extension GNOME-version compatibility (Plan 00081 F11).
#
# CLAUDE/QA.md says "ALWAYS and ONLY use ./scripts/qa-all.bash" and "NEVER use
# individual scripts directly" — and then documented these two as gates this
# script did not run. Following the stated rule, a helpers/ change got
# "✓ QA passed" with its 161-test suite never executed. The two ways to fix that
# were to run them or to stop claiming qa-all is sufficient; running them is the
# one that makes the instruction people actually follow the correct one.
#
# Hard, non-structural gates like the two above — deliberately NOT jq-merged
# stages, so they cannot disturb the positional .[0]..[5] merge below. Both are
# fast (the suite is ~0.06s; the compat check is static).
helper_out=""
if ! helper_out="$(bash "$SCRIPT_DIR/qa-helper-tests.bash" 2>&1)"; then
    echo "$helper_out" >&2
    echo "✗ QA FAILED: helper unit tests" >&2
    exit 1
fi
helper_summary=$(printf '%s' "$helper_out" | grep -oE 'Ran [0-9]+ tests?') || helper_summary="passed"
printf '✓ helper-tests: %s\n' "$helper_summary"

# The pre-commit secret scanner's own unit suite (scripts/test-secret-scan.bash).
#
# The scanner LIBRARY runs on every commit, but a false-NEGATIVE regression in it
# is silent by construction — a leak it stopped catching produces no signal at
# all. That is the whole reason the suite exists, so "it is exercised on every
# commit anyway" is not a reason to leave it unwired. Fifteen synthetic cases,
# sub-second. Same shape as the helper-tests gate above, and for the same reason.
scan_out=""
if ! scan_out="$(bash "$SCRIPT_DIR/test-secret-scan.bash" 2>&1)"; then
    echo "$scan_out" >&2
    echo "✗ QA FAILED: secret scanner unit tests" >&2
    exit 1
fi
scan_summary=$(printf '%s' "$scan_out" | grep -oE 'passed: [0-9]+') || scan_summary="passed"
printf '✓ secret-scan-tests: %s\n' "$scan_summary"

compat_out=""
if ! compat_out="$(cd "$SCRIPT_DIR/.." && python3 -m helpers.gnome.check_extension_compat 2>&1)"; then
    echo "$compat_out" >&2
    echo "✗ QA FAILED: an extension does not declare the GNOME Shell this Fedora ships" >&2
    exit 1
fi
# Print the pass line, for the same reason the drift gate does: a gate whose only
# visible output is a failure is indistinguishable from a gate that is not
# running — which is exactly how these two spent months documented but unrun.
compat_summary=$(printf '%s' "$compat_out" | grep -E '^All [0-9]+ extension') || compat_summary="OK"
printf '✓ extension-compat: %s\n' "$compat_summary"

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
