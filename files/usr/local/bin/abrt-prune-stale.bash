#!/usr/bin/env bash
# abrt-prune-stale.bash — remove ABRT problem records older than a retention window.
#
# ABRT keeps every crash record until it is reported or the spool hits
# MaxCrashReportsSize, and abrt-applet re-announces the unreported ones at every
# login. Records from unpackaged or third-party-repo executables can never be
# reported, so without an age bound they accumulate for ever. This is the age bound.
#
# Runs as root from abrt-prune-stale.timer (deployed by play-basic-configs.yml).
# Non-interactive: fails fast on a bad argument or a failed removal.
#
# stdout is the payload: one ABRT-PRUNE line the play's changed_when reads.
#
# Usage: abrt-prune-stale.bash <retention-days>
set -euo pipefail

if [ "$#" -ne 1 ]; then
    echo "usage: $(basename "$0") <retention-days>" >&2
    exit 2
fi
retention_days="$1"
if ! [[ "$retention_days" =~ ^[0-9]+$ ]]; then
    echo "error: retention days must be a non-negative integer, got '$retention_days'" >&2
    exit 2
fi

cutoff="$(date -d "${retention_days} days ago" +%s)"

# `-u` selects records OLDER than the timestamp; {short_id} is the id `remove` accepts.
mapfile -t stale_ids < <(abrt-cli list -u "$cutoff" --format '{short_id}')

removed=0
for id in "${stale_ids[@]}"; do
    [ -n "$id" ] || continue
    abrt-cli remove -f "$id" >&2
    removed=$((removed + 1))
done

if [ "$removed" -gt 0 ]; then
    echo "ABRT-PRUNE-CHANGED: removed ${removed} record(s) older than ${retention_days} day(s)"
else
    echo "ABRT-PRUNE-OK: no records older than ${retention_days} day(s)"
fi
