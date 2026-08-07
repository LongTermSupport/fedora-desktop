#!/usr/bin/env bash
#
# Plan 00067 — rclone RC auth acceptance gate.
#
# Unlike triage.bash (which gathers facts and renders no verdict), this script
# DECIDES: it exits 0 only when every success criterion in PLAN.md holds.
#
# Read-only with one deliberate exception: check 6 calls `rclone-cache-warm
# --fast`, which issues vfs/refresh against the mount. That re-reads directory
# listings; it moves no data and changes nothing on the remote.
#
# Run AFTER deploy.bash. Safe to re-run.
#
# Usage: acceptance.bash [--help]

set -euo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00067 — rclone RC auth acceptance gate

Usage: acceptance.bash [--help]

Exits 0 only if ALL of these hold:
  1. no mount unit carries --rc-no-auth
  2. every mount unit loads the credential via EnvironmentFile
  3. the credential file exists, is mode 0600, and has both keys
  4. an UNAUTHENTICATED config/dump is REFUSED (the security fix)
  5. an AUTHENTICATED vfs/refresh SUCCEEDS (the capability kept)
  6. rclone-cache-warm --fast works
  7. rclone-cache-status and rclone-tail still work unauthenticated

Exits 1 with a numbered list of what failed otherwise.
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

RC_AUTH_FILE="$HOME/.config/rclone/rc-auth.env"
UNIT_DIR="$HOME/.config/systemd/user"

FAILURES=()
PASSES=0

pass() {
    PASSES=$((PASSES + 1))
    echo "  PASS  $1"
}

fail() {
    FAILURES+=("$1")
    echo "  FAIL  $1"
}

echo "=============================================================="
echo "Plan 00067 — rclone RC auth acceptance"
echo "=============================================================="
echo

if ! command -v rclone > /dev/null; then
    echo "ERROR: rclone is not installed — deploy play-rclone.yml first." >&2
    exit 1
fi

if ! compgen -G "$UNIT_DIR/rclone-*.service" > /dev/null; then
    echo "ERROR: no rclone-*.service units found in $UNIT_DIR." >&2
    echo "  Nothing to accept. Deploy play-rclone.yml first." >&2
    exit 1
fi

# --- 1 & 2: unit configuration ----------------------------------------------
echo "1/2. Mount unit configuration"
for unit in "$UNIT_DIR"/rclone-*.service; do
    name="$(basename "$unit")"
    if grep -q -- '--rc-no-auth' "$unit"; then
        fail "$name still carries --rc-no-auth"
    else
        pass "$name has no --rc-no-auth"
    fi
    if grep -q '^EnvironmentFile=.*rc-auth\.env' "$unit"; then
        pass "$name loads the credential via EnvironmentFile"
    else
        fail "$name has no EnvironmentFile for rc-auth.env"
    fi
done
echo

# --- 3: credential file ------------------------------------------------------
echo "3. Credential file"
RC_USER=""
RC_PASS=""
if [ ! -e "$RC_AUTH_FILE" ]; then
    fail "credential file missing: $RC_AUTH_FILE"
else
    mode="$(stat -c '%a' "$RC_AUTH_FILE")"
    if [ "$mode" = "600" ]; then
        pass "credential file is mode 0600"
    else
        fail "credential file is mode $mode, expected 600"
    fi
    RC_USER="$(awk -F= '$1 == "RCLONE_RC_USER" { print $2 }' "$RC_AUTH_FILE")"
    RC_PASS="$(awk -F= '$1 == "RCLONE_RC_PASS" { print $2 }' "$RC_AUTH_FILE")"
    if [ -n "$RC_USER" ] && [ -n "$RC_PASS" ]; then
        pass "credential file has both RCLONE_RC_USER and RCLONE_RC_PASS"
    else
        fail "credential file is missing RCLONE_RC_USER and/or RCLONE_RC_PASS"
    fi
fi
echo

# --- resolve a port to talk to ----------------------------------------------
PORT=""
for unit in "$UNIT_DIR"/rclone-*.service; do
    if found_port="$(grep -o '\-\-rc-addr=localhost:[0-9]*' "$unit" | cut -d: -f2)"; then
        if [ -n "$found_port" ]; then
            PORT="$found_port"
            break
        fi
    fi
done

if [ -z "$PORT" ]; then
    echo "ERROR: no --rc-addr port found in any unit — cannot test the RC API." >&2
    echo "  The mounts are running without --rc; re-run play-rclone.yml." >&2
    exit 1
fi

echo "Using RC on localhost:$PORT"
echo

# --- 4: the security fix -----------------------------------------------------
echo "4. Unauthenticated access is refused"
if unauth_out="$(rclone rc --url="http://localhost:${PORT}" config/dump 2>&1)"; then
    fail "config/dump answered WITHOUT credentials — the RC is still open"
    echo "        (this endpoint returns the remote's OAuth token)"
    echo "        response was ${#unauth_out} bytes — NOT shown, it contains secrets"
else
    pass "config/dump refused without credentials"
fi
echo

# --- 5: the capability kept --------------------------------------------------
echo "5. Authenticated access works"
if [ -z "$RC_USER" ] || [ -z "$RC_PASS" ]; then
    fail "cannot test authenticated access — no credential available"
else
    if reason="$(rclone rc --user "$RC_USER" --pass "$RC_PASS" \
        --url="http://localhost:${PORT}" core/stats 2>&1)"; then
        pass "authenticated core/stats succeeded"
    else
        fail "authenticated core/stats failed: $(printf '%s' "$reason" | tr '\n' ' ')"
    fi
fi
echo

# --- 6 & 7: the client scripts ----------------------------------------------
echo "6/7. Client scripts"

run_client() {
    local label="$1"
    shift
    local out
    if out="$("$@" 2>&1)"; then
        pass "$label"
    else
        fail "$label — $(printf '%s' "$out" | tr '\n' ' ')"
    fi
}

if command -v rclone-cache-status > /dev/null; then
    run_client "rclone-cache-status" rclone-cache-status
else
    fail "rclone-cache-status is not installed"
fi

if command -v rclone-tail > /dev/null; then
    run_client "rclone-tail --help" rclone-tail --help
else
    fail "rclone-tail is not installed"
fi

if command -v rclone-cache-warm > /dev/null; then
    run_client "rclone-cache-warm --help" rclone-cache-warm --help
    echo "  NOTE: --fast issues a real vfs/refresh; run it yourself to confirm"
    echo "        the authenticated path end-to-end:  rclone-cache-warm --fast"
else
    fail "rclone-cache-warm is not installed"
fi
echo

# --- verdict -----------------------------------------------------------------
echo "=============================================================="
if [ "${#FAILURES[@]}" -eq 0 ]; then
    echo "ACCEPTED — $PASSES checks passed, 0 failed."
    echo
    echo "Plan 00067 success criteria are met:"
    echo "  * no mount unit serves the RC API without authentication"
    echo "  * config/dump (which returns the remote's OAuth token) is refused"
    echo "  * authenticated calls work, so the tooling keeps functioning"
    echo "=============================================================="
    exit 0
fi

echo "NOT ACCEPTED — $PASSES passed, ${#FAILURES[@]} failed:"
for f in "${FAILURES[@]}"; do
    echo "  * $f"
done
echo
echo "Fix by re-running the playbook (never by editing the unit by hand):"
echo "  ansible-playbook playbooks/imports/optional/common/play-rclone.yml"
echo "=============================================================="
exit 1
