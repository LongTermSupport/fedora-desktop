#!/usr/bin/env bash
#
# Plan 00099 — establish why rclone RC clients stopped working.
#
# READ-ONLY. Starts nothing, stops nothing, writes nothing outside its own
# log. Safe to re-run on a live system, mid-incident, as many times as needed.
#
# Writes its run log to untracked/plan-runs/00099/triage/<timestamp>/, via
# plan_start_log — never beside the plan's tracked files, which is where it used
# to go and where it would have ridden into Completed/ untracked.
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
Plan 00099 — triage the rclone remote-control clients

Usage: triage.bash [--help]

Gathers, without changing anything:
  * whether the RC answers WITHOUT credentials, and WITH them
  * whether each repo-owned rclone/ftp helper is byte-identical to its
    deployed copy under ~/.local/bin/
  * which helpers pass credentials to `rclone rc` and which do not
  * mount unit health and VFS cache state

Renders NO verdict — that is acceptance.bash's job. The run log path is
printed at the end; it lands under untracked/plan-runs/ and is UNSCRUBBED.
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

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
# Was `git rev-parse --show-toplevel`, which answers about the CWD rather than the script,
# and a plan-local `logs/` tree written with `exec > >(tee …)`. Both are forbidden by
# CLAUDE/PlanScriptStandards.md R1 and R4, and both had consequences here: the log
# directory is gitignored, so it would have ridden into Completed/ as an untracked
# leftover, and a `>(…)` process substitution cannot be waited on — so the last buffered
# chunk, the lines written as a run was dying, could be missing from the file.
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"
plan_mode gather
# R2. Every probe below reads host state — fuse mounts, `systemctl --user`, the deployed
# ~/.local/bin tree, /etc/ftp-camera. Run inside the CCY container this script does not
# fail: it completes and reports "rclone: command not found", "credential file NOT
# READABLE" and "ftp-camera ABSENT" as facts, with nothing in the output saying they are
# facts about the wrong machine. A confident wrong picture is worse than no picture.
plan_require_host "every probe reads host state — rclone mounts, user units, and the deployed ~/.local/bin tree"
plan_start_log auto

REPO_ROOT="$PLAN_REPO_ROOT"
BIN_SRC="$REPO_ROOT/files/home/.local/bin"
BIN_DEPLOYED="$HOME/.local/bin"
RC_AUTH_FILE="$HOME/.config/rclone/rc-auth.env"
# DISCOVERED, not assumed — the same defect S3 found in ftp-camera. play-rclone.yml gives
# each mount rc_port_base + mount_index, so 5572 is the FIRST mount's port only, and a
# triage that probes it on a host whose first mount is absent reports "rc unreachable" as a
# fact about a mount it never addressed. Empty when no mount is running, which every probe
# below reports rather than papering over.
RC_ADDR=""
RC_SOURCE_MOUNT=""
if RC_SOURCE_MOUNT="$(findmnt -n -o TARGET -t fuse.rclone | awk 'NR==1')" \
    && [[ -n "${RC_SOURCE_MOUNT}" ]]; then
    # shellcheck source-path=SCRIPTDIR
    # shellcheck source=../../../files/home/.local/bin/rclone-rc-auth.bash
    source "$PLAN_REPO_ROOT/files/home/.local/bin/rclone-rc-auth.bash"
    if ! RC_ADDR="$(rclone_rc_addr_for_mount "${RC_SOURCE_MOUNT}")"; then
        RC_ADDR=""
    fi
fi

# A non-zero exit is DATA, not a failure. Capture it and carry on.
#
# This is why the probes are NOT plan_gather_legs: "core/stats WITHOUT credentials returned
# 401" is the plan's central finding and exits 1, so a leg would record the run's own
# discovery as a failed leg and exit non-zero on a perfect run. A leg means the gathering
# broke; a non-zero probe means the host answered.
#
# `"$@"` here is an EXTERNAL command at every call site, never a shell function. A function
# invoked this way is an indirection shellcheck cannot follow, and in a script that ends in
# plan_finish — so never falls off the end — every such body is then reported unreachable:
# 114 SC2317s, measured, and suppressions are banned (R11). The function-backed probes below
# therefore call their function directly and hand the result to emit_probe.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    emit_probe "$label" "$rc" "$out"
}

# emit_probe <label> <rc> <output> — render one stanza from an already-captured result.
emit_probe() {
    local label="$1" rc="$2" out="$3"
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
echo "Plan 00099 triage — rclone RC clients"
echo "=============================================================="
echo

# The address is STATED, not merely used. Undiscovered, it leaves every RC probe below
# calling `--url="http://"`, and the connection error that produces reads — to someone
# chasing "does unauthenticated core/stats return 401?" — as evidence about the RC. It is
# not: it is evidence that this script never addressed an RC at all.
if [ -n "$RC_ADDR" ]; then
    printf 'RC address: %s  (discovered from the mount at %s)\n' "$RC_ADDR" "$RC_SOURCE_MOUNT"
else
    echo "RC address: NOT DISCOVERED — no rclone mount on this host publishes one."
    echo "  Every RC probe below is VOID: it addresses no RC, and its error says"
    echo "  nothing about authentication."
fi

# Recorded as a leg, not a probe, because its failure does not describe the host — it means
# the fact-finding never happened. plan_gather_leg names it in plan_finish's summary and
# drives a non-zero exit, so an incomplete run cannot be read as a complete one. The leg is
# `test`, an external command: a shell function passed here is an indirection shellcheck
# cannot follow, and would be reported unreachable (see probe() below).
plan_gather_leg "RC address discovery" test -n "$RC_ADDR"
echo

# --- the RC credential, by shape only ----------------------------------------
show_rc_credential_shape() {
    if [ ! -r "$RC_AUTH_FILE" ]; then
        echo "$RC_AUTH_FILE: NOT READABLE (or absent)"
        return 0
    fi
    ls -l "$RC_AUTH_FILE"
    echo "-- keys and value LENGTHS (values deliberately never printed) --"
    # Everything after the FIRST `=` is the value, exactly as rc_authed below parses it.
    # `length($2)` truncated at the second `=`, so a password containing one was reported
    # SHORTER than it is — and a length is the only evidence this report carries about the
    # credential, so the one figure it prints has to be the real one.
    awk -F= '{ printf "%s=<%d chars>\n", $1, length(substr($0, index($0, "=") + 1)) }' "$RC_AUTH_FILE"
}
if probe_out="$(show_rc_credential_shape 2>&1)"; then probe_rc=0; else probe_rc=$?; fi
emit_probe "RC credential file shape" "$probe_rc" "$probe_out"

# --- does the RC demand authentication? --------------------------------------
# The decisive pair. READ THIS FOR: whether the endpoints the helper scripts
# poll (core/stats, vfs/stats) require a credential. Plan 00094 assumed not.
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
    # Everything after the FIRST `=` is the value; `print $2` would truncate at
    # the second. A truncated password fails as a 401, which this very triage
    # script would then report as an auth-enforcement finding rather than a
    # parsing bug.
    u=$(awk -F= '$1 == "RCLONE_RC_USER" { print substr($0, index($0, "=") + 1) }' "$RC_AUTH_FILE")
    p=$(awk -F= '$1 == "RCLONE_RC_PASS" { print substr($0, index($0, "=") + 1) }' "$RC_AUTH_FILE")
    if [ -z "$u" ] || [ -z "$p" ]; then
        echo "(credential file missing RCLONE_RC_USER or RCLONE_RC_PASS)"
        return 1
    fi
    # Credentials via the environment, not argv: `--pass <secret>` is visible in
    # `ps` output to every user on the box for the life of the call. rclone maps
    # its client --user/--pass flags to RCLONE_USER/RCLONE_PASS.
    RCLONE_USER="$u" RCLONE_PASS="$p" rclone rc --url="http://${RC_ADDR}" "$endpoint"
}
if probe_out="$(rc_authed core/stats 2>&1)"; then probe_rc=0; else probe_rc=$?; fi
emit_probe "core/stats WITH credentials" "$probe_rc" "$probe_out"
if probe_out="$(rc_authed vfs/stats 2>&1)"; then probe_rc=0; else probe_rc=$?; fi
emit_probe "vfs/stats WITH credentials" "$probe_rc" "$probe_out"

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
if probe_out="$(show_helper_matrix 2>&1)"; then probe_rc=0; else probe_rc=$?; fi
emit_probe "helper matrix (deployment drift + RC auth awareness)" "$probe_rc" "$probe_out"

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
if probe_out="$(show_drift_detail 2>&1)"; then probe_rc=0; else probe_rc=$?; fi
emit_probe "drift detail" "$probe_rc" "$probe_out"

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
if probe_out="$(run_deployed_cache_status 2>&1)"; then probe_rc=0; else probe_rc=$?; fi
emit_probe "rclone-cache-status (deployed) live run" "$probe_rc" "$probe_out"

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
if probe_out="$(show_mount_units 2>&1)"; then probe_rc=0; else probe_rc=$?; fi
emit_probe "rclone mount units" "$probe_rc" "$probe_out"

probe "ftp-camera config" cat /etc/ftp-camera/config
probe "upload dir" ls -la /srv/ftp-camera
probe "disk usage" df -h "$HOME" /srv

echo "=============================================================="
echo "READ THIS FIRST:"
echo "  1. 'core/stats WITHOUT credentials' — a 401 there means every"
echo "     un-migrated helper is dead, whatever else looks healthy."
echo "  2. 'helper matrix' — RC-CALLS > 0 with AUTH-REFS = 0 is a broken"
echo "     helper; DEPLOYED=DRIFTED is a repo fix that never shipped."
echo "  3. If 'RC address' at the top says NOT DISCOVERED, items 1 and 2's RC"
echo "     lines are void — read the mount section instead."
echo "=============================================================="

# plan_finish prints the run log path and the failed-leg summary, and exits with a status
# that agrees with that text. It must be last: it terminates the run, so anything below it
# would be dead code. This replaced `echo "Full report: $LOG"`, whose variable the _planlib
# conversion had removed — under `set -u` the script died on its own last line, every run,
# and neither shellcheck nor qa-all.bash could see it because no gate executes this script.
plan_finish