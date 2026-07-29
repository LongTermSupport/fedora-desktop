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
#   untracked             — the repo's own scratch tree
#
# ANCHORING (Plan 00067): the exclusions above are REPO-ROOT-RELATIVE and are anchored to
# "$REPO_ROOT" for that reason. `find -path` matches the WHOLE path it prints, so an
# unanchored `*/untracked/*` excludes an entire checkout that merely LIVES under a directory
# called untracked — e.g. lts-infra vendors this repo at untracked/repos/fedora-desktop. That
# made this gate scan 0 of 86 bash files and still print "0 files OK": a control that silently
# degraded to a no-op, which is exactly what CLAUDE.md's fail-fast rule exists to prevent.
# `.git` and `node_modules` stay UNANCHORED on purpose — they legitimately occur at any depth
# (submodules/worktrees, nested package trees).
BASH_FILES=()
while IFS= read -r -d '' file; do
    BASH_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f \( -name "*.sh" -o -name "*.bash" \) \
    ! -path "*/.git/*" \
    ! -path "$REPO_ROOT/.ansible/roles/*" \
    ! -path "$REPO_ROOT/.claude/hooks-daemon/*" \
    ! -path "$REPO_ROOT/.claude/ccy/*" \
    ! -path "$REPO_ROOT/.claude/skills/*" \
    ! -path "$REPO_ROOT/roles/vendor/*" \
    ! -path "*/node_modules/*" \
    ! -path "$REPO_ROOT/untracked/*" \
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
    ! -path "$REPO_ROOT/.ansible/roles/*" \
    ! -path "$REPO_ROOT/.claude/hooks-daemon/*" \
    ! -path "$REPO_ROOT/.claude/ccy/*" \
    ! -path "$REPO_ROOT/.claude/skills/*" \
    ! -path "$REPO_ROOT/roles/vendor/*" \
    ! -path "*/node_modules/*" \
    ! -path "$REPO_ROOT/untracked/*" \
    ! -name "*.sh" \
    ! -name "*.bash")

TOTAL=${#BASH_FILES[@]}

# A gate that scanned NOTHING must not report a pass (Plan 00067, Decision 2). This repo always
# contains bash, so zero is never a legitimate answer — it means discovery is broken (a
# mis-scoped exclusion, a wrong REPO_ROOT, a rename), and "✓ 0 files OK" would be a true
# statement about the check presented as a stronger statement about the code.
if [[ "$TOTAL" -eq 0 ]]; then
    echo "✗ bash: found 0 files to check under $REPO_ROOT — refusing to report a pass."
    echo "  A zero-file scan means discovery is broken, not that the code is clean."
    echo "  Likely causes: an exclusion pattern matching the whole checkout (the exclusions"
    echo "  above are anchored to \$REPO_ROOT precisely to prevent that), or REPO_ROOT"
    echo "  resolving somewhere unexpected."
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

# Shellcheck (optional, captures JSON if available)
#
# Crash handling (probe-then-fail): shellcheck rc 0 (clean) or 1 (findings) is
# normal data; rc>=2 is an invocation error. We invoke shellcheck directly on the
# (now scoped) file array and read its real exit code from PIPESTATUS so a crash
# is distinguishable from "has findings". A bare `xargs shellcheck` would collapse
# rc 1 and rc>=2 into the single xargs rc 123 and make that impossible; the
# exclusion list keeps the file count well within ARG_MAX, so xargs is unneeded.
# jq must also succeed. This guarantees the gate cannot silently green-light a
# commit when its own analyser failed.
if command -v shellcheck >/dev/null && [[ $TOTAL -gt 0 ]]; then
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
    sc_error_count=$(jq '[.[] | select(.level == "error")] | length' "$TMP_SC")
    if [[ "$sc_count" -gt 0 ]]; then
        echo "⚠ shellcheck: $sc_count issues (see $MERGED_JSON_OUT .checks.bash.shellcheck_diagnostics)"
    fi
    if [[ "$sc_error_count" -gt 0 ]]; then
        echo "✗ shellcheck: $sc_error_count error-level finding(s) — gating"
        ERRORS=$((ERRORS + 1))
    fi
else
    printf '[]' > "$TMP_SC"
fi

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
