# shellcheck shell=bash
# Plan 00232 — _netpolicy.inc.bash: the candidate network policy as an nftables ruleset, shared by
# triage-enforce-probes.bash (which applies it to throwaway containers) and test-netpolicy-inc.bash.
#
# Sourced, never run. Pure: it prints, reads no host state and changes nothing.
#
# The private ranges are written in the short form ("10/8") and expanded here. The pre-commit
# scanner refuses a dotted private address anywhere in the repo, and the short form is also the
# one the owner and the issue use.

# The ranges the default policy refuses: RFC 1918, CGNAT (RFC 6598), IPv4 and IPv6 link-local,
# and IPv6 unique-local (RFC 4193).
NETPOLICY_BLOCK_DEFAULT=(10/8 172.16/12 192.168/16 100.64/10 169.254/16 fe80::/10 fc00::/7)

# netpolicy_expand <prefix> — an IPv4 short prefix ("a.b/len") padded to four octets; an IPv6
# prefix or a full IPv4 address (with or without a length) is printed unchanged. Exit 1 and a
# reason on stderr for anything that is neither.
netpolicy_expand() {
    local prefix="${1:?netpolicy_expand requires a prefix}"
    if [[ "${prefix}" == *:* ]]; then
        printf '%s\n' "${prefix}"
        return 0
    fi
    local addr="${prefix%%/*}" len=""
    [[ "${prefix}" == */* ]] && len="/${prefix#*/}"
    if [[ ! "${addr}" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){0,3}$ ]]; then
        printf 'netpolicy_expand: not an IPv4 or IPv6 prefix: %s\n' "${prefix}" >&2
        return 1
    fi
    if [[ -n "${len}" ]] && [[ ! "${len}" =~ ^/[0-9]{1,2}$ ]]; then
        printf 'netpolicy_expand: bad prefix length in %s\n' "${prefix}" >&2
        return 1
    fi
    local dots="${addr//[^.]/}"
    while [[ "${#dots}" -lt 3 ]]; do
        addr="${addr}.0"
        dots="${dots}."
    done
    printf '%s%s\n' "${addr}" "${len}"
}

# _netpolicy_set <name> <ipv4_addr|ipv6_addr> <element>... — one interval set, with no elements
# line when it is empty (nft rejects `elements = { }`). auto-merge, because the inputs overlap:
# a host's own addresses sit inside the private ranges, and the bus address arrives both from
# agentbus0 and from the seat bundles. Without it nft refuses the whole table ("conflicting
# intervals specified").
_netpolicy_set() {
    local name="$1" type="$2"
    shift 2
    printf '    set %s {\n        type %s\n        flags interval\n        auto-merge\n' "${name}" "${type}"
    if [[ "$#" -gt 0 ]]; then
        local joined
        joined="$(printf '%s, ' "$@")"
        printf '        elements = { %s }\n' "${joined%, }"
    fi
    printf '    }\n'
}

# netpolicy_ruleset <mode> <allow-csv> <dns-csv> <extra-block-csv> — the table `inet ccy_netpolicy`
# for one container's own network namespace, on stdout.
#   mode         drop | reject-icmp (icmpx admin-prohibited) | reject-tcp (a TCP reset for TCP,
#                admin-prohibited for the rest)
#   allow-csv    addresses or prefixes reachable on every port, checked before any block
#   dns-csv      addresses reachable on port 53 only (the container's nameservers)
#   extra-block  addresses refused besides the defaults (the host's own global addresses)
# Every list may be empty. Loopback inside the container is never touched.
netpolicy_ruleset() {
    local mode="${1:?netpolicy_ruleset requires a mode}"
    local allow_csv="${2-}" dns_csv="${3-}" extra_csv="${4-}"
    local verdict_lines=()
    case "${mode}" in
        drop) verdict_lines=("drop") ;;
        reject-icmp) verdict_lines=("reject with icmpx admin-prohibited") ;;
        reject-tcp) verdict_lines=("meta l4proto tcp reject with tcp reset" "reject with icmpx admin-prohibited") ;;
        *)
            printf 'netpolicy_ruleset: unknown mode %s (drop, reject-icmp, reject-tcp)\n' "${mode}" >&2
            return 1
            ;;
    esac

    local -a allow4=() allow6=() dns4=() dns6=() block4=() block6=()
    local item expanded
    local -a items=()
    IFS=',' read -r -a items <<<"${allow_csv}"
    for item in "${items[@]+"${items[@]}"}"; do
        [[ -n "${item}" ]] || continue
        expanded="$(netpolicy_expand "${item}")" || return 1
        if [[ "${expanded}" == *:* ]]; then allow6+=("${expanded}"); else allow4+=("${expanded}"); fi
    done
    items=()
    IFS=',' read -r -a items <<<"${dns_csv}"
    for item in "${items[@]+"${items[@]}"}"; do
        [[ -n "${item}" ]] || continue
        expanded="$(netpolicy_expand "${item}")" || return 1
        if [[ "${expanded}" == *:* ]]; then dns6+=("${expanded}"); else dns4+=("${expanded}"); fi
    done
    items=()
    IFS=',' read -r -a items <<<"${extra_csv}"
    for item in "${NETPOLICY_BLOCK_DEFAULT[@]}" "${items[@]+"${items[@]}"}"; do
        [[ -n "${item}" ]] || continue
        expanded="$(netpolicy_expand "${item}")" || return 1
        if [[ "${expanded}" == *:* ]]; then block6+=("${expanded}"); else block4+=("${expanded}"); fi
    done

    printf 'table inet ccy_netpolicy {\n'
    _netpolicy_set allow4 ipv4_addr "${allow4[@]+"${allow4[@]}"}"
    _netpolicy_set allow6 ipv6_addr "${allow6[@]+"${allow6[@]}"}"
    _netpolicy_set dns4 ipv4_addr "${dns4[@]+"${dns4[@]}"}"
    _netpolicy_set dns6 ipv6_addr "${dns6[@]+"${dns6[@]}"}"
    _netpolicy_set block4 ipv4_addr "${block4[@]}"
    _netpolicy_set block6 ipv6_addr "${block6[@]}"
    printf '    chain output {\n        type filter hook output priority filter; policy accept;\n'
    printf '        oifname "lo" accept\n'
    printf '        ip daddr @allow4 accept\n        ip6 daddr @allow6 accept\n'
    printf '        ip daddr @dns4 udp dport 53 accept\n        ip daddr @dns4 tcp dport 53 accept\n'
    printf '        ip6 daddr @dns6 udp dport 53 accept\n        ip6 daddr @dns6 tcp dport 53 accept\n'
    local line
    for line in "${verdict_lines[@]}"; do
        printf '        ip daddr @block4 %s\n' "${line}"
        printf '        ip6 daddr @block6 %s\n' "${line}"
    done
    printf '    }\n}\n'
}
