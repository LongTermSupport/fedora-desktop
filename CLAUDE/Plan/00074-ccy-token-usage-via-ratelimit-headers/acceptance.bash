#!/usr/bin/env bash
#
# Plan 00074 — acceptance: did the deploy actually land, and is the feature wired?
#
# HOST-ONLY, read-only, safe to re-run. Spends nothing: it inspects the deployed
# files, it does not call the API. Pressing `u` in ccy is what costs quota.
#
# WHY THIS EXISTS: Plan 00073's deploy ran green against the WRONG play, so the
# repo said "fixed" while the host kept the old library, and nobody noticed for a
# week. `scripts/qa-deployed-drift.bash` cannot catch that — it only covers
# files/home/.local/bin/, not files/var/local/claude-yolo/. Until that gate is
# widened, this script is the check.
#
# Unlike triage.bash (gather facts, no verdict), this renders a PASS/FAIL verdict
# and exits non-zero on failure.

set -euo pipefail

usage() {
    cat <<'EOF'
Plan 00074 acceptance — confirm the on-demand usage feature is deployed

USAGE:
    acceptance.bash [-h|--help]

CHECKS:
    1. The deployed library matches the repo library byte for byte
    2. The deployed library carries the 1.9.0 version header
    3. The deployed launcher reports CCY_VERSION 3.34.0
    4. The deployed library actually defines the usage functions and the
       `u)` menu option — not just a matching checksum

EXIT:
    0 = every check passed; start ccy and press `u`
    1 = something is not deployed; the failing check names the remedy
EOF
}

if [ $# -gt 0 ]; then
    case "$1" in
        -h|--help) usage; exit 0 ;;
        *) echo "ERROR: unknown option: $1" >&2; echo "Try: acceptance.bash --help" >&2; exit 1 ;;
    esac
fi

if [ -f /.dockerenv ] || [ -d /workspace/.claude ]; then
    echo "ERROR: this looks like a CCY container — there is no deployed state here." >&2
    echo "  Run this script on your HOST system instead." >&2
    exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
REPO_LIB="$REPO_ROOT/files/var/local/claude-yolo/lib/token-management.bash"
DEPLOYED_LIB="/var/local/claude-yolo/lib/token-management.bash"
DEPLOYED_CCY="/var/local/claude-yolo/claude-yolo"

FAILED=0

fail() {
    echo "  ✗ $1" >&2
    FAILED=1
}
pass() {
    echo "  ✓ $1"
}

echo "Plan 00074 acceptance"
echo "════════════════════════════════════════════════════════════════════════"
echo ""

# 1 — deployed == repo
echo "1. Deployed library matches the repo"
if [ ! -f "$DEPLOYED_LIB" ]; then
    fail "not deployed at all: $DEPLOYED_LIB"
    echo "     Remedy: run this plan's deploy.bash" >&2
elif cmp -s "$REPO_LIB" "$DEPLOYED_LIB"; then
    pass "identical to $REPO_LIB"
else
    fail "DRIFT — the host is running a different library to the repo"
    echo "     This is the Plan 00073 failure mode. Remedy: run deploy.bash" >&2
fi
echo ""

# 2 — version header
echo "2. Deployed library version"
if [ -f "$DEPLOYED_LIB" ] && grep -q '^# Version: 1\.9\.0' "$DEPLOYED_LIB"; then
    pass "1.9.0"
else
    fail "not 1.9.0 — an older library is deployed"
    echo "     Remedy: run deploy.bash" >&2
fi
echo ""

# 3 — launcher version
echo "3. Deployed launcher version"
if [ -f "$DEPLOYED_CCY" ] && grep -q '^CCY_VERSION="3\.34\.0"' "$DEPLOYED_CCY"; then
    pass "CCY_VERSION 3.34.0"
else
    fail "not 3.34.0 — an older launcher is deployed"
    echo "     Remedy: run deploy.bash" >&2
fi
echo ""

# 4 — the feature is really present, not merely a matching checksum
echo "4. Usage feature is wired into the deployed library"
if [ -f "$DEPLOYED_LIB" ]; then
    for symbol in usage_prime_cache usage_summary_for _usage_pct colorize_usage; do
        if grep -q "^${symbol}()" "$DEPLOYED_LIB"; then
            pass "$symbol() defined"
        else
            fail "$symbol() MISSING from the deployed library"
        fi
    done
    if grep -q 'Show usage limits' "$DEPLOYED_LIB"; then
        pass "the u) menu option is present"
    else
        fail "the u) menu option is MISSING"
    fi
else
    fail "cannot check — library not deployed"
fi
echo ""

echo "════════════════════════════════════════════════════════════════════════"
if [ "$FAILED" -ne 0 ]; then
    echo "VERDICT: FAIL — see the remedies above." >&2
    exit 1
fi

echo "VERDICT: PASS — the feature is deployed."
echo ""
echo "Now try it for real:  start ccy, and press u at the token menu."
echo ""
echo "  Usage columns appear      -> done."
echo "  EVERY row 0% or <1% while the accounts are busy"
echo "                            -> the utilisation scale is 0-1, not 0-100."
echo "                               Multiply by 100 in _usage_pct() in"
echo "                               files/var/local/claude-yolo/lib/token-management.bash"
echo "                               and redeploy. Nothing else depends on it."
