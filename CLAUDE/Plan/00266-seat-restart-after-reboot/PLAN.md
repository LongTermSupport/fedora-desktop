# Plan 00266: seat restart after reboot

**Status**: In Progress (design for the owner; nothing is built until the owner has seen it)
**Created**: 2026-11-26
**Owner**: dev
**Priority**: High (Phase A is a security gap)

## Overview

After the nightly host reboot, the dev LXC containers and the Claude seats inside them do not
come back. The owner asked for three things. First, a host-level snapshot of which LXC
containers were running at reboot, and a start of exactly those (not autostart for all).
Second, a way to restart the Claude sessions inside them. Third, a judgement on whether
"each seat knows its own startup command" is safe.

The design reads the code to answer that last question. A stored free-form command is the
hazard, so the restart must be rebuilt from a structured record whose every field is
validated. `ccy-sessions restore` is already close to that, for ccy and for host `cc`
sessions. The research found something more urgent. A container can write files in its
checkout that the host launcher applies on the next launch without asking, and one of them
can widen the container's own mounts. That is closed first, as a class, by Defence Before
Fix.

The full findings, each with file and line, are in [DESIGN.md](DESIGN.md).

## Goals

- A container session can never cause code to run on the host through anything its checkout
  holds, at an attended launch or an unattended restore.
- Every field a restore replays is validated against a fixed rule. No field is executed as
  given.
- A host `cc` seat resumes its seats' conversation on restore, as a ccy seat does.
- After a reboot, the LXC containers that were running, and the member sessions that were
  active, are started again. Nothing else is started.

## Non-Goals

- Autostarting every LXC container.
- A free-form "startup command" stored per seat.
- Switching a session's sign-in token (Plan 00146).

## Tasks

### Phase A: checkout content the host applies (security; Defence Before Fix)

- [ ] ⬜ **Task A.1**: Name the class "host launcher applies container-writable checkout
  config". Write its detector, prove it red on `.claude/ccy/mounts` and the compose start,
  and sweep for every other instance.
- [ ] ⬜ **Task A.2**: Make `.claude/ccy/mounts` an allowlist. Require approval of a changed
  file at an attended launch, with the approved file's hash kept outside the checkout. An
  unattended restore refuses a changed file.
- [ ] ⬜ **Task A.3**: Under restore, `--compose start` refuses a compose file whose hash differs
  from the one recorded at the attended launch.
- [ ] ⬜ **Task A.4**: Derive the record's `launcher` from its prefix (a fixed table), never
  executing a recorded path.
- [ ] ⬜ **Task A.5**: Replay arguments through an allowlist of the launcher's own flags. No
  unknown flag, and no word after `--`, for cc above all.

### Phase B: host cc seats and field validation

- [ ] ⬜ **Task B.1**: Resume by seat record for cc seats (`bySeats` is ccy-only today).
- [ ] ⬜ **Task B.2**: In `judgeRestore`, validate the checkout (a real git work tree, not a
  link, not at or above `$HOME`), the token (a pool name, never a path) and the seats
  (declared for this machine, of the record's kind).

### Phase C: LXC containers and member sessions

Owner (26 Nov, verbatim in substance): starting claude in an LXC container means getting in
(ssh or lxc-attach), going to the right folder, then the right claude command, and the
commands inside the container may differ from the host's. For now, log what command starts
each LXC seat and replay it. Do not agonise over the threat model for LXC yet; harden later.

So, for LXC only, each seat's record is the way in (lxc-attach or ssh, the container, the
user), the directory, and the launcher or claude command as it is inside that container. The
launch that starts the seat writes it, into a host-owned file (root or the host user, never
inside the container or a checkout it mounts), and restore replays it. The validation of
Phase A and B is owed for LXC too, and is recorded as owed, not blocking.

**The supported route for a teamchat (v2) seat inside a container** is its session unit, which
`teamchat-admin lxc start --member <container>/<key>` starts from the host (checked,
`src-go/internal/teamchat/host/lxc/unit/unit.go:1-95`).

- It runs as the container user, in the seat's checkout:
  `tmux … new-session -d … teamchat run --claude --kind lxc --key <key> --resume-recorded -- claude`.
  So it already resumes the seat's recorded conversation.
- It has no `[Install]` section and `Restart=no`, so nothing starts it at boot.
- A seat that must come back after the container boots is restarted through that unit (Task
  C.2), never through a unit of its own inside the container.
- (DESIGN.md's `agent-bus-claude-<user>.service` is the v1 bus's unit, not this one.)

**Sign-in (checked, `unit.go:84-95`):** the unit sets only HOME, XDG_RUNTIME_DIR and PATH and
passes no token, so claude uses the container user's own stored Claude login. A seat inside a
container therefore needs that user logged in to Claude once, interactively, inside the
container. The host's per-session token pool does not reach it.

- [x] ✅ **Task C.0**: How an LXC seat starts and signs in: answered above.
- [ ] ⬜ **Task C.1**: Owner ruling, given 26 Nov: seats in LXC come back after a reboot, which
  reverses Plan 00257's "a session never starts with its container" for the seats active at
  shutdown. Recorded; nothing more to decide.
- [ ] ⬜ **Task C.2**: A root unit from `play-lxc-install-config.yml`. At shutdown it records the
  running containers and their active seat units, root-owned under `/var/lib`. At boot it
  starts exactly those containers, then runs `teamchat-admin lxc start --member` for each
  recorded seat, by its declared container and key only. It runs before the user's session restore, and its logic is in Go.
- [ ] ⬜ **Task C.3**: verify-restore reports the LXC set.

## Success Criteria

- [ ] A test plants each checkout-content change of Task A.1's class, and the launch and the
  restore both refuse it.
- [ ] A restore refuses a record with a launcher, flag, checkout, token or seat outside its rule.
- [ ] After a live reboot, the containers and seats that were running are back, and no others.

## Delivery & Milestones

- Design: [DESIGN.md](DESIGN.md), read in the code on 26 Nov.
