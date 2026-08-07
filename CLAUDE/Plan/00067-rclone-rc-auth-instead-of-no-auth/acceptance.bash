#!/usr/bin/env bash
#
# Plan 00067 — rclone RC auth acceptance gate.
#
# Unlike triage.bash (which gathers facts and renders no verdict), this script
# DECIDES: it exits 0 only when every success criterion in PLAN.md holds.
#
# Read-only with one deliberate exception: check 6 calls the client scripts'
# --help, which changes nothing. It does NOT run `rclone-cache-warm --fast`
# (that would issue a real vfs/refresh) — the operator is told to do that.
#
# Writes a REDACTED report into this plan's logs/ so the failure can be read
# without copy-pasting a terminal. The RC password is substituted out before
# anything is written, and the finished report is checked for leakage.
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
  5. an AUTHENTICATED core/stats SUCCEEDS (the capability kept)
  6. the client scripts are installed and runnable

Exits 1 with a numbered list of what failed otherwise.

Writes: <this plan folder>/logs/rclone-rc-auth-acceptance.log  (password redacted)
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

# --- read the credential BEFORE any output, so the redaction filter can be
# --- installed before the first byte is written.
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

# The report lives in THIS plan's folder, so it travels with the plan into
# Completed/. Resolved from the script's own location, not the repo root, so
# that move does not break it. Plan logs/ dirs are gitignored — this is a
# public repo and the report is a dump of live host state.
PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORTS_DIR="$PLAN_DIR/logs"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/rclone-rc-auth-acceptance.log"

exec > >(redact | tee "$LOG") 2>&1

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
    echo "ERROR: rclone is not installed — deploy play-rclone.yml first."
    exit 1
fi
echo "rclone: $(rclone version | head -n 1)"
echo "units : $UNIT_DIR"
echo

if ! compgen -G "$UNIT_DIR/rclone-*.service" > /dev/null; then
    echo "ERROR: no rclone-*.service units found in $UNIT_DIR."
    echo "  Nothing to accept. Deploy play-rclone.yml first."
    exit 1
fi

# --- 1 & 2: unit configuration ----------------------------------------------
echo "## 1/2. Mount unit configuration"
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

# Always record what the units actually say — when a check above fails this is
# the evidence needed to tell "deploy did not run" from "deploy rendered
# something unexpected". Costs nothing when everything passes.
echo "### unit RC configuration as rendered (evidence)"
for unit in "$UNIT_DIR"/rclone-*.service; do
    echo "== $(basename "$unit")"
    if ! grep -E '^(EnvironmentFile|ExecStart)=' "$unit"; then
        echo "   (no EnvironmentFile or ExecStart lines found)"
    fi
    echo
done

# --- unit runtime state: a stale running unit explains an auth mismatch ------
echo "### unit runtime state (evidence)"
for unit in "$UNIT_DIR"/rclone-*.service; do
    name="$(basename "$unit")"
    # An inactive unit makes systemctl exit non-zero; that status IS the datum.
    if state="$(systemctl --user is-active "$name" 2>&1)"; then
        echo "  $name: $state"
    else
        echo "  $name: $state (systemctl exited non-zero)"
    fi
    if since="$(systemctl --user show "$name" -p ActiveEnterTimestamp --value 2>&1)"; then
        echo "      active since: ${since:-(unknown)}"
    fi
done
echo "  NOTE: if a unit is older than the last deploy, systemd is still running"
echo "        the PREVIOUS ExecStart — the file on disk is not what is live."
echo

# --- 3: credential file ------------------------------------------------------
echo "## 3. Credential file"
if [ ! -e "$RC_AUTH_FILE" ]; then
    fail "credential file missing: $RC_AUTH_FILE"
else
    mode="$(stat -c '%a' "$RC_AUTH_FILE")"
    if [ "$mode" = "600" ]; then
        pass "credential file is mode 0600"
    else
        fail "credential file is mode $mode, expected 600"
    fi
    if [ -n "$RC_USER" ] && [ -n "$RC_PASS" ]; then
        pass "credential file has both RCLONE_RC_USER and RCLONE_RC_PASS"
        echo "        RCLONE_RC_USER = $RC_USER"
        echo "        RCLONE_RC_PASS = <set, ${#RC_PASS} chars>"
    else
        fail "credential file is missing RCLONE_RC_USER and/or RCLONE_RC_PASS"
        echo "        keys present:"
        awk -F= '/^[A-Z_]+=/ { print "          " $1 }' "$RC_AUTH_FILE"
    fi
fi
echo

# --- resolve a port to talk to ----------------------------------------------
PORT=""
for unit in "$UNIT_DIR"/rclone-*.service; do
    if found_port="$(grep -o -- '--rc-addr=localhost:[0-9]*' "$unit" | cut -d: -f2)"; then
        if [ -n "$found_port" ]; then
            PORT="$found_port"
            break
        fi
    fi
done

if [ -z "$PORT" ]; then
    echo "ERROR: no --rc-addr port found in any unit — cannot test the RC API."
    echo "  The mounts are running without --rc, or --rc-addr is rendered in a"
    echo "  form this script does not recognise. The ExecStart lines above show"
    echo "  what is actually there."
    exit 1
fi

echo "Using RC on localhost:$PORT"
echo

# --- 4: the security fix -----------------------------------------------------
echo "## 4. Unauthenticated access is refused"
if unauth_out="$(rclone rc --url="http://localhost:${PORT}" config/dump 2>&1)"; then
    fail "config/dump answered WITHOUT credentials — the RC is still open"
    echo "        (this endpoint returns the remote's OAuth token)"
    echo "        response was ${#unauth_out} bytes — NOT shown, it contains secrets"
else
    pass "config/dump refused without credentials"
    echo "        refusal reason: $(printf '%s' "$unauth_out" | tr '\n' ' ')"
fi
echo

# --- 5: the capability kept --------------------------------------------------
echo "## 5. Authenticated access works"
if [ -z "$RC_USER" ] || [ -z "$RC_PASS" ]; then
    fail "cannot test authenticated access — no credential available"
else
    if reason="$(rclone rc --user "$RC_USER" --pass "$RC_PASS" \
        --url="http://localhost:${PORT}" core/stats 2>&1)"; then
        pass "authenticated core/stats succeeded"
    else
        fail "authenticated core/stats failed: $(printf '%s' "$reason" | tr '\n' ' ')"
        echo "        A 401 here with a correct-looking credential file usually"
        echo "        means the RUNNING unit predates the credential — restart it:"
        echo "          systemctl --user restart <unit>"
    fi
    # vfs/refresh is the endpoint rclone-cache-warm needs; report it explicitly
    # rather than inferring it from core/stats.
    if reason="$(rclone rc --user "$RC_USER" --pass "$RC_PASS" \
        --url="http://localhost:${PORT}" vfs/refresh 2>&1)"; then
        pass "authenticated vfs/refresh succeeded (rclone-cache-warm can work)"
    else
        fail "authenticated vfs/refresh failed: $(printf '%s' "$reason" | tr '\n' ' ')"
    fi
fi
echo

# --- 6: the client scripts ---------------------------------------------------
echo "## 6. Client scripts"

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

for c in rclone-cache-status rclone-tail rclone-cache-warm; do
    if command -v "$c" > /dev/null; then
        run_client "$c --help" "$c" --help
    else
        fail "$c is not installed"
    fi
done
echo "  NOTE: --fast issues a real vfs/refresh; run it yourself to confirm"
echo "        the authenticated path end-to-end:  rclone-cache-warm --fast"
echo

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

# --- verdict -----------------------------------------------------------------
echo "=============================================================="
if [ "${#FAILURES[@]}" -eq 0 ]; then
    echo "ACCEPTED — $PASSES checks passed, 0 failed."
    echo
    echo "Plan 00067 success criteria are met:"
    echo "  * no mount unit serves the RC API without authentication"
    echo "  * config/dump (which returns the remote's OAuth token) is refused"
    echo "  * authenticated calls work, so the tooling keeps functioning"
    echo
    echo "Report: $LOG"
    echo "=============================================================="
    verify_no_secret
    exit 0
fi

echo "NOT ACCEPTED — $PASSES passed, ${#FAILURES[@]} failed:"
for f in "${FAILURES[@]}"; do
    echo "  * $f"
done
echo
echo "Fix by re-running the playbook (never by editing the unit by hand):"
echo "  ansible-playbook playbooks/imports/optional/common/play-rclone.yml"
echo
echo "Report: $LOG"
echo "  This file is inside the plan folder, so an agent can read it directly —"
echo "  no need to copy-paste terminal output."
echo "=============================================================="
verify_no_secret
exit 1
