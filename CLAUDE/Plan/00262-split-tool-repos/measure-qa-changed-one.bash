#!/usr/bin/env bash
# measure-qa-changed-one.bash — one timed qa-changed run in one worktree, for
# measure-qa-changed.bash. Appends a table row to the report and keeps the run's output beside it.
# Exits with qa-changed's own status, so a failed run fails its leg: its time is no measurement.
#
# Usage: measure-qa-changed-one.bash <worktree> <label> <report> <suite-pass-dir>
#        measure-qa-changed-one.bash --edit <go-file>     append the one-package change
#        measure-qa-changed-one.bash --remove <dir>...    remove these worktrees if present
set -euo pipefail

if [[ "${1:-}" == --edit && $# -eq 2 ]]; then
    printf '// measure-qa-changed: a one-package change\n' >> "$2"
    exit 0
fi
if [[ "${1:-}" == --remove ]]; then
    shift
    for dir in "$@"; do
        if [[ -e "${dir}" ]]; then
            git -C "${dir%/*}" worktree remove --force "${dir}"
        fi
    done
    exit 0
fi
if [[ $# -ne 4 ]]; then
    printf 'usage: measure-qa-changed-one.bash <worktree> <label> <report> <suite-pass-dir>\n' >&2
    exit 2
fi
worktree="$1"
label="$2"
report="$3"
passes="$4"
log="$(dirname "${report}")/${label}.log"

start="$(date +%s.%N)"
if QA_SUITE_PASSES="${passes}" "${worktree}/scripts/qa-changed.bash" --base HEAD > "${log}" 2>&1; then
    status=0
else
    status=$?
fi
end="$(date +%s.%N)"

seconds="$(awk -v a="${start}" -v b="${end}" 'BEGIN { printf "%.1f", b - a }')"
skipped="$(grep -c 'skipped, unchanged since a recorded pass' "${log}" || [[ $? -eq 1 ]])"
printf '| %s | %s | %s | %s | %s |\n' "${label}" "${seconds}" "${status}" "${skipped:-0}" "$(basename "${log}")" >> "${report}"
printf '%s: %ss, exit %s, %s suite(s) skipped (log %s)\n' "${label}" "${seconds}" "${status}" "${skipped:-0}" "${log}"
exit "${status}"
