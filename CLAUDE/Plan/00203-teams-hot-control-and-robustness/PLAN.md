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
- [x] ✅ **Task 1.2**: Hot control design and spec ([SPEC-hot-control.md](SPEC-hot-control.md),
  revision 2, H1-H35, milestones M0-M7). Revised after the Fable review
  ([subagent-reports/261016-hot-control-spec-review-fable.md](subagent-reports/261016-hot-control-spec-review-fable.md)).
  Notes: [subagent-reports/261016-hot-control-design-opus.md](subagent-reports/261016-hot-control-design-opus.md).
- [ ] ⬜ **Task 1.3**: Milestones for the build, from 1.1 and 1.2, each one shippable.

### Phase 2: build (the spec's milestones, tests first)

- [x] ✅ **Task 2.2**: M1, pingbus: the 3.1 and 3.2 parsers with `checkout` (`membership.py`,
  the one reader, M2's folded in), `active_teams` from `PINGBUS_MEMBERSHIP` (H4, H8), `pingbus seat keep` as the only lock holder for every seat with `LOCK_UN` and its H10 hook line
  (D4, H9, H10; audit F5), an empty membership in `seat exec`, `seat retire`, `reach`. CCY
  3.99.1, container 2.68. Host acceptance: [ACCEPTANCE.md](ACCEPTANCE.md) M1. Notes:
  [subagent-reports/261016-m1-pingbus-opus.md](subagent-reports/261016-m1-pingbus-opus.md).

- [x] ✅ **Task 2.3**: M2, the launcher and the entrypoint: the membership directory and its
  read-only relabelled mount, the `ccy-session` label, `PINGBUS_MEMBERSHIP`, `ccy-teams internal membership-init`, the kit in every named session under the inert contract, `--no-bus`,
  `PINGBUS_SEATS` and `ccy-seats` retired for ccy; the H34 spike (Claude Code does not re-read
  `--settings`; the spec's fallback is built). CCY 3.99.0, container 2.67, plugin 1.3.0.
  Host acceptance: [ACCEPTANCE.md](ACCEPTANCE.md) M2, `acceptance.bash`, queued in
  meta-deploy. Notes: [subagent-reports/261016-m2-launcher-opus.md](subagent-reports/261016-m2-launcher-opus.md).

## Decisions

These were settled by the coordinator under the owner's standing "do not gate on me". **The owner
may reverse any of them.** Full detail: SPEC-hot-control.md section 8.

1. The bus kit is loaded in every ccy session started through ccy's tmux, with a `--no-bus`
   opt-out. This holds only if the inert contract is met and tested in M2:
   - a flag-file guard in `hooks.json`;
   - no per-second Python status line;
   - no `crossSessionInbound` accept unless the session is on a team;
   - exactly one resident process, the keeper.
2. Hot control is control, not isolation, within a checkout. **A remove does not revoke the
   token.** `--revoke` is deferred behind audit F7.
3. cc's weaker authority is accepted (the session can write its own membership file), and so
   is the new `cc --bus`.
4. `PINGBUS_SEATS` and the `ccy-seats` label are retired in M2, together with the explicit
   `PINGBUS_MEMBERSHIP` marker.

## Success Criteria

- [ ] The owner adds, removes, moves and re-roles a running ccy session from `ccy-teams`, and the
  session follows without a relaunch.
- [ ] Killing any team-system process, or restarting any homeserver, is recovered automatically.
  Acceptance proves each case on the host.
- [ ] A host reboot brings every team back with no human step.

## Delivery & Milestones

- Plan created; audit and design dispatched.
