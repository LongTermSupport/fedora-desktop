#!/usr/bin/env bash
#
# collect-diagnostics.bash — gather a comprehensive host debug snapshot
# for repo-side analysis. Invoked by play-collect-diagnostics.yml.
#
# Each diagnostic is captured into its own file under a numbered subdir,
# with the command line, timestamp and exit code recorded inline. A
# _manifest.tsv lists every capture and its exit code so an agent can see
# at a glance which probes succeeded, failed or were not applicable.
#
# Usage:
#   collect-diagnostics.bash <output_dir> <user_login>
#
# Runs as root. User-level probes (gnome-extensions, systemctl --user,
# wpctl, gsettings, ...) are executed via sudo with the user's XDG /
# DBus session env so they see the real graphical session.

set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "Usage: $0 <output_dir> <user_login>" >&2
    exit 2
fi

OUT_DIR="$1"
USER_LOGIN="$2"
USER_UID="$(id -u "$USER_LOGIN")"
USER_GID="$(id -g "$USER_LOGIN")"
USER_HOME="$(getent passwd "$USER_LOGIN" | cut -d: -f6)"
RUNTIME_DIR="/run/user/$USER_UID"
MANIFEST="$OUT_DIR/_manifest.tsv"

mkdir -p "$OUT_DIR"
printf "subdir\tname\texit_code\n" > "$MANIFEST"

# capture <subdir> <name> <argv...>
# Captures argv's stdout+stderr into $OUT_DIR/<subdir>/<name>.txt with a
# header (command, time, host) and footer (exit code). Always succeeds;
# the per-probe exit code lives in the file and the manifest. The
# `if "$@"; then rc=0; else rc=$?; fi` form lets a failing probe record
# its exit code without errexit killing the whole collection — failures
# are surfaced, not hidden.
capture() {
    local subdir="$1" name="$2"
    shift 2
    local dir="$OUT_DIR/$subdir"
    local outfile="$dir/$name.txt"
    local rc=0
    mkdir -p "$dir"
    {
        echo "# Diagnostic: $subdir/$name"
        echo "# Command  : $*"
        echo "# Captured : $(date -Iseconds)"
        echo "# Host     : $(hostname)"
        echo "# ---- BEGIN OUTPUT ----"
    } > "$outfile"

    if "$@" >> "$outfile" 2>&1; then
        rc=0
    else
        rc=$?
    fi

    {
        echo "# ---- END OUTPUT ----"
        echo "# Exit code: $rc"
    } >> "$outfile"

    printf "%s\t%s\t%d\n" "$subdir" "$name" "$rc" >> "$MANIFEST"

    if [[ $rc -eq 0 ]]; then
        printf "  ok    %s/%s\n" "$subdir" "$name" >&2
    else
        printf "  rc=%-3d %s/%s\n" "$rc" "$subdir" "$name" >&2
    fi
}

# capture_sh <subdir> <name> <bash_snippet>
# Convenience wrapper for pipelines / multi-step probes.
capture_sh() {
    local subdir="$1" name="$2" snippet="$3"
    capture "$subdir" "$name" bash -c "$snippet"
}

# capture_user <subdir> <name> <argv...>
# Runs argv as USER_LOGIN with their XDG / DBus session env so user-level
# tooling sees the real session.
capture_user() {
    local subdir="$1" name="$2"
    shift 2
    capture "$subdir" "$name" sudo -u "$USER_LOGIN" \
        env \
            HOME="$USER_HOME" \
            USER="$USER_LOGIN" \
            LOGNAME="$USER_LOGIN" \
            XDG_RUNTIME_DIR="$RUNTIME_DIR" \
            DBUS_SESSION_BUS_ADDRESS="unix:path=$RUNTIME_DIR/bus" \
            DISPLAY=":0" \
            "$@"
}

# capture_user_sh <subdir> <name> <bash_snippet>
capture_user_sh() {
    local subdir="$1" name="$2" snippet="$3"
    capture_user "$subdir" "$name" bash -c "$snippet"
}

echo "==> Collecting diagnostics for user '$USER_LOGIN' (uid=$USER_UID, home=$USER_HOME)" >&2
echo "==> Output dir: $OUT_DIR" >&2
echo "" >&2

# ---------------------------------------------------------------------------
# 01-identity — what this machine is
# ---------------------------------------------------------------------------
capture    01-identity hostnamectl     hostnamectl
capture    01-identity uname           uname -a
capture    01-identity os-release      cat /etc/os-release
capture    01-identity kernel-cmdline  cat /proc/cmdline
capture    01-identity grub-defaults   cat /etc/default/grub
capture_sh 01-identity boot-files      'ls -la /boot'
capture    01-identity machine-id      cat /etc/machine-id
capture    01-identity uptime          uptime
capture    01-identity timedatectl     timedatectl
capture    01-identity localectl       localectl

# ---------------------------------------------------------------------------
# 02-hardware — physical inventory + firmware
# ---------------------------------------------------------------------------
capture    02-hardware lscpu              lscpu
capture    02-hardware lspci-verbose      lspci -vnnk
capture    02-hardware lsusb-tree         lsusb -tv
capture    02-hardware lsblk              lsblk -fO
capture    02-hardware lsmod              lsmod
capture    02-hardware dmidecode-system   dmidecode -t system
capture    02-hardware dmidecode-bios     dmidecode -t bios
capture    02-hardware dmidecode-memory   dmidecode -t memory
capture    02-hardware fwupdmgr-devices   fwupdmgr get-devices
capture    02-hardware fwupdmgr-updates   fwupdmgr get-updates
capture    02-hardware lshw-short         lshw -short
capture    02-hardware inxi               inxi -Fxxxz --no-host
capture    02-hardware sensors            sensors

# ---------------------------------------------------------------------------
# 03-boot — performance + chain
# ---------------------------------------------------------------------------
capture    03-boot systemd-analyze                systemd-analyze
capture    03-boot systemd-analyze-blame          systemd-analyze blame
capture    03-boot systemd-analyze-critical-chain systemd-analyze critical-chain
capture    03-boot systemd-analyze-time           systemd-analyze time
capture_sh 03-boot systemd-analyze-plot           "systemd-analyze plot > '$OUT_DIR/03-boot/systemd-analyze-plot.svg' && echo '(SVG timeline written to systemd-analyze-plot.svg)'"

# ---------------------------------------------------------------------------
# 04-systemd — unit state (system + user)
# ---------------------------------------------------------------------------
capture      04-systemd failed-units            systemctl --failed --no-pager
capture      04-systemd all-units               systemctl list-units --no-pager
capture      04-systemd enabled-unit-files      systemctl list-unit-files --state=enabled --no-pager
capture      04-systemd masked-unit-files       systemctl list-unit-files --state=masked --no-pager
capture      04-systemd jobs                    systemctl list-jobs --no-pager
capture_user 04-systemd user-failed-units       systemctl --user --failed --no-pager
capture_user 04-systemd user-all-units          systemctl --user list-units --no-pager
capture_user 04-systemd user-enabled-unit-files systemctl --user list-unit-files --state=enabled --no-pager

# ---------------------------------------------------------------------------
# 05-logs — journal + dmesg + audit
# ---------------------------------------------------------------------------
capture      05-logs dmesg-full               journalctl --dmesg -b --no-pager -o short-iso
capture      05-logs dmesg-errors             dmesg --level=err,warn,crit,alert,emerg
capture      05-logs journal-current-boot     journalctl -b --no-pager -o short-iso
capture      05-logs journal-current-errors   journalctl -b -p err --no-pager -o short-iso
capture      05-logs journal-current-warnings journalctl -b -p warning --no-pager -o short-iso
capture      05-logs journal-previous-boot    journalctl -b -1 --no-pager -o short-iso
capture      05-logs journal-previous-errors  journalctl -b -1 -p err --no-pager -o short-iso
capture_user 05-logs journal-user-boot        journalctl --user -b --no-pager -o short-iso
capture      05-logs coredumps                coredumpctl list --no-pager
capture      05-logs selinux-denials          ausearch -m AVC,USER_AVC -ts boot
capture      05-logs audit-anomaly            ausearch -m ANOM_ABEND,ANOM_PROMISCUOUS -ts boot

# ---------------------------------------------------------------------------
# 06-network
# ---------------------------------------------------------------------------
capture    06-network ip-addr             ip -d addr
capture    06-network ip-route            ip -d route
capture    06-network ip-link             ip -d link
capture    06-network nmcli-status        nmcli device status
capture    06-network nmcli-connections   nmcli connection show
capture    06-network nmcli-radio         nmcli radio
capture    06-network resolvectl-status   resolvectl status
capture    06-network resolvectl-dns      resolvectl dns
capture    06-network firewall-active     firewall-cmd --get-active-zones
capture    06-network firewall-listall    firewall-cmd --list-all
capture    06-network listening-sockets   ss -tulnp
capture_sh 06-network ping-default-gw     'GW=$(ip -4 route | awk "/^default/ {print \$3; exit}"); echo "default v4 gateway: $GW"; if [[ -n "$GW" ]]; then ping -c 3 -W 2 "$GW"; else echo "(no default v4 route found)"; fi'

# ---------------------------------------------------------------------------
# 07-storage
# ---------------------------------------------------------------------------
capture    07-storage df              df -hT
capture    07-storage findmnt         findmnt
capture    07-storage swap            swapon --show
capture    07-storage fstab           cat /etc/fstab
capture    07-storage zramctl         zramctl
capture_sh 07-storage btrfs-info      'if command -v btrfs >/dev/null; then for m in $(findmnt -no TARGET -t btrfs); do echo "=== $m ==="; btrfs filesystem df "$m"; echo; btrfs filesystem usage "$m"; echo; done; else echo "(btrfs userland not installed)"; fi'
capture_sh 07-storage smart-summary   'if command -v smartctl >/dev/null; then for d in /dev/nvme?n? /dev/sd?; do if [[ -b "$d" ]]; then echo "=== $d ==="; smartctl -H -i "$d"; echo; fi; done; else echo "(smartctl not installed)"; fi'

# ---------------------------------------------------------------------------
# 08-security — SELinux + audit
# ---------------------------------------------------------------------------
capture    08-security sestatus       sestatus
capture    08-security getenforce     getenforce
capture    08-security semodule-list  semodule -l
capture    08-security getsebool      getsebool -a

# ---------------------------------------------------------------------------
# 09-packages — dnf, flatpak, rpm
# ---------------------------------------------------------------------------
capture    09-packages dnf-history       dnf history list
capture    09-packages dnf-repolist      dnf repolist --all
capture    09-packages dnf-check         dnf check
capture    09-packages dnf-upgradable    dnf check-upgrade
capture    09-packages flatpak-list      flatpak list
capture    09-packages flatpak-remotes   flatpak remotes -d
capture_sh 09-packages rpm-count         'echo "Installed RPM count: $(rpm -qa | wc -l)"'
capture_sh 09-packages largest-rpms      "rpm -qa --queryformat '%{SIZE}\t%{NAME}\n' | sort -rn | head -100"
capture    09-packages snap-list         snap list

# ---------------------------------------------------------------------------
# 10-display-audio — GPU, screen, sound
# ---------------------------------------------------------------------------
capture_sh   10-display-audio gpu-pci       'lspci -nnk | grep -A 3 -E -i "vga|3d|display"'
capture_sh   10-display-audio gpu-modules   'grep -E "^(nvidia|nouveau|amdgpu|radeon|i915|xe) " /proc/modules'
capture_user 10-display-audio glxinfo       glxinfo -B
capture_user 10-display-audio vainfo        vainfo
capture_user 10-display-audio vdpauinfo     vdpauinfo
capture_user 10-display-audio xdpyinfo      xdpyinfo
capture_user_sh 10-display-audio display-env   'env | grep -E "^(XDG_|WAYLAND_|DISPLAY|XAUTHORITY|GDK_|QT_|MUTTER_|GTK_)" | sort'
capture_user 10-display-audio pactl-info    pactl info
capture_user_sh 10-display-audio pactl-sinks   'pactl list short sinks'
capture_user_sh 10-display-audio pactl-sources 'pactl list short sources'
capture_user 10-display-audio wpctl-status  wpctl status
capture      10-display-audio aplay-l       aplay -l
capture      10-display-audio asound-cards  cat /proc/asound/cards
capture      10-display-audio asound-modules cat /proc/asound/modules

# ---------------------------------------------------------------------------
# 11-power — battery, thermals, profiles
# ---------------------------------------------------------------------------
capture    11-power power-profiles-list  powerprofilesctl list
capture    11-power power-profile-get    powerprofilesctl get
capture    11-power upower-dump          upower -d
capture_sh 11-power battery               'for b in /sys/class/power_supply/BAT*; do if [[ -d "$b" ]]; then echo "=== $b ==="; for f in status capacity energy_full energy_full_design energy_now power_now cycle_count manufacturer model_name; do if [[ -r "$b/$f" ]]; then printf "%-22s %s\n" "$f" "$(cat "$b/$f")"; fi; done; fi; done'
capture    11-power tlp-stat              tlp-stat -s
capture_sh 11-power cpu-frequency         'for f in scaling_driver scaling_governor scaling_min_freq scaling_max_freq; do p=/sys/devices/system/cpu/cpu0/cpufreq/$f; if [[ -r "$p" ]]; then printf "%-22s %s\n" "$f" "$(cat "$p")"; else printf "%-22s (n/a)\n" "$f"; fi; done'
capture_sh 11-power thermal-zones         'for z in /sys/class/thermal/thermal_zone*; do if [[ -d "$z" ]] && [[ -r "$z/temp" ]]; then printf "%-30s type=%-30s temp=%s\n" "$(basename "$z")" "$(cat "$z/type")" "$(cat "$z/temp")"; fi; done'

# ---------------------------------------------------------------------------
# 12-gnome — desktop session
# ---------------------------------------------------------------------------
capture_user 12-gnome shell-version          gnome-shell --version
capture_user 12-gnome extensions-enabled     gnome-extensions list --enabled
capture_user 12-gnome extensions-all         gnome-extensions list
capture_user 12-gnome gsettings-shell        gsettings list-recursively org.gnome.shell
capture_user 12-gnome gsettings-mutter       gsettings list-recursively org.gnome.mutter
capture_user 12-gnome gsettings-keybindings  gsettings list-recursively org.gnome.desktop.wm.keybindings
capture_user 12-gnome gsettings-input        gsettings list-recursively org.gnome.desktop.input-sources
capture      12-gnome loginctl-sessions      loginctl list-sessions --no-pager

# ---------------------------------------------------------------------------
# 13-process-state — what is running right now
# ---------------------------------------------------------------------------
capture    13-process-state ps-tree       ps auxf
capture    13-process-state top           top -bn1 -w512
capture    13-process-state loginctl      loginctl
capture    13-process-state who           who -a
capture    13-process-state systemd-cgls  systemd-cgls --no-pager
capture    13-process-state systemd-cgtop systemd-cgtop -b -n 1

# ---------------------------------------------------------------------------
# README.md — index, summary, agent guidance
# ---------------------------------------------------------------------------
generate_readme() {
    local readme="$OUT_DIR/README.md"
    local total=0 failed=0
    while IFS=$'\t' read -r col1 col2 col3; do
        if [[ "$col1" == "subdir" ]]; then continue; fi
        total=$((total + 1))
        if [[ "$col3" != "0" ]]; then
            failed=$((failed + 1))
        fi
    done < "$MANIFEST"

    local pretty_name="unknown"
    if [[ -r /etc/os-release ]]; then
        while IFS='=' read -r key val; do
            if [[ "$key" == "PRETTY_NAME" ]]; then
                pretty_name="${val//\"/}"
                break
            fi
        done < /etc/os-release
    fi

    {
        echo "# Host Diagnostic Snapshot"
        echo
        echo "| Field    | Value |"
        echo "|----------|-------|"
        echo "| Captured | \`$(date -Iseconds)\` |"
        echo "| Host     | \`$(hostname)\` |"
        echo "| Kernel   | \`$(uname -r)\` |"
        echo "| OS       | $pretty_name |"
        echo "| User     | \`$USER_LOGIN\` (uid $USER_UID) |"
        echo "| Captures | $total total, $failed non-zero exit |"
        echo
        echo "## Agent guidance"
        echo
        echo "This directory contains a comprehensive diagnostic snapshot of a Fedora"
        echo "desktop configured by the parent repository. Each \`.txt\` file under the"
        echo "numbered subdirectories is the output of a single command, with the command"
        echo "line, timestamp and exit code recorded inline. Use this snapshot to surface:"
        echo
        echo "- failed or crashed services — \`04-systemd/failed-units.txt\`,"
        echo "  \`04-systemd/user-failed-units.txt\`"
        echo "- boot errors and warnings — \`05-logs/journal-current-errors.txt\`,"
        echo "  \`05-logs/journal-current-warnings.txt\`, \`05-logs/dmesg-errors.txt\`"
        echo "- slow boot units — \`03-boot/systemd-analyze-blame.txt\`,"
        echo "  \`03-boot/systemd-analyze-critical-chain.txt\`"
        echo "- SELinux denials — \`05-logs/selinux-denials.txt\`"
        echo "- coredumps — \`05-logs/coredumps.txt\`"
        echo "- hardware without a driver — \`02-hardware/lspci-verbose.txt\`,"
        echo "  \`02-hardware/lsmod.txt\`"
        echo "- pending firmware updates — \`02-hardware/fwupdmgr-updates.txt\`"
        echo "- broken package dependencies — \`09-packages/dnf-check.txt\`"
        echo "- pending package upgrades — \`09-packages/dnf-upgradable.txt\`"
        echo "- DNS / resolver issues — \`06-network/resolvectl-status.txt\`"
        echo "- audio / pipewire problems — \`10-display-audio/wpctl-status.txt\`,"
        echo "  \`10-display-audio/pactl-info.txt\`"
        echo "- GNOME extension state — \`12-gnome/extensions-enabled.txt\`,"
        echo "  \`12-gnome/extensions-all.txt\`"
        echo
        echo "Any finding should become a concrete change to the Ansible playbooks under"
        echo "\`playbooks/imports/\`. Per the project's Infrastructure-as-Code rule, never"
        echo "apply manual fixes to the host — always update the playbook first."
        echo
        echo "## Captures with non-zero exit"
        echo
        echo "A non-zero exit often just means the probe is not applicable on this host"
        echo "(no battery, no btrfs, no Wayland, tool not installed). Inspect the file"
        echo "to see why before treating it as a defect."
        echo
        echo '```'
        while IFS=$'\t' read -r col1 col2 col3; do
            if [[ "$col1" == "subdir" ]]; then continue; fi
            if [[ "$col3" != "0" ]]; then
                printf "  rc=%-3s %s/%s\n" "$col3" "$col1" "$col2"
            fi
        done < "$MANIFEST"
        echo '```'
        echo
        echo "Full manifest: \`_manifest.tsv\` (TSV: subdir, name, exit_code)."
        echo
        echo "## Directory map"
        echo
        for d in "$OUT_DIR"/*/; do
            if [[ ! -d "$d" ]]; then continue; fi
            local base
            base="$(basename "$d")"
            echo "### $base"
            echo
            for f in "$d"*.txt "$d"*.svg; do
                if [[ -e "$f" ]]; then
                    echo "- \`$base/$(basename "$f")\`"
                fi
            done
            echo
        done
    } > "$readme"
    echo "==> Wrote $readme" >&2
}

generate_readme

# Hand the whole tree back to the invoking user so they can read / commit /
# share it without sudo.
chown -R "$USER_UID:$USER_GID" "$OUT_DIR"

echo "" >&2
echo "==> Done. Snapshot at: $OUT_DIR" >&2
echo "==> Start with:        $OUT_DIR/README.md" >&2
