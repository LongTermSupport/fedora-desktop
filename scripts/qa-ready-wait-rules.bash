#!/usr/bin/env bash
# qa-ready-wait-rules.bash — the rule ready-wait-ignores-child-exit (Plan 00156).
#
# A loop that waits for a process this code started, and never asks whether that process
# has exited, reports a crash as a timeout and hides the process's own error. The
# reasoning, and how to fix a finding, is in CLAUDE/QA.md under the identifier.
#
#   Python  .semgrep/ready-wait.yml, proven against .semgrep/ready-wait.py
#   Bash    helpers/ready_wait/bash_ready_waits.py, proven against .semgrep/ready-wait.bash
#           (semgrep's bash parser rejects about a quarter of this repo's scripts)
#
# Scope: every tracked Python and shell file as scripts/qa-discovery.bash defines them
# (extensionless scripts by shebang), minus the fixtures under .semgrep/. Python files are
# handed to semgrep by name, and every file handed in must come back scanned.
#
# Usage: scripts/qa-ready-wait-rules.bash [PATH...]   (paths: run on those files only)
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 a tool missing or broken, or a
# handed file that was not scanned.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RULES="$REPO_ROOT/.semgrep/ready-wait.yml"
PY_FIXTURE="$REPO_ROOT/.semgrep/ready-wait.py"
BASH_FIXTURE="$REPO_ROOT/.semgrep/ready-wait.bash"
BASH_RULE=(python3 -m helpers.ready_wait.bash_ready_waits)

if ! command -v semgrep >/dev/null; then
    echo "ERROR: semgrep not found. Install with: pipx install semgrep" >&2
    exit 2
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$REPO_ROOT"

# Both halves are proven against their fixtures on every run, so a rule edited into
# matching nothing fails here rather than reporting a clean repo.
if ! semgrep --test --metrics=off --disable-version-check --config "$RULES" "$PY_FIXTURE" \
    >"$work/selftest.txt" 2>&1; then
    echo "ERROR: .semgrep/ready-wait.yml failed its fixture .semgrep/ready-wait.py:" >&2
    cat "$work/selftest.txt" >&2
    exit 1
fi
if ! "${BASH_RULE[@]}" --fixture "$BASH_FIXTURE" >"$work/selftest.txt" 2>&1; then
    echo "ERROR: helpers/ready_wait/bash_ready_waits.py failed its fixture .semgrep/ready-wait.bash:" >&2
    cat "$work/selftest.txt" >&2
    exit 1
fi

py=() sh=()
if [[ "$#" -gt 0 ]]; then
    for f in "$@"; do
        case "$f" in
            *.py) py+=("$f") ;;
            *.bash | *.sh) sh+=("$f") ;;
            *) if awk 'NR == 1 { found = /^#!.*python/; exit } END { exit !found }' "$f"; then
                py+=("$f")
            else
                sh+=("$f")
            fi ;;
        esac
    done
else
    # shellcheck source-path=SCRIPTDIR
    # shellcheck source=qa-discovery.bash
    source "$REPO_ROOT/scripts/qa-discovery.bash"
    qa_tracked_python_files "$REPO_ROOT"
    qa_tracked_shell_scripts "$REPO_ROOT"
    for f in "${QA_TRACKED_PYTHON_FILES[@]}"; do
        [[ "$f" == .semgrep/* ]] || py+=("$f")
    done
    sh=("${QA_TRACKED_SHELL_FILES[@]}")
fi
if [[ "$#" -eq 0 && ( "${#py[@]}" -eq 0 || "${#sh[@]}" -eq 0 ) ]]; then
    echo "ERROR: discovery found ${#py[@]} Python and ${#sh[@]} shell files; nothing would be checked" >&2
    exit 2
fi

: >"$work/findings.txt"
scanned=0
if [[ "${#py[@]}" -gt 0 ]]; then
    rc=0
    semgrep --metrics=off --disable-version-check --config "$RULES" --no-rewrite-rule-ids \
        --scan-unknown-extensions --json --quiet "${py[@]}" \
        >"$work/scan.json" 2>"$work/scan.err" || rc=$?
    if [[ "$rc" -ge 2 ]] || [[ ! -s "$work/scan.json" ]]; then
        echo "ERROR: semgrep failed on the Python files (exit $rc):" >&2
        cat "$work/scan.err" >&2
        exit 2
    fi
    if jq -e '.errors | length > 0' "$work/scan.json" >/dev/null; then
        echo "ERROR: semgrep could not parse some Python files, so the rule did not run on them:" >&2
        jq -r '.errors[] | "    \(.path // "?"): \(.message)"' "$work/scan.json" >&2
        exit 2
    fi
    printf '%s\n' "${py[@]}" | sort -u >"$work/handed.txt"
    jq -r '.paths.scanned[] | ltrimstr("./")' "$work/scan.json" | sort -u >"$work/scanned.txt"
    comm -23 "$work/handed.txt" "$work/scanned.txt" >"$work/lost.txt"
    if [[ -s "$work/lost.txt" ]]; then
        echo "ERROR: semgrep did not scan Python files it was handed:" >&2
        awk '{print "    " $0}' "$work/lost.txt" >&2
        exit 2
    fi
    jq -r '.results[] | "\(.path | ltrimstr("./")):\(.start.line): \(.check_id)"' \
        "$work/scan.json" >>"$work/findings.txt"
    scanned=$((scanned + ${#py[@]}))
fi
if [[ "${#sh[@]}" -gt 0 ]]; then
    rc=0
    "${BASH_RULE[@]}" "${sh[@]}" >>"$work/findings.txt" 2>"$work/bash.err" || rc=$?
    if [[ "$rc" -ge 2 ]] || [[ -s "$work/bash.err" ]]; then
        echo "ERROR: the bash rule failed (exit $rc):" >&2
        cat "$work/bash.err" >&2
        exit 2
    fi
    scanned=$((scanned + ${#sh[@]}))
fi

sort -u -o "$work/findings.txt" "$work/findings.txt"
echo "scanned: ${#py[@]} Python file(s) with semgrep, ${#sh[@]} shell file(s) with the bash rule"
if [[ -s "$work/findings.txt" ]]; then
    cat "$work/findings.txt"
    files="$(awk -F: '{print $1}' "$work/findings.txt" | sort -u | wc -l)"
    echo "ready-wait rule: $(wc -l <"$work/findings.txt") finding(s) in ${files} file(s). See CLAUDE/QA.md \"ready-wait-ignores-child-exit\"."
    echo "passed: $((scanned - files)) failed: ${files}"
    exit 1
fi
echo "passed: ${scanned} failed: 0"
