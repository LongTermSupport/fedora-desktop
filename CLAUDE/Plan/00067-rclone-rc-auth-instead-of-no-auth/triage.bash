#!/usr/bin/env bash
#
# Plan 00067 — rclone RC auth triage.
#
# READ-ONLY. Starts nothing, stops nothing, changes no file. Safe to run
# repeatedly on a live system, before or after deploying play-rclone.yml.
#
# Gathers facts only — it renders NO verdict. The pass/fail gate is
# acceptance.bash. Run this to see what the live system currently does; run
# acceptance.bash to decide whether the deploy succeeded.
#
# The RC credential is redacted by substitution before anything is written, and
# the report is checked for leakage afterwards (see redact()/verify_no_secret()).
#
# Usage: triage.bash [--help]

set -euo pipefail

# --- argument parsing BEFORE any environment resolution (PlanTriage.md) -------
for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00067 — rclone RC auth triage (read-only)

Usage: triage.bash [--help]

Reports, without changing anything:
  * every rclone mount unit, and whether its ExecStart still carries
    --rc-no-auth or now uses EnvironmentFile
  * the credential file's existence, mode and key presence (never its value)
  * an endpoint auth matrix: which RC endpoints answer unauthenticated, which
    answer authenticated, and which refuse both

Writes: untracked/reports/rclone-rc-auth-triage.log
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

# --- environment -------------------------------------------------------------
REPO_ROOT="$(git rev-parse --show-toplevel)"
REPORTS_DIR="$REPO_ROOT/untracked/reports"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/rclone-rc-auth-triage.log"

RC_AUTH_FILE="$HOME/.config/rclone/rc-auth.env"
UNIT_DIR="$HOME/.config/systemd/user"

# Read the credential (if deployed) so we can (a) test authenticated calls and
# (b) redact the value out of everything we write. Read, never sourced, so a
# malformed file cannot execute code.
RC_USER=""
RC_PASS=""
if [ -r "$RC_AUTH_FILE" ]; then
    RC_USER="$(awk -F= '$1 == "RCLONE_RC_USER" { print $2 }' "$RC_AUTH_FILE")"
    RC_PASS="$(awk -F= '$1 == "RCLONE_RC_PASS" { print $2 }' "$RC_AUTH_FILE")"
fi

# Substitute the secret anywhere it appears — never anchored line-dropping,
# which is defeated by payload position (the Plan 00066 lesson).
redact() {
    if [ -n "$RC_PASS" ]; then
        awk -v secret="$RC_PASS" '{ gsub(secret, "<redacted>"); print }'
    else
        cat
    fi
}

exec > >(redact | tee "$LOG") 2>&1

# A non-zero exit is DATA, not a failure: capture it and carry on.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

echo "=============================================================="
echo "Plan 00067 — rclone RC auth triage"
echo "=============================================================="
echo

# --- dependency check: a missing tool is an IaC gap, not a skip ---------------
if ! command -v rclone > /dev/null; then
    echo "ERROR: rclone is not installed." >&2
    echo "  It is deployed by play-rclone.yml. Install it with:" >&2
    echo "    ansible-playbook playbooks/imports/optional/common/play-rclone.yml" >&2
    echo "  Do NOT install it by hand." >&2
    exit 1
fi

probe "rclone version" rclone version

# --- 1. mount units ----------------------------------------------------------
echo "## 1. rclone mount units"
echo

list_units() {
    local unit
    if ! compgen -G "$UNIT_DIR/rclone-*.service" > /dev/null; then
        echo "No rclone-*.service units found in $UNIT_DIR"
        echo "The mount playbook has probably never been deployed on this host."
        return 0
    fi
    for unit in "$UNIT_DIR"/rclone-*.service; do
        echo "== $(basename "$unit")"
        echo "-- ExecStart RC flags:"
        if ! grep -o '\-\-rc[a-z-]*[= ][^ ]*\|--rc\b' "$unit"; then
            echo "   (no --rc flags found)"
        fi
        echo "-- EnvironmentFile:"
        if ! grep '^EnvironmentFile=' "$unit"; then
            echo "   (none — credential is NOT loaded into this unit)"
        fi
        echo "-- carries --rc-no-auth:"
        if grep -q -- '--rc-no-auth' "$unit"; then
            echo "   YES — this unit serves the RC API with no authentication"
        else
            echo "   no"
        fi
        echo
    done
}
probe "mount unit RC configuration" list_units

show_unit_state() {
    local unit name out
    if ! compgen -G "$UNIT_DIR/rclone-*.service" > /dev/null; then
        echo "(no units to query)"
        return 0
    fi
    for unit in "$UNIT_DIR"/rclone-*.service; do
        name="$(basename "$unit")"
        echo "== $name"
        # An inactive unit makes systemctl exit non-zero; that status IS the
        # datum, so capture it rather than letting set -e end the run.
        if out="$(systemctl --user status "$name" --no-pager -l -n 5 2>&1)"; then
            echo "$out"
        else
            echo "(systemctl exited non-zero — unit not running)"
            echo "$out"
        fi
        echo
    done
}
probe "mount unit runtime state" show_unit_state

# --- 2. credential file ------------------------------------------------------
echo "## 2. RC credential file"
echo

describe_credential() {
    if [ ! -e "$RC_AUTH_FILE" ]; then
        echo "NOT PRESENT: $RC_AUTH_FILE"
        echo "Expected before deploy; play-rclone.yml generates it."
        return 0
    fi
    echo "path : $RC_AUTH_FILE"
    echo "mode : $(stat -c '%a %U:%G' "$RC_AUTH_FILE")"
    echo "keys :"
    awk -F= '/^RCLONE_RC_(USER|PASS)=/ { print "   " $1 " = <set, " length($2) " chars>" }' "$RC_AUTH_FILE"
    if [ -z "$RC_USER" ] || [ -z "$RC_PASS" ]; then
        echo "WARNING: one or both keys are empty — authenticated calls below will fail."
    fi
}
probe "credential file" describe_credential

# --- 3. endpoint auth matrix -------------------------------------------------
echo "## 3. endpoint auth matrix"
echo "###   READ THIS FOR: which endpoints need credentials and which do not."
echo "###   'unauth ok' = answered with no credentials."
echo "###   'auth ok'   = answered once credentials were supplied."
echo "###   A gated endpoint answering 'unauth ok' means the RC is still open."
echo

rc_ports() {
    local unit
    if ! compgen -G "$UNIT_DIR/rclone-*.service" > /dev/null; then
        return 0
    fi
    for unit in "$UNIT_DIR"/rclone-*.service; do
        grep -o '\-\-rc-addr=localhost:[0-9]*' "$unit" | cut -d: -f2
    done
}

# Capture combined output rather than discarding it — the captured reason is
# what distinguishes "refused for auth" from "nothing is listening".
probe_endpoint() {
    local port="$1" endpoint="$2" label state_unauth state_auth reason
    label="$(printf '%-18s' "$endpoint")"

    if reason="$(rclone rc --url="http://localhost:${port}" "$endpoint" 2>&1)"; then
        state_unauth="unauth ok "
    else
        state_unauth="unauth REF"
    fi

    if [ -n "$RC_USER" ] && [ -n "$RC_PASS" ]; then
        if reason="$(rclone rc --user "$RC_USER" --pass "$RC_PASS" \
            --url="http://localhost:${port}" "$endpoint" 2>&1)"; then
            state_auth="auth ok "
        else
            state_auth="auth REF"
        fi
    else
        state_auth="auth n/a (no credential file)"
    fi

    echo "  $label  $state_unauth   $state_auth"
    # Surface the reason only when BOTH paths failed — that is the case a
    # reader cannot diagnose from the matrix alone.
    if [ "$state_unauth" = "unauth REF" ] && [ "$state_auth" = "auth REF" ]; then
        echo "      reason: $(printf '%s' "$reason" | tr '\n' ' ')"
    fi
}

endpoint_matrix() {
    local port found=0
    for port in $(rc_ports | sort -u); do
        found=1
        echo "== RC on localhost:$port"
        # The endpoints the tooling actually polls, then the two this plan
        # needs authenticated, then the one that makes no-auth dangerous.
        probe_endpoint "$port" "core/stats"
        probe_endpoint "$port" "vfs/stats"
        probe_endpoint "$port" "core/stats-reset"
        probe_endpoint "$port" "vfs/refresh"
        probe_endpoint "$port" "config/dump"
        echo
    done
    if [ "$found" -eq 0 ]; then
        echo "ERROR: no --rc-addr port found in any unit — nothing to probe." >&2
        echo "  Either the mounts are not deployed, or they run without --rc." >&2
        return 1
    fi
}
probe "endpoint auth matrix" endpoint_matrix

# --- 4. client scripts -------------------------------------------------------
echo "## 4. client scripts present on this host"
echo

list_clients() {
    local c
    for c in rclone-cache-warm rclone-cache-status rclone-tail ftp-camera; do
        if command -v "$c" > /dev/null; then
            echo "  present : $c"
        else
            echo "  MISSING : $c  (deployed by its playbook — do not create by hand)"
        fi
    done
}
probe "client scripts" list_clients

# --- redaction verification --------------------------------------------------
# Assume the redaction is wrong and check. A leaked credential on disk is worse
# than losing the report.
verify_no_secret() {
    if [ -z "$RC_PASS" ]; then
        return 0
    fi
    if grep -qF "$RC_PASS" "$LOG"; then
        rm -f "$LOG"
        echo "ERROR: redaction check FAILED — the RC password reached the report." >&2
        echo "  Report deleted. Fix redact() before re-running." >&2
        return 1
    fi
    return 0
}

echo "=============================================================="
echo "END OF TRIAGE"
echo
echo "READ THIS FOR the decisive picture: section 3 (endpoint auth matrix)."
echo "  Before deploy  — if the live unit has neither --rc-no-auth nor a"
echo "                   credential, expect EVERY endpoint 'unauth REF':"
echo "                   rclone requires auth by default, so the RC refuses"
echo "                   everything and rclone-cache-warm --fast cannot work."
echo "  After deploy   — expect core/stats and vfs/stats 'unauth ok', and"
echo "                   vfs/refresh + config/dump 'unauth REF' but 'auth ok'."
echo
echo "Report: $LOG"
echo "=============================================================="

verify_no_secret
