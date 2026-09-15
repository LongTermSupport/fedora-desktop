#!/usr/bin/env bash
# Unit-test ccy_host_hostname (files/var/local/claude-yolo/lib/common-pure.bash).
#
# WHY THIS EXISTS. A container's own HOSTNAME is the short container id podman gives it, so
# nothing inside can tell which MACHINE it is running on. There is no standard for exposing
# the host's name to a container — Kubernetes' NODE_NAME via the downward API is the nearest
# convention — so CCY declares `CCY_HOST_HOSTNAME` and derives it here.
#
# The value is interpolated into a `podman run -e` argument and then read by shells inside
# the container, so the normalisation is the guard: a FQDN is reduced to the machine's own
# label, and anything that is not an RFC 1123 label is refused rather than passed through.
# Empty must be a refusal too — an empty CCY_HOST_HOSTNAME would look exactly like "the
# launcher is too old to set it", and a consumer cannot tell those apart.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB="$REPO_ROOT/files/var/local/claude-yolo/lib/common-pure.bash"

if [ ! -f "$LIB" ]; then
    echo "FAIL: common-pure.bash not found at $LIB" >&2
    exit 1
fi

# shellcheck source=/dev/null
source "$LIB"

if ! declare -F ccy_host_hostname >/dev/null; then
    echo "FAIL: ccy_host_hostname is not defined after sourcing the library" >&2
    exit 1
fi

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# accepted <raw> — the function's stdout, or "REFUSED" when it exits non-zero. Stderr is
# discarded here because the diagnostics are checked separately below.
accepted() {
    local out
    if out="$(ccy_host_hostname "$1" 2>/dev/null)"; then
        printf '%s' "$out"
    else
        printf 'REFUSED'
    fi
}

# ── the ordinary shapes ──────────────────────────────────────────────────────────────
check "a plain hostname passes through" "workbox" "$(accepted workbox)"
check "a hostname with digits is kept" "node7" "$(accepted node7)"
check "internal hyphens are kept" "build-box-2" "$(accepted build-box-2)"

# A FQDN's domain is not the machine. `uname -n` returns whichever form the host is
# configured with, so both must reduce to the same answer — otherwise the value a consumer
# sees would depend on the host's DNS configuration rather than on the machine.
check "a FQDN is reduced to its first label" "workbox" "$(accepted workbox.example.com)"
check "a trailing dot does not produce an empty name" "workbox" "$(accepted workbox.)"

# ── the refusals ─────────────────────────────────────────────────────────────────────
# Empty is indistinguishable from "the launcher is too old to set this", so it cannot be
# allowed to pass as a value.
check "an empty nodename is refused" "REFUSED" "$(accepted '')"
check "a nodename that is only a domain dot is refused" "REFUSED" "$(accepted .example.com)"
check "whitespace alone is refused" "REFUSED" "$(accepted '   ')"

# It becomes a `podman run -e VALUE` argument and is then read by shells in the container.
# The grammar is what stops anything else travelling with it.
check "a shell metacharacter is refused" "REFUSED" "$(accepted 'box;rm -rf /')"
check "a space inside the name is refused" "REFUSED" "$(accepted 'my box')"
# Built from a variable rather than written literally. A `$` inside single quotes is read
# by the linter as a mistaken expansion (SC2016); the point here is the literal character.
dollar='$'
check "a dollar sign is refused" "REFUSED" "$(accepted "box${dollar}USER")"
check "a newline is refused" "REFUSED" "$(accepted 'box
evil')"
check "a leading hyphen is refused" "REFUSED" "$(accepted -box)"
check "a trailing hyphen is refused" "REFUSED" "$(accepted box-)"
check "an underscore is refused" "REFUSED" "$(accepted my_box)"

# ── the refusal must say why, on stderr, and print nothing on stdout ─────────────────
err="$(ccy_host_hostname '' 2>&1 >/dev/null)"
case "$err" in
    *hostname*) check "the refusal explains itself" "yes" "yes" ;;
    *) check "the refusal explains itself" "yes" "no: ${err}" ;;
esac
# The non-zero status is the expected outcome here, so it is consumed explicitly rather
# than discarded: what this asserts is that nothing reached stdout ALONGSIDE the refusal.
# A caller does `name=$(ccy_host_hostname ...)`, so a refusal that still printed something
# would hand it a value it had no reason to distrust.
if out="$(ccy_host_hostname 'box;evil' 2>/dev/null)"; then
    check "a refused name prints nothing on stdout" "REFUSED" "unexpectedly accepted: ${out}"
else
    check "a refused name prints nothing on stdout" "" "$out"
fi

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
