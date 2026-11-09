#!/usr/bin/env bash
# Plan 00232 — test-acceptance-checks.bash: unit tests for the acceptance gate's own design, run
# against fakes, so a check that cannot pass (or cannot fail) on the host is caught here first.
# Pure: runs anywhere, touches nothing outside a temporary directory.
#
#   blocked  the host-other target is one the control (no policy) reaches; with no such
#            address the check exits 2 (COULD NOT ESTABLISH), never a pass on less measured
#   bus      delivery is read from the receiver's room history by the sent event id, so a
#            session that takes a turn on the message (and consumes it from its inbox) still
#            shows it arrived; a message that never arrives still fails
#   connect  the throwaway project's session is launched on the podman network, the shape an
#            ordinary session has, since a session with no network (pasta) can join none
#
# The fakes: `podman` (exec, run, inspect, rm, the probe and pingbus answered from FAKE_*
# variables), `ip` (the host's routes and addresses) and `sleep` (instant for whole seconds, so
# a 60 s wait costs nothing). Addresses are documentation ranges.
#
# Usage: ./CLAUDE/Plan/00232-ccy-network-policy-per-container/test-acceptance-checks.bash
set -euo pipefail

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CHECKS="${scriptDir}/acceptance-checks.bash"
work="$(mktemp -d)"
trap 'rm -rf -- "${work}"' EXIT
fakeBin="${work}/bin"
mkdir -p -- "${fakeBin}" "${work}/room"

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
        printf 'FAIL %s\n  missing: %s\n  in: %s\n' "${name}" "${needle}" "${hay}"
        failures=$((failures + 1))
    fi
}

realSleep="$(command -v sleep)"
cat >"${fakeBin}/sleep" <<EOF
#!/usr/bin/env bash
[[ "\$1" =~ ^[0-9]+\$ ]] && exit 0
exec "${realSleep}" "\$@"
EOF

# FAKE_ADDRS: "ifname=address ..." the host's global IPv4 addresses; the outbound is wan0.
cat >"${fakeBin}/ip" <<'EOF'
#!/usr/bin/env bash
case "$*" in
    "-j route get "*) printf '[{"dev":"wan0"}]\n' ;;
    "-j -4 addr")
        for pair in ${FAKE_ADDRS}; do
            printf '{"ifname":"%s","addr_info":[{"scope":"global","local":"%s"}]}\n' "${pair%%=*}" "${pair#*=}"
        done | jq -s . ;;
    *) echo "fake ip: unexpected: $*" >&2; exit 99 ;;
esac
EOF

# FAKE_CONTROL_REACHES: the host addresses the control (the peer container) reaches.
# FAKE_DELIVER: 1, the room holds what was said; else it is lost.
# FAKE_ROOM: where `say` keeps each message, one file per event id, named for its addressee.
cat >"${fakeBin}/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
probe() {
    local c="$1" spec kind label host rc http
    shift
    for spec in "$@"; do
        IFS='|' read -r kind label host _ <<<"${spec}"
        rc=7 http=000
        if [[ "${c}" == ccy-netpolicy-acceptance-* ]]; then
            case "${label}" in
                host-alias | peer-session) rc=0 ;;
                *) if [[ " ${FAKE_CONTROL_REACHES:-} " == *" ${host} "* ]]; then rc=0; fi ;;
            esac
        elif [[ "${label}" == homeserver ]]; then
            rc=0
        fi
        if [[ "${rc}" -eq 0 ]]; then http=200; fi
        printf 'RESULT %s kind=%s rc=%s ms=5 http=%s err=-\n' "${label}" "${kind}" "${rc}" "${http}"
    done
}
pingbus() {
    local c="$1" team="" verb id f
    shift
    if [[ "$1" == --team ]]; then team="$2"; shift 2; fi
    verb="$1"
    case "${verb}" in
        say)
            id="\$ev${RANDOM}${RANDOM}:example.org"
            if [[ "${FAKE_DELIVER:-}" == 1 ]]; then
                printf '%s\n' "$3" >"${FAKE_ROOM}/${id}.to"
                cat >"${FAKE_ROOM}/${id}.text"
            else
                cat >/dev/null
            fi
            printf 'SENT\t1\t%s\t%s\n' "${team}" "${id}" ;;
        inbox) ;;
        history)
            for f in "${FAKE_ROOM}"/*.to; do
                [[ -e "${f}" ]] || continue
                id="${f##*/}"
                id="${id%.to}"
                [[ "$(cat -- "${f}")" == "@${c}:example.org" ]] || continue
                printf 'HISTORY\t1\tin\t1000\tPEER\t%s\t%s\tsender\t1000\t%s\t-\n' "${team}" "${id}" "$(jq -Rs . <"${FAKE_ROOM}/${id}.text")"
            done ;;
        *) echo "fake pingbus: unexpected: $*" >&2; exit 99 ;;
    esac
}
case "$1" in
    exec)
        shift
        if [[ "$1" == -i ]]; then shift; fi
        c="$1"
        shift
        case "$1" in
            bash) shift 3; cat >/dev/null; probe "${c}" "$@" ;;
            python3) ;;
            cat) seat="${2%/member.json}"; seat="${seat##*/}"
                 printf '{"base_url":"http://192.0.2.9:8008","user_id":"@%s:example.org"}\n' "${seat}" ;;
            env) shift 3; if [[ "$1" == pingbus ]]; then shift; fi; pingbus "${c}" "$@" ;;
            *) echo "fake podman: unexpected exec: $*" >&2; exit 99 ;;
        esac ;;
    run) echo fakeid ;;
    inspect) echo 192.0.2.50 ;;
    container) exit 0 ;;
    rm) ;;
    *) echo "fake podman: unexpected: $*" >&2; exit 99 ;;
esac
EOF
chmod +x "${fakeBin}"/*

run_checks() {
    local rc=0
    OUT="$(PATH="${fakeBin}:${PATH}" FAKE_ROOM="${work}/room" "${CHECKS}" "$@" 2>&1)" || rc=$?
    RC="${rc}"
}
fail_lines() { awk '/^FAIL/ { n++ } END { print n + 0 }' <<<"$1"; }

echo "=== blocked: host-other is an address the control reaches ==="
# lan0's address is the one pasta copies into the rootless namespace, so even the control
# cannot reach it; lxc0's is reached. The first off-outbound address is lan0's.
export FAKE_ADDRS="wan0=198.51.100.10 lan0=203.0.113.5 lxc0=203.0.113.9 agentbus0=203.0.113.77"
export FAKE_CONTROL_REACHES="203.0.113.9"
run_checks blocked session1 claude-yolo:latest
check "an address the control reaches is chosen: the check passes" "0" "${RC}"
contains "  and the session is refused it" "PASS host-other:203.0.113.9 refused" "${OUT}"
contains "  over ssh too" "PASS host-other-ssh:203.0.113.9 refused" "${OUT}"
contains "  and the one the control cannot reach is named, not used" "NOTE not put to the session, since the control cannot reach it either: RESULT host-other:203.0.113.5" "${OUT}"
check "  and no FAIL line" "0" "$(fail_lines "${OUT}")"

export FAKE_CONTROL_REACHES=""
run_checks blocked session1 claude-yolo:latest
check "no address the control reaches: COULD NOT ESTABLISH (2), not a pass" "2" "${RC}"
contains "  and it says so" "COULD NOT ESTABLISH" "${OUT}"

export FAKE_ADDRS="wan0=198.51.100.10 agentbus0=203.0.113.77"
run_checks blocked session1 claude-yolo:latest
check "no host address but the outbound one and the bus: COULD NOT ESTABLISH (2)" "2" "${RC}"

echo "=== bus: delivery read from the receiver's room history ==="
export FAKE_DELIVER=1
rm -f -- "${work}/room"/*
run_checks bus np232a np232b team1 np232a np232b
check "delivered each way, though nothing is left pending in either inbox: passes" "0" "${RC}"
contains "  np232b received it" "PASS np232b received" "${OUT}"
contains "  np232a received it" "PASS np232a received" "${OUT}"

export FAKE_DELIVER=0
rm -f -- "${work}/room"/*
run_checks bus np232a np232b team1 np232a np232b
check "lost: fails" "1" "${RC}"
contains "  and names what did not arrive" "did not receive" "${OUT}"

echo "=== connect: the throwaway session is launched on the podman network ==="
conf="${work}/last-launch.conf"
printf 'LAST_TOKEN="t1"\nLAST_SSH_KEYS=""\nLAST_NETWORK=""\nLAST_TEAMS="team1"\n' >"${conf}"
got="$(
    export PLAN_RUN_DIR="${work}/run" LAST_LAUNCH="${conf}" CONNECT_KEY=np232c
    mkdir -p -- "${PLAN_RUN_DIR}"
    # shellcheck source-path=SCRIPTDIR
    # shellcheck source=_acceptance-sessions.inc.bash
    source "${scriptDir}/_acceptance-sessions.inc.bash"
    launch() { printf 'launch %s\n' "$1"; }
    start_connect >/dev/null
    grep -E '^LAST_(NETWORK|TEAMS)=' "${CONNECT_DIR}/.claude/ccy/.last-launch.conf" | tr '\n' ' '
)"
check "its Quick Launch names the podman network and no team" 'LAST_NETWORK="podman" LAST_TEAMS="" ' "${got}"

echo
if [[ "${failures}" -ne 0 ]]; then
    printf '%d FAILED\n' "${failures}"
    exit 1
fi
printf 'all passed\n'
