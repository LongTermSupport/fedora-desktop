#!/usr/bin/env bash
# Plan 00212 — check-media-volume.bash: one acceptance check of the INSTALLED
# wsi-lower-media-volume, run as a leg of acceptance.bash. Everything runs on a private session
# bus started with dbus-run-session, so no real media player is touched, with a fake MPRIS
# player (fake-mpris-player.py) for the music and a fake pactl (fake-pactl.py, first on PATH)
# holding its one stream (index 7, of the fake player's process), so no real volume is touched.
# Scratch files are made with mktemp -d and removed on exit.
#
#   lowers          Playing at 70%: 23.3% while held, exactly 70% again at TERM, never paused
#   changed-by-hand the volume set by hand during the hold is left at TERM
#   stream-ended    the stream ends during the hold, nothing of the app left: exit 1 at TERM,
#                   saying the next stream may start at the lowered volume
#   recreated       the stream is re-created during the hold at the lowered volume (the next
#                   track): the new stream is put back to 70% at TERM, exit 0
#   no-stream       no stream: paused while held, played again at TERM
#   left-stopped    no stream, then stopped "by hand" during the hold: not played at TERM
#   not-playing     Stopped at the start: exit 0 at once, nothing touched
#   parent-gone     the process that started it ends: the volume restored, no TERM sent
#
# Usage: check-media-volume.bash lowers|changed-by-hand|stream-ended|recreated|no-stream|left-stopped|not-playing|parent-gone
set -euo pipefail

checks='lowers|changed-by-hand|stream-ended|recreated|no-stream|left-stopped|not-playing|parent-gone'
usage="usage: check-media-volume.bash ${checks}"
[[ $# -ge 1 ]] || {
    printf '%s\n' "${usage}" >&2
    exit 64
}
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
bin="${HOME}/.local/bin/wsi-lower-media-volume"
player=org.mpris.MediaPlayer2.plan00212
iface=org.mpris.MediaPlayer2.Player
full=45875    # 70% of pactl's 65536
lowered=15292 # a third of it, rounded

if [[ "$1" != --inside ]]; then
    [[ "|${checks}|" == *"|$1|"* ]] || {
        printf '%s\n' "${usage}" >&2
        exit 64
    }
    [[ -x "${bin}" ]] || {
        printf 'FAIL: %s is not installed; run play-speech-to-text.yml\n' "${bin}" >&2
        exit 1
    }
    command -v dbus-run-session >/dev/null || {
        printf 'FAIL: dbus-run-session is missing (dbus-daemon); run play-speech-to-text.yml\n' >&2
        exit 1
    }
    python3 -c 'from gi.repository import Gio' || {
        printf 'FAIL: python3-gobject is missing; run play-speech-to-text.yml\n' >&2
        exit 1
    }
    exec timeout 60 dbus-run-session -- bash "${BASH_SOURCE[0]}" --inside "$1"
fi
check="$2"

scratch="$(mktemp -d)"
log="${scratch}/player.log"
touch "${log}"
fake_pid=''
cleanup() {
    if [[ -n "${fake_pid}" ]]; then
        kill "${fake_pid}" 2>/dev/null || printf 'note: the fake player had already ended\n' >&2
    fi
    rm -rf -- "${scratch}"
}
trap cleanup EXIT

# The fake pactl, first on PATH for the command and for this script
mkdir "${scratch}/bin"
printf '#!/bin/sh\nexec python3 %q "$@"\n' "${here}/fake-pactl.py" >"${scratch}/bin/pactl"
chmod 0700 "${scratch}/bin/pactl"
export PATH="${scratch}/bin:${PATH}" FAKE_PACTL_STATE="${scratch}/streams.json"
printf '[]\n' >"${FAKE_PACTL_STATE}"
touch "${FAKE_PACTL_STATE}.log"

fail() {
    printf 'FAIL: %s: %s\n' "${check}" "$1" >&2
    printf -- '--- the fake player log:\n%s\n--- the volume changes:\n%s\n--- the streams:\n%s\n--- wsi-lower-media-volume stderr:\n%s\n' \
        "$(cat "${log}")" "$(cat "${FAKE_PACTL_STATE}.log")" "$(cat "${FAKE_PACTL_STATE}")" \
        "$(cat "${scratch}/hold.err" 2>/dev/null || printf '(none)')" >&2
    exit 1
}

status() {
    busctl --user get-property "${player}" /org/mpris/MediaPlayer2 "${iface}" PlaybackStatus
}

# volume: the fake stream's front-left raw volume, or "gone"
volume() {
    python3 -c 'import json,sys; s=json.load(open(sys.argv[1])); print(s[0]["volume"]["front-left"]["value"] if s else "gone")' "${FAKE_PACTL_STATE}"
}

# until_is WHAT WANT [PID...]: poll WHAT (status or volume) for at most 5 s; the fake player and
# each PID given must stay alive meanwhile, so one that dies is reported at once, not as a timeout.
until_is() {
    local i p what="$1" want="$2"
    shift 2
    for ((i = 0; i < 50; i++)); do
        [[ "$("${what}" 2>/dev/null)" == "${want}" ]] && return 0
        for p in "${fake_pid}" "$@"; do
            kill -0 "${p}" 2>/dev/null || fail "process ${p} ended before the ${what} became ${want}"
        done
        sleep 0.1
    done
    fail "the ${what} never became ${want} (it is: $("${what}" 2>&1))"
}

start="Playing"
[[ "${check}" == not-playing ]] && start="Stopped"
python3 "${here}/fake-mpris-player.py" plan00212 "${start}" "${log}" &
fake_pid=$!
until_is status "s \"${start}\""
case "${check}" in
    no-stream | left-stopped) ;;
    *) pactl init "${fake_pid}" "${full}" ;;
esac

# hold: start the command in the background, its PID in $hold
hold() {
    "${bin}" 2>"${scratch}/hold.err" &
    hold=$!
}

# release [WANT]: TERM to the hold, which must then exit WANT (default 0)
release() {
    local rc=0 want="${1:-0}"
    kill -TERM "${hold}"
    wait "${hold}" || rc=$?
    [[ "${rc}" -eq "${want}" ]] || fail "exit ${rc} after TERM, want ${want}"
}

case "${check}" in
    not-playing)
        rc=0
        timeout 10 "${bin}" 2>"${scratch}/hold.err" || rc=$?
        [[ "${rc}" -eq 0 ]] || fail "exit ${rc}, want 0 at once"
        [[ ! -s "${log}" ]] || fail "a player that was not playing was called"
        [[ ! -s "${FAKE_PACTL_STATE}.log" ]] || fail "a volume was changed"
        grep -q 'nothing is playing' "${scratch}/hold.err" || fail "it did not say nothing was playing"
        ;;
    lowers | changed-by-hand | stream-ended | recreated)
        hold
        until_is volume "${lowered}" "${hold}"
        [[ "$(status)" == 's "Playing"' ]] || fail "the player was paused as well: $(status)"
        case "${check}" in
            changed-by-hand) pactl set-sink-input-volume 7 30000 30000 ;;
            stream-ended) pactl end-stream 7 ;;
            recreated) pactl recreate-stream 7 8 ;;
        esac
        if [[ "${check}" == stream-ended ]]; then release 1; else release; fi
        case "${check}" in
            lowers)
                [[ "$(volume)" == "${full}" ]] || fail "not restored to ${full}: $(volume)"
                [[ ! -s "${log}" ]] || fail "the player was called, though its stream was found"
                ;;
            changed-by-hand)
                [[ "$(volume)" == 30000 ]] || fail "the volume set by hand was overwritten: $(volume)"
                grep -q 'left as it is' "${scratch}/hold.err" || fail "it did not say it left the volume"
                ;;
            stream-ended)
                grep -q 'has ended, and no stream of it is left' "${scratch}/hold.err" || fail "it did not report the stream that ended"
                ;;
            recreated)
                [[ "$(volume)" == "${full}" ]] || fail "the new stream was not put back to ${full}: $(volume)"
                grep -q 'stream 8, a new stream of it' "${scratch}/hold.err" || fail "it did not say it restored the new stream"
                ;;
        esac
        ;;
    no-stream | left-stopped)
        hold
        until_is status 's "Paused"' "${hold}"
        if [[ "${check}" == left-stopped ]]; then
            busctl --user call "${player}" /org/mpris/MediaPlayer2 org.fedoradesktop.Test.Player SetStatus s Stopped
        fi
        release
        if [[ "${check}" == no-stream ]]; then
            [[ "$(status)" == 's "Playing"' ]] || fail "not played again: $(status)"
            [[ "$(cat "${log}")" == $'Pause Paused\nPlay Playing' ]] || fail "calls were not one Pause and one Play"
        else
            [[ "$(status)" == 's "Stopped"' ]] || fail "the stopped player was changed: $(status)"
            grep -q '^Play' "${log}" && fail "a player stopped by hand was played again"
        fi
        ;;
    parent-gone)
        # The middle bash starts the hold and ends 2 s later, as GNOME Shell ending would; the
        # hold is then a child of someone else, and restores within its 1 s poll
        bash -c '"$1" 2>"$2" & printf "%s\n" "$!" >"$3"; sleep 2' _ "${bin}" "${scratch}/hold.err" "${scratch}/hold.pid" &
        until_is volume "${lowered}" "$!"
        until_is volume "${full}"
        hold="$(cat "${scratch}/hold.pid")"
        for ((i = 0; i < 50; i++)); do
            kill -0 "${hold}" 2>/dev/null || break
            sleep 0.1
        done
        kill -0 "${hold}" 2>/dev/null && fail "still running after restoring"
        grep -q 'parent process has ended' "${scratch}/hold.err" || fail "it did not say its parent had ended"
        ;;
esac
printf 'PASS: %s\n' "${check}"
