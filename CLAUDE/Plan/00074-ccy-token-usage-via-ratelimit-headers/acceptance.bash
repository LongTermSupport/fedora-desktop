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
    2. The deployed library version matches the REPO's version header
    3. The deployed launcher CCY_VERSION matches the REPO's CCY_VERSION
    4. The deployed library actually defines the usage functions and the
       `u)` menu option — not just a matching checksum

    Checks 2 and 3 compare against whatever the repo currently declares. They
    are deliberately NOT pinned to a literal version: this script was pinned to
    CCY_VERSION 3.35.0, the ssh-handling fix took the launcher to 3.36.0, and a
    perfectly good deploy then failed with "an older launcher is deployed" and
    told the user to re-run the deploy that had just worked. A check that goes
    stale reports a confident wrong answer and sends you away from the cause —
    the exact class Plan 00075 exists to stop.

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
REPO_CCY="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"
DEPLOYED_LIB="/var/local/claude-yolo/lib/token-management.bash"
DEPLOYED_CCY="/var/local/claude-yolo/claude-yolo"

FAILED=0

# Read a `# Version: X.Y.Z - description` header. Echoes the version on stdout;
# non-zero (with nothing on stdout) when the file has no such header, so the
# caller can say "unreadable" rather than silently comparing empty strings.
lib_version() {
    local file="$1" line
    [ -f "$file" ] || return 1
    if ! line="$(grep -m1 '^# Version: ' "$file")"; then
        return 1
    fi
    line="${line#"# Version: "}"
    printf '%s' "${line%% *}"
}

# Read the CCY_VERSION="X.Y.Z" assignment. Same contract as lib_version().
ccy_version() {
    local file="$1" line
    [ -f "$file" ] || return 1
    if ! line="$(grep -m1 '^CCY_VERSION=' "$file")"; then
        return 1
    fi
    line="${line#*\"}"
    printf '%s' "${line%%\"*}"
}

# Compare a deployed file's version against the repo's, reporting BOTH values.
# The old checks printed only "not 3.35.0", which does not say what is actually
# on the host — so a stale expectation and a genuinely old deploy looked
# identical, and both got the same wrong remedy.
check_version() {
    local label="$1" reader="$2" repo_file="$3" deployed_file="$4"
    local want got

    if ! want="$("$reader" "$repo_file")"; then
        fail "cannot read the version from the repo file $repo_file"
        return 0
    fi
    if ! got="$("$reader" "$deployed_file")"; then
        fail "cannot read the deployed $label version from $deployed_file"
        echo "     Remedy: run deploy.bash" >&2
        return 0
    fi
    if [ "$got" = "$want" ]; then
        pass "$got (matches the repo)"
    else
        fail "deployed $label is $got, the repo declares $want"
        echo "     Remedy: run deploy.bash" >&2
    fi
}

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

# 2 — library version, against the repo rather than a literal
echo "2. Deployed library version"
check_version library lib_version "$REPO_LIB" "$DEPLOYED_LIB"
echo ""

# 3 — launcher version, against the repo rather than a literal
echo "3. Deployed launcher version"
check_version launcher ccy_version "$REPO_CCY" "$DEPLOYED_CCY"
echo ""

# 4 — the feature is really present, not merely a matching checksum
echo "4. Usage feature is wired into the deployed library"
if [ -f "$DEPLOYED_LIB" ]; then
    for symbol in usage_prime_cache usage_render_block _usage_bar _usage_bucket_line; do
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
echo "                               Confirm:  CCY_USAGE_DEBUG=1 ccy  then press u"
echo "                               to see the raw value the API sent, then set"
echo "                               CCY_USAGE_SCALE=fraction. Nothing else depends on it."
