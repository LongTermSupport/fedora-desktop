#!/usr/bin/env bash
# probe-power-transition.bash — capture what the system does across a live AC power
# transition. Read-only: it watches sysfs and the journal, and changes nothing.
#
# WHY THIS EXISTS: plan 00104's premise P1 asks which logind lid branch applies
# (docked -> external-power -> plain, per man logind.conf). With no dock attached the
# branch is decided purely by AC state, so unplugging the mains cord is a sufficient and
# far less disruptive test than docking. It also answers the core of H1: whether logind
# re-evaluates ANYTHING when the power source changes while the lid state is unchanged.
#
# The operator does not need to answer any prompt — this polls, so they unplug and replug
# whenever convenient. Safe to run while working: nothing suspends, nothing is reconfigured.
#
# Usage: probe-power-transition.bash <report-path> [timeout-seconds]
#
# Invoked as a leg by triage.bash --watch-power. Independently runnable for debugging.

set -euo pipefail

REPORT="${1:?usage: probe-power-transition.bash <report-path> [timeout-seconds]}"
TIMEOUT="${2:-120}"
POLL_INTERVAL=1

AC_ONLINE_PATH=/sys/class/power_supply/AC/online

if [[ ! -r "$AC_ONLINE_PATH" ]]; then
    echo "ERROR: ${AC_ONLINE_PATH} is not readable — cannot observe power transitions." >&2
    echo "  This host exposes a different power-supply topology than plan 00104 assumed;" >&2
    echo "  re-check /sys/class/power_supply/ before trusting any AC-related finding." >&2
    exit 1
fi

read_ac() { cat "$AC_ONLINE_PATH"; }

# Full lid/power/display state — the inputs to the logind branch precedence.
snapshot_state() {
    echo "AC online:            $(read_ac)"
    echo -n "logind Docked:        "
    busctl get-property org.freedesktop.login1 /org/freedesktop/login1 \
        org.freedesktop.login1.Manager Docked
    echo -n "logind LidClosed:     "
    busctl get-property org.freedesktop.login1 /org/freedesktop/login1 \
        org.freedesktop.login1.Manager LidClosed
    local s connected=0
    for s in /sys/class/drm/card*/card*/status; do
        [[ -r "$s" ]] || continue
        if [[ "$(cat "$s")" == "connected" ]]; then
            connected=$((connected + 1))
        fi
    done
    echo "connected outputs:    ${connected}"
}

# Everything logind/upower/the kernel said in the window. THE decisive output: if logind
# emits nothing here, it did not re-evaluate the lid on the power change (premise behind H1).
#
# TWO SEPARATE QUERIES, DELIBERATELY. `-u <unit>` and `-k` are MUTUALLY EXCLUSIVE — journalctl
# ANDs them, and no record is both a kernel message and a unit's message, so
# `journalctl -u systemd-logind -k` returns ZERO lines for an entire boot. An earlier revision
# of this probe combined them and reported a confident "-- No entries --" that was a property
# of the query, not of the system. Never merge these back into one invocation.
#
# The unit query is deliberately UNFILTERED: logind+upower produce only a couple of hundred
# lines per boot, so a --grep here could hide the very message being looked for, and cheapness
# is not a reason to risk a second false negative.
journal_since() {
    local since="$1"
    echo "== systemd-logind + upower (unfiltered)"
    journalctl --no-pager --since "$since" -o short-iso -q \
        -u systemd-logind -u upower
    echo
    echo "== kernel (power/lid/suspend terms)"
    journalctl --no-pager --since "$since" -o short-iso -q -k \
        --grep 'lid|Lid|suspend|Suspend|sleep|power|Power|AC|battery|Battery|charg'
}

# Poll until the AC state differs from "$1", or the timeout expires.
# Prints the new value on stdout; every diagnostic goes to stderr (stdout is the payload).
await_ac_change() {
    local from="$1" waited=0 now
    while (( waited < TIMEOUT )); do
        now="$(read_ac)"
        if [[ "$now" != "$from" ]]; then
            printf '%s' "$now"
            return 0
        fi
        sleep "$POLL_INTERVAL"
        waited=$((waited + POLL_INTERVAL))
    done
    return 1
}

FENCE="$(printf '\140\140\140')"

emit() {
    local label="$1" body="$2"
    printf '### %s\n\n%s\n%s\n%s\n\n' "$label" "$FENCE" "$body" "$FENCE" >> "$REPORT"
}

{
    echo "## Live AC power transition"
    echo
    echo "READ THIS FOR: the 'journal during unplug' section. If systemd-logind logged"
    echo "NOTHING there, it did not re-evaluate the lid when the power source changed —"
    echo "which is the mechanism behind plan 00104's H1."
    echo
} >> "$REPORT"

BASELINE_AC="$(read_ac)"
emit "baseline state" "$(snapshot_state)"

if [[ "$BASELINE_AC" == "1" ]]; then
    echo ">>> Please UNPLUG the mains cord now (waiting up to ${TIMEOUT}s)..." >&2
else
    echo ">>> Please PLUG IN the mains cord now (waiting up to ${TIMEOUT}s)..." >&2
fi

MARK1="$(date '+%Y-%m-%d %H:%M:%S')"
if ! AFTER_AC="$(await_ac_change "$BASELINE_AC")"; then
    echo "ERROR: AC state did not change within ${TIMEOUT}s (still '${BASELINE_AC}')." >&2
    echo "  Nothing was observed, so no transition section was written — an empty" >&2
    echo "  section here would read as 'the system did nothing', which is not what" >&2
    echo "  happened. Re-run and unplug the cord when prompted." >&2
    exit 1
fi

echo ">>> Transition detected: AC ${BASELINE_AC} -> ${AFTER_AC}" >&2
sleep 3   # let logind/upower finish reacting before snapshotting
emit "state after first transition (AC ${BASELINE_AC} -> ${AFTER_AC})" "$(snapshot_state)"
emit "journal during first transition" "$(journal_since "$MARK1")"

echo ">>> Now please RESTORE the cord to its original state (waiting up to ${TIMEOUT}s)..." >&2
MARK2="$(date '+%Y-%m-%d %H:%M:%S')"
if ! FINAL_AC="$(await_ac_change "$AFTER_AC")"; then
    echo "WARNING: AC state did not return within ${TIMEOUT}s (still '${AFTER_AC}')." >&2
    echo "  The first transition WAS captured and is in the report; only the restore" >&2
    echo "  leg is missing. Re-plug the cord manually." >&2
    exit 1
fi

echo ">>> Restored: AC ${AFTER_AC} -> ${FINAL_AC}" >&2
sleep 3
emit "state after restore (AC ${AFTER_AC} -> ${FINAL_AC})" "$(snapshot_state)"
emit "journal during restore" "$(journal_since "$MARK2")"

echo "Power-transition section written: $REPORT" >&2
