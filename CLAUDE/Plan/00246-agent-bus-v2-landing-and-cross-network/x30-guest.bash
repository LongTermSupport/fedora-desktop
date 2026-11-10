#!/usr/bin/env bash
# Plan 00246 Task 8.8: the X30 lab's guest fixture, run INSIDE a lab guest by x30-lab.bash:
#
#   vmtest member-ssh <guest> 'sudo bash -s -- <step> [args...]' < x30-guest.bash
#
# (stage-seat runs without sudo, as the login user whose seat store it reads.) Each step is
# idempotent, so a rerun on the same guest converges. The last line of a successful step is
# VMTEST-GUEST-PREPARE-DONE <step>; a failure prints ERROR: prepare: <reason> on stderr and
# exits non-zero (the lab's guest-prepare convention, SPEC-vmtest.md E40).
#
# Steps:
#   net <mac>=<addr>/<len>... [route=<cidr>@<gateway>@<mac>]... [forward]
#       a NetworkManager profile per link NIC, matched by the MAC vmtest member-up printed,
#       manual IPv4, never the default route, no IPv6; each route on the NIC it names; with
#       forward, IPv4 forwarding on (the router).
#   firewalld          install, enable and start firewalld (the teamchat play asserts it runs).
#   zone <mac>...      the zone x30, target %%REJECT%% with ssh, holding these link NICs: the
#                      team's rich rules are then the only accept for its port (INSTALL-PATH 1).
#   issuer             the user issuer, in wheel with a PASSWORD sudo (no NOPASSWD), the lab
#                      key authorised; its password is read from /root/x30-issuer-pw, which the
#                      controller wrote over ssh's stdin, and removed once set (D7).
#   block-ssh-out      the firewalld policy x30-no-ssh-out: what the host starts toward tcp
#                      port 22 is rejected, as the first real bus host's egress is.
#   hosts <name> <address>   /etc/hosts maps <name> to <address> alone (the team's DNS url).
#   stage-seat <team> <seat> <state-dir>
#       copies the seat place-issued put in this user's pool into <state-dir>/<team>, which
#       `teamchat run --state <state-dir> --seat <team>/<seat>` reads. No production code takes
#       a pool seat into a session yet (seat.Store.Take has no caller: the launcher, Task 4.2),
#       so the scenario does the take itself and says so.
set -euo pipefail

fail() {
    printf 'ERROR: prepare: %s\n' "$1" >&2
    exit 1
}

# iface_of <mac>: the interface holding the MAC; exactly one or a failure.
iface_of() {
    local want="${1,,}" path found=""
    for path in /sys/class/net/*/address; do
        if [[ "$(<"${path}")" == "${want}" ]]; then
            [[ -z "${found}" ]] || fail "two interfaces hold ${want}"
            found="$(basename "$(dirname "${path}")")"
        fi
    done
    [[ -n "${found}" ]] || fail "no interface holds ${want}"
    printf '%s\n' "${found}"
}

step_net() {
    local arg mac addr iface forward=0 cidr gw rmac
    local -a routes=() nics=()
    for arg in "$@"; do
        case "${arg}" in
            forward) forward=1 ;;
            route=*) routes+=("${arg#route=}") ;;
            *=*/*)
                mac="${arg%%=*}"
                addr="${arg#*=}"
                iface="$(iface_of "${mac}")"
                nics+=("${iface}=${addr}")
                if nmcli -t -f NAME connection show | grep -qx "x30-${iface}"; then
                    nmcli connection delete "x30-${iface}" >/dev/null
                fi
                nmcli connection add type ethernet con-name "x30-${iface}" ifname "${iface}" \
                    802-3-ethernet.mac-address "${mac}" ipv4.method manual ipv4.addresses "${addr}" \
                    ipv4.never-default yes ipv6.method disabled connection.autoconnect yes >/dev/null
                ;;
            *) fail "net: not <mac>=<addr>/<len>, route=<cidr>@<gw>@<mac> or forward: ${arg}" ;;
        esac
    done
    for arg in "${routes[@]}"; do
        IFS=@ read -r cidr gw rmac <<<"${arg}"
        [[ -n "${cidr}" && -n "${gw}" && -n "${rmac}" ]] || fail "net: route=${arg} is not <cidr>@<gw>@<mac>"
        iface="$(iface_of "${rmac}")"
        nmcli connection modify "x30-${iface}" +ipv4.routes "${cidr} ${gw}"
    done
    for arg in "${nics[@]}"; do
        iface="${arg%%=*}"
        nmcli connection up "x30-${iface}" >/dev/null
        ip -4 -o addr show dev "${iface}" | grep -qF " ${arg#*=} " ||
            fail "net: ${iface} does not hold ${arg#*=} after the profile came up"
    done
    if [[ "${forward}" -eq 1 ]]; then
        printf 'net.ipv4.ip_forward = 1\n' >/etc/sysctl.d/90-x30-forward.conf
        sysctl -q -w net.ipv4.ip_forward=1
        [[ "$(</proc/sys/net/ipv4/ip_forward)" == 1 ]] || fail "net: forwarding is not on"
    fi
}

step_firewalld() {
    if ! rpm -q --quiet firewalld; then
        dnf -y -q install firewalld
    fi
    systemctl enable --now firewalld
    firewall-cmd --state >/dev/null
}

step_zone() {
    local mac iface
    if ! firewall-cmd --permanent --get-zones | tr ' ' '\n' | grep -qx x30; then
        firewall-cmd --permanent --new-zone=x30 >/dev/null
    fi
    firewall-cmd --permanent --zone=x30 --set-target='%%REJECT%%' >/dev/null
    firewall-cmd --permanent --zone=x30 --add-service=ssh >/dev/null
    for mac in "$@"; do
        iface="$(iface_of "${mac}")"
        nmcli connection modify "x30-${iface}" connection.zone x30
        firewall-cmd --permanent --zone=x30 --change-interface="${iface}" >/dev/null
    done
    firewall-cmd --reload >/dev/null
    for mac in "$@"; do
        iface="$(iface_of "${mac}")"
        [[ "$(firewall-cmd --get-zone-of-interface="${iface}")" == x30 ]] || fail "zone: ${iface} is not in x30"
    done
}

step_issuer() {
    local pwfile=/root/x30-issuer-pw cloud_home issuer_home
    [[ -s "${pwfile}" ]] || fail "issuer: no ${pwfile}; the controller writes it first"
    if ! getent passwd issuer >/dev/null; then
        useradd -m -G wheel issuer
    fi
    # The lab key is the one the cloud user (the guest's sudoer that ssh logs in as) holds.
    cloud_home="$(getent passwd "${SUDO_USER:?run through sudo by the cloud user}" | cut -d: -f6)"
    issuer_home="$(getent passwd issuer | cut -d: -f6)"
    install -d -m 0700 -o issuer -g issuer "${issuer_home}/.ssh"
    install -m 0600 -o issuer -g issuer "${cloud_home}/.ssh/authorized_keys" "${issuer_home}/.ssh/authorized_keys"
    { printf 'issuer:'; head -n 1 "${pwfile}"; } | chpasswd
    rm -f "${pwfile}"
    if sudo -l -U issuer | grep -q NOPASSWD; then
        fail "issuer: a NOPASSWD rule reaches issuer; X30 needs a password sudo"
    fi
}

step_block_ssh_out() {
    if ! firewall-cmd --permanent --get-policies | tr ' ' '\n' | grep -qx x30-no-ssh-out; then
        firewall-cmd --permanent --new-policy=x30-no-ssh-out >/dev/null
    fi
    firewall-cmd --permanent --policy=x30-no-ssh-out --add-ingress-zone=HOST >/dev/null
    firewall-cmd --permanent --policy=x30-no-ssh-out --add-egress-zone=ANY >/dev/null
    firewall-cmd --permanent --policy=x30-no-ssh-out \
        --add-rich-rule='rule family="ipv4" port port="22" protocol="tcp" reject' >/dev/null
    firewall-cmd --reload >/dev/null
}

step_hosts() {
    local name="${1:?hosts <name> <address>}" addr="${2:?hosts <name> <address>}" tmp
    tmp="$(mktemp /etc/hosts.x30.XXXXXX)"
    awk -v n="${name}" '{ for (i = 2; i <= NF; i++) if ($i == n) next; print }' /etc/hosts >"${tmp}"
    printf '%s %s\n' "${addr}" "${name}" >>"${tmp}"
    chmod 0644 "${tmp}"
    mv -f "${tmp}" /etc/hosts
    [[ "$(getent hosts "${name}")" == "${addr} "* ]] || fail "hosts: ${name} does not resolve to ${addr}"
}

step_stage_seat() {
    local team="${1:?stage-seat <team> <seat> <state-dir>}" seat="${2:?}" state="${3:?}" pool
    local -a found=()
    pool="${XDG_STATE_HOME:-${HOME}/.local/state}/teamchat/seats/pool"
    shopt -s nullglob
    found=("${pool}"/*/"${team}/${seat}")
    shopt -u nullglob
    [[ "${#found[@]}" -eq 1 ]] || fail "stage-seat: ${#found[@]} pool seats ${team}/${seat} under ${pool}, want one"
    [[ -f "${found[0]}/seat.json" ]] || fail "stage-seat: ${found[0]} has no seat.json"
    install -d -m 0700 "${state}" "${state}/${team}"
    install -m 0600 "${found[0]}/seat.json" "${state}/${team}/seat.json"
    if [[ -f "${found[0]}/token" ]]; then
        install -m 0600 "${found[0]}/token" "${state}/${team}/token"
    elif [[ -f "${found[0]}/password" ]]; then
        install -m 0600 "${found[0]}/password" "${state}/${team}/password"
    else
        fail "stage-seat: ${found[0]} holds no token or password"
    fi
}

step="${1:?usage: x30-guest.bash <step> [args...]}"
shift
case "${step}" in
    net) step_net "$@" ;;
    firewalld) step_firewalld ;;
    zone) step_zone "$@" ;;
    issuer) step_issuer ;;
    block-ssh-out) step_block_ssh_out ;;
    hosts) step_hosts "$@" ;;
    stage-seat) step_stage_seat "$@" ;;
    *) fail "unknown step: ${step}" ;;
esac
printf 'VMTEST-GUEST-PREPARE-DONE %s\n' "${step}"
