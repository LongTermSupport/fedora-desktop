#!/usr/bin/env bash
# qa-seat-text.bash — the gate seat-number-text (Plan 00265 R90): no doc, embedded help or
# launcher message tells a person to type or pick a seat number, which only the machine knows.
#
# The scope is what git would publish of docs/**/*.md (not the changelog), src-go/**/usage.txt
# and help.txt, and the message lines of files/var/local/claude-yolo/ and claude-code/. The
# shapes are src-go/qa/seattext's, shared with the Go rule project_seat_number_text; the
# agent-facing lines that keep a seat are src-go/qa/seattext/allowed.tsv, with their reasons.
#
# The tool is Go (src-go/qa/seattext, command src-go/qa/cmd/qa-seat-text); lib/go-tool.bash
# builds it into a private directory under the cache and runs it, so the exit code reaches the
# caller.
#
# Exit codes: 0 clean, 1 a finding or a stale allowlist row, 2 bad usage or the run could not
# judge (git failing, no file in scope, a missing or malformed allowlist, an unreadable file).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-seat-text --scripts "$SCRIPT_DIR" "$@"
