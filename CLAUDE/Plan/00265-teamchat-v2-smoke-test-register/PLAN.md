# Plan 00265: teamchat v2 smoke test register

**Status**: In Progress
**Created**: 2026-11-25
**Owner**: dev
**Priority**: Critical

## Overview

The owner is smoke testing v2 of the agent-team tooling (teamchat, `ccy --teams`, `ccy-teams`,
`ccy-sessions`) and judges that today it is worse than v1. This plan keeps the single running
register of every v2 fault found while it is used for real work, by this seat, its sub-agents, the
other seats of the team and the owner. Every fault is logged the moment it is seen, however small,
and stays open until a fix is proven.

This repository's seat is the primary fixer. Faults are fixed one per unit, each unit in its own
worktree and branch, in parallel. Each unit merges into the integration branch `v2-smoke`. CI runs
on that branch when a batch is ready, and it lands on F44 after one review, as the owner's CI
directive asks.

## Register

[REGISTER.md](REGISTER.md): one entry per fault (R1, R2, ...), with what was seen, where and
when, who hit it, how to reproduce it, its severity, issue, unit and state.

## Goals

- Every v2 fault seen is an entry in the register, with how to reproduce it.
- Every entry is fixed and proven, or explicitly deferred by the owner.
- Nothing v1 did is lost in v2 (no degrade against v1).

## Non-Goals

- New features beyond what makes v2 at least as usable as v1.
- The model-class refusal for coordinator and L2 seats (owner: later, not now).

## Tasks

### Phase 1: the faults that stop real work

- [ ] 🔄 **Task 1.1** (U1): R6, R18, #421: a refusal names its cause and the fix; detector first.
  Merged into `v2-smoke` (ea576bbbf); live proof owed.

- [ ] 🔄 **Task 1.2** (U2): R1, R2, R21, #422: every PEER reaches the session or its sender is
  told why; reply works on what history lists. On F44 (c1b20004f, the home pin);
  live proof owed after the re-pin and a worker restart.

- [ ] 🔄 **Task 1.3** (U3): R3, #423: seat choice at launch is at least v1's. Merged into
  `v2-smoke` (d9b11903f); live proof owed.

- [ ] 🔄 **Task 1.4** (U4): R4, #417: a seat's conversation survives joining a team and `/exit`.
  Merged into `v2-smoke` (15fbc90cd, ccy 3.124.45); the chooser's never-used seat goes back to
  U3; next F44 batch with U3, before any worker restart.

- [ ] 🔄 **Task 1.5** (U5): R5, #416: a seat is added in a couple of commands with a derived name.
  First slice merged (751bdeab3): `ccy-teams seats add` on the homeserver's machine; the in-launch
  creation follows U3; other-machine, coordinator requests and named seats wait on the owner (Q6).

- [ ] 🔄 **Task 1.6** (U8): R22, R23, #425: LXC members on another host (Plan 00257 B.4). R23 on
  F44 (5fea8d97f); L2 to L4 on F44 (c1b20004f) with U2; the key is trusted at home (R31),
  so the estate's re-pin (L5) is with the coordinator. Route: other-host only (Q8). See [ROUTE-TO-LIVE.md](ROUTE-TO-LIVE.md).

- [ ] 🔄 **Task 1.7** (U10): R41, R42: a seat is added to a running v2 session as v1 allowed
  (R42 fixed on `v2-smoke`, cdac15584; R41 next, in a worktree once the branch pool has room)
  (`ccy-teams session add`), and `--to-coordinator` leaves out a team this session coordinates.
  Next after batch 3 lands (F19: no-degrade).

### Phase 2: threads, procedure and authority

- [ ] 🔄 **Task 2.1** (U6): R10, R12: reply to an edited human message; a worker can verify a
  relayed human answer. Merged (c3c336cd4); live proof owed.
- [ ] 🔄 **Task 2.4** (U9): R34: a human's in-thread answer is delivered with its question.
  Merged (b3044e711); live proof owed.
- [ ] ⬜ **Task 2.3**: R7, R9, R11.
- [ ] ⬜ **Task 2.2**: R8, R17, #420.

### Phase 3: what misleads, and comfort

- [ ] 🔄 **Task 3.1** (U7): R14: presence reflects a live link. Merged (b1d62a0e0); live proof
  owed.
- [ ] ⬜ **Task 3.3**: R13, R15, R16.
- [ ] ⬜ **Task 3.2**: R19 (#424), R20.

## Success Criteria

- [ ] Every register entry is proven fixed or deferred by the owner.
- [ ] `v2-smoke` has passed CI and one review, and has landed on F44.

## Delivery & Milestones

- Register opened on branch `v2-smoke`.
