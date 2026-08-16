#!/usr/bin/env bash
#
# Plan 00072 — establish why rclone RC clients stopped working.
#
# READ-ONLY. Starts nothing, stops nothing, writes nothing outside its own
# log. Safe to re-run on a live system, mid-incident, as many times as needed.
#
# Writes its report to <plan folder>/logs/rclone-rc-clients-triage.log.
# That directory is gitignored — these dumps contain live host state and this
# is a public repo. NO CREDENTIAL VALUE is ever written: the RC password is
# reported by LENGTH only, and the authenticated probes never echo their argv.
#
# Usage: triage.bash [--help]

set -euo pipefail

# --- argument parsing FIRST, before any environment resolution ---------------
for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00072 — triage the rclone remote-control clients

Usage: triage.bash [--help]

Gathers, without changing anything:
  * whether the RC answers WITHOUT credentials, and WITH them
  * whether each repo-owned rclone/ftp helper is byte-identical to its
    deployed copy under ~/.local/bin/
  * which helpers pass credentials to `rclone rc` and which do not
  * mount unit health and VFS cache state

Renders NO verdict — that is acceptance.bash's job. Report is written to
  <plan folder>/logs/rclone-rc-clients-triage.log
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: triage.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORTS_DIR="$PLAN_DIR/logs"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/rclone-rc-clients-triage.log"
exec > >(tee "$LOG") 2>&1

REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"
BIN_SRC="$REPO_ROOT/files/home/.local/bin"
BIN_DEPLOYED="$HOME/.local/bin"
RC_AUTH_FILE="$HOME/.config/rclone/rc-auth.env"
RC_ADDR="localhost:5572"

# A non-zero exit is DATA, not a failure. Capture it and carry on.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

# grep -c exits 1 on zero matches, which is a legitimate count, not an error.
# Distinguishing "no matches" from "grep failed" explicitly beats collapsing
# both into a count of zero.
count_matches() {
    local pattern="$1" file="$2" n rc
    if n=$(grep -c -- "$pattern" "$file"); then
        rc=0
    else
        rc=$?
    fi
    case "$rc" in
        0) printf '%s' "$n" ;;
        1) printf '0' ;;
        *) printf 'grep-error' ;;
    esac
}

# The helpers this plan cares about: every repo-owned script that talks to a
# mount. Enumerated by glob, not hand-listed, so a new one is picked up.
helper_sources() {
    local src
    for src in "$BIN_SRC"/rclone-* "$BIN_SRC"/ftp-camera; do
        if [ -f "$src" ]; then
            printf '%s\n' "$src"
        fi
    done
}

echo "=============================================================="
echo "Plan 00072 triage — rclone RC clients"
echo "=============================================================="
echo

# --- the RC credential, by shape only ----------------------------------------
show_rc_credential_shape() {
    if [ ! -r "$RC_AUTH_FILE" ]; then
        echo "$RC_AUTH_FILE: NOT READABLE (or absent)"
        return 0
    fi
    ls -l "$RC_AUTH_FILE"
    echo "-- keys and value LENGTHS (values deliberately never printed) --"
    awk -F= '{ printf "%s=<%d chars>\n", $1, length($2) }' "$RC_AUTH_FILE"
}
probe "RC credential file shape" show_rc_credential_shape

# --- does the RC demand authentication? --------------------------------------
# The decisive pair. READ THIS FOR: whether the endpoints the helper scripts
# poll (core/stats, vfs/stats) require a credential. Plan 00067 assumed not.
probe "core/stats WITHOUT credentials" \
    rclone rc --url="http://${RC_ADDR}" core/stats

probe "vfs/stats WITHOUT credentials" \
    rclone rc --url="http://${RC_ADDR}" vfs/stats

# Authenticated probes run through a helper so the password never reaches the
# report via an echoed argv.
rc_authed() {
    local endpoint="$1"
    local u p
    if [ ! -r "$RC_AUTH_FILE" ]; then
        echo "(no credential file — cannot probe authenticated)"
        return 1
    fi
    u=$(awk -F= '$1 == "RCLONE_RC_USER" { print $2 }' "$RC_AUTH_FILE")
    p=$(awk -F= '$1 == "RCLONE_RC_PASS" { print $2 }' "$RC_AUTH_FILE")
    if [ -z "$u" ] || [ -z "$p" ]; then
        echo "(credential file missing RCLONE_RC_USER or RCLONE_RC_PASS)"
        return 1
    fi
    # Credentials via the environment, not argv: `--pass <secret>` is visible in
    # `ps` output to every user on the box for the life of the call. rclone maps
    # its client --user/--pass flags to RCLONE_USER/RCLONE_PASS.
    RCLONE_USER="$u" RCLONE_PASS="$p" rclone rc --url="http://${RC_ADDR}" "$endpoint"
}
probe "core/stats WITH credentials" rc_authed core/stats
probe "vfs/stats WITH credentials" rc_authed vfs/stats

# --- which helpers authenticate, and which are drifted? ----------------------
# READ THIS FOR: a helper with RC-CALLS > 0 and AUTH-REFS = 0 cannot talk to an
# authenticated RC at all. DEPLOYED=DRIFTED is a repo fix that never shipped.
show_helper_matrix() {
    local src f dep calls auth state
    printf '%-24s %-12s %-10s %-10s\n' HELPER DEPLOYED "RC-CALLS" "AUTH-REFS"
    while IFS= read -r src; do
        f="$(basename "$src")"
        dep="$BIN_DEPLOYED/$f"
        calls=$(count_matches 'rclone rc ' "$src")
        auth=$(count_matches 'RCLONE_RC_USER\|rclone_rc\b' "$src")
        if [ ! -f "$dep" ]; then
            state="ABSENT"
        elif cmp -s "$src" "$dep"; then
            state="in-sync"
        else
            state="DRIFTED"
        fi
        printf '%-24s %-12s %-10s %-10s\n' "$f" "$state" "$calls" "$auth"
    done < <(helper_sources)
}
probe "helper matrix (deployment drift + RC auth awareness)" show_helper_matrix

show_drift_detail() {
    local src f dep
    while IFS= read -r src; do
        f="$(basename "$src")"
        dep="$BIN_DEPLOYED/$f"
        if [ ! -f "$dep" ]; then
            continue
        fi
        if cmp -s "$src" "$dep"; then
            continue
        fi
        echo "== $f: deployed differs from repo =="
        stat -c '  %n  %s bytes  mtime=%y' "$dep" "$src"
        echo "  --- diff (deployed -> repo) ---"
        # diff exits 1 when the files differ, which is the expected case here.
        if diff -u "$dep" "$src"; then
            echo "  (no differences — cmp and diff disagree, investigate)"
        fi
        echo
    done < <(helper_sources)
}
probe "drift detail" show_drift_detail

probe "every 'rclone rc' call site in the repo" \
    grep -rn 'rclone rc ' "$BIN_SRC" "$REPO_ROOT/scripts" "$REPO_ROOT/playbooks"

# --- do the helpers actually work right now? ---------------------------------
run_deployed_cache_status() {
    if [ ! -x "$BIN_DEPLOYED/rclone-cache-status" ]; then
        echo "NOT DEPLOYED"
        return 0
    fi
    timeout 60 "$BIN_DEPLOYED/rclone-cache-status"
}
probe "rclone-cache-status (deployed) live run" run_deployed_cache_status

# --- mount health ------------------------------------------------------------
probe "rclone mounts (findmnt)" findmnt -n -o TARGET,SOURCE,FSTYPE -t fuse.rclone

list_rclone_units() {
    systemctl --user list-units --type=service --all --no-legend --plain \
        | grep -o 'rclone-[a-z0-9-]*\.service' | sort -u
}

show_mount_units() {
    local unit
    while IFS= read -r unit; do
        if [ -z "$unit" ]; then
            continue
        fi
        echo "== $unit =="
        # Filter any line mentioning a password before it reaches the report.
        systemctl --user status "$unit" --no-pager -l | grep -v -i 'pass'
        echo
    done < <(list_rclone_units)
}
probe "rclone mount units" show_mount_units

probe "ftp-camera config" cat /etc/ftp-camera/config
probe "upload dir" ls -la /srv/ftp-camera
probe "disk usage" df -h "$HOME" /srv

echo "=============================================================="
echo "READ THIS FIRST:"
echo "  1. 'core/stats WITHOUT credentials' — a 401 there means every"
echo "     un-migrated helper is dead, whatever else looks healthy."
echo "  2. 'helper matrix' — RC-CALLS > 0 with AUTH-REFS = 0 is a broken"
echo "     helper; DEPLOYED=DRIFTED is a repo fix that never shipped."
echo
echo "Full report: $LOG"
echo "=============================================================="
