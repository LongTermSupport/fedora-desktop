#!/usr/bin/env bash
set -euo pipefail

# triage.bash — FACT-FINDING for the ftp-camera retry loop + hotspot gap, Plan 00066.
#
# Scope: establish grounded facts. It gathers and logs state; it renders NO
# pass/fail verdict — confirming a fix worked is a separate gate
# (acceptance.bash), run post-fix.
#
# Run this on the HOST (never in the CCY container — the container has no
# vsftpd, no NetworkManager, and no camera on its network).
#
# It changes NOTHING: no service is started or stopped, no file is moved or
# deleted, no firewall or NM state is touched. Safe to re-run as often as you
# like. Its whole job is to print + log a report, so its stdout IS the payload
# (the CLAUDE/StderrHygiene.md exception for report commands).
#
#   CLAUDE/Plan/00066-ftp-camera-airbnb-wifi-and-hotspot-triage/triage.bash
#
# Pattern: CLAUDE/PlanWorkflow.md → "Plan-Local Scripts & Artifacts".
#
# It WRITES ITS OWN REPORT to untracked/reports/ (gitignored scratch inside the
# repo tree). That directory is bind-mounted into the CCY container, so the
# agent assisting on this plan reads the report directly at the same
# repo-relative path — no copy-paste of terminal output required.
#
# Best run RIGHT AFTER a failing camera session, while the vsftpd log still
# holds the evidence.
#
# It needs sudo for /var/log/vsftpd.log and for reading inside the upload tree.

REPO_ROOT="$(git rev-parse --show-toplevel)"

VSFTPD_LOG="/var/log/vsftpd.log"
CONFIG_FILE="/etc/ftp-camera/config"

# ── Args ─────────────────────────────────────────────────────────────────────
# Default is the passive report. --capture additionally records the FTP
# control channel while a live ftp-camera session runs, because the verb
# stream is the only thing that distinguishes the remaining hypotheses and no
# amount of after-the-fact log reading substitutes for it.
CAPTURE_SECONDS=0

usage() {
    cat <<'EOF'
Usage: triage.bash [--capture [SECONDS]]

  (no args)            Passive report only. Read-only, changes nothing.
  --capture [SECONDS]  ALSO record the FTP control channel for SECONDS
                       (default 180) before producing the report.

--capture requires a live ftp-camera session — start one in another
terminal first, then run this. It fails fast if vsftpd is not running,
because a capture with no traffic in it is worse than no capture (it
looks like evidence of absence).

The FTP password is filtered out of the trace before anything is written
to disk.
EOF
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --capture)
            CAPTURE_SECONDS=180
            # Optional numeric argument. Anything non-numeric is the next
            # flag (or a typo), so leave it for the loop to handle.
            if [ "${2:-}" ] && [ -z "${2//[0-9]/}" ]; then
                CAPTURE_SECONDS="$2"
                shift
            fi
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $1" >&2
            echo "Run 'triage.bash --help' for usage" >&2
            exit 2
            ;;
    esac
    shift
done

# Resolved AFTER arg parsing so --help works on a machine that has never run
# play-ftp-camera.yml.
#
# `getent passwd camera` exits 2 when the key is absent. Under `set -e` with
# pipefail, doing this as a bare pipeline assignment killed the script at that
# line with rc=2 and printed nothing — so the friendly error below was
# unreachable on exactly the machines that needed it. Probe, then check.
UPLOAD_DIR=""
if _passwd_line="$(getent passwd camera)"; then
    UPLOAD_DIR="$(printf '%s' "$_passwd_line" | cut -d: -f6)"
fi
if [ -z "$UPLOAD_DIR" ]; then
    echo "ERROR: 'camera' user not found — run play-ftp-camera.yml first" >&2
    echo "  ansible-playbook playbooks/imports/optional/common/play-ftp-camera.yml" >&2
    exit 1
fi

REPORTS_DIR="$REPO_ROOT/untracked/reports"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/ftp-camera-triage.log"
exec > >(tee "$LOG") 2>&1

# Combined-output capture without hiding errors: prints the command's rc and
# its stdout+stderr. A non-zero rc is DATA here, not a failure — so we record
# it and carry on, never abort.
probe() {
    local label="$1"; shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

# Probe helpers. These exist as functions rather than `bash -c '...'` strings so
# the quoting stays readable and shellcheck can actually lint the bodies.

# Per-interface wifi link detail: signal strength, negotiated bitrate, channel
# width. Establishes whether the radio link is genuinely weak (which would
# support a network explanation) or healthy (which would not).
show_wifi_links() {
    local dev
    for dev in $(iw dev | awk '/Interface/ {print $2}'); do
        echo "== $dev"
        iw dev "$dev" link
    done
}

# Does this radio advertise AP mode at all? Determines whether a hotspot
# profile can work on this hardware before we build one.
show_ap_capability() {
    iw list | grep -A 12 'Supported interface modes'
}

# Recent SELinux denials. A denial against vsftpd would be a first-class
# suspect for silently-broken data connections.
show_selinux_denials() {
    if ! command -v ausearch > /dev/null; then
        echo "(ausearch not installed — audit denials unavailable)"
        return 0
    fi
    # No match is DATA (it means "no denials"), not a failure — report it.
    if ! sudo ausearch -m avc -ts recent -i; then
        echo "(no AVC denials recorded in the recent window)"
    fi
}

# Any wifi profile configured as an access point, under ANY name. Catches the
# case where the hotspot still exists but was renamed, which would look
# identical to "deleted" from ftp-camera's point of view (it matches on name).
show_ap_profiles_on_disk() {
    local store="/etc/NetworkManager/system-connections"
    if ! sudo test -d "$store"; then
        echo "(no $store on this machine)"
        return 0
    fi
    # grep rc=1 means "no AP profiles", which is DATA, not a failure.
    if ! sudo grep -l 'mode=ap' "$store"/* ; then
        echo "(no profile on disk is configured as an access point)"
    fi
}

# NetworkManager logs profile add/remove. The journal may have rotated past
# the event, in which case this is silent — absence here is NOT evidence the
# deletion did not happen.
show_nm_profile_events() {
    if ! sudo journalctl -u NetworkManager --no-pager --since "30 days ago" \
        --grep 'ifcfg-rh|keyfile|connection (added|removed|updated)|Hotspot'; then
        echo "(no matching NetworkManager profile events in the retained journal;"
        echo " the journal may simply have rotated past them)"
    fi
}

# Record the FTP control channel while a live camera session runs.
#
# Port 21 is plaintext, so a packet capture yields the same verb stream that
# vsftpd's log_ftp_protocol=YES gives — without needing the wrapper redeployed
# first. This is the ONLY probe that separates the remaining hypotheses, all
# of which produce an identical upload-side log:
#
#   camera sends SIZE/LIST/MLSD after STOR and is answered 550
#       -> the sort moved the file out from under it
#   session goes silent after STOR, no 226 ever delivered
#       -> the control connection died mid-transfer
#   camera closes at a consistent interval regardless of progress
#       -> client-side timeout
#
# Read-only with respect to system state: captures packets, writes one text
# file under untracked/. Starts and stops nothing.
capture_ftp_control() {
    local seconds="$1"
    local trace="$REPORTS_DIR/ftp-control-trace.txt"

    echo "──────── 0. FTP CONTROL-CHANNEL CAPTURE ────────"
    echo

    if ! command -v tcpdump > /dev/null; then
        echo "ERROR: tcpdump is not installed." >&2
        echo "  It is declared in play-ftp-camera.yml. Deploy it with:" >&2
        echo "    ansible-playbook playbooks/imports/optional/common/play-ftp-camera.yml" >&2
        echo "  Do NOT install it by hand — the host's tool inventory is owned" >&2
        echo "  by Ansible (CLAUDE.md, 'Missing Dependencies')." >&2
        return 1
    fi

    # A capture containing no traffic reads as evidence of absence, which is
    # worse than no capture. Refuse rather than write a misleading empty file.
    if ! systemctl is-active --quiet vsftpd; then
        echo "ERROR: vsftpd is not running — there is no FTP traffic to capture." >&2
        echo "" >&2
        echo "  Start a camera session in ANOTHER terminal first:" >&2
        echo "    ftp-camera --no-tui" >&2
        echo "  wait until the camera is transferring, then re-run:" >&2
        echo "    $0 --capture" >&2
        return 1
    fi

    echo "Capturing the FTP control channel for ${seconds}s."
    echo "Leave the camera transferring while this runs — one or two upload"
    echo "cycles is enough to be conclusive."
    echo

    # The PASS verb carries the FTP password in cleartext. It is filtered
    # BEFORE anything reaches disk, so the trace never holds the credential,
    # even transiently.
    local rc=0
    if ! sudo timeout "$seconds" \
            tcpdump -i any -nn -A -s0 'tcp port 21' 2>&1 \
            | grep -v '^PASS ' > "$trace"; then
        rc=$?
    fi

    # timeout exits 124 when the window elapses — that is the SUCCESS path
    # here. grep exits 1 when it wrote nothing, which is handled by the
    # emptiness check in analyse_ftp_trace. Anything else is worth reporting.
    if [ "$rc" -ne 0 ] && [ "$rc" -ne 124 ] && [ "$rc" -ne 1 ]; then
        echo "  NOTE: capture pipeline exited rc=$rc — trace may be incomplete" >&2
    fi

    echo "Capture written to: $trace"
    echo
}

# Extract the verb/response stream from a capture, in order. This is the
# payload of the whole exercise — everything else in this report is context.
analyse_ftp_trace() {
    local trace="$REPORTS_DIR/ftp-control-trace.txt"
    local verbs='(USER|TYPE|PASV|EPSV|PORT|STOR|RETR|LIST|MLSD|NLST|SIZE|MDTM|DELE|RNFR|RNTO|CWD|PWD|QUIT|FEAT|SYST|NOOP|ABOR|REST|APPE)'

    if [ ! -s "$trace" ]; then
        echo "  (no capture present — re-run this script with --capture while"
        echo "   an ftp-camera session is live)"
        return 0
    fi

    echo "### FTP verbs and responses, in order"
    echo "###   READ THIS FOR: what the camera does after each STOR."
    echo "###     SIZE/LIST/MLSD answered 550 -> the sort moved the file away"
    echo "###     nothing at all, no 226      -> control connection died"
    echo "###     consistent cutoff interval  -> camera-side timeout"
    grep -aoE "${verbs}[^\r]*|[1-5][0-9][0-9][ -][^\r]*" "$trace"
    echo

    echo "### verb frequency (a STOR count far above the file count = re-sending)"
    grep -aoE "${verbs}" "$trace" | sort | uniq -c | sort -rn
    echo
}

show_firewall_openings() {
    echo "services:"
    sudo firewall-cmd --list-services
    echo "ports:"
    sudo firewall-cmd --list-ports
}

show_exiftool_version() {
    if ! command -v exiftool > /dev/null; then
        echo "(exiftool NOT installed — sorting cannot read EXIF dates)"
        return 0
    fi
    command -v exiftool
    exiftool -ver
}

echo "════════════════════════════════════════════════════════════"
echo " Plan 00066 — ftp-camera triage"
echo " repo:   $REPO_ROOT"
echo " user:   $(id -un) (uid $(id -u))   host: $(uname -n)"
echo " upload: $UPLOAD_DIR"
echo "════════════════════════════════════════════════════════════"
echo

# ── Section 0: live capture (only with --capture) ────────────────────────────
# Runs first so the rest of the report describes the state the capture was
# taken in. A capture failure is fatal: the user explicitly asked for one, and
# silently producing the passive report instead would look like it worked.
if [ "$CAPTURE_SECONDS" -gt 0 ]; then
    capture_ftp_control "$CAPTURE_SECONDS"
fi

# ── Section 1: network state ─────────────────────────────────────────────────
# Establishes which IP ftp-camera would advertise as pasv_address and whether
# the camera's subnet is actually reachable. F2/F6 context.
echo "──────── 1. NETWORK ────────"
echo

probe "IPv4 addresses" ip -4 addr show
probe "default route" ip -4 route show default
probe "wifi link quality (signal, bitrate, channel width)" show_wifi_links
probe "neighbour table (is the camera ARP-visible?)" ip neigh show

# ── Section 2: NetworkManager / hotspot (F10) ────────────────────────────────
# The --hotspot failure is a missing NM profile. Record what profiles DO exist
# and whether the radio can even do AP mode, so the fix targets reality.
echo "──────── 2. NETWORKMANAGER / HOTSPOT ────────"
echo

probe "all NM connection profiles" nmcli -t -f NAME,TYPE,DEVICE connection show
probe "active NM connections" nmcli -t -f NAME,TYPE,DEVICE connection show --active
probe "wifi devices and their state" nmcli -t -f DEVICE,TYPE,STATE device

# The owner reports --hotspot working previously, so the profile is not simply
# absent-since-forever: something removed it. These probes look for the
# residue. This matters beyond curiosity — if a process is actively deleting
# the profile, declaring it in Ansible fixes today and not tomorrow.
probe "on-disk NM connection store (timestamps show when profiles were written)" \
    sudo ls -la --time-style=long-iso /etc/NetworkManager/system-connections/
probe "any AP-mode profile on disk, whatever it is named" show_ap_profiles_on_disk
probe "NetworkManager add/delete events still in the journal" show_nm_profile_events
probe "does the radio advertise AP mode?" show_ap_capability
probe "regulatory domain (governs 40MHz + AP concurrency)" iw reg get
probe "ftp-camera config (which profile name --hotspot expects)" cat "$CONFIG_FILE"

# ── Section 3: vsftpd service + runtime config ───────────────────────────────
echo "──────── 3. VSFTPD ────────"
echo

probe "vsftpd service state" systemctl status vsftpd --no-pager -l
probe "deployed config /etc/vsftpd/vsftpd.conf" sudo cat /etc/vsftpd/vsftpd.conf
probe "runtime config /run/vsftpd-camera.conf (pasv_address lives here)" \
    sudo cat /run/vsftpd-camera.conf
probe "systemd override" sudo cat /run/systemd/system/vsftpd.service.d/camera-ftp.conf
probe "firewalld active zones" sudo firewall-cmd --get-active-zones
probe "firewalld default-zone services/ports" show_firewall_openings
probe "SELinux booleans for ftpd" getsebool -a
probe "SELinux denials in the recent window" show_selinux_denials

# ── Section 4: THE DECISIVE ONE — per-file upload counts (F4/F5) ─────────────
# H1 predicts each frame is uploaded MANY times, all logged OK. A weak-network
# explanation predicts FAIL UPLOAD entries. This section separates them.
echo "──────── 4. VSFTPD LOG ANALYSIS ────────"
echo

if ! sudo test -r "$VSFTPD_LOG"; then
    echo "### vsftpd log not readable at $VSFTPD_LOG — nothing to analyse"
    echo
else
    probe "log size / mtime" sudo stat -c '%s bytes, modified %y' "$VSFTPD_LOG"

    echo "### event tallies across the whole log"
    sudo awk '
        /OK UPLOAD/    { ok_up++ }
        /FAIL UPLOAD/  { fail_up++ }
        /OK LOGIN/     { ok_login++ }
        /FAIL LOGIN/   { fail_login++ }
        /CONNECT/      { conn++ }
        END {
            printf "  CONNECT:      %d\n", conn+0
            printf "  OK LOGIN:     %d\n", ok_login+0
            printf "  FAIL LOGIN:   %d\n", fail_login+0
            printf "  OK UPLOAD:    %d\n", ok_up+0
            printf "  FAIL UPLOAD:  %d\n", fail_up+0
        }' "$VSFTPD_LOG"
    echo

    # The money shot. Count = how many times the camera successfully stored
    # the SAME path. 1 per frame is healthy. >1 is the retry loop.
    echo "### successful uploads per filename  (count | filename)"
    echo "###   1 per frame = healthy;  >1 = the camera is re-sending"
    sudo awk -F'"' '/OK UPLOAD: Client/ {print $4}' "$VSFTPD_LOG" \
        | sort | uniq -c | sort -rn
    echo

    # A 0-byte or short "successful" upload explains the no-EXIF skip (F7).
    echo "### byte size of each successful upload (spot 0-byte / truncated stores)"
    sudo awk -F'"' '
        /OK UPLOAD: Client/ {
            name = $4
            n = split($5, parts, ",")
            gsub(/[^0-9]/, "", parts[2])
            printf "  %-28s %s bytes\n", name, (parts[2] == "" ? "?" : parts[2])
        }' "$VSFTPD_LOG"
    echo

    echo "### last 60 raw log lines (full context, newest last)"
    sudo tail -n 60 "$VSFTPD_LOG"
    echo

    # The decisive evidence. Present whenever --capture has been run at least
    # once; summarised here so it lands in the same report as its context.
    echo "──────── 4b. FTP CONTROL-CHANNEL TRACE ────────"
    echo
    analyse_ftp_trace

    # Alternative source for the same verb stream, if the deployed wrapper was
    # run with --debug-ftp instead of using --capture.
    echo "### FTP protocol trace, if --debug-ftp was enabled for a session"
    if sudo grep -q 'FTP command' "$VSFTPD_LOG"; then
        echo "  (protocol logging IS present — commands the camera sent:)"
        echo "  READ THIS FOR: what the camera does immediately after STOR."
        echo "  A SIZE / LIST / MLSD on the just-stored path, answered with a"
        echo "  550/no-such-file, means the camera could not verify its own"
        echo "  upload — which is what makes it re-send the frame."
        # PASS carries the FTP password as its argument; drop it so this report
        # (which is written to a file) can never capture a credential.
        sudo grep -E 'FTP command:|FTP response:' "$VSFTPD_LOG" \
            | grep -v 'FTP command:.*"PASS' \
            | tail -n 150
    else
        echo "  (no protocol trace in this log — re-run a session with"
        echo "   'ftp-camera --async-copy --debug-ftp' to capture one)"
    fi
    echo
fi

# ── Section 5: upload tree state (F7, F9) ────────────────────────────────────
echo "──────── 5. UPLOAD TREE ────────"
echo

probe "upload dir ownership + mode" sudo ls -ld "$UPLOAD_DIR"
probe "upload ROOT contents (unsorted / partial files live here)" \
    sudo ls -la "$UPLOAD_DIR"
probe "POSIX ACLs on the upload dir" sudo getfacl -p "$UPLOAD_DIR"
probe "SELinux context of the upload dir" sudo ls -Zd "$UPLOAD_DIR"
probe "filesystem free space" df -h "$UPLOAD_DIR"

# F9: the Permission denied noise. Identify what owns .cache and what is in it,
# so Phase 4 fixes the cause rather than muting the symptom.
echo "### the .cache entry that trips 'Permission denied' (F9)"
if sudo test -e "$UPLOAD_DIR/.cache"; then
    probe ".cache ownership + mode" sudo ls -ldZ "$UPLOAD_DIR/.cache"
    probe ".cache contents" sudo ls -la "$UPLOAD_DIR/.cache"
    probe ".cache size on disk" sudo du -sh "$UPLOAD_DIR/.cache"
    probe ".cache ACLs" sudo getfacl -p "$UPLOAD_DIR/.cache"
else
    echo "  (no .cache under $UPLOAD_DIR right now)"
fi
echo

# Zero-byte files are the fingerprint of an interrupted or racing STOR (F7/F8).
echo "### zero-byte files anywhere in the upload tree (racing/aborted STOR)"
sudo find "$UPLOAD_DIR" -type f -size 0 -printf '  %p\n'
echo "  (empty list above = none found)"
echo

echo "### 20 most recently modified files in the tree"
sudo find "$UPLOAD_DIR" -type f -printf '%T@ %TY-%Tm-%Td %TH:%TM  %10s  %p\n' \
    | sort -rn | cut -d' ' -f2- | head -n 20
echo

# ── Section 6: toolchain presence ────────────────────────────────────────────
echo "──────── 6. TOOLCHAIN ────────"
echo

probe "exiftool" show_exiftool_version
probe "rclone mounts" findmnt -t fuse.rclone
probe "rclone user services" systemctl --user list-units 'rclone-*' --no-pager

echo "════════════════════════════════════════════════════════════"
echo " Triage complete."
echo " Report written to: $LOG"
echo
echo " Read section 4 first: if 'successful uploads per filename' shows a"
echo " count above 1 while FAIL UPLOAD is 0, the camera is re-sending frames"
echo " that already transferred — a server-side behaviour, not a weak link."
echo "════════════════════════════════════════════════════════════"
