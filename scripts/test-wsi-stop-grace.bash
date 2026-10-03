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
# the stub microphone is told to stop. It then runs tests/speech_to_text/: unit tests
# of the wsi-stream stop state, handlers, grace reader and drain; pre-buffer mode run
# end to end with a stub RealtimeSTT and fake pw-record. Standard streaming is NOT run
# end to end.
#
# Continuous dictation (Plan 00148 Phase 2): wsi-stream-server's segmenter and ordered
# commit as pure units; its dictation session run for real with a fake pw-record, a
# stub VAD and a stub transcriber (STOP drains the microphone to EOF and transcribes
# the last audio; every failure and safety stop); and wsi-stream's server-mode client
# loop against a stub server. The real Silero VAD and Whisper model are not loaded here
# (they need the host's packages); the plan's triage.bash runs them on the host.
#
# It also covers what shares those scripts (Plan 00148 Tasks 7.4 and 8.x): the
# settings reader wsi-setting, the model wsi picks for `auto` (wsi-resolve-model, with
# a stub ctranslate2 answering the GPU question), the keep-warm keys in the schema,
# that the play deploys every helper and the start-at-login unit, and as unit tests the
# resolver, the server's idle timeout and PID-file lock, and wsi-stream's start at login.
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

# gsettings: answers the stop-grace key from $STUB_GRACE and any other key from the
# file $STUB_SETTINGS/<key>, printed as gsettings would (strings quoted); records its argv.
cat > "$stubs/gsettings" <<'EOF'
#!/usr/bin/bash
printf '%s\n' "$*" > "$STUB_EVENTS/gsettings.argv"
if [ "${STUB_GSETTINGS_FAIL:-}" = 1 ]; then
    echo "No such schema" >&2
    exit 1
fi
key="${*: -1}"
if [ "$key" = stop-grace-seconds ]; then
    echo "$STUB_GRACE"
elif [ -f "${STUB_SETTINGS:-/nonexistent}/$key" ]; then
    cat "$STUB_SETTINGS/$key"
else
    echo "No such key \"$key\"" >&2
    exit 1
fi
EOF

# ctranslate2: the one call wsi-resolve-model makes, answered from $STUB_CUDA_DEVICES
mkdir -p "$stubs/python"
cat > "$stubs/python/ctranslate2.py" <<'EOF'
import os
def get_cuda_device_count():
    return int(os.environ["STUB_CUDA_DEVICES"])
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
printf '%s\n' "${WHISPER_MODEL-<unset>}" > "$STUB_EVENTS/whisper.model"
printf '%s\n' "${WHISPER_LANGUAGE-<unset>}" > "$STUB_EVENTS/whisper.language"
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

settings="$work/settings"
mkdir -p "$settings" "$work/dev"
# wsi run without --language reads this; LANG is unset below, so "system" means en
printf "'system'\n" > "$settings/language"

run_env=(
    env -i
    "PATH=$stubs:/usr/bin:/bin"
    "HOME=$work/home"
    "USER=$test_user"
    "STUB_EVENTS=$events"
    "STUB_SETTINGS=$settings"
    "PYTHONPATH=$stubs/python"
    "STUB_CUDA_DEVICES=0"
    # An empty stand-in for /dev: no NVIDIA device nodes, whatever this machine has
    "WSI_DEV_DIR=$work/dev"
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

# start_wsi <grace> [wsi args...] — launch the real wsi in the background; sets WSI_PID.
# STUB_CUDA_DEVICES in the caller's environment (default 0) is what the GPU probe says.
start_wsi() {
    local grace="$1"
    shift
    reset_events
    # WSI_TEST_TRACE=1 runs wsi under `bash -x`; the trace lands in the stderr dump
    "${run_env[@]}" "STUB_GRACE=$grace" "STUB_CUDA_DEVICES=${STUB_CUDA_DEVICES:-0}" \
        bash ${WSI_TEST_TRACE:+-x} "$BIN/wsi" "$@" \
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
echo "=== wsi-setting (the settings reader) ==="
#----------------------------------------------------------------------------
reset_events
printf "'server'\n" > "$settings/streaming-startup-mode"
got=$("${run_env[@]}" STUB_GRACE=3 bash "$BIN/wsi-setting" streaming-startup-mode); rc=$?
check "prints a string value without its GVariant quotes" "server" "$got"
check "…and exits 0" "0" "$rc"
check "reads the extension schema from the extension's own schema dir" \
    "--schemadir $work/home/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/schemas get org.gnome.shell.extensions.speech-to-text streaming-startup-mode" \
    "$(cat "$events/gsettings.argv")"
printf '20\n' > "$settings/server-idle-timeout-minutes"
got=$("${run_env[@]}" STUB_GRACE=3 bash "$BIN/wsi-setting" server-idle-timeout-minutes); rc=$?
check "prints a number as it is" "20" "$got"

got=$("${run_env[@]}" STUB_GRACE=3 bash "$BIN/wsi-setting" no-such-key 2> "$work/err"); rc=$?
check "an unreadable key fails" "1" "$rc"
check "…with nothing on stdout" "" "$got"
check "…and names the play to re-run" "1" "$(grep -c 'play-speech-to-text.yml' "$work/err")"

"${run_env[@]}" STUB_GRACE=3 bash "$BIN/wsi-setting" 2>/dev/null; rc=$?
check "no key is a usage error" "2" "$rc"
"${run_env[@]}" STUB_GRACE=3 bash "$BIN/wsi-setting" 'bad key;' 2>/dev/null; rc=$?
check "a key that is not a GSettings key name is refused" "2" "$rc"

#----------------------------------------------------------------------------
echo "=== schema: the single source ==="
#----------------------------------------------------------------------------
schema_key() {
    python3 - "$SCHEMA" "$1" <<'EOF'
import sys, xml.etree.ElementTree as ET
key = ET.parse(sys.argv[1]).find(f".//key[@name='{sys.argv[2]}']")
r = key.find("range")
print(key.get("type"), key.findtext("default").strip(), *((r.get("min"), r.get("max")) if r is not None else ()))
EOF
}
check "stop-grace-seconds: int, default 3, range 0..30" "i 3 0 30" "$(schema_key stop-grace-seconds)"
check "server-idle-timeout-minutes: int, default 20, range 0..1440" "i 20 0 1440" \
    "$(schema_key server-idle-timeout-minutes)"
check "server-start-at-login: boolean, default off" "b false" "$(schema_key server-start-at-login)"
check "continuous-dictation: boolean, default off until verified" "b false" \
    "$(schema_key continuous-dictation)"
check "max-recording-minutes: int, default 60, range 1..480" "i 60 1 480" \
    "$(schema_key max-recording-minutes)"
check "silence-autostop-seconds: int, default 120, range 0..3600" "i 120 0 3600" \
    "$(schema_key silence-autostop-seconds)"

PLAY="$REPO_ROOT/playbooks/imports/optional/common/play-speech-to-text.yml"
for helper in wsi-stop-grace wsi-setting wsi-resolve-model wsi-stream wsi-stream-server; do
    check "the play deploys $helper" "1" \
        "$(grep -c "files/home/.local/bin/$helper\"" "$PLAY")"
done
check "the play deploys the start-at-login unit" "1" \
    "$(grep -c 'files/home/.config/systemd/user/wsi-stream-server-at-login.service"' "$PLAY")"
check "the play enables the start-at-login unit" "1" \
    "$(grep -c 'name: wsi-stream-server-at-login.service' "$PLAY")"

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
    check "the panel is told the stop is pending (STOPPING)" "1" \
        "$(grep -c 'StateChanged STOPPING' "$events/gdbus.log")"
    check "auto without a GPU transcribes with small" "small" "$(cat "$events/whisper.model")"
    check "…in the system language when run without --language" "en" \
        "$(cat "$events/whisper.language")"
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
echo "=== wsi: grace 0 stops at once (English, a GPU: auto is distil-large-v3.5) ==="
#----------------------------------------------------------------------------
STUB_CUDA_DEVICES=1 start_wsi 0 --language en
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
    check "auto with a GPU and English transcribes with distil-large-v3.5, as its repo" \
        "distil-whisper/distil-large-v3.5-ct2" "$(cat "$events/whisper.model")"
    check "…and the transcriber is told English" "en" "$(cat "$events/whisper.language")"
else
    failed=$((failed + 1))
    echo "  FAIL: wsi never reached RECORDING; stderr:"
    cat "$work/wsi.err"
    kill -KILL "$WSI_PID" 2>/dev/null
fi

#----------------------------------------------------------------------------
echo "=== wsi by hand, no --language: the setting's language for model and transcriber ==="
#----------------------------------------------------------------------------
printf "'de'\n" > "$settings/language"
STUB_CUDA_DEVICES=1 start_wsi 0
if wait_for_recording_state; then
    kill -TERM "$WSI_PID"
    wait "$WSI_PID"; rc=$?
    check_wsi_exit "$rc"
    check "auto picks the multilingual model for the setting's language" "large-v3-turbo" \
        "$(cat "$events/whisper.model")"
    check "…and the transcriber gets that same language" "de" "$(cat "$events/whisper.language")"
else
    failed=$((failed + 1))
    echo "  FAIL: wsi never reached RECORDING; stderr:"
    cat "$work/wsi.err"
    kill -KILL "$WSI_PID" 2>/dev/null
fi
printf "'system'\n" > "$settings/language"

#----------------------------------------------------------------------------
echo "=== wsi: no grace value, no recording ==="
#----------------------------------------------------------------------------
reset_events
"${run_env[@]}" STUB_GSETTINGS_FAIL=1 STUB_GRACE=3 bash "$BIN/wsi" > "$work/wsi.out" 2> "$work/wsi.err"; rc=$?
check "wsi exits 1 when the grace cannot be read" "1" "$rc"
check "…before the microphone opens" "absent" \
    "$([ -e "$events/pw-record.started" ] && echo present || echo absent)"

#----------------------------------------------------------------------------
echo "=== wsi: no model, no recording ==="
#----------------------------------------------------------------------------
reset_events
"${run_env[@]}" STUB_GRACE=3 STUB_CUDA_DEVICES=not-a-number bash "$BIN/wsi" \
    > "$work/wsi.out" 2> "$work/wsi.err"; rc=$?
check "wsi exits 1 when auto cannot ask for a GPU" "1" "$rc"
check "…before the microphone opens" "absent" \
    "$([ -e "$events/pw-record.started" ] && echo present || echo absent)"
check "…and says why" "1" "$(grep -c 'cannot ask CTranslate2' "$work/wsi.err")"

reset_events
"${run_env[@]}" STUB_GRACE=3 WHISPER_MODEL=base.en bash "$BIN/wsi" --language de \
    > "$work/wsi.out" 2> "$work/wsi.err"; rc=$?
check "wsi refuses an English-only model for another language" "1" "$rc"
check "…before the microphone opens" "absent" \
    "$([ -e "$events/pw-record.started" ] && echo present || echo absent)"

#----------------------------------------------------------------------------
echo "=== qa-stt-limits.bash: a limit planted back in is caught ==="
#----------------------------------------------------------------------------
limits_root="$work/limits"
mkdir -p "$limits_root/files/home/.local/bin" "$limits_root/extensions/speech-to-text@fedora-desktop"
cp "$BIN/wsi" "$BIN/wsi-stream" "$BIN/wsi-stream-server" "$limits_root/files/home/.local/bin/"
cp "$REPO_ROOT/extensions/speech-to-text@fedora-desktop/extension.js" \
    "$REPO_ROOT/extensions/speech-to-text@fedora-desktop/prefs.js" \
    "$limits_root/extensions/speech-to-text@fedora-desktop/"
bash "$SCRIPT_DIR/qa-stt-limits.bash" "$limits_root" > "$work/limits.out" 2>&1; rc=$?
check "the gate passes on an unchanged copy" "0" "$rc"
printf '        const limit = this._streamingMode ? 120 : 30;\n' \
    >> "$limits_root/extensions/speech-to-text@fedora-desktop/extension.js"
bash "$SCRIPT_DIR/qa-stt-limits.bash" "$limits_root" > "$work/limits.out" 2>&1; rc=$?
check "…and fails once the panel holds a copy of the streaming limit" "1" "$rc"
check "…naming the rule" "1" "$(grep -c 'FAIL: panel: no 120' "$work/limits.out")"

#----------------------------------------------------------------------------
echo "=== wsi-stream, wsi-stream-server, wsi-resolve-model: unit tests (tests/speech_to_text) ==="
#----------------------------------------------------------------------------
if (cd "$REPO_ROOT" && python3 -m unittest \
        tests/speech_to_text/test_stop_grace.py \
        tests/speech_to_text/test_prebuffer_stop.py \
        tests/speech_to_text/test_continuous_segmenter.py \
        tests/speech_to_text/test_continuous_session.py \
        tests/speech_to_text/test_server_client.py \
        tests/speech_to_text/test_resolve_model.py \
        tests/speech_to_text/test_keep_warm.py) > "$work/unit.out" 2>&1; then
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
