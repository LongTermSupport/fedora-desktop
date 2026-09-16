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

# The root defaults to this script's own repository and is overridable by argument, so a test
# can drive the REAL script against a fixture tree. Without it the exit codes documented above
# were reachable only by reasoning: every one of them needs a tree this repository is not, and
# nothing could produce one. `scripts/test-qa-docs-exit-codes.bash` is the caller, and the
# zero-file guard below is what stops a mistyped root from ever reporting a pass.
REPO_ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
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
# The scan population is filesystem-derived — `collect_scope` walks the tree — so an in-scope
# document nobody committed is scanned here and simply absent in CI. The denominator says so
# out loud, the way `qa-helper-tests.bash` prints `65 modules (65 tracked)`. Equal numbers
# mean the two machines are reading the same set.
TRACKED=$(jq -r '.tracked' "$TMP_RAW")
NFINDINGS=$(jq -r '.findings | length' "$TMP_RAW")
# Links into a vendored repository, by outcome. Reported rather than silently dropped: an
# exemption nobody counts reads exactly like a check that ran and found nothing, which is the
# defect class this repo keeps finding.
#
# TYPES, NOT JUST PRESENCE, and the difference is the whole guard. `has("broken")` answers
# yes for `{"broken": null}`, and `null | length` is 0 in jq — so a checker that emitted the
# key with nothing in it would read as a clean zero, the ⚠ block would be empty, and the ✓
# line would assert `0 broken`. `{"broken": {}}` is worse still: silent rather than visibly
# null. That is a blind read byte-identical to a clean one, inside the guard written to
# prevent exactly that. So the two counters must be numbers and the two lists must be
# arrays, or this gate refuses to print a stage line at all.
#
# jq's own complaint is captured and shown, the way the payload validation above does it.
# Two adjacent guards with two conventions is how one of them ends up the weaker one.
vendored_check=""
if ! vendored_check="$(jq -e '
        (.vendored.ok | type == "number")
        and (.vendored.unverifiable | type == "number")
        and (.vendored.broken | type == "array")
        and (.vendored_warning | type == "array")' "$TMP_RAW" 2>&1)"; then
    echo "✗ docs: link_check did not emit usable vendored counts — the boundary check did" >&2
    echo "  not run, or stopped reporting. Refusing to print a stage line that would read" >&2
    echo "  as clean. jq said: $vendored_check" >&2
    exit 2
fi
V_OK=$(jq -r '.vendored.ok' "$TMP_RAW")
V_UNVERIFIABLE=$(jq -r '.vendored.unverifiable' "$TMP_RAW")
V_BROKEN=$(jq -r '.vendored.broken | length' "$TMP_RAW")

# Reshape into the shape qa-all.bash's jq merge expects.
jq '{
        "type": "docs",
        "status": .status,
        "summary": {
            "total":  .scanned,
            "passed": (.scanned - (.findings | map(.file) | unique | length)),
            "failed": (.findings | map(.file) | unique | length)
        },
        "vendored": .vendored,
        "results": .findings,
        "failures": [.findings[] | {
            "file":  .file,
            "type":  "docs",
            "status":"fail",
            "error": ("\(.target) — \(.problem)")
        }]
    }' "$TMP_RAW" > "$JSON_OUT"

# A broken link into a vendored repo that IS PRESENT is a serious warning, not a finding:
# demonstrably wrong — usually that repo moved the file and our generated pointers are stale
# — but not ours to fix, so it must not fail this repo's CI. It gets its own `⚠` stage line
# because the count alone would leave nobody able to act on it, and `qa-patterns.bash`
# already establishes a ⚠-then-✓ pair as a shape `verdicts.py` parses.
#
# NO CONDITION HERE, deliberately. The state that produces a warning needs a vendored repo
# present AND a stale pointer into it, so an `if [[ "$V_BROKEN" -gt 0 ]]` guarding the
# formatting would be a branch whose first execution is the one nobody is watching. The list
# is composed and tested in `vendored_warning_lines()` and is empty on a clean run, so this
# jq runs every time and prints nothing — the same defect class this gate exists to remove.
#
# NOT the same thing as the multi-line stage line removed from `vmtest-manifest`, and the
# difference is worth stating because they look alike. There, ONE printf interpolated a
# capture containing newlines, so `verdicts.py` kept the first line and silently dropped the
# rest. Here each line is emitted on its own, only the first carries a stage symbol, and the
# indented detail lines that follow are correctly not read as stages — measured, not assumed.
jq -r '.vendored_warning[]' "$TMP_RAW"

VENDORED_SUMMARY="VENDORED: $V_OK verified, $V_UNVERIFIABLE unverifiable (repo absent), $V_BROKEN broken"

if [[ "$NFINDINGS" -eq 0 ]]; then
    echo "✓ docs: $SCANNED files ($TRACKED tracked) OK (links, anchors, playbook catalogue," \
        "topic index) — $VENDORED_SUMMARY"
    exit 0
fi

echo "✗ docs: $NFINDINGS finding(s) across $SCANNED files ($TRACKED tracked) — $VENDORED_SUMMARY"
jq -r '.findings[] | "  \(.file):\(.line)  \(.target)  — \(.problem)"' "$TMP_RAW"
echo "  Details: jq '.failures[]' $JSON_OUT"
exit 1
