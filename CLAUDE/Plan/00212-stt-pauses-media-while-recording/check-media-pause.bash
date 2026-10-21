#!/usr/bin/env bash
# Plan 00212 — check-media-pause.bash: one acceptance check of the INSTALLED wsi-pause-media,
# run as a leg of acceptance.bash. Everything runs on a private session bus started with
# dbus-run-session, so no real media player is paused, and a fake MPRIS player
# (fake-mpris-player.py) stands for the music. Scratch files are made with mktemp -d and
# removed on exit.
#
#   resumes        Playing: paused while held, played again at TERM, exit 0
#   left-stopped   Playing, then stopped "by hand" during the hold: not played at TERM
#   not-playing    Stopped at the start: exit 0 at once, never touched
#   parent-gone    Playing, and the process that started it ends: played again, no TERM sent
#
# Usage: check-media-pause.bash resumes|left-stopped|not-playing|parent-gone
set -euo pipefail

usage='usage: check-media-pause.bash resumes|left-stopped|not-playing|parent-gone'
[[ $# -ge 1 ]] || {
    printf '%s\n' "${usage}" >&2
    exit 64
}
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
bin="${HOME}/.local/bin/wsi-pause-media"
player=org.mpris.MediaPlayer2.plan00212
iface=org.mpris.MediaPlayer2.Player

if [[ "$1" != --inside ]]; then
    case "$1" in
        resumes | left-stopped | not-playing | parent-gone) ;;
        *)
            printf '%s\n' "${usage}" >&2
            exit 64
            ;;
    esac
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

fail() {
    printf 'FAIL: %s: %s\n' "${check}" "$1" >&2
    printf -- '--- the fake player log:\n%s\n--- wsi-pause-media stderr:\n%s\n' \
        "$(cat "${log}")" "$(cat "${scratch}/hold.err" 2>/dev/null || printf '(none)')" >&2
    exit 1
}

status() {
    busctl --user get-property "${player}" /org/mpris/MediaPlayer2 "${iface}" PlaybackStatus
}

# until_status WANT [PID...]: poll for at most 5 s; the fake player and each PID given must stay
# alive meanwhile, so one that dies is reported at once, not as a timeout.
until_status() {
    local i p want="$1"
    shift
    for ((i = 0; i < 50; i++)); do
        [[ "$(status 2>/dev/null)" == "s \"${want}\"" ]] && return 0
        for p in "${fake_pid}" "$@"; do
            kill -0 "${p}" 2>/dev/null || fail "process ${p} ended before the player became ${want}"
        done
        sleep 0.1
    done
    fail "the player never became $1 (it is: $(status 2>&1))"
}

start="Playing"
[[ "${check}" == not-playing ]] && start="Stopped"
python3 "${here}/fake-mpris-player.py" plan00212 "${start}" "${log}" &
fake_pid=$!
until_status "${start}"

case "${check}" in
    not-playing)
        rc=0
        timeout 10 "${bin}" 2>"${scratch}/hold.err" || rc=$?
        [[ "${rc}" -eq 0 ]] || fail "exit ${rc}, want 0 at once"
        [[ ! -s "${log}" ]] || fail "a player that was not playing was called"
        grep -q 'nothing is playing' "${scratch}/hold.err" || fail "it did not say nothing was playing"
        ;;
    resumes | left-stopped)
        "${bin}" 2>"${scratch}/hold.err" &
        hold=$!
        until_status Paused "${hold}"
        if [[ "${check}" == left-stopped ]]; then
            busctl --user call "${player}" /org/mpris/MediaPlayer2 org.fedoradesktop.Test.Player SetStatus s Stopped
        fi
        kill -TERM "${hold}"
        rc=0
        wait "${hold}" || rc=$?
        [[ "${rc}" -eq 0 ]] || fail "exit ${rc} after TERM, want 0"
        if [[ "${check}" == resumes ]]; then
            [[ "$(status)" == 's "Playing"' ]] || fail "not played again: $(status)"
            [[ "$(cat "${log}")" == $'Pause Paused\nPlay Playing' ]] || fail "calls were not one Pause and one Play"
        else
            [[ "$(status)" == 's "Stopped"' ]] || fail "the stopped player was changed: $(status)"
            grep -q '^Play' "${log}" && fail "a player stopped by hand was played again"
        fi
        ;;
    parent-gone)
        # The middle bash starts the hold and ends 2 s later, as GNOME Shell ending would; the
        # hold is then a child of someone else, and resumes within its 1 s poll
        bash -c '"$1" 2>"$2" & printf "%s\n" "$!" >"$3"; sleep 2' _ "${bin}" "${scratch}/hold.err" "${scratch}/hold.pid" &
        until_status Paused "$!"
        until_status Playing
        hold="$(cat "${scratch}/hold.pid")"
        for ((i = 0; i < 50; i++)); do
            kill -0 "${hold}" 2>/dev/null || break
            sleep 0.1
        done
        kill -0 "${hold}" 2>/dev/null && fail "still running after resuming"
        grep -q 'parent process has ended' "${scratch}/hold.err" || fail "it did not say its parent had ended"
        ;;
esac
printf 'PASS: %s\n' "${check}"
