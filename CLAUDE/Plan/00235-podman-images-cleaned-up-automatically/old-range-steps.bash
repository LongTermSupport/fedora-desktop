#!/usr/bin/env bash
# Plan 00235 — old-range-steps.bash: the read-only legs of old-range.bash, each writing its
# output to a file in the run directory (a leg is a command, so its redirect lives here).
#
#   scan    BIN USER OUT                    sudo BIN old-range scan --user USER > OUT
#   triage  BIN SCAN PROPOSED REPORT        BIN old-range triage ... > REPORT
#   dry-run BIN SCAN DECISIONS PLAN REPORT  BIN old-range plan ... > PLAN, then the dry runs
#                                           of root's reown and the user's remove > REPORT
#
# Nothing here changes the store: reown and remove are run without --yes.
set -euo pipefail

step="${1:?usage: old-range-steps.bash scan|triage|dry-run ARGS}"
shift
case "${step}" in
    scan)
        # The scan file is the user's, in the run directory: only the scan runs as root.
        scanned="$(sudo "$1" old-range scan --user "$2")"
        printf '%s\n' "${scanned}" > "$3"
        ;;
    triage)
        "$1" old-range triage --scan "$2" --proposed "$3" > "$4"
        ;;
    dry-run)
        "$1" old-range plan --scan "$2" --decisions "$3" > "$4"
        {
            echo "# root: old-range reown (dry run)"
            sudo "$1" old-range reown --plan "$4"
            echo "# you: old-range remove (dry run)"
            "$1" old-range remove --plan "$4"
        } > "$5"
        ;;
    *)
        printf '[FATAL] unknown step %s\n' "${step}" >&2
        exit 64
        ;;
esac
