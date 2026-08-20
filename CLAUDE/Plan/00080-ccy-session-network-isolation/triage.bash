#!/usr/bin/env bash
# Plan 00080 triage — grounded facts for CCY session network isolation.
#
# Run this on the HOST (not inside a CCY container). It writes its full report
# to this plan's logs/ directory (gitignored), so an agent can read it from
# inside a container at the same repo-relative path.
#
# TWO MODES:
#   (default)        READ-ONLY. Queries podman and reads /proc inside live
#                    sessions via `podman exec`. Starts, stops, creates and
#                    removes NOTHING.
#   --reachability   Additionally creates throwaway containers and one throwaway
#                    network, all named ccy80-probe-*, and removes them again.
#                    Never touches a live session.
#
# Probes map to PLAN.md hypotheses and to research/findings.md's U-numbers:
#   P1  U1   podman/netavark versions — decides whether bridge isolation is
#            default-strict (netavark 2.0 / podman 6.0) or must be asked for
#   P2  F4   the default network's real config (DNS off? isolate? subnet?)
#   P3  U10  who is ACTUALLY on the shared bridge — CCY sessions and what else
#   P4  U3   THE decisive probe: what does a live session LISTEN on, and where
#   P5  U10  every network and its member count
#   P6  U2   can two containers on the shared bridge reach each other? (H1)
#   P7  U1   is a second network isolated from the first by default?
#   P8  U6   does `network connect` still work from a user-created bridge?
#   P9  U5   does a fresh network still reach the internet and the host?
#   P10 U9   would a fresh network be DNS-enabled? (ensure_network_dns fallout)
#   P11 U4   does --rm survive SIGKILL of the podman client? (the leak rate)
#   P12 U8   where network configs live; does a leak strand an interface?
set -euo pipefail

usage() {
    cat << 'EOF'
Plan 00080 — CCY session network isolation triage (HOST ONLY)

Usage: triage.bash [--reachability] [--help]

  --reachability   Also run the active probes (P6-P12). These create and remove
                   throwaway containers and one throwaway network, all named
                   ccy80-probe-*. They never touch a live session.
  --help           Show this help and exit (creates nothing).

Default is passive: it only queries podman and reads /proc inside live sessions.

Writes its report to this plan's logs/network-isolation-triage.log.

PRIVACY: the report names container names, projects, GitHub accounts and IP
addresses. logs/ is gitignored for that reason — do not paste it into an issue,
a PR, or a gist.
EOF
}

REACHABILITY=0
for arg in "$@"; do
    case "$arg" in
        -h | --help)
            usage
            exit 0
            ;;
        --reachability)
            REACHABILITY=1
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: triage.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"

if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "ERROR: this looks like a CCY container (/workspace)." >&2
    echo "  This triage inspects the HOST's podman, which is not reachable" >&2
    echo "  from in here. Run it on the host." >&2
    exit 1
fi

if ! command -v podman > /dev/null; then
    echo "ERROR: podman is not installed." >&2
    echo "  It is declared in playbooks/imports/play-podman.yml. Deploy it:" >&2
    echo "    ansible-playbook playbooks/imports/play-podman.yml" >&2
    echo "  Do NOT install it by hand." >&2
    exit 1
fi

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/network-isolation-triage.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

# A non-zero exit is DATA here, not a failure — capture it and carry on.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

PROBE_IMAGE="docker.io/library/alpine"
PROBE_PREFIX="ccy80-probe"

echo "================================================================"
echo "Plan 00080 triage — CCY session network isolation"
echo "  mode: $([ "$REACHABILITY" -eq 1 ] && echo 'passive + reachability' || echo 'passive only')"
echo "================================================================"
echo

# =============================================================================
# P1 — versions. GATING for everything about isolation defaults.
# =============================================================================
echo "### READ THIS FOR: P1/U1 — which side of the isolation change is this host?"
echo "###   netavark 2.0 (with podman 6.0) made bridge networks STRICTLY isolated"
echo "###   from each other by default. BELOW that, a per-session network would"
echo "###   look isolated without being isolated unless --opt isolate= is passed."
echo "###   This single number decides whether Option 2 is a flag or a trap."
probe "podman --version" podman --version
probe "network backend" podman info --format '{{.Host.NetworkBackend}}'
probe "backend version" podman info --format '{{.Host.NetworkBackendInfo.Backend}} {{.Host.NetworkBackendInfo.Version}}'
probe "aardvark-dns" podman info --format '{{.Host.NetworkBackendInfo.DNS.Package}} {{.Host.NetworkBackendInfo.DNS.Version}}'
probe "rootless network cmd (expect pasta)" podman info --format '{{.Host.RootlessNetworkCmd}}'
probe "rpm versions" rpm -q podman netavark aardvark-dns

# =============================================================================
# P2 — the shared network's real configuration.
# =============================================================================
echo "### READ THIS FOR: P2/F4 — the default network as configured, not as documented"
echo "###   Expect dns_enabled=false (the default network has no DNS, so any"
echo "###   cross-session reach is BY IP ONLY) and no isolate option set."
probe "network inspect podman" podman network inspect podman

# =============================================================================
# P3 — membership. "Seven sessions" was a count of CCY, not of members.
# =============================================================================
echo "### READ THIS FOR: P3/U10+U11 — the REAL blast radius of the shared bridge"
echo "###   The first list is everything on it, CCY or not. The second decomposes"
echo "###   the CCY rows by project and account: sessions sharing one identity"
echo "###   have far less to take from each other than sessions that do not."
probe "everything on the shared bridge" \
    podman ps --all --filter network=podman --format '{{.Names}}\t{{.State}}\t{{.Image}}'
probe "the CCY rows, by identity" \
    podman ps --all --filter network=podman \
    --format '{{.Names}}\t{{.State}}\tproject={{index .Labels "ccy-project"}}\tgithub={{index .Labels "ccy-github"}}'

# =============================================================================
# P4 — THE decisive probe.
#
# `ss` is NOT in the CCY image (verified from inside one), so /proc is the path
# that actually works, and its raw hex is unreadable. Capture inside, decode
# here. A LISTEN row is st==0A; local_address is little-endian hex.
# =============================================================================
decode_listeners() {
    local session="$1" raw
    if ! raw="$(podman exec "$session" cat /proc/net/tcp /proc/net/tcp6 2>&1)"; then
        echo "  could not read /proc/net/tcp* in $session:"
        printf '%s\n' "$raw"
        return 0
    fi
    printf '%s\n' "$raw" | awk '
        function h2d(s) { return strtonum("0x" s) }
        $4 == "0A" {
            split($2, a, ":")
            addr = a[1]; port = h2d(a[2])
            if (length(addr) == 8) {
                ip = h2d(substr(addr,7,2)) "." h2d(substr(addr,5,2)) "." \
                     h2d(substr(addr,3,2)) "." h2d(substr(addr,1,2))
            } else {
                ip = "[ipv6:" addr "]"
            }
            printf "  LISTEN  %s:%s\n", ip, port
            found = 1
        }
        END { if (!found) print "  (no listening sockets)" }
    '
}

# The session set is the UNION of two selectors, not the label alone.
#
# The first version of this filtered on `label=ccy=true` and guarded only the
# EMPTY case. Run 1 then found five live CCY sessions of which exactly one had
# been relaunched under 3.40.0, probed that one, and printed it under a header
# inviting the reader to conclude "the exposure is THEORETICAL". A 1-of-5 sample
# wearing a 5-of-5 header — an under-match, which is silent, unlike an over-match
# which names itself in the output. That is this repo's own defect class
# (CLAUDE/AgentNotes.md) committed inside the probe written to avoid it.
#
# So: select by label OR by the name pattern podfreeze already uses for
# pre-3.40.0 sessions, probe every one, and state the coverage as a number.
CCY_NAME_PATTERN='^.+_(yolo|browser)(_[0-9]+)?$'

ccy_session_names() {
    local all name
    if ! all="$(podman ps --format '{{.Names}}')"; then
        echo "ERROR: could not list containers: $all" >&2
        return 1
    fi
    while read -r name; do
        if [ -n "$name" ] && [[ "$name" =~ $CCY_NAME_PATTERN ]]; then
            printf '%s\n' "$name"
        fi
    done <<< "$all"
}

show_all_listeners() {
    local labelled name total=0 n_labelled=0
    local -a sessions=()

    if ! labelled="$(podman ps --filter label=ccy=true --format '{{.Names}}')"; then
        echo "ERROR: could not list labelled CCY sessions: $labelled" >&2
        return 1
    fi
    if ! mapfile -t sessions < <(ccy_session_names); then
        return 1
    fi

    total="${#sessions[@]}"
    if [ "$total" -eq 0 ]; then
        # An empty section here would read as "nothing listens", which is the
        # opposite of what it means. Fail loudly instead.
        echo "ERROR: no running container matches a CCY session, by label or by" >&2
        echo "  name pattern ($CCY_NAME_PATTERN). Refusing to report an empty" >&2
        echo "  listener set that would read as 'nothing is exposed'." >&2
        return 1
    fi

    for name in "${sessions[@]}"; do
        if printf '%s\n' "$labelled" | grep -qx -- "$name"; then
            n_labelled=$(( n_labelled + 1 ))
        fi
    done

    echo "  COVERAGE: probing $total CCY session(s); $n_labelled carry ccy=true."
    if [ "$n_labelled" -lt "$total" ]; then
        echo "  NOTE: $(( total - n_labelled )) session(s) predate CCY 3.40.0 and are"
        echo "  unlabelled. They are included here BY NAME. A label-only probe would"
        echo "  have silently covered just $n_labelled of $total — do not read a"
        echo "  label-filtered listener set as a fleet-wide answer."
    fi
    echo ""

    for name in "${sessions[@]}"; do
        if printf '%s\n' "$labelled" | grep -qx -- "$name"; then
            echo "== $name  [labelled]"
        else
            echo "== $name  [UNLABELLED — pre-3.40.0, matched by name]"
        fi
        decode_listeners "$name"
    done
}

echo "### READ THIS FOR: P4/U3 — **THE decisive probe**"
echo "###   0.0.0.0 or [::] = reachable from every other container on the bridge."
echo "###   127.0.0.1 = reachable only from inside that session; harmless here."
echo "###   If every session shows '(no listening sockets)', the exposure is"
echo "###   THEORETICAL and 'change nothing' becomes the proportionate answer."
echo "###   CHECK THE COVERAGE LINE FIRST: that conclusion needs EVERY session"
echo "###   probed, not merely every session the ccy=true label selected."
echo "###   Note this is a SNAPSHOT: a session that is idle now may run a dev"
echo "###   server later, so re-run it while something is actually being built."
probe "listeners inside each live CCY session" show_all_listeners

# =============================================================================
# P5 — networks and member counts.
# =============================================================================
count_members_per_network() {
    local n
    while read -r n; do
        if [ -n "$n" ]; then
            printf '%s\t%s members\n' "$n" \
                "$(podman network inspect "$n" --format '{{len .Containers}}')"
        fi
    done < <(podman network ls --format '{{.Name}}')
}

echo "### READ THIS FOR: P5/U10 — network inventory and how loaded each one is"
# `.NetworkID` is not a field of network.ListPrintReports — run 1 returned rc=125
# with "can't evaluate field NetworkID", printing one row and then dying, so the
# inventory was simply absent. The field is `.ID`.
probe "networks" podman network ls --format '{{.Name}}\t{{.Driver}}\t{{.ID}}'
probe "members per network" count_members_per_network

if [ "$REACHABILITY" -eq 0 ]; then
    echo "================================================================"
    echo "END OF PASSIVE REPORT."
    echo
    echo "Read P4 first — it decides whether this plan has a real problem or a"
    echo "theoretical one. P1 decides whether the obvious fix actually fixes it."
    echo
    echo "The active probes (P6-P12) settle whether containers on the shared"
    echo "bridge CAN reach each other, and whether a per-session network would"
    echo "isolate, keep --connect, and clean up after a kill. They create and"
    echo "remove throwaway ccy80-probe-* objects and never touch a live session:"
    echo "    $0 --reachability"
    echo "================================================================"
    exit 0
fi

# =============================================================================
# ACTIVE PROBES
# =============================================================================
cleanup_probes() {
    local leftover
    echo
    echo "### cleanup"
    if ! leftover="$(podman rm --force --ignore \
        "${PROBE_PREFIX}-listener" "${PROBE_PREFIX}-connect" "${PROBE_PREFIX}-kill" 2>&1)"; then
        echo "  container removal reported: $leftover"
    fi
    if ! leftover="$(podman network rm --force "${PROBE_PREFIX}-net" 2>&1)"; then
        echo "  network removal reported: $leftover"
    fi

    # Assert the end state rather than assuming the rm calls worked — the whole
    # subject of this plan is objects that outlive what should have removed them.
    if leftover="$(podman ps --all --filter "name=${PROBE_PREFIX}" --format '{{.Names}}')" \
        && [ -n "$leftover" ]; then
        echo "  WARNING: probe containers still present: $leftover"
    else
        echo "  no probe containers remain"
    fi
    if leftover="$(podman network ls --filter "name=${PROBE_PREFIX}" --format '{{.Name}}')" \
        && [ -n "$leftover" ]; then
        echo "  WARNING: probe networks still present: $leftover"
    else
        echo "  no probe networks remain"
    fi
}
trap cleanup_probes EXIT

if ! podman image exists "$PROBE_IMAGE"; then
    echo "ERROR: $PROBE_IMAGE is not present locally, and this triage will not" >&2
    echo "  pull it for you — a probe that silently reaches the network is not" >&2
    echo "  read-only in the way this script promises. Pull it deliberately:" >&2
    echo "    podman pull $PROBE_IMAGE" >&2
    exit 1
fi

echo
echo "### READ THIS FOR: P6/U2 — can two containers on the shared bridge talk? (H1)"
echo "###   The listener binds 0.0.0.0 ON PURPOSE: this measures the NETWORK,"
echo "###   not somebody's bind choice. REACHED = H1 confirmed on this host."
probe "start listener on the shared bridge" \
    podman run -d --name "${PROBE_PREFIX}-listener" --network podman \
    "$PROBE_IMAGE" sh -c 'nc -l -p 8080 -e echo REACHED'

LISTENER_IP=""
if ! LISTENER_IP="$(podman inspect "${PROBE_PREFIX}-listener" \
    --format '{{.NetworkSettings.Networks.podman.IPAddress}}' 2>&1)"; then
    echo "ERROR: could not read the listener's IP: $LISTENER_IP" >&2
    echo "  Without it P6-P7 would silently measure nothing." >&2
    exit 1
fi
echo "### listener IP: $LISTENER_IP"
echo

probe "same bridge -> listener (expect REACHED)" \
    podman run --rm --network podman "$PROBE_IMAGE" \
    sh -c "nc -w 3 $LISTENER_IP 8080"

echo "### READ THIS FOR: P7/U1 — is a SECOND network isolated from the first?"
echo "###   REACHED here means this host predates strict isolation, so a"
echo "###   per-session network would NOT isolate unless --opt isolate= is set."
echo "###   Silence/timeout means isolation is already the default."
probe "create probe network" podman network create "${PROBE_PREFIX}-net"
probe "other network -> listener (expect NO output)" \
    podman run --rm --network "${PROBE_PREFIX}-net" "$PROBE_IMAGE" \
    sh -c "nc -w 3 $LISTENER_IP 8080"

echo "### READ THIS FOR: P8/U6 — does --connect survive a per-session network?"
echo "###   This is the ONLY reason the shared bridge exists (F2). rc=0 means"
echo "###   Option 2 keeps what the shared bridge was adopted to preserve."
probe "container on a user-created bridge" \
    podman run -d --name "${PROBE_PREFIX}-connect" --network "${PROBE_PREFIX}-net" \
    "$PROBE_IMAGE" sleep 60
probe "network connect onto a second network" \
    podman network connect podman "${PROBE_PREFIX}-connect"

echo "### READ THIS FOR: P9/U5 — does a fresh network still reach the internet + host?"
echo "###   If not, Option 2 breaks Claude Code itself and is dead on arrival."
probe "egress + host alias from the probe network" \
    podman run --rm --network "${PROBE_PREFIX}-net" "$PROBE_IMAGE" \
    sh -c 'wget -q -O- --timeout=10 http://example.com > /dev/null && echo EGRESS-OK; getent hosts host.containers.internal || echo NO-HOST-ALIAS'

echo "### READ THIS FOR: P10/U9 — would a per-session network change DNS behaviour?"
echo "###   User-created networks are DNS-ENABLED by default, and CCY's"
echo "###   ensure_network_dns() adds public resolvers to any network that is."
echo "###   dns=true here means Option 2 would ship a resolver change nobody"
echo "###   asked for unless --disable-dns is passed at create time."
probe "probe network DNS config" \
    podman network inspect "${PROBE_PREFIX}-net" \
    --format 'dns={{.DNSEnabled}} subnets={{range .Subnets}}{{.Subnet}} {{end}} opts={{json .Options}}'

echo "### READ THIS FOR: P11/U4 — THE LEAK RATE, which decides Option 2's viability"
echo "###   Kills the podman CLIENT and asks whether --rm still cleaned up."
echo "###   A surviving container means a per-session network cannot be removed"
echo "###   either, so every crash would strand one. That is the whole risk."
probe "start a container to kill" \
    podman run -d --name "${PROBE_PREFIX}-kill" --network "${PROBE_PREFIX}-net" \
    "$PROBE_IMAGE" sleep 300
probe "kill the podman client process" pkill -KILL -f "podman .*${PROBE_PREFIX}-kill"
probe "did --rm still fire?" \
    podman ps --all --filter "name=${PROBE_PREFIX}-kill" --format '{{.Names}}\t{{.State}}'
probe "can the network be removed while that is so? (rc!=0 => stranded)" \
    podman network rm "${PROBE_PREFIX}-net"

echo "### READ THIS FOR: P12/U8 — where a leak would physically live"
probe "network configs (config home)" \
    ls -la "${XDG_CONFIG_HOME:-$HOME/.config}/containers/networks/"
probe "network configs (data home)" \
    ls -la "$HOME/.local/share/containers/networks/"
probe "interfaces in the rootless netns" \
    podman unshare --rootless-netns ip -br link show

echo
echo "================================================================"
echo "END OF REPORT."
echo
echo "Read in this order: P4 (is there anything to reach at all?), then P1+P7"
echo "(would a per-session network actually isolate on this host?), then P11"
echo "(does it clean up after a kill?). Those three decide the plan."
echo "================================================================"
