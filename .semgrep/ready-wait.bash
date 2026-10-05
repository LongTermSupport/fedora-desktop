#!/usr/bin/env bash
# Fixture for the bash half of ready-wait-ignores-child-exit
# (helpers/ready_wait/bash_ready_waits.py; CLAUDE/QA.md). A `ruleid:` comment marks the
# loop on the next line as one that must be reported; every other loop must not be.
set -euo pipefail

work="$(mktemp -d)"
events="$work/events"

# The originating shape in tmate-share: the URL is polled, the tmate that may have died
# at once is not.
tmate_share() {
    tmate -F &
    TMATE_PID=$!
    # ruleid: ready-wait-ignores-child-exit
    for _ in $(seq 1 30); do
        if tmate show-messages | grep -q "read only"; then
            break
        fi
        sleep 0.5
    done
    wait "$TMATE_PID"
}

# Cleared: each try first asks whether tmate is still running.
tmate_share_fixed() {
    tmate -F &
    TMATE_PID=$!
    for _ in $(seq 1 30); do
        if ! kill -0 "$TMATE_PID" 2>/dev/null; then
            wait "$TMATE_PID"
            exit 1
        fi
        if tmate show-messages | grep -q "read only"; then
            break
        fi
        sleep 0.5
    done
}

# The test-harness shape: one function starts the program and keeps its pid, another
# waits for it, and the caller runs the first then the second.
wait_for_recording_state() {
    local _
    # ruleid: ready-wait-ignores-child-exit
    for _ in $(seq 1 100); do
        grep -q "StateChanged RECORDING" "$events/gdbus.log" && return 0
        sleep 0.05
    done
    return 1
}

start_program() {
    program >"$work/out" 2>"$work/err" &
    PROGRAM_PID=$!
}

start_program
if wait_for_recording_state; then
    kill -TERM "$PROGRAM_PID"
fi
wait "$PROGRAM_PID"

# Cleared: the stub proxy's port-file wait also asks whether the proxy is alive.
python3 "$work/proxy.py" "$work/port" &
proxy_pid=$!
while [ ! -s "$work/port" ]; do
    kill -0 "$proxy_pid"
    sleep 0.1
done
wait "$proxy_pid"

# Not a wait: a monitor that sleeps, with nothing started in the background.
watch_power() {
    while true; do
        cat /sys/power/state
        sleep 1
    done
}

# Not a wait: a signal fired in the background, whose pid nobody keeps.
emit_state() {
    gdbus emit --session --signal StateChanged "$1" &
}
emit_state READY
until gdbus call --session --method Ready; do
    sleep 1
done
