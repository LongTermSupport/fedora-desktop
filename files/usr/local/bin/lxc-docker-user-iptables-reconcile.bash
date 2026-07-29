#!/usr/bin/env bash
# ANSIBLE MANAGED — edit the source in the fedora-desktop repo
# (files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash) and redeploy
# via playbooks/imports/play-lxc-install-config.yml. Do not edit on the target.
#
# Re-applies the DOCKER-USER ACCEPT rules + POSTROUTING MASQUERADE rule that
# give LXC containers on lxcbr0 outbound connectivity through Docker's
# DOCKER-USER chain. These rules are RUNTIME-ONLY kernel state — they vanish
# on host reboot and whenever docker.service restarts (Docker flushes/
# recreates DOCKER-USER on daemon start). This script is the single source of
# truth for the rule set; it is invoked BOTH by play-lxc-install-config.yml
# during provisioning (immediate apply) AND by
# lxc-docker-user-iptables-reconcile.service, which is ordered/bound to
# docker.service so it reruns on every boot and every docker.service restart
# (the persist-across-reboot path).
#
# Idempotent: every rule is probed with `iptables -C` before insertion, so
# re-running never duplicates a rule. Prints a stable marker line so Ansible
# can derive changed_when without parsing free-form output.
set -euo pipefail

readonly bridge=lxcbr0

if ! iptables -n -L DOCKER-USER; then
    echo "lxc-docker-user-iptables-reconcile: DOCKER-USER chain does not exist" \
         "(is docker.service actually up?) — refusing to proceed" >&2
    exit 1
fi

subnet_cidr="$(ip -o -4 route show dev "$bridge" proto kernel | awk '{print $1}' | head -n1)"
if [ -z "$subnet_cidr" ]; then
    echo "lxc-docker-user-iptables-reconcile: could not derive ${bridge} subnet" \
         "from 'ip route show dev ${bridge} proto kernel' — is lxc-net running?" >&2
    exit 1
fi

changed=0

if ! iptables -C DOCKER-USER -i "$bridge" -j ACCEPT; then
    iptables -I DOCKER-USER -i "$bridge" -j ACCEPT
    changed=1
fi

if ! iptables -C DOCKER-USER -o "$bridge" -j ACCEPT; then
    iptables -I DOCKER-USER -o "$bridge" -j ACCEPT
    changed=1
fi

if ! iptables -t nat -C POSTROUTING -s "$subnet_cidr" ! -o "$bridge" -j MASQUERADE; then
    iptables -t nat -I POSTROUTING -s "$subnet_cidr" ! -o "$bridge" -j MASQUERADE
    changed=1
fi

if [ "$changed" -eq 1 ]; then
    echo "LXC-IPTABLES-RECONCILE-CHANGED"
else
    echo "LXC-IPTABLES-RECONCILE-NOOP"
fi
