#!/usr/bin/env bash
# probe-lid.bash — the probe body for triage.bash. Appends a markdown section to the report
# path given as $1. Read-only: reads config, sysfs, D-Bus properties and the RPM database.
#
# Invoked as a leg by triage.bash (a leg is a command, not a shell function — see
# CLAUDE/PlanScriptStandards.md). Independently runnable for debugging:
#   ./probe-lid.bash untracked/scratch/lid-report.md

set -euo pipefail

REPORT="${1:?usage: probe-lid.bash <report-path>}"

# printf octal \140 is a backtick; a literal fence in a single-quoted format reads as an
# unterminated command substitution to shellcheck (SC2016), and suppressions are banned.
FENCE="$(printf '\140\140\140')"

# A non-zero exit is DATA, not a failure — capture the status and carry on.
# (CLAUDE/PlanTriage.md, "The probe() helper".)
probe() {
    local label="$1"; shift
    local out rc
    printf '  ... %s\n' "$label" >&2
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n\n%s\n%s\n%s\n\n' \
        "$label" "$rc" "$FENCE" "${out:-(no output)}" "$FENCE" >> "$REPORT"
    return 0
}

show_acpi_lid() {
    local f
    for f in /proc/acpi/button/lid/*/state; do
        [[ -r "$f" ]] || { echo "no readable lid state under /proc/acpi/button/lid/"; return 1; }
        printf '%s: %s\n' "$f" "$(cat "$f")"
    done
}

show_drm_connectors() {
    local d
    for d in /sys/class/drm/card*-*; do
        [[ -r "$d/status" ]] || continue
        printf '%-28s status=%-14s enabled=%s\n' \
            "$(basename "$d")" "$(cat "$d/status")" "$(cat "$d/enabled")"
    done
}

upower_property() {
    busctl get-property org.freedesktop.UPower /org/freedesktop/UPower \
        org.freedesktop.UPower "$1"
}

show_upower_lid() {
    local p
    for p in LidIsPresent LidIsClosed OnBattery; do
        printf '%-14s %s\n' "$p" "$(upower_property "$p")"
    done
}

show_logind_lid() {
    local p
    for p in LidClosed Docked HandleLidSwitch HandleLidSwitchExternalPower HandleLidSwitchDocked; do
        printf '%-30s %s\n' "$p" "$(busctl get-property org.freedesktop.login1 \
            /org/freedesktop/login1 org.freedesktop.login1.Manager "$p")"
    done
}

# Over D-Bus, not powerprofilesctl: the host's provider is tuned-ppd, which serves the
# PowerProfiles API without shipping that CLI.
show_power_profile() {
    printf 'active profile: %s\n' "$(busctl get-property org.freedesktop.UPower.PowerProfiles \
        /org/freedesktop/UPower/PowerProfiles org.freedesktop.UPower.PowerProfiles ActiveProfile)"
    printf 'power-saver-profile-on-low-battery: %s\n' \
        "$(gsettings get org.gnome.settings-daemon.plugins.power power-saver-profile-on-low-battery)"
}

show_logind_dropins() {
    local f
    for f in /etc/systemd/logind.conf.d/*.conf; do
        [[ -r "$f" ]] || { echo "no logind drop-ins"; return 0; }
        printf '== %s\n' "$f"
        cat "$f"
    done
}

# Mutter's own view of the monitors: whether eDP-1 is part of a logical monitor is the
# symptom itself, independent of what sysfs says the connector is doing.
show_mutter_state() {
    gdbus call --session --dest org.gnome.Mutter.DisplayConfig \
        --object-path /org/gnome/Mutter/DisplayConfig \
        --method org.gnome.Mutter.DisplayConfig.GetCurrentState
}

{
    printf '## Lid, panel and power facts\n\n'
    printf 'Captured: %s\n\n' "$(date --iso-8601=seconds)"
} >> "$REPORT"

probe "ACPI lid state (kernel)" show_acpi_lid
probe "DRM connectors (sysfs)" show_drm_connectors
probe "UPower lid properties" show_upower_lid
probe "logind lid properties" show_logind_lid
probe "Inhibitors" systemd-inhibit --list --no-pager
probe "Power profile" show_power_profile
probe "IgnoreLid lines in UPower.conf" grep -n 'IgnoreLid' /etc/UPower/UPower.conf
probe "UPower config directory" ls -l --time-style=full-iso /etc/UPower/
# rc 1 means "the files differ", which is the answer being asked for.
probe "UPower.conf.rpmsave vs UPower.conf" diff -u /etc/UPower/UPower.conf.rpmsave /etc/UPower/UPower.conf
probe "upower package and install time" rpm -q --qf '%{NAME}-%{VERSION}-%{RELEASE} installed %{INSTALLTIME:date}\n' upower
# rc 1 with output means files are modified from the package: data, not failure.
probe "upower package verification" rpm -V upower
probe "logind drop-ins" show_logind_dropins
probe "Mutter display state" show_mutter_state
