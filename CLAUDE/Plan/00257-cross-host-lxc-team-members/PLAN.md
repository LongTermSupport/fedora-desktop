# Plan 00257: cross host lxc team members

**Status**: In Progress (architecture note written; awaiting the owner's choice of A or B; common work started)
**Created**: 2026-11-19
**Owner**: joseph
**Priority**: High

## Overview

An LXC container on any host is to join any v2 (`teamchat`) team through a per-container step,
with the team's homeserver on the same host or on another one (owner, Q44). Today `lxc apply`
issues and places an LXC seat in one root process on the bus host itself
(Plan 00209 DESIGN-v2-C-host.md 3.9), so a container on another host cannot be a member.

[ARCHITECTURE.md](ARCHITECTURE.md) compares per-seat placement on the container's host (A) with
a per-host teamchat proxy (B), and recommends A.

Related plans:

- Plan 00246 Task 8.8 built cross-network ccy pool seats: url-keyed seats, TLS with the team
  CA, and `seat place-issued`. This plan reuses that issuance path for LXC guests.
- Plan 00220's overlay is dropped from the bus (Plan 00209 direction 7). Its remote-seat
  needs are met by 00246 for ccy seats, and by this plan for LXC.

## Goals

- A container on a host that runs no homeserver for the team is placed as a member of that
  team, by one owner step per container.
- The local `lxc apply` path (the bus host's own containers) is unchanged.

## Non-Goals

- A teamchat proxy, unless the owner chooses B.
- Docker or VM guests.
- Any v1 change: v1 is feature-frozen.
- Any member kind other than LXC.

## Tasks

### Phase 0: decision

- [ ] ⬜ **Task 0.1**: the owner chooses A or B (ARCHITECTURE.md), through the coordinator (Q44).

### Phase 1: common work (needed by A and by B)

- [ ] 🔄 **Task 1.1**: a spec for placing the kit and the session unit into a local guest whose
  team's homeserver is remote: `lxc apply` steps 02-05 with no child, the url and CA taken
  from the issued document.
- [ ] ⬜ **Task 1.2**: tests first (RED), then the placement (GREEN), under the existing fakes
  of `host/lxc`.
- [ ] ⬜ **Task 1.3**: the guest seat keyed on the team url (as pool seats are), tests first.

### Phase 2: per-container step (after Task 0.1)

- [ ] ⬜ **Task 2.1**: the issuance side for a remote LXC seat, and the owner's one command per
  container.
- [ ] ⬜ **Task 2.2**: the play side on the container's host: programs only, no team.
- [ ] ⬜ **Task 2.3**: docs, and acceptance on a second host queued in `meta-deploy.list`.

### Phase 3: review

- [ ] ⬜ **Task 3.1**: the `qa-reviewer` agent over the finished work.

## Success Criteria

- [ ] A container on host Y holds a working seat on a team whose homeserver runs on host X:
  it sends, receives, and goes offline without blocking the team.
- [ ] The bus host's own `lxc apply` passes its existing tests unchanged.

## Delivery & Milestones

- Architecture note: ARCHITECTURE.md.
