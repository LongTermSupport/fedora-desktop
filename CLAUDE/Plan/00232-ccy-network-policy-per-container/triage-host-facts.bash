#!/usr/bin/env bash
# Plan 00232 — triage-host-facts.bash: the PASSIVE leg of triage.bash (P1-P9). Read-only: it
# lists, inspects and reads; it starts, stops and changes nothing. Renders no verdict.
#
# Usage (from triage.bash): triage-host-facts.bash <report-file> <repo-root>
# Writes everything to <report-file> and then prints it. A probe's non-zero exit is data, shown
# as rc=N beside it; the leg itself fails only when its tools are missing.
set -euo pipefail

report="${1:?usage: triage-host-facts.bash <report-file> <repo-root>}"
repoRoot="${2:?usage: triage-host-facts.bash <report-file> <repo-root>}"

# The product owns these paths; read them from it, never copy them (QA.md checker-copies-cited-constant).
product_value() {
    python3 -P "${repoRoot}/CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py" product-value "$@"
}
SEATS_REL="$(product_value helpers/pingbus/seat.py CHECKOUT_SEATS)"
APPLIED_DIR="$(product_value helpers/agent_bus/checkout.py APPLIED_DIR)"
MEMBERSHIP_MOUNT="$(product_value helpers/pingbus/membership.py CCY_MOUNT)"

probe() {
    local label="$1" out rc
    shift
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${out:-(no output)}"
    return 0
}

need_tools() {
    local tool missing=0
    for tool in podman jq ip awk; do
        if ! command -v "${tool}" >/dev/null; then
            printf 'ERROR: %s is not installed; it is part of the base install (play-basic-configs.yml, play-claude-yolo.yml). Do NOT install it by hand.\n' "${tool}" >&2
            missing=1
        fi
    done
    return "${missing}"
}

podman_network_facts() {
    podman info --format json | jq '{version: .version.Version, rootless: .host.security.rootless,
        networkBackend: .host.networkBackend, networkBackendInfo: .host.networkBackendInfo,
        rootlessNetworkCmd: .host.rootlessNetworkCmd, pasta: .host.pasta, slirp4netns: .host.slirp4netns}'
}

containers_conf_network_keys() {
    local f found=0
    for f in /usr/share/containers/containers.conf /etc/containers/containers.conf \
        /etc/containers/containers.conf.d/*.conf "${HOME}/.config/containers/containers.conf" \
        "${HOME}"/.config/containers/containers.conf.d/*.conf; do
        [[ -f "${f}" ]] || continue
        found=1
        printf '== %s\n' "${f}"
        awk '/^\[/ { section = $0 } /^[[:space:]]*(default_rootless_network_cmd|pasta_options|firewall_driver|default_subnet|default_subnet_pools|dns_servers|base_hosts_file|host_containers_internal_ip|network_cmd_options|default_network|network_backend)[[:space:]]*=/ { print section " " $0 }' "${f}"
    done
    [[ "${found}" -eq 1 ]] || printf '(no containers.conf found in any searched location)\n'
}

host_addresses() {
    ip -j addr | jq -r '.[] | .ifname as $i | .addr_info[] | "\($i) \(.family) \(.local)/\(.prefixlen) scope=\(.scope)"'
}

route_to_documentation_address() {
    ip -j route get 192.0.2.1 | jq -r '.[0] | "dev=\(.dev) prefsrc=\(.prefsrc) gateway=\(.gateway // "-")"'
}

bus_address() {
    ip -j addr show dev agentbus0 | jq -r '.[] | .addr_info[] | "\(.family) \(.local)/\(.prefixlen) scope=\(.scope)"'
}

applied_teams() {
    local d="${APPLIED_DIR}" f
    ls -la "${d}"
    for f in "${d}"/*.json; do
        [[ -e "${f}" ]] || continue
        printf '== %s\n' "${f}"
        jq -c '.' "${f}"
    done
}

ccy_containers() {
    podman ps --filter label=ccy=true --format '{{.Names}}'
}

count_non_loopback_hosts() {
    awk '!/^#/ && NF >= 2 && $1 !~ /^(127\.|::1$)/ { n++ } END { print n + 0 }' "$1"
}

host_alias_address() {
    awk '/host\.containers\.internal/ { print $1 }' "$1"
}

ccy_session_network_facts() {
    local names name rp hp
    names="$(ccy_containers)"
    if [[ -z "${names}" ]]; then
        printf '(no running container carries the ccy=true label)\n'
        return 0
    fi
    while IFS= read -r name; do
        printf '== %s\n' "${name}"
        podman inspect "${name}" | jq -r --arg mount "${MEMBERSHIP_MOUNT}" '.[0] | "networkMode=\(.HostConfig.NetworkMode) privileged=\(.HostConfig.Privileged) userns=\(.HostConfig.UsernsMode) seats=\(.Config.Labels["ccy-seats"] // "-")",
            "effectiveCaps=\(.EffectiveCaps | join(","))",
            "capAdd=\(.HostConfig.CapAdd // [] | join(","))",
            (.NetworkSettings.Networks // {} | to_entries[] | "network \(.key) ip=\(.value.IPAddress) gw=\(.value.Gateway) ipv6=\(.value.GlobalIPv6Address)"),
            (.Mounts[] | select(.Destination == $mount) | "membership mount \(.Source) rw=\(.RW)")'
        rp="$(podman inspect --format '{{.ResolvConfPath}}' "${name}")"
        hp="$(podman inspect --format '{{.HostsPath}}' "${name}")"
        printf -- '-- resolv.conf nameservers\n'
        probe "resolv.conf of ${name}" awk '/^nameserver/' "${rp}"
        printf -- '-- hosts entries that are not loopback (a count; the names are install-specific)\n'
        probe "non-loopback hosts entries of ${name}" count_non_loopback_hosts "${hp}"
        probe "host.containers.internal in ${name}" host_alias_address "${hp}"
    done <<<"${names}"
}

seat_bundles_in_checkout() {
    local root="$1" f found=0
    while IFS= read -r f; do
        [[ -n "${f}" ]] || continue
        found=1
        printf '== %s\n' "${f#"${root}"/}"
        jq -r '"base_url=\(.base_url) plain_http_hosts=\(.plain_http_hosts // [] | join(","))"' "${f}"
    done < <(find "${root}/${SEATS_REL}" -name member.json -type f)
    [[ "${found}" -eq 1 ]] || printf '(no seat bundle in this checkout)\n'
}

pasta_processes() {
    ps -eo pid=,comm=,args= | awk '$2 ~ /^pasta/ { print }'
}

deployed_launcher_network_lines() {
    awk '/NETWORK_FLAG="--network podman"/ || /^CCY_VERSION=/ { print FILENAME ":" FNR ": " $0 }' /var/local/claude-yolo/claude-yolo
}

main() {
    need_tools
    printf '# Plan 00232 host facts (passive)\n\n'
    printf '## P1 container engine and network stack\n'
    probe "podman network stack (podman info)" podman_network_facts
    probe "pasta version" pasta --version
    probe "netavark version" /usr/libexec/podman/netavark --version
    probe "nft on the host" nft --version
    probe "nsenter on the host" nsenter --version
    printf '## P2 containers.conf network keys\n'
    probe "containers.conf network keys" containers_conf_network_keys
    printf '## P3 podman networks\n'
    probe "podman network ls" podman network ls --format '{{.Name}} {{.Driver}} {{.ID}}'
    probe "the podman network" podman network inspect podman
    printf '## P4 host addresses and routes (the host-itself block list is derived from these)\n'
    probe "host addresses" host_addresses
    probe "IPv4 routes" ip -4 route
    probe "IPv6 routes" ip -6 route
    probe "route to a documentation address (the host's primary source address)" route_to_documentation_address
    printf '## P5 the agent bus (READ THIS FOR: what the default policy must always allow)\n'
    probe "agentbus0 address" bus_address
    probe "applied teams" applied_teams
    probe "seat bundles in this checkout" seat_bundles_in_checkout "${repoRoot}"
    printf '## P6 live ccy sessions (READ THIS FOR: capabilities, networks, nameservers)\n'
    probe "live ccy sessions" ccy_session_network_facts
    printf '## P7 pasta processes and their options (rootless netns and pasta-mode containers)\n'
    probe "pasta processes" pasta_processes
    printf '## P8 the deployed launcher\n'
    probe "deployed launcher network default" deployed_launcher_network_lines
    printf '## P9 the default policy candidate parses with this host nft\n'
    probe "ruleset unit tests with nft --check" "$(dirname "${BASH_SOURCE[0]}")/test-netpolicy-inc.bash"
}

# main runs as a pipeline element, not as an `if` condition: in a condition `set -e` is off in
# every function it calls, so a failed step would run on. With pipefail the pipeline's status,
# and so the script's, is main's. No closing `exit`: after one, shellcheck calls every probe
# function that is only named to probe() unreachable.
main 2>&1 | tee "${report}"
