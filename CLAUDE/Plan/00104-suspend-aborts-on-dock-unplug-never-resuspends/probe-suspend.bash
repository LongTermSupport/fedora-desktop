#!/usr/bin/env bash
# probe-suspend.bash — the probe body for triage.bash. Appends a markdown section to the
# report path given as $1. Read-only: reads the journal and sysfs, changes nothing.
#
# Invoked as a leg by triage.bash (a leg is a command, not a shell function — see
# CLAUDE/PlanScriptStandards.md R10). Independently runnable for debugging:
#   ./probe-suspend.bash /tmp/report.md
#
# NOTE ON SANITISATION: this writes RAW host state (hostname, MACs, LAN addresses,
# container names) into a gitignored *-runs/ directory. It is never committed. The
# sanitised, committable transcription lives in TRIAGE-EVIDENCE.md.

set -euo pipefail

REPORT="${1:?usage: probe-suspend.bash <report-path> [incident-boot-selector]}"

# Which boot to treat as "the incident". Defaults to -1 (the boot before the current one),
# which was correct on 2026-09-08 — but it is RELATIVE: one more reboot and -1 points at an
# innocent boot, and every timeline/density probe below would then look CLEAN rather than
# blind. The report prints `journalctl --list-boots` so the reader can see which boot was
# actually measured, and this is overridable rather than baked in.
INCIDENT_BOOT="${2:--1}"

# printf octal \140 is a backtick. The markdown code fence is built this way rather than
# written literally because three backticks inside a single-quoted printf format read as an
# unterminated command substitution to shellcheck (SC2016), and suppressions are banned (R11).
FENCE="$(printf '\140\140\140')"

# A non-zero exit is DATA, not a failure — capture the status and carry on.
# (CLAUDE/PlanTriage.md, "The probe() helper".)
probe() {
    local label="$1"; shift
    local out rc
    # Progress to stderr: probes write their payload to the report file, so without this a
    # slow probe shows nothing at all and reads as a hang.
    printf '  ... %s\n' "$label" >&2
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n\n%s\n%s\n%s\n\n' \
        "$label" "$rc" "$FENCE" "${out:-(no output)}" "$FENCE" >> "$REPORT"
    return 0
}

# --- probe helpers are functions, not `bash -c` strings ----------------------------------

show_wakeup_enabled() {
    local f
    for f in /sys/bus/*/devices/*/power/wakeup; do
        [[ -r "$f" ]] || continue
        if [[ "$(cat "$f")" == "enabled" ]]; then
            echo "$f"
        fi
    done
}

show_suspend_stats() {
    local f
    for f in /sys/power/suspend_stats/*; do
        [[ -r "$f" ]] || continue
        printf '%-22s %s\n' "$(basename "$f")" "$(cat "$f")"
    done
}

show_gnome_power_settings() {
    local key
    for key in sleep-inactive-battery-type sleep-inactive-battery-timeout \
               sleep-inactive-ac-type sleep-inactive-ac-timeout; do
        printf '%-32s ' "$key"
        gsettings get org.gnome.settings-daemon.plugins.power "$key"
    done
    printf '%-32s ' "idle-delay"
    gsettings get org.gnome.desktop.session idle-delay
}

# Every suspend/resume/lid transition in a boot. $1 = journalctl boot selector.
show_sleep_timeline() {
    local boot="$1"
    journalctl --no-pager -b "$boot" -o short-iso \
        --grep 'PM: suspend entry|PM: suspend exit|The system will suspend now|Lid closed|Lid opened|System resumed|Performing sleep operation'
}

# Journal volume per minute — an awake machine logs continuously, a suspended one has a gap.
#
# SCOPED DELIBERATELY. A whole boot here is both unusable and painfully slow: boot -1 on this
# host is 7.77 MILLION lines, which is ~2880 histogram rows and minutes of piping. So the
# window starts at the LAST 'PM: suspend entry' in the boot — the moment the machine was
# supposed to go to sleep, which is exactly the region where a gap either appears or does not.
show_journal_density() {
    local boot="$1" since="" entry=""
    # Kernel-only search: 'PM: suspend entry' is a kernel message and -k is far cheaper than
    # scanning the full journal.
    entry="$(journalctl --no-pager -b "$boot" -k -o short-iso -q \
        --grep 'PM: suspend entry' | tail -n 1)"

    if [[ -n "$entry" ]]; then
        since="${entry%%+*}"
        since="${since/T/ }"
        echo "window: from the last 'PM: suspend entry' (${since}) to the end of boot ${boot}"
    else
        echo "NO 'PM: suspend entry' in boot ${boot} — the machine never attempted suspend"
        echo "in this boot, so there is no post-suspend window to measure. Showing the final"
        echo "30 minutes of the boot instead, which answers a DIFFERENT question."
        since="$(journalctl --no-pager -b "$boot" -o short-iso -q -n 1 \
            | grep -oE '^[0-9-]+T[0-9:]+' | tr 'T' ' ')"
        if [[ -z "$since" ]]; then
            echo "ERROR: could not determine the end of boot ${boot}." >&2
            return 1
        fi
        since="$(date -d "${since} -30 minutes" '+%Y-%m-%d %H:%M:%S')"
    fi
    echo
    journalctl --no-pager -b "$boot" --since "$since" -o short-iso -q \
        | grep -oE '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}' \
        | uniq -c
}

show_lid_and_power_state() {
    echo "== logind LidClosed"
    busctl get-property org.freedesktop.login1 /org/freedesktop/login1 \
        org.freedesktop.login1.Manager LidClosed
    echo "== /proc/acpi/button/lid"
    grep -H . /proc/acpi/button/lid/*/state
    echo "== power_supply online"
    grep -H . /sys/class/power_supply/*/online
}

show_logind_config() {
    local f
    echo "== /etc/systemd/logind.conf.d/"
    for f in /etc/systemd/logind.conf.d/*.conf; do
        [[ -r "$f" ]] || continue
        echo "--- $f"
        cat "$f"
    done
    echo
    echo "== EFFECTIVE settings, including unset defaults (HandleLidSwitch / ...Docked /"
    echo "   ...ExternalPower). These are logind MANAGER properties, so 'systemctl show"
    echo "   systemd-logind --property=HandleLidSwitch' prints nothing and still exits 0 —"
    echo "   use busctl, or a blind check is indistinguishable from a clean one."
    busctl get-property org.freedesktop.login1 /org/freedesktop/login1 \
        org.freedesktop.login1.Manager \
        HandleLidSwitch HandleLidSwitchDocked HandleLidSwitchExternalPower
}

# Which lid branch applies: docked -> external-power -> plain (man logind.conf).
# Run this with the dock ATTACHED and DETACHED to settle premise P1 — whether logind counts
# evdi/DisplayLink virtual outputs when deciding "docked".
show_lid_branch_inputs() {
    echo "== logind Docked property (true => HandleLidSwitchDocked branch applies)"
    busctl get-property org.freedesktop.login1 /org/freedesktop/login1 \
        org.freedesktop.login1.Manager Docked
    echo "== connected DRM outputs (more than one also selects the Docked branch)"
    local s connected=0
    for s in /sys/class/drm/card*/card*/status; do
        [[ -r "$s" ]] || continue
        if [[ "$(cat "$s")" == "connected" ]]; then
            echo "connected: $s"
            connected=$((connected + 1))
        fi
    done
    echo "connected output count: ${connected}"
}

# --- report ------------------------------------------------------------------------------

{
    echo "# Plan 00104 triage — suspend abort / failure to re-suspend"
    echo
    echo "Generated: $(date --iso-8601=seconds)"
    echo
    echo "READ THIS FOR: the 'sleep timeline, previous boot' and 'journal density' sections."
    echo "A 'PM: suspend entry' with no matching 'PM: suspend exit', followed by unbroken"
    echo "per-minute journal volume, is the machine failing to stay suspended."
    echo
    echo "RAW HOST STATE — gitignored, never commit. Sanitised copy: TRIAGE-EVIDENCE.md"
    echo
} >> "$REPORT"

probe "available sleep states (/sys/power/mem_sleep)" cat /sys/power/mem_sleep
probe "suspend statistics" show_suspend_stats
probe "boot index (WHICH boot is '${INCIDENT_BOOT}'?)" journalctl --no-pager --list-boots
probe "sleep timeline, current boot" show_sleep_timeline 0
probe "sleep timeline, incident boot ${INCIDENT_BOOT}" show_sleep_timeline "${INCIDENT_BOOT}"
probe "journal density per minute, incident boot ${INCIDENT_BOOT}" \
    show_journal_density "${INCIDENT_BOOT}"
probe "thermal / throttle events, incident boot ${INCIDENT_BOOT}" \
    journalctl --no-pager -b "${INCIDENT_BOOT}" -k --grep 'thermal|throttl|critical|overheat'
probe "wakeup-armed devices" show_wakeup_enabled
probe "lid and power-source state" show_lid_and_power_state
probe "logind lid configuration" show_logind_config
probe "which lid branch applies (P1)" show_lid_branch_inputs
probe "GNOME power settings" show_gnome_power_settings
probe "inhibitor locks" systemd-inhibit --list --no-pager
probe "system-sleep hooks" ls -la /usr/lib/systemd/system-sleep/

echo "Report written: $REPORT" >&2
