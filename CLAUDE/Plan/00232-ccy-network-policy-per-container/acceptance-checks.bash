#!/usr/bin/env bash
# Plan 00232 — acceptance-checks.bash: the checks acceptance.bash runs, one per invocation, so
# each is its own check. Each prints PASS or FAIL lines with the evidence and exits 0 only when all
# of its lines pass.
#
#   caps <session>                 the session is not privileged and holds neither NET_ADMIN nor
#                                  SYS_ADMIN, so nothing inside can change its network namespace
#   held <session>                 the policy table is in the session's own network namespace,
#                                  read from the host
#   blocked <session> <image>      a host listener (through host.containers.internal, and on a host
#                                  address off the outbound interface when the host has one: pasta
#                                  copies the outbound interface's own addresses into the
#                                  container side, so those never reach the host, policy or not)
#                                  and a peer on the podman network are
#                                  reachable from a throwaway container WITHOUT the policy (the
#                                  control), and refused from the session at once (under 2 s, not
#                                  a timeout)
#   allowed <session>              DNS, the forge and the Claude API still work
#   connect <session> <image> <launcher> <checkout>
#                                  a peer on a throwaway project network is refused, then reached
#                                  after `ccy --connect <network>` (whose refresh opens its subnet
#                                  and names it in the session's context file), then refused again
#                                  after `ccy --disconnect <network>`
#   context <session>              the session was started with its network limits in its system
#                                  prompt (the marker CCY NETWORK POLICY)
#   bus <session> <peer> <team> <seat> <peer-seat>
#                                  both reach their homeserver, and a message sent each way with
#                                  `pingbus say` arrives in the other's inbox (`pingbus inbox`,
#                                  which does not consume it). Both must be throwaway seated
#                                  sessions of <team> (not working agents: they receive a message)
#   ready <session> <team>         the session's pingbus answers for <team> (its seat is claimed);
#                                  no PASS line, only the status
#
# Every probe must give one RESULT line per target and exit 0 (probe), or the check fails: a
# check never passes on nothing measured.
#
# Side effects: `blocked` runs a throwaway container (label ccy-netpolicy-acceptance) and a
# python3 http.server on a random high port for its duration; `connect` makes a throwaway
# network (same label) with a peer on it, and `ccy --connect` joins EVERY running ccy session of
# <checkout> to it until its `ccy --disconnect`; the checkout's saved default network is put back
# as it was. `bus` posts one message each way in <team>'s room. Nothing else is changed.
set -euo pipefail

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
INSIDE="${scriptDir}/netprobe-inside.bash"
# The checkout's seat directory is the product's (helpers/pingbus/seat.py CHECKOUT_SEATS); read, never copied.
M7_CHECK="${scriptDir}/../00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py"
SEATS_REL="$(python3 -P "${M7_CHECK}" product-value helpers/pingbus/seat.py CHECKOUT_SEATS)"
# A podman exec does not get the session's pingbus environment, so it is given the session home
# the entrypoint uses (seat.py, the first line of m7_check.py session-paths) and the one team.
CTR_PINGBUS_HOME="$(python3 -P "${M7_CHECK}" session-paths | awk 'NR == 1')"
MARKER="CCY NETWORK POLICY"
FAST_MS=2000
failed=0

pass() { printf 'PASS %s\n' "$*"; }
fail() {
    printf 'FAIL %s\n' "$*"
    failed=1
}

run_inside() {
    local name="$1"
    shift
    podman exec -i "${name}" bash -s -- "$@" <"${INSIDE}"
}

# probe <session> <target...> — the probe's RESULT lines in PROBED, one per target; 1, with a
# FAIL line, when the probe itself failed or a target gave no line.
PROBED=()
probe() {
    local c="$1" out rc=0
    shift
    out="$(run_inside "${c}" "$@" 2>&1)" || rc=$?
    mapfile -t PROBED < <(awk '/^RESULT /' <<<"${out}")
    if [[ "${rc}" -ne 0 ]] || [[ "${#PROBED[@]}" -ne "$#" ]]; then
        fail "the probe in ${c} exited ${rc} with ${#PROBED[@]} RESULT line(s) for $# target(s), so nothing was measured: ${out}"
        PROBED=()
        return 1
    fi
}

# field <RESULT-line> <key> — the value of key=... on a netprobe RESULT line.
field() {
    awk -v key="$2" '{ for (i = 1; i <= NF; i++) if (index($i, key "=") == 1) { print substr($i, length(key) + 2); exit } }' <<<"$1"
}

check_caps() {
    local c="$1" priv capeff
    priv="$(podman inspect --format '{{.HostConfig.Privileged}}' "${c}")"
    if [[ "${priv}" == false ]]; then pass "${c} is not privileged"; else fail "${c} is privileged"; fi
    capeff="$(podman exec "${c}" awk '/^CapEff:/ { print $2 }' /proc/1/status)"
    if (((16#${capeff} >> 12) & 1)); then fail "${c} holds NET_ADMIN (CapEff ${capeff})"; else pass "${c} has no NET_ADMIN (CapEff ${capeff})"; fi
    if (((16#${capeff} >> 21) & 1)); then fail "${c} holds SYS_ADMIN (CapEff ${capeff})"; else pass "${c} has no SYS_ADMIN"; fi
}

check_held() {
    local c="$1" pid out
    pid="$(podman inspect --format '{{.State.Pid}}' "${c}")"
    if out="$(podman unshare nsenter -t "${pid}" -n nft list table inet ccy_netpolicy 2>&1)"; then
        pass "the policy table is in ${c}'s own network namespace"
        printf '%s\n' "${out}"
    else
        fail "no policy table in ${c}'s network namespace: ${out}"
    fi
}

# judge_blocked <line> — refused at once, not left to time out.
judge_blocked() {
    local line="$1" label rc ms
    label="$(awk '{ print $2 }' <<<"${line}")"
    rc="$(field "${line}" rc)"
    ms="$(field "${line}" ms)"
    if [[ "${rc}" == 0 ]]; then
        fail "${label} was reached: ${line}"
    elif [[ "${ms}" -ge "${FAST_MS}" ]]; then
        fail "${label} failed only after ${ms} ms (a silent drop; the agent waits and learns nothing): ${line}"
    else
        pass "${label} refused in ${ms} ms: ${line}"
    fi
}

check_blocked() {
    local c="$1" image="$2" dir port pid hostDev hostOther peer peerIp line out i
    dir="$(mktemp -d)"
    port="$((20000 + RANDOM % 20000))"
    python3 -m http.server --bind :: "${port}" --directory "${dir}" >"${dir}.log" 2>&1 &
    pid=$!
    peer="ccy-netpolicy-acceptance-$$"
    stop() {
        trap - RETURN
        kill "${pid}"
        rm -rf -- "${dir}" "${dir}.log"
        if podman container exists "${peer}"; then podman rm -f -t 0 "${peer}" >/dev/null; fi
    }
    trap stop RETURN
    local gone
    for i in $(seq 1 50); do
        if out="$(curl -sS -o /dev/null "http://127.0.0.1:${port}/" 2>&1)"; then break; fi
        if ! gone="$(kill -0 "${pid}" 2>&1)"; then
            fail "the host listener exited before it answered (${gone}): $(cat "${dir}.log")"
            return 0
        fi
        if [[ "${i}" -eq 50 ]]; then
            fail "the host listener did not start: ${out}"
            return 0
        fi
        sleep 0.1
    done
    hostDev="$(ip -j route get 192.0.2.1 | jq -r '.[0].dev')"
    hostOther="$(ip -j -4 addr | jq -r --arg out "${hostDev}" '[.[] | select(.ifname != $out and .ifname != "agentbus0") | .addr_info[] | select(.scope == "global") | .local] | first // ""')"
    podman run -d --rm --label ccy-netpolicy-acceptance --network podman --name "${peer}" \
        --entrypoint python3 "${image}" -m http.server 8080 >/dev/null
    # Wait inside the peer for its own server (its main process: if that dies, the exec fails).
    if ! out="$(podman exec "${peer}" python3 -c '
import socket, sys, time
for _ in range(50):
    try:
        socket.create_connection(("127.0.0.1", 8080), 1).close()
        sys.exit(0)
    except OSError:
        time.sleep(0.2)
sys.exit("nothing listens on 8080")
' 2>&1)"; then
        fail "the peer did not start listening: ${out}"
        return 0
    fi
    peerIp="$(podman inspect --format '{{(index .NetworkSettings.Networks "podman").IPAddress}}' "${peer}")"
    local -a targets=("http|host-alias|host.containers.internal|${port}" "http|peer-session|${peerIp}|8080")
    local -a sessionOnly=()
    if [[ -n "${hostOther}" ]]; then
        targets+=("http|host-other|${hostOther}|${port}")
        sessionOnly+=("ssh|host-other-ssh|${hostOther}|${port}")
    else
        printf 'NOTE the host has no global address off its outbound interface %s; host.containers.internal is its only address a session can reach\n' "${hostDev}"
    fi

    # The control: the same targets from the peer, which has no policy, must answer.
    probe "${peer}" "${targets[@]}" || return 0
    for line in "${PROBED[@]}"; do
        if [[ "$(field "${line}" rc)" == 0 ]]; then pass "control reaches it without the policy: ${line}"; else fail "control could not reach it, so a refusal would prove nothing: ${line}"; fi
    done

    probe "${c}" "${targets[@]}" "${sessionOnly[@]+"${sessionOnly[@]}"}" || return 0
    for line in "${PROBED[@]}"; do
        judge_blocked "${line}"
    done
}

check_allowed() {
    local c="$1" line rc http label
    probe "${c}" "dns|dns-forge|api.github.com" "url|forge|https://api.github.com/" "url|anthropic|https://api.anthropic.com/" || return 0
    for line in "${PROBED[@]}"; do
        label="$(awk '{ print $2 }' <<<"${line}")"
        rc="$(field "${line}" rc)"
        http="$(field "${line}" http)"
        if [[ "${rc}" == 0 ]] && [[ "${http}" != 000 ]]; then pass "${label}: ${line}"; else fail "${label} is not reachable: ${line}"; fi
    done
}

# ccy_in <launcher> <checkout> <args...> — the launcher run in the checkout, as the owner runs it.
ccy_in() {
    local launcher="$1" checkout="$2"
    shift 2
    (cd -- "${checkout}" && "${launcher}" "$@") </dev/null
}

# peer_reach <session> <target> <want: reached|refused> <when>
peer_reach() {
    local c="$1" target="$2" want="$3" when="$4" line
    probe "${c}" "${target}" || return 0
    line="${PROBED[0]}"
    if [[ "${want}" == refused ]]; then
        judge_blocked "${line}"
    elif [[ "$(field "${line}" rc)" == 0 ]]; then
        pass "${when}: the peer is reached: ${line}"
    else
        fail "${when}: the peer is not reached: ${line}"
    fi
}

# context_names <session> <network> <yes|no> <when> — the session's context file names it or not.
context_names() {
    local c="$1" net="$2" want="$3" when="$4" text
    if ! text="$(podman exec "${c}" cat /run/ccy/network-policy 2>&1)"; then
        fail "${when}: ${c}'s context file cannot be read: ${text}"
    elif [[ "${text}" == *"project network ${net} ("* ]] && [[ "${want}" == yes ]]; then
        pass "${when}: ${c}'s context file names ${net}"
    elif [[ "${text}" != *"project network ${net} ("* ]] && [[ "${want}" == no ]]; then
        pass "${when}: ${c}'s context file no longer names ${net}"
    else
        fail "${when}: ${c}'s context file is not as expected (names ${net}: ${want}): ${text}"
    fi
}

check_connect() {
    local c="$1" image="$2" launcher="$3" checkout="$4" net peer peerIp name out pref saved="" connected=""
    net="ccy-np232-net-$$"
    peer="ccy-netpolicy-acceptance-net-$$"
    # The saved default network, as the launcher keeps it (lib/network-management.bash
    # get_network_persistence_file): --connect writes it and --disconnect clears it.
    pref="${HOME}/.claude-tokens/ccy/projects/$(printf '%s' "${checkout}" | sha256sum | cut -c1-16)/network"
    if [[ -f "${pref}" ]]; then saved="$(cat -- "${pref}")"; fi
    stop_connect() {
        trap - RETURN
        if [[ -n "${connected}" ]] && ! out="$(ccy_in "${launcher}" "${checkout}" --disconnect "${net}" 2>&1)"; then
            fail "the cleanup's ccy --disconnect ${net} failed: ${out}"
        fi
        if podman container exists "${peer}"; then podman rm -f -t 0 "${peer}" >/dev/null; fi
        if podman network exists "${net}"; then podman network rm "${net}" >/dev/null; fi
        if [[ -n "${saved}" ]]; then printf '%s\n' "${saved}" >"${pref}"; fi
    }
    trap stop_connect RETURN
    podman network create --label ccy-netpolicy-acceptance "${net}" >/dev/null
    podman run -d --rm --label ccy-netpolicy-acceptance --network "${net}" --name "${peer}" \
        --entrypoint python3 "${image}" -m http.server 8080 >/dev/null
    peerIp="$(podman inspect --format "{{(index .NetworkSettings.Networks \"${net}\").IPAddress}}" "${peer}")"
    printf 'NOTE throwaway network %s, peer %s at %s\n' "${net}" "${peer}" "${peerIp}"
    peer_reach "${c}" "http|net-peer|${peerIp}|8080" refused "before --connect"

    if ! out="$(ccy_in "${launcher}" "${checkout}" --connect "${net}" 2>&1)"; then
        fail "ccy --connect ${net} failed: ${out}"
        return 0
    fi
    connected=1
    name="$(podman inspect --format '{{.Name}}' "${c}")"
    if [[ "${out}" == *"Network policy of ${name#/} refreshed"* ]]; then pass "ccy --connect refreshed ${name#/}'s policy: $(awk '/refreshed/' <<<"${out}")"; else fail "ccy --connect printed no refresh of ${name#/}: ${out}"; fi
    peer_reach "${c}" "http|net-peer|${peerIp}|8080" reached "after --connect"
    context_names "${c}" "${net}" yes "after --connect"

    if ! out="$(ccy_in "${launcher}" "${checkout}" --disconnect "${net}" 2>&1)"; then
        fail "ccy --disconnect ${net} failed: ${out}"
        return 0
    fi
    connected=""
    peer_reach "${c}" "http|net-peer|${peerIp}|8080" refused "after --disconnect"
    context_names "${c}" "${net}" no "after --disconnect"
}

check_context() {
    local c="$1" cmd
    cmd="$(podman inspect --format '{{json .Config.Cmd}}' "${c}")"
    if [[ "${cmd}" == *"${MARKER}"* ]]; then pass "${c} was told its network limits at start"; else fail "${c}'s command line carries no '${MARKER}' system prompt"; fi
}

# member_json <session> <team> <seat> — the seat's bundle, as the session sees it.
member_json() {
    podman exec "$1" cat "/workspace/${SEATS_REL}/$2/$3/member.json"
}

# bus <session> <team> <pingbus args...> — the session's pingbus, as its own session runs it.
bus() {
    local c="$1" team="$2"
    shift 2
    podman exec -i "${c}" env "PINGBUS_HOME=${CTR_PINGBUS_HOME}" "PINGBUS_TEAMS=${team}" pingbus --team "${team}" "$@"
}

check_bus() {
    local a="$1" b="$2" team="$3" seatA="$4" seatB="$5" urlA idA idB line
    urlA="$(member_json "${a}" "${team}" "${seatA}" | jq -r '.base_url')"
    idA="$(member_json "${a}" "${team}" "${seatA}" | jq -r '.user_id')"
    idB="$(member_json "${b}" "${team}" "${seatB}" | jq -r '.user_id')"
    probe "${a}" "url|homeserver|${urlA%/}/_matrix/client/versions" || return 0
    line="${PROBED[0]}"
    if [[ "$(field "${line}" http)" == 200 ]]; then pass "${a} reaches its homeserver: ${line}"; else fail "${a} cannot reach its homeserver: ${line}"; fi
    send_and_see "${a}" "${b}" "${idB}" "${team}"
    send_and_see "${b}" "${a}" "${idA}" "${team}"
}

# send_and_see <from> <to> <to-user-id> <team> — one message, then up to 60 s for it to arrive.
send_and_see() {
    local from="$1" to="$2" toId="$3" team="$4" nonce out i
    nonce="netpolicy-acceptance-$(date +%s)-${RANDOM}"
    if ! out="$(printf 'Plan 00232 acceptance: a network-policy probe message, %s. No reply needed.\n' "${nonce}" |
        bus "${from}" "${team}" say --to "${toId}" 2>&1)"; then
        fail "${from} could not send to ${toId}: ${out}"
        return 0
    fi
    pass "${from} sent ${nonce}"
    for i in $(seq 1 30); do
        if out="$(bus "${to}" "${team}" inbox 2>&1 </dev/null)" && [[ "${out}" == *"${nonce}"* ]]; then
            pass "${to} received ${nonce} after about $((i * 2)) s"
            return 0
        fi
        sleep 2
    done
    fail "${to} did not receive ${nonce} within 60 s; its last inbox read: ${out}"
}

case "${1-}" in
    caps) check_caps "${2:?caps <session>}" ;;
    held) check_held "${2:?held <session>}" ;;
    blocked) check_blocked "${2:?blocked <session> <image>}" "${3:?blocked <session> <image>}" ;;
    allowed) check_allowed "${2:?allowed <session>}" ;;
    connect) check_connect "${2:?connect <session> <image> <launcher> <checkout>}" "${3:?connect <session> <image> <launcher> <checkout>}" \
        "${4:?connect <session> <image> <launcher> <checkout>}" "${5:?connect <session> <image> <launcher> <checkout>}" ;;
    context) check_context "${2:?context <session>}" ;;
    bus) check_bus "${2:?bus <session> <peer> <team> <seat> <peer-seat>}" "${3:?bus <session> <peer> <team> <seat> <peer-seat>}" \
        "${4:?bus <session> <peer> <team> <seat> <peer-seat>}" "${5:?bus <session> <peer> <team> <seat> <peer-seat>}" \
        "${6:?bus <session> <peer> <team> <seat> <peer-seat>}" ;;
    ready) bus "${2:?ready <session> <team>}" "${3:?ready <session> <team>}" inbox </dev/null >/dev/null ;;
    *)
        printf 'usage: %s caps|held|blocked|allowed|connect|context|bus|ready <args>\n' "$0" >&2
        exit 64
        ;;
esac
[[ "${failed}" -eq 0 ]]
