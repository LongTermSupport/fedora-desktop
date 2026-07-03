# Plan 00057: lxc-net vs NetworkManager bridge-ownership race

**Status**: In Progress
**Created**: 2026-07-03
**Owner**: Claude Code Agent
**Priority**: High

## Overview

On a Fedora desktop provisioned by this repo, `lxc-net.service` can fail at boot
with `RTNETLINK answers: File exists`, after which **no DHCP server runs on
`lxcbr0`**. LXC containers then start, bring up `eth0`, but their DHCP requests
go unanswered forever — so they never obtain an IP address and are unreachable
over SSH or anything else. The bridge interface itself stays up (so a naive
"is the bridge up?" check passes), which makes the failure silent until someone
tries to reach a container.

The root cause is a **bridge-ownership race between NetworkManager and
`lxc-net`**. The current `play-lxc-install-config.yml` sets the `lxcbr0`
firewall zone with `nmcli connection modify lxcbr0 connection.zone trusted`.
Running `nmcli connection modify` on NM's auto-generated connection for the
bridge **persists it to a saved keyfile**
(`/etc/NetworkManager/system-connections/lxcbr0.nmconnection`) with
`ipv4.method=manual`, `ipv4.addresses=10.0.x.1/24` (the stock `lxc-net`
`LXC_ADDR` default), and `autoconnect=yes`. From then on, at every boot
NetworkManager creates `lxcbr0` and statically assigns that address to it
**before** `lxc-net` runs (`lxc-net` is ordered `After=network-online.target`).
When `lxc-net` then runs its `_ifup` step (`ip addr add <LXC_ADDR>/24 dev lxcbr0`), the address already exists →
`RTNETLINK answers: File exists` → `set -e` aborts the script via its `cleanup`
trap → **`dnsmasq` is never launched**.

This plan follows **"defence before fix"**: first add detection that turns this
silent failure into a loud, one-command diagnosis (and a real playbook gate),
then fix the root cause so NetworkManager no longer owns `lxcbr0`, and make the
play able to recover a host that is already in the broken state.

## Goals

- **Defence (quality ratchet)**: make the degraded state impossible to miss.
  - A plan-local `triage.bash` that reports, read-only: is `lxc-net` active, is
    the `dnsmasq`/DHCP server actually up on `lxcbr0`, is `lxcbr0` wrongly
    NM-managed, and do running containers hold leases.
  - A **real** playbook sanity gate that fails fast when `lxc-net` is not active
    or its DHCP server did not come up — replacing the current false-positive
    "bridge interface is up" check.
- **Fix (prevent recurrence)**: stop NetworkManager from managing/owning
  `lxcbr0` so `lxc-net` is the sole owner of the bridge and its address.
  - Remove the `nmcli connection modify lxcbr0 …` zone tasks (the source of the
    persisted NM profile); keep the firewalld interface→zone binding, which does
    not depend on NM.
  - Deploy a persistent `NetworkManager` `conf.d` rule marking `lxcbr0`
    unmanaged, and set the device unmanaged at runtime (any already-saved
    `lxcbr0` profile is then inert; it is deliberately NOT deleted).
  - Explicitly `enable` `lxc-net.service` (today only `lxc.service` is enabled).
- **Recovery**: re-running the play repairs a host that is already broken,
  without bouncing running containers (safe because `lxc-net stop` keeps a
  bridge that still has attached container veths).

## Non-Goals

- No change to the LXC subnet, DHCP range, or container definitions.
- No change to the Docker/`DOCKER-USER` iptables reconciliation already in the
  play (that is a separate, working concern).
- Not switching the bridge to a fundamentally different networking model
  (e.g. moving containers off `lxcbr0`).

## Context & Background

Key mechanics established during diagnosis (all from the stock Fedora
`ganto/lxc4` `lxc-net` script and NM state):

- `/usr/libexec/lxc/lxc-net start` runs `_ifup()` → `ip addr add ${LXC_ADDR}/${MASK} dev lxcbr0`. If the address is already present this returns
  `File exists`; `set -e` + the `cleanup` trap then abort **before** `dnsmasq`
  starts. Net effect: bridge up, **no DHCP**.
- `lxc-net stop force` only deletes the bridge **if it has no attached
  interfaces** (`ls /sys/class/net/lxcbr0/brif/*`). While containers are running
  their veths are attached, so a `lxc-net` restart keeps the bridge and is
  **non-disruptive** to containers — they re-acquire leases within seconds.
- `lxc-net.service` is `Before=lxc.service`, `After=network-online.target`.
  NetworkManager reaches `network-online` having already created/addressed
  `lxcbr0`, which is what loses the race for `lxc-net`.

## Tasks

### Phase 1: Defence (detection first — the ratchet)

- [x] ✅ **Task 1.1**: Write plan-local `triage.bash` (read-only, re-runnable)
  - [x] ✅ Report `lxc-net` active state, `/run/lxc/network_up` sentinel,
    `dnsmasq` bound on `lxcbr0` UDP :67, `lxcbr0` NM-managed?, per-container
    lease presence.
  - [x] ✅ Exit non-zero when the degraded state is detected (fail-fast signal).
- [x] ✅ **Task 1.2**: Add a **real** DHCP-readiness sanity gate to
  `play-lxc-install-config.yml`, augmenting the false-positive "bridge is up"
  check with asserts on `lxc-net` active + `/run/lxc/network_up` present.
  - [ ] ⬜ Run QA: `./scripts/qa-all.bash`

### Phase 2: Fix (prevent recurrence)

- [x] ✅ **Task 2.1**: Deploy a persistent NM `conf.d` rule marking `lxcbr0`
  unmanaged (`files/etc/NetworkManager/conf.d/99-lxc-unmanaged.conf`).
- [x] ✅ **Task 2.2**: In the play, remove the `nmcli connection modify lxcbr0 connection.zone trusted` tasks (root cause of the persisted profile);
  rely on the existing `ansible.posix.firewalld` interface→zone binding.
- [x] ✅ **Task 2.3**: Set `lxcbr0` NM-unmanaged at runtime (probe-then-act, no
  device teardown). **Do not** delete the saved profile — deleting an active
  bridge profile risks tearing the bridge down and unenslaving live container
  veths; once unmanaged, any saved profile is inert.
- [x] ✅ **Task 2.4**: `enable` `lxc-net.service` (not only `lxc.service`).
- [x] ✅ **Task 2.5**: Trigger `reload NetworkManager` then `restart lxc-net`
  (handlers run in definition order) via an in-play `flush_handlers`, so the fix
  both recovers the current host and applies cleanly on re-run.
  - [ ] ⬜ Run QA: `./scripts/qa-all.bash` + `ansible-playbook --syntax-check`

### Phase 3: Deploy & verify (HOST)

- [ ] ⬜ **Task 3.1**: `triage.bash` before deploy — capture the broken state.
- [ ] ⬜ **Task 3.2**: Run the play on the HOST.
- [ ] ⬜ **Task 3.3**: `triage.bash` after deploy — confirm `lxc-net` active,
  DHCP up, containers hold leases, `lxcbr0` NM-unmanaged.
- [ ] ⬜ **Task 3.4**: Confirm a container is reachable again over SSH.

## Technical Decisions

### Decision 1: Make `lxcbr0` NM-unmanaged rather than reorder services

**Context**: The race is NM assigning the bridge address before `lxc-net`.
**Options considered**:

- (A) Reorder `lxc-net` before NM brings the bridge up — fragile, fights
  `network-online.target` ordering, and NM would still re-assert its saved
  profile.
- (B) Make `lxcbr0` NM-unmanaged so `lxc-net` is the sole owner — matches
  upstream LXC expectations (lxc-net creates and owns its bridge), removes the
  conflicting address assignment entirely, and is declarative via `conf.d`.
  **Decision**: (B). Stop creating the profile (drop the `nmcli connection modify` zone tasks; keep firewalld) and set the device unmanaged. Do NOT
  delete an active saved bridge profile — once unmanaged it is inert, and
  deleting it risks tearing the bridge down and unenslaving live container veths.
  **Date**: 2026-07-03

### Decision 2: Firewall zone via firewalld only, not nmcli

**Context**: The `nmcli connection modify lxcbr0 connection.zone trusted` tasks
are what persisted the NM bridge profile that causes the race.
**Decision**: Drop those tasks. The play already binds `lxcbr0` to the `trusted`
zone via `ansible.posix.firewalld` (by interface name), which works regardless
of whether NM manages the device.
**Date**: 2026-07-03

## Success Criteria

- [ ] `triage.bash` exits 0 on a healthy host and non-zero on the broken state.
- [ ] After deploy: `systemctl is-active lxc-net` = active, `/run/lxc/network_up`
  present, `dnsmasq` bound on `lxcbr0`, running containers hold leases.
- [ ] `lxcbr0` shows as **unmanaged** by NetworkManager (any saved `lxcbr0`
  profile is inert and cannot reactivate).
- [ ] Playbook sanity gate fails fast if DHCP did not come up.
- [ ] QA passes (`./scripts/qa-all.bash`) and `ansible-playbook --syntax-check`
  is clean.

## Risks & Mitigations

| Risk                                                                   | Impact | Probability | Mitigation                                                                                                                                            |
| ---------------------------------------------------------------------- | ------ | ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| Marking `lxcbr0` unmanaged flaps the bridge and drops a container      | M      | L           | `lxc-net` restart keeps the bridge while veths are attached; containers re-lease in seconds                                                           |
| firewalld zone binding lost when NM stops managing the interface       | M      | L           | Play re-asserts the firewalld interface→zone binding (permanent + immediate) after the NM change                                                      |
| Setting `lxcbr0` unmanaged drops the address before `lxc-net` restarts | M      | L           | The `lxc-net` restart handler runs right after the NM change and re-adds `LXC_ADDR` + starts `dnsmasq`; unmanaging leaves the device up (no teardown) |

## Notes & Updates

### 2026-07-03

- Diagnosed live: `lxc-net.service` failed at boot with `RTNETLINK answers: File exists`; no `dnsmasq`, nothing on UDP :67; containers stuck in a
  `DHCPDISCOVER` retry loop with no `eth0` address. Traced to a persisted
  NetworkManager `lxcbr0` bridge profile (`ipv4.method=manual`,
  `10.0.x.1/24`, `autoconnect=yes`) racing `lxc-net`'s `_ifup`.
- No system changes were made during diagnosis (read-only only).
- Adopting "defence before fix": detection (triage + real playbook gate) lands
  first, then the NM-unmanaged root-cause fix + recovery.
