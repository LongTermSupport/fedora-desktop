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

### Phase 2: server-side robustness (audit gaps 3, 4, 9, 10)

Built on `fix/teams-server-robustness`; design and evidence in
`subagent-reports/261016-server-robustness-opus.md`, host proof in [ACCEPTANCE.md](ACCEPTANCE.md)
(`acceptance.bash`, R1-R9; B1-B2 after a reboot).

- [ ] 🔄 **Task 2.1** (gap 4): homeserver readiness, watchdog and restart on every exit; a
  minutely health check that restarts one whose client API stops answering.
- [ ] 🔄 **Task 2.2** (gap 3): `agent-bus seat check`/`take` wait, bounded, for a starting
  homeserver.
- [ ] 🔄 **Task 2.3** (gap 10): stale or failed backups and restarts on the board and in
  `agent-bus health status`; the sign-in limits said where a password is issued.
- [ ] 🔄 **Task 2.4** (gap 9): `service: true` LXC members run as a unit in their container.
- [ ] ⬜ **Task 2.5**: host acceptance passed (meta-deploy), and B1-B2 after a reboot.

### Phase 3: hot control build (the spec's milestones, tests first)

- [x] ✅ **Task 3.0** (M1; its commits call it Task 2.2): pingbus. The 3.1 and 3.2 parsers with
  `checkout` (`membership.py`, the one reader, M2's folded in), `active_teams` from
  `PINGBUS_MEMBERSHIP` (H4, H8), `pingbus seat keep` as the only lock holder for every seat with
  `LOCK_UN` and its H10 hook line (D4, H9, H10; audit F5), an empty membership in `seat exec`
  (one `seat exec`, keeping M2's launch-time inbound rule), `seat retire`, `reach`. With M2
  merged and F44's health check fix: CCY 3.99.3, container 2.70. Host acceptance: [ACCEPTANCE.md](ACCEPTANCE.md) M1,
  `_acceptance-m1.inc.bash`. Notes:
  [subagent-reports/261016-m1-pingbus-opus.md](subagent-reports/261016-m1-pingbus-opus.md).
- [x] ✅ **Task 3.1** (M2; its commits call it Task 2.3): the launcher and the entrypoint: the membership directory and its
  read-only relabelled mount, the `ccy-session` label, `PINGBUS_MEMBERSHIP`, `ccy-teams internal membership-init`, the kit in every named session under the inert contract, `--no-bus`,
  `PINGBUS_SEATS` and `ccy-seats` retired for ccy; the H34 spike (Claude Code does not re-read
  `--settings`, so inbound is decided at launch: a session on no team gets none, and its first
  join relaunches it once). Fixed after the Fable review
  ([subagent-reports/261016-m2-review-fable.md](subagent-reports/261016-m2-review-fable.md)).
  CCY 3.99.1, container 2.68, plugin 1.3.0. Host acceptance: [ACCEPTANCE.md](ACCEPTANCE.md) M2,
  `acceptance.bash` (a dispatcher; `_acceptance-m2.inc.bash` starts its own throwaway
  sessions), then the Plan 00201 and 00161 acceptances, ported to the membership, all queued in
  meta-deploy. Notes: [subagent-reports/261016-m2-launcher-opus.md](subagent-reports/261016-m2-launcher-opus.md).
  After the first host run (CCY 3.99.4.2): a ccy run from inside another session's pane is
  unnamed and writes no session's record, `record-seats` included (it had taken the outer
  session's identity: the U20 abort); the relabel is proved by labels on a Permissive host too;
  the 00201 and 00161 launches are unnamed by construction, and U20 M2.11 proves a named seated
  session. After the second (F44 7fd72106): M2's and U20 M2.11's tmux sessions run ccy under the
  pane's own shell, never `exec`'d (with every stream redirected, tmux hung up a pane nothing
  held, before ccy started), and record its exit status; U20 M2.3 stops the supervised watcher
  (SPEC-inject E26) and proves the `watcher_failed` path to `pingbus wait`.
- [x] ✅ **Task 3.2** (M3): the first end-to-end hot add and remove. `ccy-teams session add` and
  `remove` (per-step ✓/✗, idempotent, rolled back; re-baseline and goodbye on remove; the first
  join of a zero-seat session relaunches once through the supervisor's restart replay), one
  shared session discovery (`internal/teams/discover`, the shutdown lookup folded in), the
  watcher and waiter following the membership with the keeper starting the watcher (H11), the
  `MEMBERSHIP` kind with its hook fallback (H12, H13), the status line (H14), mid-turn removal
  (H15, H16). CCY 3.99.8, container 2.73, lifecycle plugin 1.1.0. Host acceptance:
  [ACCEPTANCE.md](ACCEPTANCE.md) M3, `_acceptance-m3.inc.bash`. Notes:
  [subagent-reports/261016-m3-opus.md](subagent-reports/261016-m3-opus.md).
- [ ] 🔄 **Task 3.3** (M4): the full non-interactive surface, built on `feat/hot-control-m4`;
  Fable review and host acceptance to come. `ccy-teams session role`, `move`, `list` and
  `repair`, the coordinator options of remove and move (`--coordinator=demote|hand-to=<seat>`),
  `add --role`, `remove --park` (H23-H25, H10); one change at a time per session and one seat
  allocation at a time per checkout, `agent-bus seat take` and `seat choose` honouring both
  (H20, H21, with Fable's N4: a stopped session's restorable record names its seats); the
  roster limit (H27), the session ending mid-change (H28), `ccy-teams remove <team>` taking its
  sessions off first (H29). CCY 3.99.9, container 2.74. Host acceptance:
  [ACCEPTANCE.md](ACCEPTANCE.md) M4, `_acceptance-m4.inc.bash`. As built, and what is not:
  SPEC-hot-control.md H21. Notes:
  [subagent-reports/261016-m4-opus.md](subagent-reports/261016-m4-opus.md).

## Decisions

These were settled by the coordinator under the owner's standing "do not gate on me". **The owner
may reverse any of them.** Full detail: SPEC-hot-control.md section 8.

1. The bus kit is loaded in every ccy session started through ccy's tmux, with a `--no-bus`
   opt-out. This holds only if the inert contract is met and tested in M2:
   - a flag-file guard in `hooks.json`;
   - no per-second Python status line;
   - no `crossSessionInbound` accept unless the session is on a team. Claude Code reads it only
     at launch (the M2 spike), so a session launched on no team has no inbox socket, and its
     first join relaunches it once, automatically, through the restart path; every later
     change is hot (SPEC H22, H34);
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
