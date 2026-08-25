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
# `ruff.toml` enumerates `select` explicitly (`E4`, `E7`, `E9`, `F`) precisely so
# the enforced ruleset does NOT drift with ruff's own default set — see that
# file's own rationale. But the version is still worth pinning: a ruff upgrade
# can change how the SAME selected rules behave (new checks inside E/F, fixed
# false negatives, parser changes), so an unpinned ruff can still silently
# change the verdict for the identical `select` list. Asserting the version
# converts that divergence from silent to loud.
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

# Discover files — via the SHARED discovery library, the same one the bash gates
# use, so all three agree on what a repo-owned source file is.
#
# The shebang branch used to require `-executable`. That is Plan 00076's bash
# defect, still live in the Python gate a fortnight later (Plan 00081 F3): six
# tracked Python programs totalling ~4,000 lines are mode 0644 with a
# `#!/usr/bin/env python3` shebang — their plays deploy them 0755 — so nothing
# here ever opened them, while this gate printed "✓ python: 35 files OK". The
# repo's own QA.md names `wsi-stream` as THE example of Python needing care; it
# was one of the six.
# shellcheck source-path=SCRIPTDIR
# shellcheck source=qa-discovery.bash
source "$REPO_ROOT/scripts/qa-discovery.bash"

qa_discover_python_files "$REPO_ROOT"
PY_FILES=("${QA_PYTHON_FILES[@]}")

TOTAL=${#PY_FILES[@]}

# A discovery that finds nothing is a BROKEN GATE, not a clean repo — the same
# guard the bash gates carry (Plan 00075).
if [[ $TOTAL -eq 0 ]]; then
    echo "✗ python: file discovery found 0 Python files under $REPO_ROOT" >&2
    echo "  This repo always has Python files, so the discovery is broken —" >&2
    echo "  reporting a pass here would vouch for code nothing had read." >&2
    exit 2
fi

# Zero discovery is guarded above. This guards PARTIAL discovery, which is the
# case that actually happened. A guard on emptiness is half a guard: the state
# this gate was in for months was "some files", and some files reads exactly
# like all of them.
qa_tracked_python_files "$REPO_ROOT"

declare -A DISCOVERED=()
for file in "${PY_FILES[@]}"; do
    DISCOVERED["${file#"$REPO_ROOT"/}"]=1
done

MISSED=()
for rel in "${QA_TRACKED_PYTHON_FILES[@]}"; do
    [[ -n "${DISCOVERED[$rel]:-}" ]] || MISSED+=("$rel")
done

if [[ ${#MISSED[@]} -gt 0 ]]; then
    echo "✗ python: discovery missed ${#MISSED[@]} tracked Python file(s):" >&2
    printf '    %s\n' "${MISSED[@]}" >&2
    echo "  These are Python files this gate would have reported a pass over" >&2
    echo "  without reading. Widen the discovery in scripts/qa-discovery.bash" >&2
    echo "  — do not exclude the files to make the message go away." >&2
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
