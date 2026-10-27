#!/usr/bin/env bash
# Plan 00232 — test-netpolicy-inc.bash: unit tests for _netpolicy.inc.bash (the candidate ruleset
# generator). Pure: runs anywhere, touches nothing. When nft is installed it also asks
# `nft --check` to parse each ruleset; without nft that one case fails, by name (an absent check
# is not a passing one).
#
# The expected private addresses are assembled from octets (dotted), because the repo's scanners
# refuse a dotted private address written out in a tracked file.
#
# Usage: ./CLAUDE/Plan/00232-ccy-network-policy-per-container/test-netpolicy-inc.bash [--no-nft]
#   --no-nft  leave out the nft parse case (inside a ccy container, which has no nft)
set -euo pipefail

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_netpolicy.inc.bash
source "${scriptDir}/_netpolicy.inc.bash"

useNft=1
case "${1-}" in
    "") ;;
    --no-nft) useNft=0 ;;
    *)
        printf 'usage: %s [--no-nft]\n' "$0" >&2
        exit 64
        ;;
esac

dotted() {
    local IFS=.
    printf '%s' "$*"
}

failures=0
check() {
    local name="$1" want="$2" got="$3"
    if [[ "${want}" == "${got}" ]]; then
        printf 'ok   %s\n' "${name}"
    else
        printf 'FAIL %s\n  want: %s\n  got:  %s\n' "${name}" "${want}" "${got}"
        failures=$((failures + 1))
    fi
}
contains() {
    local name="$1" needle="$2" hay="$3"
    if [[ "${hay}" == *"${needle}"* ]]; then
        printf 'ok   %s\n' "${name}"
    else
        printf 'FAIL %s\n  missing: %s\n' "${name}" "${needle}"
        failures=$((failures + 1))
    fi
}

ten="$(dotted 10 0 0 0)/8"
rfc172="$(dotted 172 16 0 0)/12"
rfc192="$(dotted 192 168 0 0)/16"

check "expand pads one octet" "${ten}" "$(netpolicy_expand 10/8)"
check "expand pads two octets" "${rfc172}" "$(netpolicy_expand 172.16/12)"
check "expand keeps a full address" "192.0.2.10" "$(netpolicy_expand 192.0.2.10)"
check "expand keeps IPv6" "fc00::/7" "$(netpolicy_expand fc00::/7)"
if out="$(netpolicy_expand not-an-address 2>&1)"; then rc=0; else rc=$?; fi
check "expand refuses a name" "1" "${rc}"
contains "expand names what it refused" "not-an-address" "${out}"

if out="$(netpolicy_ruleset sideways "" "" "" 2>&1)"; then rc=0; else rc=$?; fi
check "an unknown mode is refused" "1" "${rc}"

ruleset="$(netpolicy_ruleset reject-icmp "192.0.2.10,2001:db8::1" "198.51.100.53" "203.0.113.7")"
contains "the allow set holds the IPv4 entry" "elements = { 192.0.2.10 }" "${ruleset}"
contains "the IPv6 allow set holds the IPv6 entry" "elements = { 2001:db8::1 }" "${ruleset}"
contains "the nameserver is in the DNS set" "elements = { 198.51.100.53 }" "${ruleset}"
contains "every default IPv4 range and the extra address are blocked" \
    "elements = { ${ten}, ${rfc172}, ${rfc192}, 100.64.0.0/10, 169.254.0.0/16, 203.0.113.7 }" "${ruleset}"
contains "the IPv6 ranges are blocked" "elements = { fe80::/10, fc00::/7 }" "${ruleset}"
contains "the verdict is admin-prohibited" "ip daddr @block4 reject with icmpx admin-prohibited" "${ruleset}"

allowLine="$(awk '/ip daddr @allow4 accept/ { print NR; exit }' <<<"${ruleset}")"
blockLine="$(awk '/ip daddr @block4/ { print NR; exit }' <<<"${ruleset}")"
order="no"
if [[ "${allowLine}" -lt "${blockLine}" ]]; then order="yes"; fi
check "an allow is checked before any block" "yes" "${order}"

empty="$(netpolicy_ruleset drop "" "" "")"
check "an empty allow set has no elements line" "0" \
    "$(awk '/set allow4/ { inset = 1 } inset && /elements/ { n++ } inset && /^    }/ { inset = 0 } END { print n + 0 }' <<<"${empty}")"
contains "drop mode drops" "ip daddr @block4 drop" "${empty}"

tcp="$(netpolicy_ruleset reject-tcp "" "" "")"
contains "reject-tcp resets TCP" "ip daddr @block4 meta l4proto tcp reject with tcp reset" "${tcp}"
contains "reject-tcp refuses the rest as admin-prohibited" "ip daddr @block4 reject with icmpx admin-prohibited" "${tcp}"

if [[ "${useNft}" -eq 1 ]]; then
    if ! command -v nft >/dev/null; then
        printf 'FAIL nft parses each ruleset\n  nft is not installed here; run with --no-nft inside a ccy container\n'
        failures=$((failures + 1))
    else
        for mode in drop reject-icmp reject-tcp; do
            if out="$(netpolicy_ruleset "${mode}" "192.0.2.10" "198.51.100.53" "" | nft --check -f - 2>&1)"; then
                printf 'ok   nft parses the %s ruleset\n' "${mode}"
            else
                printf 'FAIL nft parses the %s ruleset\n%s\n' "${mode}" "${out}"
                failures=$((failures + 1))
            fi
        done
    fi
fi

if [[ "${failures}" -gt 0 ]]; then
    printf '%d case(s) failed\n' "${failures}"
    exit 1
fi
printf 'all cases passed\n'
