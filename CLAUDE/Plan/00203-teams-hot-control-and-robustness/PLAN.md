# Plan 00203: teams hot control and robustness

**Status**: In Progress
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: High

## Overview

The owner's request, in their own words: "i would like to be able to "hot" control ccy sessions -
add them, remove them, update them into teams, set roles etc all whilst the ccy session is
running. the whole system needs to be totally robust, systemd services with restart for the
required server side, etc etc. take this to the fully robust and solid level that I need".

Today a session's teams, seats and role are fixed when it is launched. Changing them means a
relaunch. Parts of the system also stop working after a transient fault and do not come back. The
known case: a session's watcher exits for good when its homeserver restarts. This plan makes
membership a live, controllable property of a running session, and makes every server-side and
session-side part of the team system recover by itself.

## Goals

- From `ccy-teams`, as the owner on the host, while the session runs:
  - add a running ccy session to a team;
  - remove it from a team;
  - move it between teams;
  - change its role (coordinator or worker).
- The session picks the change up without a relaunch, and says so in its own conversation.
- Every long-running server-side part is a systemd service that restarts on failure and starts at
  boot. Every session-side part recovers from a transient fault (a homeserver restart, a network
  blip, a host reboot) without a human.
- Health is visible: `ccy-teams` and `pingbus status` say plainly what is down and what to do.

## Non-Goals

- Control from inside a session, or from the bus. Hot control is the owner's, run locally on the
  host, never a Matrix message a peer could send.
- Multi-host teams.

## Tasks

### Phase 1: audit and design

- [ ] 🔄 **Task 1.1**: Robustness audit of the whole team system, server and session side
  (`subagent-reports/261016-robustness-audit-opus.md`).
- [ ] 🔄 **Task 1.2**: Hot control design and spec (`SPEC-hot-control.md`).
- [ ] ⬜ **Task 1.3**: Milestones for the build, from 1.1 and 1.2, each one shippable.

## Success Criteria

- [ ] The owner adds, removes, moves and re-roles a running ccy session from `ccy-teams`, and the
  session follows without a relaunch.
- [ ] Killing any team-system process, or restarting any homeserver, is recovered automatically.
  Acceptance proves each case on the host.
- [ ] A host reboot brings every team back with no human step.

## Delivery & Milestones

- Plan created; audit and design dispatched.
