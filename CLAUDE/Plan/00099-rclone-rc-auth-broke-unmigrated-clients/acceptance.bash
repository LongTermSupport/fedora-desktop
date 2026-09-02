#!/usr/bin/env bash
#
# Plan 00099 — acceptance gate. Renders a VERDICT (unlike triage.bash, which
# only reports facts).
#
# Exercises the DEPLOYED scripts, not the repo copies. The whole defect this
# plan fixes was a repo that was correct and a host that was not, so a gate
# reading the source tree would have passed throughout the outage.
#
# READ-ONLY with respect to system state: it queries the RC and runs the
# read-only reporting tools. It copies nothing and restarts nothing.
#
# Usage: acceptance.bash [--help]
# Exit 0 = ACCEPTED, 1 = REJECTED.

set -euo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00099 — acceptance gate

Usage: acceptance.bash [--help]

Checks, against the DEPLOYED artifacts:
  1. the mount's RC rejects an unauthenticated call (auth is actually on)
  2. the credential helper library is deployed and has a non-empty credential
  3. no deployed script calls `rclone rc` without going through the helper
  4. rclone-cache-status reports live figures, not an rc error
  5. rclone-tail --once reports live figures, not an rc error
  6. ftp-camera's copy preflight authenticates successfully
  7. no repo-owned script has drifted from its deployed copy

Exit 0 = ACCEPTED, 1 = REJECTED.
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: acceptance.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"
BIN="$HOME/.local/bin"
RC_LIB="$BIN/rclone-rc-auth.bash"
RC_ADDR="localhost:5572"

PASS=0
FAIL=0

ok() {
    echo "  PASS  $1"
    PASS=$((PASS + 1))
}
bad() {
    echo "  FAIL  $1" >&2
    if [ $# -gt 1 ]; then
        echo "        $2" >&2
    fi
    FAIL=$((FAIL + 1))
}

echo "=============================================================="
echo "Plan 00099 acceptance — rclone RC clients"
echo "=============================================================="
echo

# --- 0. precondition: there is a mount to talk to ----------------------------
# Without this, the gate is worthless on a host with no rclone mount:
# rclone-cache-status and rclone-tail both print "No rclone mounts found." and
# exit 0, so checks 4 and 5 would PASS having exercised no RC call at all.
# Refuse to render a verdict rather than issue a false ACCEPTED.
echo "[0] precondition: at least one rclone mount is present"
if ! findmnt -n -t fuse.rclone > /dev/null; then
    echo "  ABORT  no fuse.rclone mount found on this host." >&2
    echo "         This gate proves nothing without one — checks 4 and 5 would" >&2
    echo "         pass on empty output. Start the mount and re-run:" >&2
    echo "           systemctl --user start rclone-<name>" >&2
    exit 1
fi
ok "$(findmnt -n -t fuse.rclone | wc -l) rclone mount(s) present"
echo

# --- 1. auth is genuinely enforced -------------------------------------------
# If this passes trivially because the RC is DOWN, checks 4-6 will catch it.
echo "[1] RC rejects unauthenticated calls"
unauth_out=""
if unauth_out=$(rclone rc --url="http://${RC_ADDR}" core/stats 2>&1); then
    bad "unauthenticated core/stats SUCCEEDED" \
        "the mount is serving its RC with no authentication — re-run play-rclone.yml"
else
    case "$unauth_out" in
        *401* | *Unauthorized*)
            ok "unauthenticated core/stats refused with 401"
            ;;
        *)
            bad "unauthenticated core/stats failed, but not with 401" "$unauth_out"
            ;;
    esac
fi
echo

# --- 2. the credential helper is deployed ------------------------------------
echo "[2] credential helper deployed and usable"
if [ ! -r "$RC_LIB" ]; then
    bad "$RC_LIB is missing" "run deploy.bash — play-rclone.yml deploys it"
else
    ok "$RC_LIB is present"
    # shellcheck source=/dev/null
    . "$RC_LIB"
    if rclone_rc_load_credentials 2> /dev/null; then
        ok "credential loaded (non-empty user and password)"
    else
        bad "credential could not be loaded" "run deploy.bash"
    fi
fi
echo

# --- 3. no deployed script bypasses the helper -------------------------------
# A bare `rclone rc ` call is an unauthenticated call, which is the exact bug.
#
# The pattern must match an INVOCATION, not the text. Requiring a command
# position — line start, a separator, a command substitution, or `!` — is what
# distinguishes `$(rclone rc …)` from the diagnostic string
# `echo "ERROR: rclone rc failed."`, which an unanchored grep flags as a bypass
# it is not. `rclone_rc` is excluded by requiring a space after `rclone`.
# Whitespace is allowed AFTER the command position, so `&& rclone rc`,
# `|| rclone rc` and `; rclone rc` are caught as well as `$(rclone rc`.
RC_BYPASS_RE='(^|[;&|(]|\$\(|!)[[:space:]]*rclone rc '
echo "[3] no deployed script calls 'rclone rc' directly"
bypass_found=0
for f in "$BIN"/rclone-* "$BIN"/ftp-camera; do
    if [ ! -f "$f" ]; then
        continue
    fi
    # Skip the library itself: it is the one place allowed to make the call.
    if [ "$f" = "$RC_LIB" ]; then
        continue
    fi
    hits=""
    if hits=$(grep -nE "$RC_BYPASS_RE" "$f"); then
        bypass_found=1
        bad "$(basename "$f") calls 'rclone rc' directly" "$hits"
    fi
done
if [ "$bypass_found" -eq 0 ]; then
    ok "every deployed client goes through rclone_rc"
fi
echo

# --- 4. rclone-cache-status works --------------------------------------------
echo "[4] rclone-cache-status reports live figures"
if [ ! -x "$BIN/rclone-cache-status" ]; then
    bad "rclone-cache-status is not deployed"
else
    cs_out=""
    if ! cs_out=$(timeout 60 "$BIN/rclone-cache-status" --no-dir 2>&1); then
        bad "rclone-cache-status exited non-zero" "$cs_out"
    elif printf '%s' "$cs_out" | grep -q 'No rclone mounts found'; then
        # It exits 0 on this path, so "no error in the output" is NOT evidence
        # that an RC call succeeded. Demand positive proof instead.
        bad "rclone-cache-status found no mounts — nothing was queried" "$cs_out"
    elif printf '%s' "$cs_out" | grep -q 'rc unreachable\|rejected credentials\|credential missing'; then
        bad "rclone-cache-status could not reach the RC" "$cs_out"
    elif ! printf '%s' "$cs_out" | grep -q 'cache:'; then
        bad "rclone-cache-status printed no cache figures" "$cs_out"
    else
        ok "rclone-cache-status reported live cache figures"
    fi
fi
echo

# --- 5. rclone-tail works ----------------------------------------------------
echo "[5] rclone-tail --once reports live figures"
if [ ! -x "$BIN/rclone-tail" ]; then
    bad "rclone-tail is not deployed"
else
    rt_out=""
    if ! rt_out=$(timeout 60 "$BIN/rclone-tail" --once 2>&1); then
        bad "rclone-tail exited non-zero" "$rt_out"
    elif printf '%s' "$rt_out" | grep -q 'No rclone mounts found'; then
        # Same trap as check 4: this path exits 0 having queried nothing.
        bad "rclone-tail found no mounts — nothing was queried" "$rt_out"
    elif printf '%s' "$rt_out" | grep -q 'rc unreachable\|rejected credentials\|credential missing'; then
        bad "rclone-tail could not reach the RC" "$rt_out"
    elif ! printf '%s' "$rt_out" | grep -qE 'idle|queued|active|uploading'; then
        bad "rclone-tail printed no per-mount state" "$rt_out"
    else
        ok "rclone-tail reported live per-mount state"
    fi
fi
echo

# --- 6. ftp-camera's copy preflight authenticates ----------------------------
# Runs the SAME function the deployed ftp-camera calls, sourced from the SAME
# deployed library — not a re-implementation of it. A copy is not started here
# because that would move real data; the preflight is the step that was failing.
echo "[6] ftp-camera copy preflight authenticates"
if [ ! -x "$BIN/ftp-camera" ]; then
    bad "ftp-camera is not deployed"
elif ! grep -q 'rclone_rc_available' "$BIN/ftp-camera"; then
    bad "deployed ftp-camera does not use rclone_rc_available" \
        "it is the pre-migration build — run deploy.bash"
elif [ ! -r "$RC_LIB" ]; then
    bad "cannot test the preflight without $RC_LIB"
elif rclone_rc_available "http://${RC_ADDR}" 2> /dev/null; then
    ok "preflight probe (core/stats, authenticated) succeeded"
else
    bad "preflight probe failed — ftp-camera --copy would refuse to run"
fi
echo

# --- 6b. the vfs/refresh path authenticates ----------------------------------
# rclone-cache-warm --fast is the only client of vfs/refresh, and it was the one
# endpoint Plan 00094 correctly identified as gated — so it is the one call whose
# migration a grep would happily vouch for while it was in fact broken. Exercise
# it for real. vfs/refresh only re-reads directory listings into the VFS cache;
# it moves no data and deletes nothing, so it is safe in an acceptance gate.
echo "[6b] vfs/refresh (rclone-cache-warm --fast's endpoint) authenticates"
if [ ! -r "$RC_LIB" ]; then
    bad "cannot test vfs/refresh without $RC_LIB"
else
    refresh_fs=""
    refresh_fs=$(findmnt -n -o SOURCE -t fuse.rclone | head -n1)
    if [ -z "$refresh_fs" ]; then
        bad "no rclone mount source to refresh"
    else
        refresh_out=""
        if refresh_out=$(rclone_rc vfs/refresh "fs=$refresh_fs" 2>&1); then
            ok "vfs/refresh accepted on $refresh_fs"
        else
            bad "vfs/refresh was refused — rclone-cache-warm --fast would fail" \
                "$refresh_out"
        fi
    fi
fi
echo

# --- 7. nothing has drifted --------------------------------------------------
echo "[7] no repo-owned script differs from its deployed copy"
drift_out=""
if drift_out=$(bash "$REPO_ROOT/scripts/qa-deployed-drift.bash" 2>&1); then
    ok "repo and host are in sync"
else
    bad "deployed scripts differ from the repo" "$drift_out"
fi
echo

echo "=============================================================="
if [ "$FAIL" -eq 0 ]; then
    echo "ACCEPTED — $PASS check(s) passed."
    echo "=============================================================="
    exit 0
fi
echo "REJECTED — $FAIL check(s) failed, $PASS passed." >&2
echo "  Run deploy.bash, then re-run this gate." >&2
echo "=============================================================="
exit 1
