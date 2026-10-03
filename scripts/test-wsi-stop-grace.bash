#!/usr/bin/env bash
# Delayed stop for speech-to-text (Plan 00148 Phase 0).
#
# WHY THIS TEST EXISTS. Pressing Insert sends the recorder SIGTERM, and every recorder
# stopped capturing at once, cutting off the words still being spoken. The first TERM
# now keeps recording for a grace read from the extension's GSettings key (via
# wsi-stop-grace); a second TERM stops at once; 0 means no grace.
#
# This drives the REAL batch recorder (files/home/.local/bin/wsi) end to end with stub
# pw-record / sox / soxi / faster-whisper / gdbus / gsettings on PATH, and measures when
# the stub microphone is told to stop. It then runs the wsi-stream unit tests
# (tests/speech_to_text/), which cover the streaming modes without RealtimeSTT.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN="$REPO_ROOT/files/home/.local/bin"
SCHEMA="$REPO_ROOT/extensions/speech-to-text@fedora-desktop/schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml"

work=$(mktemp -d)
test_user="wsi-grace-test-$$"
cleanup() {
    rm -rf "$work"
    rm -f "/dev/shm/stt-recording-$test_user.pid" "/dev/shm/wfile-$test_user.wav"
}
trap cleanup EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

#----------------------------------------------------------------------------
# Stubs
#----------------------------------------------------------------------------
stubs="$work/stubs"
events="$work/events"
mkdir -p "$stubs" "$events" "$work/home"

# gsettings: answers the stop-grace key from $STUB_GRACE, records its argv.
cat > "$stubs/gsettings" <<'EOF'
#!/usr/bin/bash
printf '%s\n' "$*" > "$STUB_EVENTS/gsettings.argv"
if [ "${STUB_GSETTINGS_FAIL:-}" = 1 ]; then
    echo "No such schema" >&2
    exit 1
fi
echo "$STUB_GRACE"
EOF

# pw-record: Python, because a bash stub backgrounded by a non-interactive shell starts
# with SIGINT ignored and cannot trap it; the real pw-record installs its own handler.
cat > "$stubs/pw-record" <<'EOF'
#!/usr/bin/env python3
import os, signal, sys, time
events = os.environ["STUB_EVENTS"]
def stopped(signum, frame):
    with open(f"{events}/pw-record.stopped", "w") as f:
        f.write(f"{time.time()}\n")
    sys.exit(0)
signal.signal(signal.SIGINT, stopped)
signal.signal(signal.SIGTERM, stopped)
with open(sys.argv[-1], "wb") as f:
    f.write(b"RIFF-stub-audio")
open(f"{events}/pw-record.started", "w").close()
while True:
    time.sleep(0.02)
EOF

cat > "$stubs/sox" <<'EOF'
#!/usr/bin/bash
cp "$1" "$2"
EOF
cat > "$stubs/soxi" <<'EOF'
#!/usr/bin/bash
echo 2.0
EOF
cat > "$stubs/faster-whisper-transcribe" <<'EOF'
#!/usr/bin/bash
echo "hello world"
EOF
# gdbus: records every call; answers Notify like the real notification daemon.
cat > "$stubs/gdbus" <<'EOF'
#!/usr/bin/bash
printf '%s\n' "$*" >> "$STUB_EVENTS/gdbus.log"
if [ "$1" = call ]; then
    echo "(uint32 7,)"
fi
EOF
chmod 755 "$stubs"/*

run_env=(
    env -i
    "PATH=$stubs:/usr/bin:/bin"
    "HOME=$work/home"
    "USER=$test_user"
    "STUB_EVENTS=$events"
)

reset_events() {
    rm -f "$events"/*
}

now() { date +%s.%N; }

# seconds_between <from> <to> → whole tenths, so comparisons stay in integer bash
tenths_between() {
    awk -v a="$1" -v b="$2" 'BEGIN { printf "%d\n", (b - a) * 10 }'
}

check_wsi_exit() {
    check "wsi exits 0" "0" "$1"
    if [ "$1" != 0 ]; then
        echo "    wsi stderr:"
        cat "$work/wsi.err"
    fi
}

wait_for_recording_state() {
    local _
    for _ in $(seq 1 100); do
        grep -q "StateChanged RECORDING" "$events/gdbus.log" 2>/dev/null && return 0
        sleep 0.05
    done
    return 1
}

# start_wsi <grace> — launch the real wsi in the background; sets WSI_PID
start_wsi() {
    reset_events
    # WSI_TEST_TRACE=1 runs wsi under `bash -x`; the trace lands in the stderr dump
    "${run_env[@]}" "STUB_GRACE=$1" bash ${WSI_TEST_TRACE:+-x} "$BIN/wsi" \
        > "$work/wsi.out" 2> "$work/wsi.err" &
    WSI_PID=$!
}

#----------------------------------------------------------------------------
echo "=== wsi-stop-grace (the one reader) ==="
#----------------------------------------------------------------------------
reset_events
got=$("${run_env[@]}" STUB_GRACE=3 bash "$BIN/wsi-stop-grace"); rc=$?
check "prints the key's value" "3" "$got"
check "…and exits 0" "0" "$rc"
check "reads the extension schema from the extension's own schema dir" \
    "--schemadir $work/home/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/schemas get org.gnome.shell.extensions.speech-to-text stop-grace-seconds" \
    "$(cat "$events/gsettings.argv")"

got=$("${run_env[@]}" STUB_GSETTINGS_FAIL=1 bash "$BIN/wsi-stop-grace" 2> "$work/err"); rc=$?
check "an unreadable key fails" "1" "$rc"
check "…with nothing on stdout" "" "$got"
check "…and names the play to re-run" "1" "$(grep -c 'play-speech-to-text.yml' "$work/err")"

got=$("${run_env[@]}" "STUB_GRACE=uint32 3" bash "$BIN/wsi-stop-grace" 2>/dev/null); rc=$?
check "a value that is not a whole number fails" "1" "$rc"

"${run_env[@]}" STUB_GRACE=3 bash "$BIN/wsi-stop-grace" extra 2>/dev/null; rc=$?
check "arguments are refused" "2" "$rc"

#----------------------------------------------------------------------------
echo "=== schema: the single source ==="
#----------------------------------------------------------------------------
schema_fact=$(python3 - "$SCHEMA" <<'EOF'
import sys, xml.etree.ElementTree as ET
key = ET.parse(sys.argv[1]).find(".//key[@name='stop-grace-seconds']")
r = key.find("range")
print(key.get("type"), key.findtext("default").strip(), r.get("min"), r.get("max"))
EOF
)
check "stop-grace-seconds: int, default 3, range 0..30" "i 3 0 30" "$schema_fact"
check "the play deploys the reader" "1" \
    "$(grep -c 'files/home/.local/bin/wsi-stop-grace"' "$REPO_ROOT/playbooks/imports/optional/common/play-speech-to-text.yml")"

#----------------------------------------------------------------------------
echo "=== wsi: first TERM keeps recording for the grace ==="
#----------------------------------------------------------------------------
start_wsi 2
if wait_for_recording_state; then
    t_term=$(now)
    kill -TERM "$WSI_PID"
    sleep 1
    check "microphone still open 1s into a 2s grace" "absent" \
        "$([ -e "$events/pw-record.stopped" ] && echo present || echo absent)"
    wait "$WSI_PID"; rc=$?
    check_wsi_exit "$rc"
    t_stop=$(cat "$events/pw-record.stopped" 2>/dev/null) || t_stop="$t_term"
    gap=$(tenths_between "$t_term" "$t_stop")
    check "microphone stopped 1.8-3.0s after the TERM (got ${gap} tenths)" "yes" \
        "$([ "$gap" -ge 18 ] && [ "$gap" -le 30 ] && echo yes || echo no)"
    check "the text is still delivered" "Hello world" "$(cat "$work/wsi.out")"
    check "the pending stop is announced" "1" \
        "$(grep -c 'Notify .*Stopping in 2s' "$events/gdbus.log")"
    check "no new extension state is invented for it" "0" \
        "$(grep -c 'StateChanged \(STOPPING\|PENDING\)' "$events/gdbus.log")"
else
    failed=$((failed + 1))
    echo "  FAIL: wsi never reached RECORDING; stderr:"
    cat "$work/wsi.err"
    kill -KILL "$WSI_PID" 2>/dev/null
fi

#----------------------------------------------------------------------------
echo "=== wsi: a second TERM during the grace stops at once ==="
#----------------------------------------------------------------------------
start_wsi 10
if wait_for_recording_state; then
    kill -TERM "$WSI_PID"
    sleep 0.5
    t_term=$(now)
    kill -TERM "$WSI_PID"
    wait "$WSI_PID"; rc=$?
    check_wsi_exit "$rc"
    t_stop=$(cat "$events/pw-record.stopped" 2>/dev/null) || t_stop=0
    gap=$(tenths_between "$t_term" "$t_stop")
    check "microphone stopped within 1s of the second TERM (got ${gap} tenths)" "yes" \
        "$([ "$gap" -ge 0 ] && [ "$gap" -le 10 ] && echo yes || echo no)"
    check "the text is delivered" "Hello world" "$(cat "$work/wsi.out")"
else
    failed=$((failed + 1))
    echo "  FAIL: wsi never reached RECORDING; stderr:"
    cat "$work/wsi.err"
    kill -KILL "$WSI_PID" 2>/dev/null
fi

#----------------------------------------------------------------------------
echo "=== wsi: grace 0 stops at once ==="
#----------------------------------------------------------------------------
start_wsi 0
if wait_for_recording_state; then
    t_term=$(now)
    kill -TERM "$WSI_PID"
    wait "$WSI_PID"; rc=$?
    check_wsi_exit "$rc"
    t_stop=$(cat "$events/pw-record.stopped" 2>/dev/null) || t_stop=0
    gap=$(tenths_between "$t_term" "$t_stop")
    check "microphone stopped within 1s (got ${gap} tenths)" "yes" \
        "$([ "$gap" -ge 0 ] && [ "$gap" -le 10 ] && echo yes || echo no)"
    check "no pending-stop notice" "0" "$(grep -c 'Stopping in' "$events/gdbus.log")"
else
    failed=$((failed + 1))
    echo "  FAIL: wsi never reached RECORDING; stderr:"
    cat "$work/wsi.err"
    kill -KILL "$WSI_PID" 2>/dev/null
fi

#----------------------------------------------------------------------------
echo "=== wsi: no grace value, no recording ==="
#----------------------------------------------------------------------------
reset_events
"${run_env[@]}" STUB_GSETTINGS_FAIL=1 STUB_GRACE=3 bash "$BIN/wsi" > "$work/wsi.out" 2> "$work/wsi.err"; rc=$?
check "wsi exits 1 when the grace cannot be read" "1" "$rc"
check "…before the microphone opens" "absent" \
    "$([ -e "$events/pw-record.started" ] && echo present || echo absent)"

#----------------------------------------------------------------------------
echo "=== wsi-stream: unit tests (tests/speech_to_text) ==="
#----------------------------------------------------------------------------
if (cd "$REPO_ROOT" && python3 -m unittest tests/speech_to_text/test_stop_grace.py) > "$work/unit.out" 2>&1; then
    passed=$((passed + 1))
    echo "  PASS: $(grep -E '^Ran [0-9]+ tests' "$work/unit.out")"
else
    failed=$((failed + 1))
    echo "  FAIL: wsi-stream unit tests"
    cat "$work/unit.out"
fi

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
