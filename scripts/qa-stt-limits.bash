#!/usr/bin/env bash
# Speech-to-text recording limits: each one has ONE home (Plan 00148 Task 4.5).
#
# WHY THIS GATE EXISTS. The 120 s streaming limit was once held in six places (the panel's
# 117 and 120, wsi-stream's --timeout and its fallbacks, the server's 125 s watchdog) with
# nothing keeping them in step; raising one and not another lost whole transcripts. Now:
#   - batch:      wsi's MAX_RECORDING_SECONDS
#   - streaming:  wsi-stream's STREAMING_MAX_SECONDS (also server mode without continuous
#                 dictation: the client sends it to the server)
#   - continuous: the GSettings keys max-recording-minutes and silence-autostop-seconds,
#                 read by wsi-stream and the panel; the server only applies what START says
# This gate fails if a literal copy of a limit reappears anywhere else in those files.
# wsi-article's CHUNK_DURATION (120) is a flush interval, not a limit, and is not checked.
#
# Usage: scripts/qa-stt-limits.bash [REPO_ROOT]   (a root other than this repo is for the
# gate's own test, which runs it against a copy with a limit planted back in)
# Prints one PASS/FAIL line per rule and "passed: N  failed: M"; exits 1 on any failure,
# 2 if a file it checks is missing.
set -euo pipefail

root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
bin="$root/files/home/.local/bin"
ext="$root/extensions/speech-to-text@fedora-desktop"

passed=0
failed=0

# The ERE for NUMBER as a whole token (awk has no \b)
num() {
    printf '(^|[^0-9A-Za-z_.])%s([^0-9A-Za-z_]|$)' "$1"
}

# Lines of FILE matching ERE, comment-only lines (# or //) excluded
count() {
    local file="$1" re="$2"
    if [[ ! -f "$file" ]]; then
        printf 'qa-stt-limits: %s does not exist\n' "$file" >&2
        exit 2
    fi
    awk -v re="$re" '!/^[[:space:]]*(#|\/\/)/ && $0 ~ re {n++} END {print n+0}' "$file"
}

expect() {
    local label="$1" file="$2" re="$3" want="$4" got
    got=$(count "$file" "$re")
    if [[ "$got" == "$want" ]]; then
        passed=$((passed + 1))
        printf '  PASS: %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL: %s (%s matching line(s) in %s, wanted %s)\n' "$label" "$got" "$file" "$want"
    fi
}

echo "=== speech-to-text recording limits: one home each ==="
expect "batch: wsi holds 30 once" "$bin/wsi" "$(num 30)" 1
expect "…as MAX_RECORDING_SECONDS" "$bin/wsi" '^readonly MAX_RECORDING_SECONDS=30$' 1
expect "streaming: wsi-stream holds 120 once" "$bin/wsi-stream" "$(num 120)" 1
expect "…as STREAMING_MAX_SECONDS" "$bin/wsi-stream" '^STREAMING_MAX_SECONDS = 120$' 1
expect "server: no watchdog" "$bin/wsi-stream-server" 'WATCHDOG' 0
expect "server: no 125" "$bin/wsi-stream-server" "$(num 125)" 0
expect "server: no default recording limit" "$bin/wsi-stream-server" \
    'max_seconds[[:space:]]*=[[:space:]]*[0-9]|"max_seconds",' 0
expect "panel: no 117" "$ext/extension.js" "$(num 117)" 0
expect "panel: no 120" "$ext/extension.js" "$(num 120)" 0
expect "panel: no 27" "$ext/extension.js" "$(num 27)" 0
expect "panel: no per-mode limit literal" "$ext/extension.js" \
    '_streamingMode[[:space:]]*[?][[:space:]]*[0-9]' 0
expect "panel: the maximum length comes from GSettings" "$ext/extension.js" \
    "get_int[(]'max-recording-minutes'[)]" 1
expect "settings: number ranges come from the schema" "$ext/prefs.js" \
    'new_with_range[(][[:space:]]*[0-9]' 0

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[[ "$failed" -eq 0 ]]
