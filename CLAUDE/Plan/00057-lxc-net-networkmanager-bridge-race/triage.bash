#!/usr/bin/env bash
#
# Plan 00057 — triage: detect the "lxc-net dead / no DHCP on lxcbr0" degraded
# state (NetworkManager vs lxc-net bridge-ownership race).
#
# READ-ONLY and re-runnable. Makes no changes. Run it:
#   • at planning stage, to capture the broken state, and
#   • after deploy, to confirm the fix landed.
#
# Exits 0 when LXC networking is healthy, non-zero when the degraded state is
# detected — so it doubles as a fail-fast signal in scripts/CI-style checks.
#
# The LXC bridge subnet is derived from the live bridge at runtime (no hardcoded
# addresses). Some checks need root (reading /run/lxc, attaching to containers),
# so the script re-invokes the privileged probes via sudo. It uses
# `set -uo pipefail` (no `-e`): a failing probe inside $(...) yields an empty
# string and the check below reports it explicitly, rather than aborting or
# being silently hidden.
set -uo pipefail

BRIDGE="lxcbr0"
FAILS=0
WARNS=0

# Colour only when stderr is a TTY (rule 13 of InteractiveScripts).
if [ -t 2 ]; then
    C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_YEL=$'\033[33m'; C_RST=$'\033[0m'
else
    C_RED=""; C_GRN=""; C_YEL=""; C_RST=""
fi

ok()   { printf '%s  OK  %s %s\n' "$C_GRN" "$C_RST" "$1" >&2; }
fail() { printf '%s FAIL %s %s\n' "$C_RED" "$C_RST" "$1" >&2; FAILS=$((FAILS + 1)); }
warn() { printf '%s WARN %s %s\n' "$C_YEL" "$C_RST" "$1" >&2; WARNS=$((WARNS + 1)); }
info() { printf '       %s\n' "$1" >&2; }

printf '=== Plan 00057 LXC networking triage (read-only) ===\n' >&2

# Derive the bridge address and /24 prefix from the live interface, so this
# script hardcodes no IP and works on any lxc-net subnet. Empty if the bridge
# currently has no IPv4 (e.g. fully down) — checks below handle that.
bridge_cidr="$(ip -o -4 addr show "$BRIDGE" 2>&1 | awk '{print $4}' | head -n1)"
bridge_ip="${bridge_cidr%/*}"
subnet_prefix=""
if printf '%s' "$bridge_ip" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$'; then
    subnet_prefix="${bridge_ip%.*}"
fi

# 1) lxc-net.service must be active. A failed/inactive unit means no DHCP server.
if systemctl is-active --quiet lxc-net; then
    ok "lxc-net.service is active"
else
    state="$(systemctl is-active lxc-net 2>&1)"
    fail "lxc-net.service is not active (state: ${state})"
    reason="$(systemctl status lxc-net --no-pager -l 2>&1)"
    if printf '%s' "$reason" | grep -qi 'File exists'; then
        info "-> 'RTNETLINK File exists': NetworkManager owns ${BRIDGE} and races lxc-net."
    fi
fi

# 2) lxc-net writes /run/lxc/network_up ONLY after dnsmasq launches. Its absence
#    is the definitive "DHCP never came up" signal (survives the false-positive
#    'bridge is up' check).
if sudo test -f /run/lxc/network_up; then
    ok "/run/lxc/network_up sentinel present (lxc-net completed startup)"
else
    fail "/run/lxc/network_up missing — lxc-net aborted before starting dnsmasq"
fi

# 3) A dnsmasq must be listening on the bridge address for DHCP (UDP :67).
dhcp_listen="$(sudo ss -H -lnup 2>&1)"
if [ -n "$bridge_ip" ] && printf '%s' "$dhcp_listen" | grep -qE "(${bridge_ip//./\\.}|\*|0\.0\.0\.0):67([[:space:]]|$)"; then
    ok "DHCP server listening on UDP :67 (dnsmasq)"
elif [ -z "$bridge_ip" ]; then
    fail "no IPv4 on ${BRIDGE} and no DHCP listener — lxc-net did not configure the bridge"
else
    fail "nothing listening on UDP :67 — containers cannot obtain a lease"
fi

# 4) NetworkManager must NOT manage the bridge. If it does, it will re-assign the
#    bridge address and race lxc-net at the next boot (the root cause).
nm_state="$(nmcli -t -f DEVICE,STATE device 2>&1 | grep -E "^${BRIDGE}:")"
if [ -z "$nm_state" ]; then
    warn "${BRIDGE} not present in NetworkManager device list (bridge may be down)"
elif printf '%s' "$nm_state" | grep -qi 'unmanaged'; then
    ok "${BRIDGE} is unmanaged by NetworkManager (correct — lxc-net owns it)"
else
    fail "${BRIDGE} is NM-managed (${nm_state#*:}) — will race lxc-net at boot"
    saved="$(sudo ls /etc/NetworkManager/system-connections/ 2>&1 | grep -i "${BRIDGE}")"
    [ -n "$saved" ] && info "-> saved NM connection present: ${saved}"
fi

# 5) Every RUNNING container should hold an IPv4 on the LXC subnet. This is the
#    user-visible symptom (no IP -> unreachable).
running="$(sudo lxc-ls -f 2>&1 | awk 'NR>1 && $2=="RUNNING" {print $1}')"
if [ -z "$running" ]; then
    info "no RUNNING containers to check"
elif [ -z "$subnet_prefix" ]; then
    warn "cannot derive LXC subnet from ${BRIDGE}; skipping per-container lease check"
else
    for c in $running; do
        cip="$(sudo lxc-attach -n "$c" -- ip -o -4 addr show eth0 2>&1 | awk '{print $4}')"
        if printf '%s' "$cip" | grep -qE "^${subnet_prefix//./\\.}\."; then
            ok "container ${c}: has lease ${cip}"
        else
            fail "container ${c}: no ${subnet_prefix}.x lease on eth0 (got: '${cip:-none}')"
        fi
    done
fi

printf '=== triage summary: %d failure(s), %d warning(s) ===\n' "$FAILS" "$WARNS" >&2
[ "$FAILS" -eq 0 ]
