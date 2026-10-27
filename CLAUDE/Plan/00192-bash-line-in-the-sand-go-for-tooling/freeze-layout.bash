#!/usr/bin/env bash
# Plan 00192 — freeze-layout.bash: does a Go freeze tool's output keep the layout the bash tool
# printed, whatever containers the machine runs now?
#
# Usage: ./freeze-layout.bash <baseline> <output> [<allowed>...]
#   <baseline>  what the bash tool printed at the first deploy (before-<tool>-<label>.txt)
#   <output>    what the Go tool prints now
#   <allowed>   further outputs whose layout lines are also allowed: the tool's empty-machine
#               golden, so a section that has since emptied may print "(none)"
#
# The LAYOUT of an output is every line except container rows (the lines under a `  NAME`
# column header, up to the next blank or unindented line), with counts written as N. Every
# layout line of <output> must be a layout line of <baseline> or an <allowed> file: so rows
# may be added, removed or change state and counts may move, but a column header, a heading
# or a line the bash tool never printed fails. stdout: one line per finding (the payload).
#
# EXIT: 0 the layout holds; 1 it does not; 64 usage.
set -euo pipefail

if [[ "$#" -lt 2 ]]; then
    printf 'usage: freeze-layout.bash <baseline> <output> [<allowed>...]\n' >&2
    exit 64
fi
for file in "$@"; do
    if [[ ! -r "${file}" ]]; then
        printf '[FATAL] cannot read %s\n' "${file}" >&2
        exit 1
    fi
done

# layout <file> — the file's layout lines, counts as N.
layout() {
    awk '
        /^  NAME /           { print; inRows = 1; next }
        inRows && /^  [^ ]/  { next }
                             { inRows = 0 }
        {
            gsub(/\([0-9]+\)/, "(N)")
            gsub(/[0-9]+ container\(s\)/, "N container(s)")
            print
        }
    ' "$1"
}

output="$2"
known="$(for file in "$1" "${@:3}"; do layout "${file}"; done)"
findings=0
while IFS= read -r line; do
    if ! grep -qxF -- "${line}" <<<"${known}"; then
        printf 'a line the bash tool never printed: [%s]\n' "${line}"
        findings=$((findings + 1))
    fi
done < <(layout "${output}")

[[ "${findings}" -eq 0 ]]
