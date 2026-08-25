#!/usr/bin/bash
# Documentation integrity QA - LLM-friendly
#
# Three checks, each of which would have caught a real defect in the Plan 00070
# documentation audit — that is the bar for being here, not "seems useful":
#   1. every relative link target exists, and every #anchor matches a real
#      heading          -> findings 13, 17, 19, 21, 22
#   2. every playbook imported by playbook-main.yml is named in BOTH
#      docs/playbooks.md and docs/architecture.md   -> finding 2
#   3. every CLAUDE/*.md topic file has a row in CLAUDE.md   -> finding 3
#
# The audit ran five read-only passes by hand. This gate found four defects
# those passes missed, and showed that a hand-written finding ("two broken
# links") had undercounted. Hand-auditing prose does not scale; this does.
#
# stdout:  terse — findings + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-docs-results.json}
#
# jq usage:
#   jq '.status'        # "pass" or "fail"
#   jq '.failures[]'    # every finding, with file/line/target/problem
#
# Exit codes:
#   0  pass
#   1  fail (at least one broken link, anchor, or catalogue gap)
#   2  discovery found zero in-scope files, or the checker crashed — either way
#      NOT a clean result
#
# SCOPE: core docs only — docs/, CLAUDE/*.md (top level), README.md, CLAUDE.md,
# any directory's own CLAUDE.md, and .claude/rules/. `CLAUDE/Plan/**` is
# EXCLUDED on principle, not for convenience: a core gate that sweeps plan
# content is a core->plan dependency, so archiving a plan could flip core CI's
# verdict without a core file changing. Plan markdown has its own linting.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-docs-results.json}"
TMP_RAW=$(mktemp)
trap 'rm -f "$TMP_RAW"' EXIT

# Fail fast: the checker is a repo helper, so its absence is a broken checkout.
if [[ ! -f "$REPO_ROOT/helpers/docs/link_check.py" ]]; then
    echo "✗ docs: helpers/docs/link_check.py is missing — broken checkout" >&2
    exit 2
fi

rc=0
( cd "$REPO_ROOT" && python3 -m helpers.docs.link_check "$REPO_ROOT" ) \
    > "$TMP_RAW" 2>&1 || rc=$?

# rc 2 = zero-file discovery. A gate that scanned NOTHING must never report a
# pass (CLAUDE/QA.md — "A gate reporting 0 files is a FAILURE, not a pass").
if [[ $rc -eq 2 ]]; then
    echo "✗ docs: found 0 in-scope markdown files under $REPO_ROOT — refusing to report a pass."
    echo "  A zero-file scan means discovery is broken, not that the docs are clean."
    echo "  Likely cause: REPO_ROOT resolving unexpectedly, or in_scope() over-excluding."
    cat "$TMP_RAW"
    exit 2
fi

# Any other non-zero that is not the checker's own "findings" exit is a crash,
# and a crash must never be reported as "no findings" (the ruff lesson).
if [[ $rc -ne 0 && $rc -ne 1 ]]; then
    echo "✗ docs: link_check crashed (exit $rc) — this is NOT a clean result" >&2
    cat "$TMP_RAW" >&2
    exit 2
fi

# Validate the payload before trusting it. jq's own complaint is captured and
# printed, never discarded.
jq_check=""
if ! jq_check="$(jq -e 'has("findings") and has("scanned")' "$TMP_RAW" 2>&1)"; then
    echo "✗ docs: link_check did not emit the expected JSON — hard failure" >&2
    echo "  jq said: $jq_check" >&2
    cat "$TMP_RAW" >&2
    exit 2
fi

SCANNED=$(jq -r '.scanned' "$TMP_RAW")
NFINDINGS=$(jq -r '.findings | length' "$TMP_RAW")

# Reshape into the shape qa-all.bash's jq merge expects.
jq '{
        "type": "docs",
        "status": .status,
        "summary": {
            "total":  .scanned,
            "passed": (.scanned - (.findings | map(.file) | unique | length)),
            "failed": (.findings | map(.file) | unique | length)
        },
        "results": .findings,
        "failures": [.findings[] | {
            "file":  .file,
            "type":  "docs",
            "status":"fail",
            "error": ("\(.target) — \(.problem)")
        }]
    }' "$TMP_RAW" > "$JSON_OUT"

if [[ "$NFINDINGS" -eq 0 ]]; then
    echo "✓ docs: $SCANNED files OK (links, anchors, playbook catalogue, topic index)"
    exit 0
fi

echo "✗ docs: $NFINDINGS finding(s) across $SCANNED files"
jq -r '.findings[] | "  \(.file):\(.line)  \(.target)  — \(.problem)"' "$TMP_RAW"
echo "  Details: jq '.failures[]' $JSON_OUT"
exit 1
