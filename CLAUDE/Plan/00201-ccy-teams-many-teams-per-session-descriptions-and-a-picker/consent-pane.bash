#!/usr/bin/env bash
# Plan 00201 — consent-pane.bash: what acceptance-consent.bash runs in its private tmux pane.
# Runs `<cc> --teams <list>` in the pane's directory (the scratch checkout), writes cc's exit
# status to <status-file> when it ends (renamed into place, so a reader never sees half of it),
# then keeps the pane open so its last screen can still be read. Not run by hand.
#
# Usage: consent-pane.bash <status-file> <cc> <seat@team,...>
set -euo pipefail

if [[ $# -ne 3 ]]; then
    printf 'usage: consent-pane.bash <status-file> <cc> <seat@team,...>\n' >&2
    exit 64
fi
status_file="$1"
status=0
"$2" --teams "$3" || status=$?
printf '%s\n' "${status}" >"${status_file}.tmp"
mv -f -- "${status_file}.tmp" "${status_file}"
exec sleep 3600
