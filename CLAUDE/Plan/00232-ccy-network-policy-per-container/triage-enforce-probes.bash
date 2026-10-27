#!/usr/bin/env bash
# Plan 00232 — triage-enforce-probes.bash: the ACTIVE leg of triage.bash (A1-A10). It answers, on
# throwaway containers only, where a per-container network policy can be enforced so that the
# container cannot lift it, and what a blocked connection looks like from inside. Renders no
# verdict: it prints RESULT lines and the commands' own output.
#
# Usage (from triage.bash --active):
#   triage-enforce-probes.bash <report-file> <image> <run-id> <bus-url-csv>
#
# EFFECT ON THE HOST, all removed on the way out (and swept again by triage.bash's cleanup):
#   - throwaway containers of <image>, labelled ccy-netpolicy-triage=<run-id>, on the podman
#     network, with their entrypoint replaced (sleep, bash or python3), never a ccy session;
#   - one throwaway podman network with the same label (A6, the --connect probe);
#   - a python3 http.server on a random high port, on all host addresses, serving an empty
#     directory, for the length of the run (the "host itself" target; firewalld decides whether
#     the LAN could reach it meanwhile);
#   - a throwaway workspace in the system temp directory (A10), removed at the end of its probe;
#   - nft rules inside the throwaway containers' OWN network namespaces only. Nothing is written
#     to the host's ruleset or to the rootless network namespace shared by every session (A7
#     only lists that one).
# It never touches a running ccy session.
set -euo pipefail

report="${1:?usage: triage-enforce-probes.bash <report-file> <image> <run-id> <bus-url-csv>}"
image="${2:?usage: triage-enforce-probes.bash <report-file> <image> <run-id> <bus-url-csv>}"
runId="${3:?usage: triage-enforce-probes.bash <report-file> <image> <run-id> <bus-url-csv>}"
busUrlCsv="${4-}"

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_netpolicy.inc.bash
source "${scriptDir}/_netpolicy.inc.bash"
INSIDE="${scriptDir}/netprobe-inside.bash"
LABEL="ccy-netpolicy-triage=${runId}"
PREFIX="ccy-netpolicy-triage-${runId}"
# Run inside a peer by `podman exec`: wait up to 10 s for its own server on 8080. The server is
# the container's main process, so if it dies the container stops and the exec fails at once.
PEER_READY_PY='
import socket, sys, time
for _ in range(50):
    try:
        socket.create_connection(("127.0.0.1", 8080), 1).close()
        sys.exit(0)
    except OSError as exc:
        last = exc
        time.sleep(0.2)
sys.exit(f"nothing listens on 8080: {last}")
'
listenerPid=""
listenerDir=""
listenerPort=""

cleanup() {
    local -a ids=()
    mapfile -t ids < <(podman ps -aq --filter "label=${LABEL}")
    if [[ "${#ids[@]}" -gt 0 ]]; then
        podman rm -f "${ids[@]}" >/dev/null
    fi
    mapfile -t ids < <(podman network ls -q --filter "label=${LABEL}")
    if [[ "${#ids[@]}" -gt 0 ]]; then
        podman network rm "${ids[@]}" >/dev/null
    fi
    if [[ -n "${listenerPid}" ]]; then
        kill "${listenerPid}"
        listenerPid=""
    fi
    if [[ -n "${listenerDir}" ]]; then
        rm -rf -- "${listenerDir}" "${listenerDir}.log"
    fi
}

probe() {
    local label="$1" out rc
    shift
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${out:-(no output)}"
    return 0
}

# must <label> <cmd>... — as probe, but a failure stops the run: every probe after it would
# measure a container with no policy and read as if it had one.
must() {
    local label="$1" out rc
    shift
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${out:-(no output)}"
    if [[ "${rc}" -ne 0 ]]; then
        printf 'ERROR: %s failed (rc=%d); the probes that depend on it cannot run\n' "${label}" "${rc}" >&2
        exit 1
    fi
}

need_tools() {
    local tool missing=0
    for tool in podman jq ip nft nsenter setpriv python3 curl; do
        if ! command -v "${tool}" >/dev/null; then
            printf 'ERROR: %s is not installed on this host. It must be declared in the play that owns ccy (playbooks/imports/play-claude-yolo.yml) and deployed from there. Do NOT install it by hand.\n' "${tool}" >&2
            missing=1
        fi
    done
    if ! podman image exists "${image}"; then
        printf 'ERROR: the image %s is not present; launch ccy once (it builds it), or pass --image=.\n' "${image}" >&2
        missing=1
    fi
    return "${missing}"
}

start_host_listener() {
    listenerDir="$(mktemp -d)"
    listenerPort="$((20000 + RANDOM % 20000))"
    python3 -m http.server --bind :: "${listenerPort}" --directory "${listenerDir}" >"${listenerDir}.log" 2>&1 &
    listenerPid=$!
    local i out gone
    for i in $(seq 1 50); do
        if out="$(curl -sS -o /dev/null "http://127.0.0.1:${listenerPort}/" 2>&1)"; then
            printf 'host listener on port %s (pid %s), up after %s tries\n' "${listenerPort}" "${listenerPid}" "${i}"
            return 0
        fi
        if ! gone="$(kill -0 "${listenerPid}" 2>&1)"; then
            printf 'ERROR: the host listener exited before it answered (%s); its log:\n' "${gone}" >&2
            cat "${listenerDir}.log" >&2
            listenerPid=""
            return 1
        fi
        sleep 0.1
    done
    printf 'ERROR: the host listener on port %s did not answer (%s); its log:\n' "${listenerPort}" "${out}" >&2
    cat "${listenerDir}.log" >&2
    return 1
}

# start_peer <name> <network> — a container serving HTTP on 8080; prints its address there.
start_peer() {
    local name="$1" network="$2" out
    podman run -d --rm --label "${LABEL}" --network "${network}" --name "${name}" \
        --entrypoint python3 "${image}" -m http.server 8080 >/dev/null
    if ! out="$(podman exec "${name}" python3 -c "${PEER_READY_PY}" 2>&1)"; then
        printf 'ERROR: peer %s did not start listening: %s\n' "${name}" "${out}" >&2
        return 1
    fi
    podman inspect --format "{{(index .NetworkSettings.Networks \"${network}\").IPAddress}}" "${name}"
}

# create_held <name> [network] — created and initialised (its network namespace exists), not
# started. The network defaults to podman, ccy's default; pasta is what --no-network gives.
create_held() {
    podman create --label "${LABEL}" --network "${2:-podman}" --name "$1" --entrypoint sleep "${image}" 900 >/dev/null
    podman init "$1" >/dev/null
}

# ro_mount_escape <workdir> — a throwaway workspace with a read-only file bound over it, as ccy
# binds ccy.env.local: can the container put a file of its own at that path by moving the parent
# directory aside? Prints what the HOST sees at the path afterwards.
ro_mount_escape() {
    local work="$1" name="${PREFIX}-a10"
    mkdir -p "${work}/.claude/ccy"
    printf 'ORIGINAL\n' >"${work}/.claude/ccy/ccy.env.local"
    podman run --rm --label "${LABEL}" --name "${name}" --network none \
        -v "${work}:/workspace" -v "${work}/.claude/ccy/ccy.env.local:/workspace/.claude/ccy/ccy.env.local:ro" \
        --entrypoint bash "${image}" -c 'cd /workspace/.claude
            if printf "WRITTEN\n" > ccy/ccy.env.local; then echo "direct write: succeeded"; else echo "direct write: refused"; fi
            if mv ccy ccy-moved; then echo "rename of the parent: succeeded"; else echo "rename of the parent: refused"; fi
            if mkdir -p ccy && printf "REPLACED\n" > ccy/ccy.env.local; then echo "new file at the path: written"; else echo "new file at the path: refused"; fi'
    printf 'the host now reads at .claude/ccy/ccy.env.local: %s\n' "$(cat "${work}/.claude/ccy/ccy.env.local")"
    rm -rf -- "${work}"
}

pid_of() {
    podman inspect --format '{{.State.Pid}}' "$1"
}

nameservers_csv() {
    local path
    path="$(podman inspect --format '{{.ResolvConfPath}}' "$1")"
    podman unshare cat "${path}" | awk '/^nameserver/ { printf "%s%s", sep, $2; sep = "," }'
}

in_netns() {
    local pid="$1"
    shift
    podman unshare nsenter -t "${pid}" -n "$@"
}

apply_policy() {
    local name="$1" mode="$2" allow="$3" dns="$4" extra="$5" pid
    pid="$(pid_of "${name}")"
    netpolicy_ruleset "${mode}" "${allow}" "${dns}" "${extra}" | in_netns "${pid}" nft -f -
    in_netns "${pid}" nft list table inet ccy_netpolicy
}

# as_container <name> <extra-cap|-> <cmd>... — the host's tool, run in the container's network
# namespace with no more capability than the container itself holds (its bounding set), plus
# <extra-cap> for the control run. The image has no nft or ip, so this is how the container's own
# right to change its table is measured.
as_container() {
    local name="$1" extra="$2" caps cap bounding="-all"
    shift 2
    if [[ "${extra}" != - ]]; then bounding="${bounding},+${extra}"; fi
    caps="$(podman inspect --format '{{join .EffectiveCaps ","}}' "${name}")"
    local -a capList=()
    IFS=',' read -r -a capList <<<"${caps}"
    for cap in "${capList[@]}"; do
        cap="${cap,,}"
        bounding="${bounding},+${cap#cap_}"
    done
    in_netns "$(pid_of "${name}")" setpriv --inh-caps=-all --ambient-caps=-all --bounding-set="${bounding}" "$@"
}

run_inside() {
    local name="$1"
    shift
    podman exec -i "${name}" bash -s -- "$@" <"${INSIDE}"
}

host_global_addresses_csv() {
    ip -j addr | jq -r '[.[] | select(.ifname != "agentbus0") | .addr_info[] | select(.scope == "global") | .local] | join(",")'
}

# host_other_address <outbound-dev> — a global IPv4 address of the host on another interface
# than the outbound one, or nothing. pasta copies the outbound interface's addresses into the
# container's side, so only an address like this reaches the host itself.
host_other_address() {
    ip -j -4 addr | jq -r --arg out "$1" '[.[] | select(.ifname != $out and .ifname != "agentbus0") | .addr_info[] | select(.scope == "global") | .local] | first // ""'
}

# bus_address — the agentbus0 address, or nothing (and a note on stderr) when the host has no bus.
bus_address() {
    local out
    if ! out="$(ip -j addr show dev agentbus0 2>&1)"; then
        printf 'no agentbus0 on this host (%s)\n' "${out}" >&2
        return 0
    fi
    jq -r '[.[] | .addr_info[] | select(.scope != "link") | .local] | first // ""' <<<"${out}"
}

url_host() {
    local rest="${1#*://}"
    rest="${rest%%/*}"
    if [[ "${rest}" == \[* ]]; then
        rest="${rest#\[}"
        printf '%s' "${rest%%\]*}"
    else
        printf '%s' "${rest%%:*}"
    fi
}

main() {
    need_tools
    printf '# Plan 00232 enforcement probes (active), run %s, image %s\n\n' "${runId}" "${image}"

    printf '## A1 the targets\n'
    start_host_listener
    local hostLan hostDev hostOther hostExtra busIp peerIp
    hostLan="$(ip -j route get 192.0.2.1 | jq -r '.[0].prefsrc')"
    hostDev="$(ip -j route get 192.0.2.1 | jq -r '.[0].dev')"
    hostOther="$(host_other_address "${hostDev}")"
    hostExtra="$(host_global_addresses_csv)"
    busIp="$(bus_address)"
    peerIp="$(start_peer "${PREFIX}-peer" podman)"
    printf 'host primary source address: %s (on %s, which pasta copies into the container side)\nhost address on another interface: %s\nhost global addresses blocked besides the ranges: %s\nbus address: %s\npeer on the podman network: %s\n\n' \
        "${hostLan}" "${hostDev}" "${hostOther:-none}" "${hostExtra:-none}" "${busIp:-none (no agentbus0)}" "${peerIp}"

    local -a targets=(
        "http|host-lan|${hostLan}|${listenerPort}"
        "http|host-alias|host.containers.internal|${listenerPort}"
        "nc|host-lan-nc|${hostLan}|${listenerPort}"
        "ssh|host-lan-ssh|${hostLan}|${listenerPort}"
        "http|peer-session|${peerIp}|8080"
        "dns|dns-forge|api.github.com"
        "url|forge|https://api.github.com/"
        "url|anthropic|https://api.anthropic.com/"
    )
    if [[ -n "${hostOther}" ]]; then
        targets+=("http|host-other|${hostOther}|${listenerPort}" "ssh|host-other-ssh|${hostOther}|${listenerPort}"
            "nc|host-other-nc|${hostOther}|${listenerPort}")
    fi
    local allow="${busIp}" url n=0
    local -a busUrls=()
    IFS=',' read -r -a busUrls <<<"${busUrlCsv}"
    for url in "${busUrls[@]+"${busUrls[@]}"}"; do
        [[ -n "${url}" ]] || continue
        n=$((n + 1))
        targets+=("url|bus-${n}|${url%/}/_matrix/client/versions")
        allow="${allow}${allow:+,}$(url_host "${url}")"
    done
    if [[ -n "${busIp}" ]] && [[ "${n}" -eq 0 ]]; then
        printf 'NOTE: agentbus0 exists but no bus URL was found or given (--bus-url=); the bus is not probed by URL\n\n'
    fi

    printf '## A2 the host can enter a created, not yet started, container network namespace\n'
    create_held "${PREFIX}-a2"
    local pid
    pid="$(pid_of "${PREFIX}-a2")"
    probe "pid of the initialised container" printf '%s\n' "${pid}"
    probe "nsenter WITHOUT podman unshare (expected to need the user namespace)" nsenter -t "${pid}" -n nft list ruleset
    probe "nsenter inside podman unshare: list the container ruleset" in_netns "${pid}" nft list ruleset
    probe "the container nameservers (read on the host before start)" nameservers_csv "${PREFIX}-a2"
    podman rm -f "${PREFIX}-a2" >/dev/null

    printf '## A3 each candidate verdict (READ THIS FOR: time to fail and the message the agent sees)\n'
    local mode name dns
    for mode in none drop reject-icmp reject-tcp; do
        name="${PREFIX}-${mode}"
        printf '#### mode %s\n' "${mode}"
        create_held "${name}"
        dns="$(nameservers_csv "${name}")"
        if [[ "${mode}" != none ]]; then
            must "apply the ${mode} policy (allow=${allow:-none} dns=${dns})" apply_policy "${name}" "${mode}" "${allow}" "${dns}" "${hostExtra}"
        fi
        podman start "${name}" >/dev/null
        probe "probes from inside, mode ${mode}" run_inside "${name}" "${targets[@]}"
        if [[ "${mode}" == reject-icmp ]]; then
            printf '## A4 what the container itself holds (READ THIS FOR: can it lift the policy)\n'
            probe "facts from inside under reject-icmp" run_inside "${name}" facts
            probe "the container's capability set" podman inspect --format '{{join .EffectiveCaps ","}}' "${name}"
            probe "control: the same harness WITH net_admin added lists the table (so a refusal below is the capability, not the harness)" \
                as_container "${name}" net_admin nft list table inet ccy_netpolicy
            probe "with the container's capabilities: delete the policy table" as_container "${name}" - nft delete table inet ccy_netpolicy
            probe "with the container's capabilities: flush the whole ruleset" as_container "${name}" - nft flush ruleset
            probe "with the container's capabilities: delete the default route" as_container "${name}" - ip route del default
            probe "the ruleset is still in place afterwards" in_netns "$(pid_of "${name}")" nft list table inet ccy_netpolicy
        fi
        podman rm -f "${name}" >/dev/null
    done

    printf '## A5 create, init, apply, then start ATTACHED with --rm (the launch sequence a design would use)\n'
    name="${PREFIX}-a5"
    podman create --rm --label "${LABEL}" --network podman --name "${name}" -i --entrypoint bash "${image}" \
        -s -- "http|host-lan|${hostLan}|${listenerPort}" "url|forge|https://api.github.com/" >/dev/null
    podman init "${name}" >/dev/null
    must "apply reject-icmp before start" apply_policy "${name}" reject-icmp "${allow}" "$(nameservers_csv "${name}")" "${hostExtra}"
    probe "start attached, the probe script on stdin" podman start -ai "${name}" <"${INSIDE}"
    probe "the container is gone after it exits (rc=1 means --rm was honoured by start)" podman container exists "${name}"
    name="${PREFIX}-a5-exit"
    podman create --rm --label "${LABEL}" --network podman --name "${name}" -i --entrypoint bash "${image}" -c 'exit 75' >/dev/null
    podman init "${name}" >/dev/null
    must "apply reject-icmp before start (exit status)" apply_policy "${name}" reject-icmp "${allow}" "$(nameservers_csv "${name}")" "${hostExtra}"
    probe "start attached, the process exits 75 (ccy's restart request; rc must be 75)" podman start -ai "${name}"

    printf '## A6 ccy --connect after the policy is in place (READ THIS FOR: does the policy survive, is the new subnet blocked, does adding it to the allow set open it)\n'
    local net="${PREFIX}-net" subnet peer2
    podman network create --label "${LABEL}" "${net}" >/dev/null
    subnet="$(podman network inspect "${net}" | jq -r '.[0].subnets[0].subnet')"
    peer2="$(start_peer "${PREFIX}-peer2" "${net}")"
    name="${PREFIX}-a6"
    create_held "${name}"
    must "apply reject-icmp" apply_policy "${name}" reject-icmp "${allow}" "$(nameservers_csv "${name}")" "${hostExtra}"
    podman start "${name}" >/dev/null
    probe "podman network connect (what ccy --connect runs)" podman network connect "${net}" "${name}"
    pid="$(pid_of "${name}")"
    probe "the ruleset after the connect" in_netns "${pid}" nft list table inet ccy_netpolicy
    probe "the project peer before its subnet is allowed" run_inside "${name}" "http|project-peer|${peer2}|8080"
    must "add the project subnet ${subnet} to the allow set from the host" in_netns "${pid}" nft add element inet ccy_netpolicy allow4 "{ ${subnet} }"
    probe "the project peer after its subnet is allowed" run_inside "${name}" "http|project-peer|${peer2}|8080"
    probe "podman network disconnect" podman network disconnect "${net}" "${name}"
    podman rm -f "${name}" >/dev/null

    printf '## A7 the shared rootless network namespace (list only)\n'
    probe "rootless netns addresses" podman unshare --rootless-netns ip -br addr
    probe "rootless netns ruleset (netavark tables; ours must not be here)" podman unshare --rootless-netns nft list ruleset

    printf '## A8 IPv6 inside a container on the podman network\n'
    name="${PREFIX}-a8"
    create_held "${name}"
    podman start "${name}" >/dev/null
    probe "IPv6 addresses inside" podman exec "${name}" cat /proc/net/if_inet6
    podman rm -f "${name}" >/dev/null

    printf '## A9 the same policy on a pasta container (what ccy --no-network gives)\n'
    name="${PREFIX}-a9"
    create_held "${name}" pasta
    must "apply reject-icmp to the pasta container" apply_policy "${name}" reject-icmp "${allow}" "$(nameservers_csv "${name}")" "${hostExtra}"
    podman start "${name}" >/dev/null
    local -a pastaTargets=("http|host-lan|${hostLan}|${listenerPort}" "http|host-alias|host.containers.internal|${listenerPort}"
        "dns|dns-forge|api.github.com" "url|forge|https://api.github.com/")
    if [[ -n "${hostOther}" ]]; then pastaTargets+=("http|host-other|${hostOther}|${listenerPort}"); fi
    probe "probes from inside the pasta container" run_inside "${name}" "${pastaTargets[@]}"
    podman rm -f "${name}" >/dev/null

    printf '## A10 can a container replace a read-only bound file by moving its parent (READ THIS FOR: issue #83, where a policy may live)\n'
    probe "read-only bind escape attempt" ro_mount_escape "$(mktemp -d)"
}

# See triage-host-facts.bash for why main runs as a pipeline element. Its cleanup is trapped
# inside that subshell, where the listener's pid lives.
main_with_cleanup() {
    trap cleanup EXIT
    main
}
main_with_cleanup 2>&1 | tee "${report}"
