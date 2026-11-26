# Plan 00266: seat restart after reboot

**Status**: Not Started (design for the owner; nothing is built until the owner has seen it)
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

### Phase C: LXC containers and member sessions (needs the owner's ruling)

- [ ] ⬜ **Task C.1**: The owner rules on reversing Plan 00257's "a session never starts with its
  container", in the narrow form: restart only the member units active at shutdown.
- [ ] ⬜ **Task C.2**: A root unit from `play-lxc-install-config.yml`. At shutdown it records the
  running containers and their active member units, root-owned under `/var/lib`. At boot it
  starts exactly those: names in both the snapshot and `agent_bus_lxc_members`, units by
  their derived name only. It runs before the user's session restore, and its logic is in Go.
- [ ] ⬜ **Task C.3**: verify-restore reports the LXC set.

## Success Criteria

- [ ] A test plants each checkout-content change of Task A.1's class, and the launch and the
  restore both refuse it.
- [ ] A restore refuses a record with a launcher, flag, checkout, token or seat outside its rule.
- [ ] After a live reboot, the containers and seats that were running are back, and no others.

## Delivery & Milestones

- Design: [DESIGN.md](DESIGN.md), read in the code on 26 Nov.
