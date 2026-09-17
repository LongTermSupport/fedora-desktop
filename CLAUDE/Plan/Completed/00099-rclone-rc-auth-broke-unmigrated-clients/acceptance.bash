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

# ONE list of this gate's checks. --help and the COVERAGE arithmetic both read it, so a
# check cannot be described in the help text and absent from the coverage arithmetic, or
# the reverse. Two hand-maintained copies of the same list agree until the day they do not,
# and the only symptom is a verdict that is quietly measuring the wrong thing.
#
# COVERAGE exists because the PASS count cannot carry it: check [2] emits two passes or one
# fail, check [3] emits one pass or N fails, and [2]'s second half, [6] and [6b] are each
# conditional on $RC_LIB. So "ACCEPTED — 10 check(s) passed" reads identically whether 10 of
# 10 ran or 10 of 12. Coverage implied by a count rather than stated is this repo's named
# recurring defect class.
CHECK_CATALOGUE=(
    "0|precondition: an rclone mount is present and publishes an RC address"
    "1|the mount's RC rejects an unauthenticated call (auth is actually on)"
    "2|the credential helper library is deployed and has a non-empty credential"
    "3|no deployed script calls \`rclone rc\` without going through the helper"
    "4|rclone-cache-status reports live figures, not an rc error"
    "5|rclone-tail --once reports live figures, not an rc error"
    "6|ftp-camera's copy preflight authenticates at the address the CLIENT resolves"
    "6b|vfs/refresh (rclone-cache-warm --fast's endpoint) authenticates"
    "7|no repo-owned script has drifted from its deployed copy"
)

EXPECTED_CHECKS=()
for entry in "${CHECK_CATALOGUE[@]}"; do
    EXPECTED_CHECKS+=("${entry%%|*}")
done

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00099 — acceptance gate

Usage: acceptance.bash [--help]

Checks, against the DEPLOYED artifacts:
EOF
            for entry in "${CHECK_CATALOGUE[@]}"; do
                printf '  %-4s%s\n' "${entry%%|*}." "${entry#*|}"
            done
            cat << 'EOF'

The verdict carries a COVERAGE line counting these against what actually ran,
so a check that stops executing is visible rather than absorbed into a lower
pass count. An incomplete run is REJECTED even with no failures, and so is a
run that executes a check this list does not name.

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
RC_ADDR=""

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
# Context the operator needs that is neither a pass nor a failure, and so counts as
# neither. Defined here rather than assumed: it was CALLED before it existed, and under
# `set -euo pipefail` an undefined command is exit 127 — the gate died mid-check with no
# verdict at all, on the branch check [0]'s own comment calls the normal case. Nothing
# caught it: shellcheck does not resolve command names, and no harness executes this file
# past check [0], which aborts in a container.
note() {
    echo "  NOTE  $1"
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
# Count, then assert the count. `findmnt -n -t fuse.rclone` exits 0 with ZERO rows, so the
# exit status alone said nothing and this ABORT could not fire — the check printed
# "PASS 0 rclone mount(s) present", which is the blind-result-reads-like-a-clean-result
# class this gate exists to stop, sitting in the check whose whole job is to stop it. With
# rc_mount then empty, the cmdline matcher below degenerated to *"  "* and matched any
# process argv containing two consecutive spaces.
mount_count=$(findmnt -n -t fuse.rclone | wc -l)
if [ "$mount_count" -eq 0 ]; then
    echo "  ABORT  no fuse.rclone mount found on this host." >&2
    echo "         This gate proves nothing without one — checks 4 and 5 would" >&2
    echo "         pass on empty output. Start the mount and re-run:" >&2
    echo "           systemctl --user start rclone-<name>" >&2
    exit 1
fi
ok "$mount_count rclone mount(s) present"

# Read the RC address off the mount's own rclone process, the way every client this plan
# fixed now does. Taking the first mount is deliberate and stated, and the COVERAGE note
# below says so in the output rather than only in this comment: one authenticated mount is
# enough to prove the migration, but a gate that exercises 1 of 3 and prints "every
# assertion passed" has stated a scope it does not have.
#
# First row by expansion, not `| head -n1`: a failing producer in a pipeline yields exit 0
# and an empty value wherever the caller runs `set -e` without pipefail, and an empty
# rc_mount degenerates the cmdline matcher below into one that matches any process.
mount_targets=$(findmnt -n -o TARGET -t fuse.rclone)
rc_mount="${mount_targets%%$'\n'*}"
for rc_pid in $(pgrep -f 'rclone [m]ount'); do
    # Guarded, because the process can exit between pgrep and this read — which is not
    # this mount's problem and must not abort a gate under `set -e`. The shared library
    # grew exactly this guard in the same change that left this copy without it.
    if ! rc_cmdline=$(tr '\0' ' ' < "/proc/$rc_pid/cmdline"); then
        continue
    fi
    case "$rc_cmdline" in
        *" $rc_mount "* | *" $rc_mount")
            # grep exits 1 when this mount publishes NO --rc-addr, which is precisely the
            # case the ABORT block below was written for. At top level under `set -euo
            # pipefail` an assignment from a failing pipeline kills the script, so that
            # block was UNREACHABLE: the gate exited 1 after check [0]'s first PASS with no
            # ABORT, no COVERAGE and no verdict at all. Round 5's `note` defect again —
            # a branch nothing had ever executed, on the multi-mount case check [0]'s own
            # comment calls normal.
            #
            # The library's copy of this line survives only because its callers wrap it in
            # an `if` condition, where errexit is suspended. That is the CALLER's property,
            # not the line's. Measured: same line, top level, dies; inside a function used
            # as a condition, reaches the guard.
            #
            # First match by expansion rather than `| head -n1`, which yields exit 0 and an
            # empty value when the producer fails in a caller without pipefail.
            if rc_addr_raw=$(grep -oE -- '--rc-addr=[^ ]+' <<< "$rc_cmdline"); then
                RC_ADDR="${rc_addr_raw%%$'\n'*}"
                RC_ADDR="${RC_ADDR#--rc-addr=}"
            else
                RC_ADDR=""
            fi
            break
            ;;
    esac
done
# Mount coverage, STATED. This gate has a COVERAGE discipline for its checks and had none
# for mounts: on a three-mount host it exercised one and printed "every assertion passed",
# claiming a scope it does not have. The limit is deliberate — one authenticated mount
# proves the migration — but a deliberate limit still has to be visible at verdict time,
# not only in a comment above the loop.
if [ "$mount_count" -gt 1 ]; then
    note "MOUNT COVERAGE: 1 of $mount_count mounts exercised ($rc_mount) — checks 1, 6 and 6b"
    note "  speak for that mount alone. The others are unexamined, not proven healthy."
else
    note "MOUNT COVERAGE: 1 of 1 mounts exercised ($rc_mount)"
fi
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
#
# And counted against a DENOMINATOR, which it was not. "3 scanned" is a numerator with
# nothing to compare it to: with rclone-cache-warm simply never deployed, the gate scanned
# the other three, found no bypass, and ACCEPTED — while the client carrying round 6's
# blocking defect was absent from the host entirely. The drift gate does not cover it
# either, because it classes a never-deployed file as NOT_DEPLOYED rather than as drift.
#
# The denominator comes from the REPO, which is the source of truth for what this plan
# owns. Enumerated by the same glob, so a sixth client added later is counted without
# anyone remembering to update a list here.
owned=()
for repo_client in "$REPO_ROOT/files/home/.local/bin"/rclone-* "$REPO_ROOT/files/home/.local/bin"/ftp-camera; do
    if [ ! -f "$repo_client" ]; then
        continue
    fi
    if [ "$(basename "$repo_client")" = "$(basename "$RC_LIB")" ]; then
        continue
    fi
    owned+=("$(basename "$repo_client")")
done

missing_clients=()
for owned_name in "${owned[@]+"${owned[@]}"}"; do
    if [ ! -f "$BIN/$owned_name" ]; then
        missing_clients+=("$owned_name")
    fi
done

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
elif [ "${#missing_clients[@]}" -ne 0 ]; then
    bad "${#missing_clients[@]} of ${#owned[@]} client(s) this plan owns are NOT DEPLOYED: ${missing_clients[*]}" \
        "a client that is absent cannot bypass the library, so scanning the rest proves nothing about it — run deploy.bash"
elif [ "$bypass_found" -eq 0 ]; then
    ok "every deployed client goes through rclone_rc ($scanned of ${#owned[@]} this plan owns)"
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
    # 'rc helper library missing' is the client's OWN name for the failure this plan is
    # about — the library not deployed — and it was absent from this list, so that exact
    # regression degraded to the vaguer "printed no cache figures" below and pointed the
    # reader at the wrong thing.
    elif printf '%s' "$cs_out" | grep -q 'rc unreachable\|rejected credentials\|credential missing\|rc helper library missing'; then
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
    elif printf '%s' "$rt_out" | grep -q 'rc unreachable\|rejected credentials\|credential missing\|rc helper library missing'; then
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
elif ! grep -q -- '--copy-preflight' "$BIN/ftp-camera"; then
    # Asserted, not attempted: the deployed build's argument parser REJECTS an unknown
    # flag with exit 2, which is indistinguishable here from a preflight that ran and
    # failed. A gate that cannot tell "the check failed" from "the check does not exist"
    # is the blind-reads-like-clean case, inverted.
    bad "the deployed ftp-camera has no --copy-preflight, so its own preflight cannot be run" \
        "this gate invokes the client rather than approximating it — run deploy.bash"
elif [ ! -r "$RC_LIB" ]; then
    bad "cannot test the preflight without $RC_LIB"
else
    # rclone_rc_available reports the actual probe error on stderr. Keep it:
    # "preflight probe failed" alone tells the operator nothing about whether
    # the RC is down, the credential is wrong, or the helper is absent.
    probe_err=$(mktemp)
    TEMP_FILES+=("$probe_err")
    # RUN THE CLIENT. `ftp-camera --copy-preflight` is exactly what `--copy` does before it
    # moves a byte — the same find_mount_path, the same rclone_rc_addr_for_mount, the same
    # authenticated core/stats probe — and then stops. It copies nothing and starts nothing.
    #
    # Not a stand-in, because three rounds of stand-ins were each defeated by a different
    # normalisation that still broke the client: a path inside the mount, then the mount
    # root with a trailing dot (any canonicalisation removes it), then a one-level
    # subdirectory (a `dirname` satisfies it, while find_mount_path returns the remote's
    # whole multi-component offset). Every one of those passed while `--copy` aborted on
    # every run. The population of "normalisations that are not findmnt --target" cannot be
    # enumerated, so the gate stops trying to approximate the client's input and invokes
    # the client.
    if ! client_addr=$("$BIN/ftp-camera" --copy-preflight 2> "$probe_err"); then
        bad "ftp-camera's own copy preflight failed — --copy aborts here" \
            "$(cat "$probe_err")"
    elif [ -z "$client_addr" ]; then
        bad "ftp-camera's preflight succeeded but named no RC address" \
            "it prints the address it resolved; an empty answer means the deployed build predates --copy-preflight, so nothing here was exercised"
    elif [[ ! "$client_addr" =~ ^[^[:space:]]+:[0-9]+$ ]]; then
        # The disagreement branch below used to accept ANY non-empty stdout as "an address
        # the client resolved". Measured with two stub clients: a banner line before the
        # address, and an error string printed to stdout with exit 0 — both produced
        # `PASS ... authenticated at its own mount's ERROR: rclone remote control did not
        # answer`, and the gate ACCEPTED. The deployed client's stdout is clean today, so it
        # was latent rather than a live false green; but the one branch that admits a
        # disagreement was admitting everything, in the check this plan rebuilt four times.
        #
        # So the shape is required first. What the client prints must look like an address
        # before any verdict treats it as one.
        bad "ftp-camera's preflight printed something that is not a host:port address" \
            "stdout was: $client_addr"
    elif [ "$client_addr" != "$RC_ADDR" ]; then
        # Not necessarily a fault: check [0] takes the FIRST fuse.rclone mount, which need
        # not be ftp-camera's. Reported rather than judged, with both numbers, because
        # silently accepting a disagreement is how the addresses diverged in the first place.
        note "ftp-camera resolved $client_addr; check [0] probes $RC_ADDR (different mounts, or one of them is wrong)"
        # CORRECT THE COVERAGE CLAIM. Check [0] printed "checks 1, 6 and 6b speak for that
        # mount alone" before it could know which mount [6] would use — and here it used a
        # different one. Left uncorrected, the verdict asserts a scope wider than the
        # evidence in one direction and narrower in the other, which is exactly the
        # read-clean-whether-or-not-it-looked defect this gate was rebuilt to remove.
        note "  COVERAGE CORRECTION: check [6] speaks for $client_addr, not $RC_ADDR."
        note "  Check [1] proved auth is ENFORCED only at $RC_ADDR — nothing here shows"
        note "  ftp-camera's own mount rejects unauthenticated calls."
        ok "ftp-camera's copy preflight authenticated at its own mount's $client_addr"
    else
        ok "ftp-camera's copy preflight authenticated at $client_addr"
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
# RUN THE CLIENT, for the same reason check [6] does.
#
# This check used to issue `rclone_rc --url=… vfs/refresh "fs=$REFRESH_FS"` from the gate
# itself, while its own catalogue entry named rclone-cache-warm. That is the stand-in
# rounds 2-5 spent four rounds removing from check [6], still in place one check down: the
# client's real call is `("fs=$RCLONE_SOURCE" "recursive=true")` plus `remote=$REL_PATH`,
# after its own --rc-addr walk — which is where round 6's blocking defect lived. The gate's
# approximation would have passed throughout, because the gate never took that walk.
#
# --fast is read-only: vfs/refresh re-reads directory listings into the VFS cache. It moves
# no data and deletes nothing.
if [ ! -x "$BIN/rclone-cache-warm" ]; then
    bad "rclone-cache-warm is not deployed, so its vfs/refresh path cannot be exercised" \
        "run deploy.bash"
elif [ -z "$rc_mount" ]; then
    bad "no rclone mountpoint to refresh"
else
    warm_err=$(mktemp)
    TEMP_FILES+=("$warm_err")
    if "$BIN/rclone-cache-warm" --fast "$rc_mount" > /dev/null 2> "$warm_err"; then
        ok "rclone-cache-warm --fast authenticated and refreshed $rc_mount"
    else
        bad "rclone-cache-warm --fast failed — the vfs/refresh path is still broken" \
            "$(cat "$warm_err")"
    fi
    rm -f "$warm_err"
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
    #
    # And the SAME laundering, one level further in. Plan 00081 made that gate state
    # `$NOT_DEPLOYED not installed on this host` ALWAYS, including zero, precisely so a
    # reader sees it (`qa-deployed-drift.bash:285-288`). Reducing its whole output to a
    # `*skipped*` substring test discarded that number and printed "repo and host are in
    # sync" over the top of `; 12 not installed on this host` — the m7/S1 pattern
    # recurring, on the same check, for the third time. A never-deployed file is not drift
    # by that gate's definition, so nothing else covers it.
    case "$drift_out" in
        *skipped*)
            bad "the drift gate SKIPPED and compared nothing — this is not a pass" \
                "$drift_out"
            ;;
        *)
            # The count is carried through rather than summarised away. Non-zero is not a
            # failure of this check — the host legitimately does not run every play — but
            # it is the number that would have said "the client you are vouching for is
            # not on this machine", so it is stated where the verdict is read.
            not_deployed_clause=""
            case "$drift_out" in
                *"not installed on this host"*)
                    not_deployed_clause="${drift_out#*match the repo; }"
                    not_deployed_clause="${not_deployed_clause%% not installed on this host*}"
                    ;;
            esac
            if [ -z "$not_deployed_clause" ]; then
                bad "the drift gate did not state its not-installed count" \
                    "Plan 00081 made that count unconditional; its absence means this check is reading output it does not understand: $drift_out"
            elif [ "$not_deployed_clause" = "0" ]; then
                ok "repo and host are in sync, with 0 repo-owned files not installed here"
            else
                note "$not_deployed_clause repo-owned file(s) are not installed on this host — not drift,"
                note "  but not compared either. Check [3] names any of this plan's own clients among them."
                ok "every deployed repo-owned file matches the repo"
            fi
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

# And the other direction, which was missing: a check that RAN without being declared.
# Coverage was compared one way only, so adding a check and forgetting the declaration
# printed "COVERAGE: 10 of 9" and still ACCEPTED — the count contradicted itself in the
# verdict line and nothing acted on it. An undeclared check is not a bonus; it means the
# list this gate measures itself against is no longer the gate.
undeclared=()
for ran in "${RAN_CHECKS[@]+"${RAN_CHECKS[@]}"}"; do
    case " ${EXPECTED_CHECKS[*]} " in
        *" $ran "*) ;;
        *) undeclared+=("$ran") ;;
    esac
done

# The SAME check id emitted twice. Both `missing` and `undeclared` come back empty, and
# `COVERAGE: 10 of 9` prints and ACCEPTS.
duplicates=()
seen_checks=""
for ran in "${RAN_CHECKS[@]+"${RAN_CHECKS[@]}"}"; do
    case " ${seen_checks} " in
        *" $ran "*) duplicates+=("$ran") ;;
        *) seen_checks="${seen_checks}${seen_checks:+ }$ran" ;;
    esac
done

# And the same defect on the OTHER list: a CHECK_CATALOGUE entry declared twice inflates
# the denominator, giving `COVERAGE: 9 of 10` with nothing missing, nothing undeclared and
# nothing duplicated among the runs — ACCEPTED, exit 0.
#
# Three causes of a self-contradicting count have now been found one at a time, each after
# the previous fix was described as closing the class. So this no longer enumerates causes:
# the two counts are compared directly, which is the property the COVERAGE line asserts and
# which no fourth cause can satisfy while contradicting itself.
catalogue_duplicates=()
seen_declared=""
for declared_id in "${EXPECTED_CHECKS[@]}"; do
    case " ${seen_declared} " in
        *" $declared_id "*) catalogue_duplicates+=("$declared_id") ;;
        *) seen_declared="${seen_declared}${seen_declared:+ }$declared_id" ;;
    esac
done

echo "=============================================================="
echo "COVERAGE: ${#RAN_CHECKS[@]} of ${#EXPECTED_CHECKS[@]} checks executed" \
    "(${PASS} assertion(s) passed, ${FAIL} failed)"
if [ "${#missing[@]}" -ne 0 ]; then
    echo "  NOT RUN: ${missing[*]}" >&2
fi
if [ "${#undeclared[@]}" -ne 0 ]; then
    echo "  RAN BUT NOT DECLARED: ${undeclared[*]}" >&2
    echo "  Add them to CHECK_CATALOGUE, or this gate is measuring itself against" >&2
    echo "  a list that no longer describes it." >&2
fi
if [ "${#duplicates[@]}" -ne 0 ]; then
    echo "  RAN MORE THAN ONCE: ${duplicates[*]}" >&2
    echo "  A repeated check id inflates the executed count above the declared one," >&2
    echo "  so the COVERAGE line contradicts itself." >&2
fi
if [ "${#catalogue_duplicates[@]}" -ne 0 ]; then
    echo "  DECLARED MORE THAN ONCE: ${catalogue_duplicates[*]}" >&2
    echo "  A repeated CHECK_CATALOGUE entry inflates the declared count, so the" >&2
    echo "  COVERAGE line understates its own coverage." >&2
fi
# The counts themselves, compared directly. The four named conditions above each describe a
# KNOWN way they can disagree; this holds whether or not the cause has a name yet.
#
# CORRECTION to what this comment used to claim. It said this was "the only form of this
# assertion a fifth cause cannot walk past", and that overstates it: with `missing`,
# `undeclared`, `duplicates` and `catalogue_duplicates` all empty, the two arrays are
# repeat-free sets of the same elements, so their sizes MUST agree and this branch is
# unreachable on its own. Four mutants confirmed it — COUNT MISMATCH printed only ever
# alongside a named cause. It is a backstop against the four detectors themselves being
# wrong, which is worth keeping and is a smaller claim than the one made here before.
if [ "${#RAN_CHECKS[@]}" -ne "${#EXPECTED_CHECKS[@]}" ]; then
    echo "  COUNT MISMATCH: ${#RAN_CHECKS[@]} executed against ${#EXPECTED_CHECKS[@]} declared" >&2
    count_disagrees=1
else
    count_disagrees=0
fi

if [ "$FAIL" -eq 0 ] && [ "${#missing[@]}" -eq 0 ] && [ "${#undeclared[@]}" -eq 0 ] \
    && [ "${#duplicates[@]}" -eq 0 ] && [ "${#catalogue_duplicates[@]}" -eq 0 ] \
    && [ "$count_disagrees" -eq 0 ]; then
    echo "ACCEPTED — every declared check ran exactly once and every assertion passed."
    echo "=============================================================="
    exit 0
fi
# The NAMED causes first, the unnamed fallback LAST — which is what its own comment above
# says it is for. Checked first, the generic "the counts disagree" preempted every specific
# verdict, because each named cause also makes the counts differ by one: three carefully
# worded headlines became reachable only when two faults happened to cancel out. The
# detail lines still printed, so nothing was lost from the full output, but the line the
# operator actually reads had regressed from specific to generic in exactly the cases the
# specific wording was written for.
if [ "$FAIL" -eq 0 ] && [ "${#duplicates[@]}" -ne 0 ]; then
    echo "REJECTED — ${#duplicates[@]} check id(s) ran more than once." >&2
elif [ "$FAIL" -eq 0 ] && [ "${#catalogue_duplicates[@]}" -ne 0 ]; then
    echo "REJECTED — ${#catalogue_duplicates[@]} check id(s) are declared more than once." >&2
elif [ "$FAIL" -eq 0 ] && [ "${#undeclared[@]}" -ne 0 ]; then
    echo "REJECTED — ${#undeclared[@]} check(s) ran that this gate does not declare." >&2
elif [ "$FAIL" -eq 0 ] && [ "${#missing[@]}" -ne 0 ]; then
    echo "REJECTED — no assertion failed, but ${#missing[@]} declared check(s) never ran." >&2
elif [ "$FAIL" -eq 0 ] && [ "$count_disagrees" -ne 0 ]; then
    echo "REJECTED — the executed and declared check counts disagree, for a reason this" >&2
    echo "  gate has no name for. Read the COVERAGE line above." >&2
else
    echo "REJECTED — $FAIL assertion(s) failed, $PASS passed." >&2
fi
echo "  Run deploy.bash, then re-run this gate." >&2
echo "=============================================================="
exit 1
