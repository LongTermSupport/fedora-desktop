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
# Canonical merged results written by qa-all.bash (where diagnostics survive).
# QA_JSON_OUT, when set by qa-all.bash, points at a temp file the parent's
# trap deletes on EXIT — never point users at that. Point them at the merged file.
MERGED_JSON_OUT="${QA_JSON_OUT_MERGED:-/tmp/qa-results.json}"
TMP_RESULTS=$(mktemp)
TMP_SC=$(mktemp)
TMP_SC_ERR=$(mktemp)
trap 'rm -f "$TMP_RESULTS" "$TMP_SC" "$TMP_SC_ERR"' EXIT
ERRORS=0

# Discover files
# Exclusions cover upstream/vendor/runtime trees the project must not gate on:
#   .claude/hooks-daemon  — upstream dependency (also excluded in qa-python.bash)
#   .claude/ccy           — whole CCY runtime tree (snapshots, plugins, history)
#   .claude/skills        — installed skill payloads
#   roles/vendor          — vendored Ansible roles
#   .semgrep              — annotated rule FIXTURES. Deliberately full of the
#                           broken patterns the rules must catch, so linting it
#                           reports faults that are the point of the file. It is
#                           validated by `semgrep --test` in qa-patterns.bash,
#                           which is the check that actually belongs to it.
BASH_FILES=()
while IFS= read -r -d '' file; do
    BASH_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f \( -name "*.sh" -o -name "*.bash" \) \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/.claude/hooks-daemon/*" \
    ! -path "*/.claude/ccy/*" \
    ! -path "*/.claude/skills/*" \
    ! -path "*/roles/vendor/*" \
    ! -path "*/.semgrep/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    -print0)

while IFS= read -r file; do
    # Skip binary / non-text files before shebang sniffing — avoids
    # "command substitution: ignored null byte" noise from head on executables.
    grep -Iq . "$file" || continue
    first_line=$(head -n1 "$file")
    if [[ "$first_line" =~ ^#!/.*bash ]] || [[ "$first_line" == "#!/bin/sh" ]] || [[ "$first_line" == "#!/usr/bin/sh" ]]; then
        BASH_FILES+=("$file")
    fi
done < <(find "$REPO_ROOT" -type f -executable \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/.claude/hooks-daemon/*" \
    ! -path "*/.claude/ccy/*" \
    ! -path "*/.claude/skills/*" \
    ! -path "*/roles/vendor/*" \
    ! -path "*/.semgrep/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    ! -name "*.sh" \
    ! -name "*.bash")

TOTAL=${#BASH_FILES[@]}

# A discovery that finds nothing is a BROKEN GATE, not a clean repo. Without
# this, a bad exclusion or a wrong REPO_ROOT makes every loop below iterate zero
# times and the script prints "✓ bash: 0 files OK" — reporting PASS having
# checked nothing. That is the same defect class this gate exists to catch,
# applied to the gate itself (Plan 00075).
if [[ $TOTAL -eq 0 ]]; then
    echo "✗ bash: file discovery found 0 bash files under $REPO_ROOT" >&2
    echo "  This repo always has bash files, so the discovery is broken —" >&2
    echo "  reporting a pass here would vouch for code nothing had read." >&2
    exit 2
fi

# Syntax check each file
for file in "${BASH_FILES[@]}"; do
    rel_path="${file#"$REPO_ROOT"/}"
    if err=$(bash -n "$file" 2>&1); then
        jq -nc --arg f "$rel_path" '{"file":$f,"type":"bash","status":"pass"}' >> "$TMP_RESULTS"
    else
        echo "✗ bash: $rel_path: $err"
        jq -nc --arg f "$rel_path" --arg e "$err" \
            '{"file":$f,"type":"bash","status":"fail","error":$e}' >> "$TMP_RESULTS"
        ERRORS=$((ERRORS + 1))
    fi
done

# The shellcheck analyser is REQUIRED, not optional (Plan 00075).
# (This comment must not begin with the tool's name — a line starting
# "# shellcheck ..." is parsed as a directive and fails SC1072/SC1073.)
#
# It used to be skipped when absent, writing an empty findings array — so on any
# machine without shellcheck this stage reported a clean pass having run no
# static analysis at all. A gate that cannot fail is worse than no gate, because
# the green tick is read as evidence. This now matches how qa-patterns.bash
# treats semgrep and qa-python.bash treats ruff: a missing analyser is an IaC
# gap (fix the playbook), never a runtime condition to shrug at.
if ! command -v shellcheck >/dev/null; then
    echo "ERROR: shellcheck not found — this gate cannot report a pass without it." >&2
    echo "  It is installed by playbooks/imports/play-devtools.yml." >&2
    echo "  Deploy it: ansible-playbook playbooks/imports/play-devtools.yml" >&2
    echo "  Do NOT install it by hand." >&2
    exit 2
fi

# Shellcheck (captures JSON)
#
# Crash handling (probe-then-fail): shellcheck rc 0 (clean) or 1 (findings) is
# normal data; rc>=2 is an invocation error. We invoke shellcheck directly on the
# (now scoped) file array and read its real exit code from PIPESTATUS so a crash
# is distinguishable from "has findings". A bare `xargs shellcheck` would collapse
# rc 1 and rc>=2 into the single xargs rc 123 and make that impossible; the
# exclusion list keeps the file count well within ARG_MAX, so xargs is unneeded.
# jq must also succeed. This guarantees the gate cannot silently green-light a
# commit when its own analyser failed.
{
    # Presence and a non-zero file count are both guaranteed above — this block
    # is unconditional so there is no path on which the analyser is skipped.
    # An `if` head suppresses `set -e` for the pipeline (shellcheck exits 1 when it
    # has findings — expected, not an error) while still populating PIPESTATUS, so
    # we can inspect the real exit codes below instead of aborting prematurely.
    # An error-swallowing suffix is intentionally NOT used here: it would clobber
    # PIPESTATUS and hide a genuine analyser crash.
    sc_pipe_status=()
    if shellcheck --format json "${BASH_FILES[@]}" 2>"$TMP_SC_ERR" \
        | jq -s 'add // []' > "$TMP_SC"; then
        sc_pipe_status=("${PIPESTATUS[@]}")
    else
        # Capture PIPESTATUS before any other command resets it.
        sc_pipe_status=("${PIPESTATUS[@]}")
    fi
    sc_rc=${sc_pipe_status[0]}
    jq_rc=${sc_pipe_status[1]}
    if [[ $sc_rc -ge 2 ]]; then
        echo "✗ bash: shellcheck invocation failed (rc=$sc_rc):" >&2
        cat "$TMP_SC_ERR" >&2
        exit 2
    fi
    if [[ $jq_rc -ne 0 ]]; then
        echo "✗ bash: jq failed to parse shellcheck output (rc=$jq_rc)" >&2
        cat "$TMP_SC_ERR" >&2
        exit 2
    fi
    sc_count=$(jq 'length' "$TMP_SC")
    # Gate on error-level findings only; warning/info/style stay advisory.
    #
    # Plan 00075 measured what raising this bar would cost, so the choice is
    # informed rather than inherited: across 125 repo-owned bash files there are
    # 0 error, 26 warning, 79 info and 0 style findings. Sixteen of the 26
    # warnings are SC2155 ("Declare and assign separately to avoid masking return
    # values") — which IS the defect class Plan 00075 exists to stop, currently
    # reported as advisory noise. Raising to `warning` therefore costs ~26 fixes
    # and is tracked as a plan task; it is staged separately from the semgrep
    # rules so this commit does not mix a new gate with a bulk refactor.
    sc_error_count=$(jq '[.[] | select(.level == "error")] | length' "$TMP_SC")
    if [[ "$sc_count" -gt 0 ]]; then
        echo "⚠ shellcheck: $sc_count issues (see $MERGED_JSON_OUT .checks.bash.shellcheck_diagnostics)"
    fi
    if [[ "$sc_error_count" -gt 0 ]]; then
        echo "✗ shellcheck: $sc_error_count error-level finding(s) — gating"
        ERRORS=$((ERRORS + 1))
    fi
}

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
