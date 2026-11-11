# Plan 00251: team seat status for the coordinator

**Status**: In Progress (research done; awaiting the owner's design choice)
**Created**: 2026-11-11
**Owner**: joseph
**Priority**: High

## Overview

The team bus (v2, teamchat) carries messages between seats, but nothing tells the coordinator
whether a seat is working, idle or blocked. A team can idle-lock: the coordinator believes a
worker is busy, the worker has stopped (finished, waiting on a dialog, waiting on the
coordinator), and nothing moves until a human notices.

This plan gives the coordinator one live view of every seat on its team, each with a state
(working, idle, blocked, gone) and how long it has held it, and lets the coordinator watch the
view or be notified when a seat changes state. The state is produced by deterministic code from
signals the seat already emits (turn start and stop, the pane, the bus link), never by asking an
agent, and a notification costs an agent turn only when a state change is one the coordinator
must act on.

It works for v2 seats in ccy containers, on the host and in LXC members, and across networks
(seats of one team on different machines).

What exists is narrower. The ccy-teams board's STATE column (`internal/teams/board`) shows
whether a member's session is listening on the bus now, or parked: liveness, read on the local
host through the v1 bus, with no working, idle, blocked or rate-limited and nothing the
coordinator receives. Related plans: 00203 (reads each ccy pane for idle moments, for its idle
shutdown), 00194 (wakes an idle seat to deliver), 00224 (the coordinator's desktop
notifications, a possible carrier for the notify half), 00221 (the board above, with member
labels and a tree; it keeps the STATE column), and 00209 and 00246 (v2, its landing and
cross-network).

## Goals

- One status report per team, all seats, each with state, since-when and the reason (for
  blocked: on what).
- A rate-limited seat (usage limit hit) is its own state, told apart from offline, crashed and
  idle, with its reset time where known; messages to it are shown as waiting for the reset.
- The coordinator can read it on demand and subscribe to changes, filtered to the changes worth
  a turn (a seat idle while the coordinator thinks it is working; a seat blocked).
- Deterministic: states derived by code from hooks, pane and bus signals; no polling agent.
- v2 only, cross-network: seat state travels on the team's own bus, not a host-local file.

## Non-Goals

- v1 (pingbus) seats.
- Restarting or driving a stuck seat automatically (a later plan may build on the status).
- A human dashboard beyond what falls out of the same report.

## Tasks

### Phase 1: Research

- [x] ✅ **Task 1.1**: Brainstorm designs: signals, where state lives, transport, notification,
  cost in turns ([RESEARCH-seat-status.md](RESEARCH-seat-status.md))
- [x] ✅ **Task 1.2**: Lift concepts from the watch list (herdr's screen-state manifests and the
  other session supervisors; research section 5)
- [x] ✅ **Task 1.3**: Owner picks a design: the defaults of research section 7, Q1-Q8. The
  state machine runs in each seat's link and is published as Matrix presence `status_msg`
  (revising v2's A 6.6). The coordinator's link builds `teamchat team-status [--watch]`; a
  whole team idle with requests open wakes the coordinator and the humans, once. Thresholds are
  constants; delivery to a rate-limited seat is held; the coordinator dispatches by ping; every
  member sees the states and only the coordinator is notified; the host-wide view waits for
  00224 and 00221; the token hot-swap proxy is host-local and triggered by a seat's
  rate-limited change.
- [ ] 🔄 **Task 1.4**: The probes research section 7 lists, before a spec. The documentation
  half is in [PROBES-claude-code.md](PROBES-claude-code.md): blocked is `PermissionRequest` and
  `Elicitation` (immediate), the reset time only the status line's `rate_limits.*.resets_at`.
  Live, still owed: which hook ends a turn at a usage limit, the usage banner's text, Tuwunel
  presence `status_msg` across syncs and a restart. Also asked of the hooks daemon's seat: its
  session-state records (session modes, usage pause and its resume time, stop events and
  failures, limit events), and whether a supported read of them exists or can

## Success Criteria

- [ ] An idle-locked team is visible to its coordinator without a human looking.
- [ ] A status change the coordinator need not act on costs no agent turn.
- [ ] A seat on another machine reports its state to the coordinator.

## Delivery & Milestones

- Research brief dispatched.
