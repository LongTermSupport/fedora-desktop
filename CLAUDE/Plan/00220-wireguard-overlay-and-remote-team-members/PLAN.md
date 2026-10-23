# Plan 00220: wireguard overlay and remote team members

**Status**: Not Started
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

## Overview

The owner wants to test agent teams across machines: first on the home network (this desktop
and a second laptop), later from outside the home (a static public address, port forwarding
available, a DMZ machine under consideration). Today nothing on another machine can reach a
team: the homeserver listens only on loopback and the private `agentbus0` address, and its
unit filter and firewalld rules admit no other machine.

This plan adds a WireGuard overlay to the desktop's IaC (one overlay per machine, hub and spoke
with this desktop as the hub), puts a team's homeserver on the overlay address as a second
listen address for named peers only, and proves a `host`-type member on the second laptop
sending, receiving and going offline without blocking anyone. Remote ccy seats need bus code,
which is built in v2 only (Plan 00209); this plan records what that work depends on.

The design, the security section and the decisions are in [DESIGN.md](DESIGN.md).

## Decisions (owner, 2026-10-23)

- **D46's later phase is lifted for this test.** Plan 00161 D46 kept teams spanning machines
  for a later phase; the owner's wish of 2026-10-23 is to test cross-machine teams now, on the
  home network and later from outside.
- **v1 gets no bus code for remote ccy seats.** That work is v2-only (Plan 00209): v1 is to be
  retired, and v2 already has the per-team `url` field and the owner approval it needs. Phases 1
  and 2 need no bus code and work on v1.

Open owner questions Q1-Q8 and the coordinator defaults D-C1 to D-C7 are in
[DESIGN.md section 6](DESIGN.md#6-decisions).

## Goals

- A declared, vault-backed WireGuard overlay on any machine built from this repository, applied
  by `play-vpn.yml`, with a dedicated firewalld zone and one public UDP port on the hub.
- A team's homeserver reachable from a named overlay peer and from nothing else on any other
  network, proven on the host.
- A `host` member on the second laptop that sends, receives, and sleeps without blocking the
  team.
- Phase 3's dependencies on Plans 00209, 00205 and 00197 written down, and the design note for
  the v2 build handed to the coordinator.

## Non-Goals

- Bus code for remote ccy seats in v1 (Phase 3 is v2, Plan 00209).
- A DMZ host, a dedicated always-on hub, a third-party control plane, or teams with machines the
  owner does not own (owner decisions Q2, Q3, Q5, Q6; each its own plan if chosen).
- TLS on the bus.
- IPv6 on the overlay; spoke-to-spoke routing; a full tunnel.

## Tasks

### Phase 1: the WireGuard overlay (v1, no bus code)

- [ ] ⬜ **Task 1.1**: Go tests first (RED) for `wg-overlay check` and `wg-overlay render` in
  `src-go/`: every refusal in DESIGN.md 2.3, the rendered keyfile as golden files (hub and
  spoke), and a test that no key byte reaches stdout, stderr or an error on any path.
- [ ] ⬜ **Task 1.2**: the command (GREEN), reading the declaration on stdin, writing the keyfile
  0600 root by temp file and rename.
- [ ] ⬜ **Task 1.3**: a VM lab scenario, checker first (RED), as DESIGN.md 2.8: manifest entry
  `server-wireguard-overlay-hub` (`base: server-fast`, `reboot_before_checks: true`, `planned`);
  a `guest-prepare-` fixture that writes a throwaway unvaulted declaration and runs
  `play-vpn.yml` by `run.bash`'s single-play headless mode (`RUN_BASH_HEADLESS=1`); the
  checker's test-scaffolding exception recorded in `docs/vm-acceptance-testing.md`; a checker that builds a peer and a
  non-peer in network namespaces and proves the handshake after the reboot, the peer admitted
  by a source `/32` rule, a local client over `lo` unaffected, no handshake for an unknown key,
  the non-peer's packets to the overlay address and its spoofed overlay source dropped by the
  guard (its `wg-overlay-guard` kernel log lines), the zone bound by NetworkManager only, and
  no new listener but the WireGuard port.
- [ ] ⬜ **Task 1.4**: the play (GREEN): the overlay tasks in `play-vpn.yml` as DESIGN.md 2.4
  (zone, guard policy, keyfile, read-back by key-free forms), skipped when `wireguard_overlay`
  is not defined; removal by `state: absent`, the address read from the installed connection,
  refused while a present team listens on it; the commented placeholder in `localhost.yml.dist`
  with `vault.bash set` for the keys.
- [ ] ⬜ **Task 1.5**: owner question Q8 (DESIGN.md section 6): whether `play-agent-bus.yml`
  refuses a listen address outside loopback, `agentbus0`, a bridge or the overlay. It narrows
  the owner's 2026-10-06 decision in Plan 00161 DESIGN.md 3.3, so it is built (test first, with
  Plan 00161 3.3 and `docs/agent-bus.md`'s "does not refuse" text updated in the same commit)
  only if the owner says yes.
- [ ] ⬜ **Task 1.6**: the create wizard's "Bus address" and "Other machines" texts corrected
  (`askAddress`, `askAllowFrom`, DESIGN.md 2.5), with their tests.
- [ ] ⬜ **Task 1.7**: docs: the overlay in `docs/` (declaring, key generation, the router
  forward as an owner step, the LAN endpoint at home, revocation), and `docs/agent-bus.md`'s
  WireGuard lines pointing at it (per-peer `/32`, not the subnet); Plan 00161 DESIGN.md D46
  notes the lift.
- [ ] ⬜ **Task 1.8**: `deploy.bash` (both plays on the hub) and `acceptance.bash` for the host
  agent: a real handshake with laptop B; the homeserver of a throwaway team reachable from B at
  `<wg_ip_A>:<port>`; refusal from a non-peer (B with the overlay down, routing to `<wg_ip_A>`
  through the hub's LAN address) shown as a guard drop (a `wg-overlay-guard dst` kernel log
  line), not only a closed port; `ss` on the hub showing every bus port on loopback, `agentbus0` or the overlay
  only; the guard covering every zone but `wg-overlay`; the hub rebooted with no peer up and the
  homeserver active. On a Workstation hub the WireGuard port is already inside the default
  zone's open range, so the acceptance does not claim the forward is the only open port there.
  Queued in `meta-deploy.list`.

### Phase 2: a `host` member on the second laptop (v1)

- [ ] ⬜ **Task 2.1**: laptop B's steps documented: the repository, `play-vpn.yml` as a spoke,
  `play-agent-bus.yml` with no team, `gh` logged in for the agent user, the bundle carried over
  the overlay and installed as `README.host` says.
- [ ] ⬜ **Task 2.2**: the acceptance on a throwaway team (DESIGN.md section 3): B sends and
  receives pings and free text with a ccy seat on A; B suspended, every command on A completes
  in its usual bounds and shows B offline; B woken, it catches up. The team removed in an `EXIT`
  trap and at start.

### Phase 3: remote ccy seats (v2, Plan 00209)

- [ ] ⬜ **Task 3.1**: the coordinator hands the design note (DESIGN.md 4.1) to seat 3 for Plan
  00209; this plan does not message seat 3.
- [ ] ⬜ **Task 3.2**: when Plan 00209 builds remote seats, the acceptance here is extended to a
  ccy session on laptop B; Q7 (ssh or broker) decided first.

### Phase 4: review

- [ ] ⬜ **Task 4.1**: the `qa-reviewer` agent over the finished work.

## Success Criteria

- [ ] `wg-overlay` refuses every malformed declaration with the field named, and no key appears
  in any output (Go tests).
- [ ] The VM lab scenario passes: handshake, peer admitted, unknown key refused, a non-peer's
  packets to the overlay address and a spoofed overlay source dropped by the firewall.
- [ ] On the host: laptop B reaches the throwaway team's homeserver over the overlay; a non-peer
  does not; no bus port is bound outside loopback, `agentbus0` and the overlay; the homeserver
  survives a reboot with no peer up.
- [ ] A `host` member on laptop B sends and receives, and its sleep blocks nothing on A.
- [ ] Phase 3's dependencies recorded and the design note listed for the coordinator.

## Delivery & Milestones

- Plan written; reviews recorded in the journal.
