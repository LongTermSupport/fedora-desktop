#!/usr/bin/env bash
# Plan 00232 — acceptance-checks.bash: the checks acceptance.bash runs, one per invocation, so
# each is its own check. Each prints PASS or FAIL lines with the evidence and exits 0 only when all
# of its lines pass.
#
#   caps <session>                 the session is not privileged and holds neither NET_ADMIN nor
#                                  SYS_ADMIN, so nothing inside can change its network namespace
#   held <session>                 the policy table is in the session's own network namespace,
#                                  read from the host
#   blocked <session> <image>      a host listener (through host.containers.internal, and on each
#                                  host address off the outbound interface that the control
#                                  reaches: pasta copies its template interface's address into the
#                                  rootless namespace, so that one never reaches the host, policy
#                                  or not) and a peer on the podman network are
#                                  reachable from a throwaway container WITHOUT the policy (the
#                                  control), and refused from the session at once (under 2 s, not
#                                  a timeout); exit 2 (COULD NOT ESTABLISH) when the control
#                                  reaches no such host address
#   allowed <session>              DNS, the forge and the Claude API still work
#   sole <session>                 the session is the only running container `ccy --connect` in
#                                  its project reaches; exit 2 (COULD NOT ESTABLISH), naming the
#                                  others, when it is not
#   connect <session> <image> <launcher> <project-dir>
#                                  a peer on a throwaway project network is refused, then reached
#                                  after `ccy --connect <network>` run in <project-dir> (whose
#                                  refresh opens its subnet and names it in the session's context
#                                  file), then refused again after `ccy --disconnect <network>`;
#                                  the session must be its project's sole one (exit 2 if not)
#   context <session>              the session was started with its network limits in its system
#                                  prompt (the marker CCY NETWORK POLICY)
#   bus <session> <peer> <team> <seat> <peer-seat>
#                                  both reach their homeserver, and a message sent each way with
#                                  `pingbus say` is in the other's `pingbus history` (read from
#                                  the room by the receiver's own seat) as a PEER item it received,
#                                  by the event id say printed. Not `pingbus inbox`: it shows only
#                                  what is still pending, never the text, and the receiving
#                                  session, woken by the message, consumes it. Both must be
#                                  throwaway seated sessions of <team> (not working agents: they
#                                  receive a message)
#   ready <session> <team>         the session's pingbus answers for <team> (its seat is claimed);
#                                  no PASS line, only the status
#
# Every probe must give one RESULT line per target and exit 0 (probe), or the check fails: a
# check never passes on nothing measured.
#
# Side effects: `blocked` runs a throwaway container (label ccy-netpolicy-acceptance) and a
# python3 http.server on a random high port for its duration; `connect` makes a throwaway
# network (same label) with a peer on it, and `ccy --connect` in <project-dir> joins the one
# session it is checked to reach to it until its `ccy --disconnect`; that project's saved
# default network is put back as it was. `bus` posts one message each way in <team>'s room. Nothing else is changed.
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
unestablished=0

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
    local c="$1" image="$2" dir port pid hostDev peer peerIp line out i
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
    # Every host address but the outbound interface's (refused even unpoliced, above) and the
    # bus's (the policy allows it): the control is asked which of them it reaches.
    local addrs
    local -a hostOthers=()
    addrs="$(ip -j -4 addr | jq -r --arg out "${hostDev}" '.[] | select(.ifname != $out and .ifname != "agentbus0") | .addr_info[] | select(.scope == "global") | .local')"
    if [[ -n "${addrs}" ]]; then mapfile -t hostOthers <<<"${addrs}"; fi
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
    local -a candidates=() sessionOnly=()
    local addr label
    for addr in "${hostOthers[@]+"${hostOthers[@]}"}"; do
        candidates+=("http|host-other:${addr}|${addr}|${port}")
    done

    # The control: the same targets from the peer, which has no policy, must answer. Of the
    # other host addresses only those it reaches are put to the session: pasta copies its
    # template interface's address into the rootless namespace, so that one is local there and
    # unreachable, policy or not, and a refusal of it would prove nothing.
    probe "${peer}" "${targets[@]}" "${candidates[@]+"${candidates[@]}"}" || return 0
    for line in "${PROBED[@]}"; do
        label="$(awk '{ print $2 }' <<<"${line}")"
        if [[ "${label}" != host-other:* ]]; then
            if [[ "$(field "${line}" rc)" == 0 ]]; then pass "control reaches it without the policy: ${line}"; else fail "control could not reach it, so a refusal would prove nothing: ${line}"; fi
        elif [[ "$(field "${line}" rc)" == 0 ]]; then
            pass "control reaches it without the policy: ${line}"
            addr="${label#host-other:}"
            targets+=("http|${label}|${addr}|${port}")
            sessionOnly+=("ssh|host-other-ssh:${addr}|${addr}|${port}")
        else
            printf 'NOTE not put to the session, since the control cannot reach it either: %s\n' "${line}"
        fi
    done
    if [[ "${#sessionOnly[@]}" -eq 0 ]]; then
        printf 'COULD NOT ESTABLISH: no host address off the outbound interface %s is reachable without the policy (of: %s), so the host is measured only through host.containers.internal\n' \
            "${hostDev}" "${hostOthers[*]:-none}"
        unestablished=1
    fi

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

# check_sole <session> — `ccy --connect` reaches every running container whose name starts
# <project>_yolo (lib/network-management.bash _project_running_containers), the project being
# the session's ccy-project label. Exit 2 unless this session is the only one.
check_sole() {
    local c="$1" project name others
    project="$(podman inspect --format '{{index .Config.Labels "ccy-project"}}' "${c}")"
    name="$(podman inspect --format '{{.Name}}' "${c}")"
    name="${name#/}"
    others="$(podman ps --format '{{.Names}}' | awk -v p="${project}_yolo" -v n="${name}" 'index($0, p) == 1 && $0 != n')"
    if [[ -z "${project}" ]] || [[ "${name}" != "${project}_yolo"* ]]; then
        printf 'COULD NOT ESTABLISH: %s (project %s) is not a session ccy --connect in its project would reach\n' "${name}" "${project:-none}" >&2
        exit 2
    fi
    if [[ -n "${others}" ]]; then
        printf 'COULD NOT ESTABLISH: ccy --connect in project %s would also reach %s, not only %s\n' "${project}" "${others//$'\n'/ }" "${name}" >&2
        exit 2
    fi
    pass "${name} is the only running session ccy --connect in project ${project} reaches"
}

check_connect() {
    local c="$1" image="$2" launcher="$3" checkout="$4" net peer peerIp name out pref saved="" connected=""
    check_sole "${c}"
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
# Arrival is read from the receiver's own `pingbus history` (from the room, through its own
# network): a PEER item it received, with the event id `say` printed and the nonce in its text.
# Not `pingbus inbox`: that lists only what is still pending, and never the text, while the
# receiving session is woken by the message and consumes it.
send_and_see() {
    local from="$1" to="$2" toId="$3" team="$4" nonce out eventId i
    nonce="netpolicy-acceptance-$(date +%s)-${RANDOM}"
    if ! out="$(printf 'Plan 00232 acceptance: a network-policy probe message, %s. No reply needed.\n' "${nonce}" |
        bus "${from}" "${team}" say --to "${toId}" 2>&1)"; then
        fail "${from} could not send to ${toId}: ${out}"
        return 0
    fi
    # SENT <version> <team> <event id>, tab-separated.
    eventId="$(awk -F '\t' '$1 == "SENT" { print $4 }' <<<"${out}")"
    if [[ -z "${eventId}" ]]; then
        fail "${from}'s pingbus say printed no SENT line: ${out}"
        return 0
    fi
    pass "${from} sent ${nonce} as ${eventId}"
    for i in $(seq 1 30); do
        # HISTORY <version> in <ts> PEER <team> <event id> <sender> <ts> <text> ..., tab-separated.
        if out="$(bus "${to}" "${team}" history --limit 50 2>&1 </dev/null)" &&
            awk -F '\t' -v id="${eventId}" -v nonce="${nonce}" \
                '$1 == "HISTORY" && $3 == "in" && $5 == "PEER" && $7 == id && index($0, nonce) { found = 1 } END { exit !found }' <<<"${out}"; then
            pass "${to} received ${eventId} after about $((i * 2)) s"
            return 0
        fi
        sleep 2
    done
    fail "${to} did not receive ${eventId} (${nonce}) within 60 s; its last history read: ${out}"
}

case "${1-}" in
    caps) check_caps "${2:?caps <session>}" ;;
    held) check_held "${2:?held <session>}" ;;
    blocked) check_blocked "${2:?blocked <session> <image>}" "${3:?blocked <session> <image>}" ;;
    allowed) check_allowed "${2:?allowed <session>}" ;;
    sole) check_sole "${2:?sole <session>}" ;;
    connect) check_connect "${2:?connect <session> <image> <launcher> <project-dir>}" "${3:?connect <session> <image> <launcher> <project-dir>}" \
        "${4:?connect <session> <image> <launcher> <project-dir>}" "${5:?connect <session> <image> <launcher> <project-dir>}" ;;
    context) check_context "${2:?context <session>}" ;;
    bus) check_bus "${2:?bus <session> <peer> <team> <seat> <peer-seat>}" "${3:?bus <session> <peer> <team> <seat> <peer-seat>}" \
        "${4:?bus <session> <peer> <team> <seat> <peer-seat>}" "${5:?bus <session> <peer> <team> <seat> <peer-seat>}" \
        "${6:?bus <session> <peer> <team> <seat> <peer-seat>}" ;;
    ready) bus "${2:?ready <session> <team>}" "${3:?ready <session> <team>}" inbox </dev/null >/dev/null ;;
    *)
        printf 'usage: %s caps|held|blocked|allowed|sole|connect|context|bus|ready <args>\n' "$0" >&2
        exit 64
        ;;
esac
[[ "${failed}" -eq 0 ]] || exit 1
[[ "${unestablished}" -eq 0 ]] || exit 2
