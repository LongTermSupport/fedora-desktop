#!/usr/bin/env bash
#
# Plan 00057 — deploy: apply the lxc-net / NetworkManager bridge-race fix.
#
# HOST-ONLY. Never run inside the CCY container (it has no lxc, no systemd
# target state). Fail-fast throughout (this is a non-interactive deploy wrapper,
# not a human-prompted script).
#
# What it runs: only `play-lxc-install-config.yml`. That single play carries the
# whole fix — it marks lxcbr0 NetworkManager-unmanaged, drops the nmcli-zone
# tasks, enables lxc-net, restarts lxc-net (recovering a broken host without
# bouncing containers), and asserts DHCP actually came up.
#
# Vault: the repo's ansible.cfg points at ./vault-pass.secret, so no
# --ask-vault-pass is needed on a configured host. Extra args are passed through
# (e.g. `-K` for a sudo password, `--check` for a dry run).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# CCY container guard — deploying Ansible from the container is forbidden.
if [ -d /workspace ] && [ "$REPO_ROOT" = /workspace ]; then
    echo "ERROR: this looks like the CCY container (/workspace). Deploy on the HOST." >&2
    exit 1
fi

PLAY="playbooks/imports/play-lxc-install-config.yml"
TRIAGE="$REPO_ROOT/CLAUDE/Plan/00057-lxc-net-networkmanager-bridge-race/triage.bash"

echo "== Plan 00057 deploy: applying the lxc-net / NetworkManager bridge-race fix =="
echo

# Pre-deploy triage is informational: on a broken host it exits non-zero, which
# is expected — capture it without aborting this script.
echo "-- pre-deploy triage (informational; failures here are the state we fix) --"
if "$TRIAGE"; then
    echo "   pre-deploy state already healthy."
else
    echo "   pre-deploy state degraded (expected) — proceeding with the fix."
fi
echo

echo "-- running: ansible-playbook $PLAY $* --"
ansible-playbook "$PLAY" "$@"
echo

# Post-deploy triage MUST pass — set -e makes a non-zero exit fail the deploy,
# so a fix that did not actually restore DHCP is reported loudly.
echo "-- post-deploy triage (must be green) --"
"$TRIAGE"
echo
echo "== Plan 00057 deploy complete: LXC DHCP healthy =="
