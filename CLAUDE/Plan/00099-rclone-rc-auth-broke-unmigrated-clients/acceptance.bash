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
  0.  precondition: an rclone mount is present and publishes an RC address
  1.  the mount's RC rejects an unauthenticated call (auth is actually on)
  2.  the credential helper library is deployed and has a non-empty credential
  3.  no deployed script calls `rclone rc` without going through the helper
  4.  rclone-cache-status reports live figures, not an rc error
  5.  rclone-tail --once reports live figures, not an rc error
  6.  ftp-camera's copy preflight authenticates successfully
  6b. vfs/refresh (rclone-cache-warm --fast's endpoint) authenticates
  7.  no repo-owned script has drifted from its deployed copy

The verdict carries a COVERAGE line counting these against what actually ran,
so a check that stops executing is visible rather than absorbed into a lower
pass count. An incomplete run is REJECTED even with no failures.

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

# Discovered by check [0] from the mount's own rclone process. NOT hardcoded:
# since the unit template defaults each mount's RC port to
# rclone_rc_port_base + mount_index, only the FIRST mount is on 5572, and an
# explicit rc_port can move even that one. A fixed address would make this gate
# REJECT a healthy multi-mount host for a reason unrelated to what it tests.
# REFRESH_FS is the remote belonging to that same address, so check [6b] cannot
# ask one mount's RC to refresh another mount's filesystem.
RC_ADDR=""
REFRESH_FS=""

# The checks this gate is expected to run. The verdict prints COVERAGE against
# this list because the PASS count cannot carry it: check [2] emits two passes
# or one fail, check [3] emits one pass or N fails, and [2]'s second half, [6]
# and [6b] are each conditional on $RC_LIB. So "ACCEPTED — 10 check(s) passed"
# reads identically whether 10 of 10 ran or 10 of 12. Coverage implied by a
# count rather than stated is this repo's named recurring defect class.
EXPECTED_CHECKS=(0 1 2 3 4 5 6 6b 7)
RAN_CHECKS=()

PASS=0
FAIL=0

# Announce a check AND record that it ran. Every numbered section starts here;
# a section that prints its own header instead is invisible to COVERAGE.
check() {
    RAN_CHECKS+=("$1")
    echo "[$1] $2"
}
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

# Temp files are removed on the way out, not only at the end of the block that made them.
# Both `rm -f` calls below sit after an `if` that can die under `set -e`, so any failure in
# between leaked the file. A plain EXIT trap is correct HERE specifically because this
# script deliberately does not source `_planlib.inc.bash` — there is no library handler to
# displace. (Scripts that DO source it must use `plan_on_cleanup` instead; see
# PlanScriptStandards.md R4.) Why this one does not: converting it needs `plan_require_host`,
# which would make the container harness that falsifies the COVERAGE mechanism unrunnable —
# the owner trade-off recorded in this plan's 14:28 handoff entry.
#
# The trap body is INLINE rather than a named function: a function reachable only from a
# trap string reads as unreachable to shellcheck (SC2317), and suppression directives are
# banned in this repo, so the way to keep the linter honest is to give it nothing to be
# wrong about.
TEMP_FILES=()
trap 'if [ "${#TEMP_FILES[@]}" -gt 0 ]; then rm -f "${TEMP_FILES[@]}"; fi' EXIT

echo "=============================================================="
echo "Plan 00099 acceptance — rclone RC clients"
echo "=============================================================="
echo

# --- 0. precondition: there is a mount to talk to ----------------------------
# Without this, the gate is worthless on a host with no rclone mount:
# rclone-cache-status and rclone-tail both print "No rclone mounts found." and
# exit 0, so checks 4 and 5 would PASS having exercised no RC call at all.
# Refuse to render a verdict rather than issue a false ACCEPTED.
check 0 "precondition: an rclone mount is present and publishes an RC address"
if ! findmnt -n -t fuse.rclone > /dev/null; then
    echo "  ABORT  no fuse.rclone mount found on this host." >&2
    echo "         This gate proves nothing without one — checks 4 and 5 would" >&2
    echo "         pass on empty output. Start the mount and re-run:" >&2
    echo "           systemctl --user start rclone-<name>" >&2
    exit 1
fi
ok "$(findmnt -n -t fuse.rclone | wc -l) rclone mount(s) present"

# Read the RC address off the mount's own rclone process, the way every client
# this plan fixed now does. Taking the first mount is deliberate and stated:
# one authenticated mount is enough to prove the migration, and pairing its
# address with its OWN remote is what stops [6b] refreshing across mounts.
REFRESH_FS=$(findmnt -n -o SOURCE -t fuse.rclone | head -n1)
rc_mount=$(findmnt -n -o TARGET -t fuse.rclone | head -n1)
for rc_pid in $(pgrep -f 'rclone [m]ount'); do
    rc_cmdline=$(tr '\0' ' ' < "/proc/$rc_pid/cmdline")
    case "$rc_cmdline" in
        *" $rc_mount "* | *" $rc_mount")
            RC_ADDR=$(grep -oE -- '--rc-addr=[^ ]+' <<< "$rc_cmdline" | head -n1 | cut -d= -f2)
            break
            ;;
    esac
done
if [ -z "$RC_ADDR" ]; then
    echo "  ABORT  the mount on $rc_mount publishes no --rc-addr." >&2
    echo "         Checks 1, 6 and 6b have no RC to talk to, so this gate" >&2
    echo "         cannot render a verdict. Re-deploy the mount:" >&2
    echo "           ansible-playbook playbooks/imports/optional/common/play-rclone.yml" >&2
    exit 1
fi
ok "RC address discovered from the mount process: $RC_ADDR"
echo

# --- 1. auth is genuinely enforced -------------------------------------------
# If this passes trivially because the RC is DOWN, checks 4-6 will catch it.
check 1 "RC rejects unauthenticated calls"
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
check 2 "credential helper deployed and usable"
if [ ! -r "$RC_LIB" ]; then
    bad "$RC_LIB is missing" "run deploy.bash — play-rclone.yml deploys it"
else
    ok "$RC_LIB is present"
    # shellcheck source=/dev/null
    . "$RC_LIB"
    # Keep the library's stderr. It distinguishes "credential file absent" from
    # "present but valueless" and names the play to run; discarding it and
    # printing a generic failure is the same discarded-diagnosis defect this
    # plan exists to fix, one level up.
    cred_err=$(mktemp)
    TEMP_FILES+=("$cred_err")
    if rclone_rc_load_credentials 2> "$cred_err"; then
        ok "credential loaded (non-empty user and password)"
    else
        bad "credential could not be loaded" "$(cat "$cred_err")"
    fi
    rm -f "$cred_err"
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
check 3 "no deployed script calls 'rclone rc' directly"
bypass_found=0
# COUNTED, because "no client bypasses the library" and "there are no clients"
# produce identical output otherwise. On a host where the glob matches nothing —
# nothing deployed, a renamed directory — every iteration hits the `continue` and
# bypass_found stays 0, which was read as proof. A blind scanner and a clean one
# must not report alike; that is this plan's own subject.
scanned=0
for f in "$BIN"/rclone-* "$BIN"/ftp-camera; do
    if [ ! -f "$f" ]; then
        continue
    fi
    # Skip the library itself: it is the one place allowed to make the call.
    if [ "$f" = "$RC_LIB" ]; then
        continue
    fi
    scanned=$((scanned + 1))
    hits=""
    if hits=$(grep -nE "$RC_BYPASS_RE" "$f"); then
        bypass_found=1
        bad "$(basename "$f") calls 'rclone rc' directly" "$hits"
    fi
done
if [ "$scanned" -eq 0 ]; then
    bad "no deployed client was scanned, so nothing was established" \
        "looked for $BIN/rclone-* and $BIN/ftp-camera — run deploy.bash first"
elif [ "$bypass_found" -eq 0 ]; then
    ok "every deployed client goes through rclone_rc ($scanned scanned)"
fi
echo

# --- 4. rclone-cache-status works --------------------------------------------
check 4 "rclone-cache-status reports live figures"
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
check 5 "rclone-tail --once reports live figures"
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
#
# AND at the same address, which was briefly untrue and is the more interesting
# half. When this gate moved to a discovered RC_ADDR, ftp-camera was still
# hardcoding `localhost:5572` — so the gate ran the right function against the
# wrong endpoint, and the one thing it claimed to cover was the one thing it had
# stopped touching. ftp-camera now discovers the address from the mount's own
# process too, via the shared library, so "same function, same library" is once
# again "same call".
check 6 "ftp-camera copy preflight authenticates"
if [ ! -x "$BIN/ftp-camera" ]; then
    bad "ftp-camera is not deployed"
elif ! grep -q 'rclone_rc_available' "$BIN/ftp-camera"; then
    bad "deployed ftp-camera does not use rclone_rc_available" \
        "it is the pre-migration build — run deploy.bash"
elif [ ! -r "$RC_LIB" ]; then
    bad "cannot test the preflight without $RC_LIB"
else
    # rclone_rc_available reports the actual probe error on stderr. Keep it:
    # "preflight probe failed" alone tells the operator nothing about whether
    # the RC is down, the credential is wrong, or the helper is absent.
    probe_err=$(mktemp)
    TEMP_FILES+=("$probe_err")
    if rclone_rc_available "http://${RC_ADDR}" 2> "$probe_err"; then
        ok "preflight probe (core/stats, authenticated) succeeded"
    else
        bad "preflight probe failed — ftp-camera --copy would refuse to run" \
            "$(cat "$probe_err")"
    fi
    rm -f "$probe_err"
fi
echo

# --- 6b. the vfs/refresh path authenticates ----------------------------------
# rclone-cache-warm --fast is the only client of vfs/refresh, and it was the one
# endpoint Plan 00094 correctly identified as gated — so it is the one call whose
# migration a grep would happily vouch for while it was in fact broken. Exercise
# it for real. vfs/refresh only re-reads directory listings into the VFS cache;
# it moves no data and deletes nothing, so it is safe in an acceptance gate.
check 6b "vfs/refresh (rclone-cache-warm --fast's endpoint) authenticates"
if [ ! -r "$RC_LIB" ]; then
    bad "cannot test vfs/refresh without $RC_LIB"
elif [ -z "$REFRESH_FS" ]; then
    bad "no rclone mount source to refresh"
else
    # $REFRESH_FS and $RC_ADDR were read from the SAME mount in check [0].
    # Choosing them independently would let this ask one mount's RC to refresh
    # another mount's remote, which fails for a reason that is not the one
    # under test.
    refresh_out=""
    if refresh_out=$(rclone_rc --url="http://${RC_ADDR}" vfs/refresh "fs=$REFRESH_FS" 2>&1); then
        ok "vfs/refresh accepted on $REFRESH_FS"
    else
        bad "vfs/refresh was refused — rclone-cache-warm --fast would fail" \
            "$refresh_out"
    fi
fi
echo

# --- 7. nothing has drifted --------------------------------------------------
check 7 "no repo-owned script differs from its deployed copy"
drift_out=""
if drift_out=$(bash "$REPO_ROOT/scripts/qa-deployed-drift.bash" 2>&1); then
    # EXIT 0 IS TWO DIFFERENT ANSWERS, and only one of them is a pass. All three
    # of the drift gate's skip paths exit 0 having compared nothing — and this is
    # not only the container's problem: the LINKED-WORKTREE skip fires on the
    # host, so a run from a worktree would report "in sync" having looked at
    # nothing at all.
    #
    # The gate's own m7 fix changed its skip marker from ✓ to ⚠ for exactly this
    # reason; consuming the exit code alone laundered that distinction straight
    # back out one level up. Success criterion 3 rests on this check, so a skip
    # here is "not established", never "in sync".
    case "$drift_out" in
        *skipped*)
            bad "the drift gate SKIPPED and compared nothing — this is not a pass" \
                "$drift_out"
            ;;
        *)
            ok "repo and host are in sync"
            ;;
    esac
else
    bad "deployed scripts differ from the repo" "$drift_out"
fi
echo

# Coverage is stated, not inferred. A check that is deleted, renumbered, or
# skipped by an early exit disappears from RAN_CHECKS and is NAMED here — and
# an incomplete run is REJECTED even with zero failures, because a gate that
# did not run all of its checks has not established what it claims to.
missing=()
for expected in "${EXPECTED_CHECKS[@]}"; do
    case " ${RAN_CHECKS[*]} " in
        *" $expected "*) ;;
        *) missing+=("$expected") ;;
    esac
done

echo "=============================================================="
echo "COVERAGE: ${#RAN_CHECKS[@]} of ${#EXPECTED_CHECKS[@]} checks executed" \
    "(${PASS} assertion(s) passed, ${FAIL} failed)"
if [ "${#missing[@]}" -ne 0 ]; then
    echo "  NOT RUN: ${missing[*]}" >&2
fi

if [ "$FAIL" -eq 0 ] && [ "${#missing[@]}" -eq 0 ]; then
    echo "ACCEPTED — every declared check ran and every assertion passed."
    echo "=============================================================="
    exit 0
fi
if [ "$FAIL" -eq 0 ]; then
    echo "REJECTED — no assertion failed, but ${#missing[@]} declared check(s) never ran." >&2
else
    echo "REJECTED — $FAIL assertion(s) failed, $PASS passed." >&2
fi
echo "  Run deploy.bash, then re-run this gate." >&2
echo "=============================================================="
exit 1
