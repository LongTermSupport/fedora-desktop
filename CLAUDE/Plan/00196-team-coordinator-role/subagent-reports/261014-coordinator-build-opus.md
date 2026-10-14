# Plan 00196 build report: team coordinator role

Built on branch `feat/team-coordinator` (rebased onto F44; not pushed, not merged).

## What exists

- **Protocol, pingbus, agent-bus.** Role `coordinator`; `orchestrator` reads as coordinator in
  the team record, the registry, the LXC member entries and `agent-bus list`; writers write
  `coordinator`. `set-role` and `add-member` refuse a second coordinator, naming the holder (a
  parked holder still holds). `--to-coordinator` on `send` and `say`, combinable with `--to`;
  `--to-orchestrator` is gone with no alias. Auto-copy: a worker's `say`/`send` naming a human
  also names the coordinator (no new wire kind, protocol still version 1); the coordinator's
  `PING`/`PEER` line carries a new last field `copy` (`copy:alice` or `-`). Session start says the
  agent's role and who the coordinator is; the skill has a Roles section.
- **ccy-teams, launcher.** The board marks the coordinator's seat; the seat menu has Make
  coordinator / Stop being coordinator (asks, names the holder, sudo). `ccy-teams choose-seat`
  (the old bash chooser, ported) asks the role of an existing seat; `describe-seats` gives Quick
  Launch's summary seat and role. CCY 3.97.1, container 2.55. The launcher's statements did not
  grow; its seats library shrank.
- **Host run.** `deploy.bash` in the plan folder and a `meta-deploy.list` entry before the 00161
  acceptance.

## qa-reviewer findings, and what was done

- Blocking: the queue file moved on F44 (rebased, entry now in `meta-deploy.list`); the 00161
  acceptance used `orchestrator` (updated to `coordinator`); the queue order puts this plan first.
- Container version not bumped although the pingbus kit is in the image (bumped to 2.55).
- LXC members: two coordinators in a team are refused at parse time; workers join before a
  coordinator so a handover works in any list order.
- Nits: the protocol example sender, the 32-addressee limit sentence, milestones.

## Owner decisions and notes

1. A new seat has no member until the launch makes it, so `ccy --teams` asks the role only for a
   seat that exists; a new seat joins as a worker and says how to change it. Say if you want the
   role applied after the seat is made instead.
2. A member declared in `agent_bus_lxc_members` has its entry's role (worker by default), set again
   on every play run: a coordinator chosen in ccy-teams for such a member is reset unless the entry
   says `role: coordinator`.
3. Every member of a team must take the new pingbus before the team's record names a coordinator
   (older builds read the record as untrusted), the same rule `org` and `exclude` set.
4. The receiver does not refuse a record listing more than one coordinator (an old record may);
   the writers hold the limit of one.
5. The 00161 acceptance may count messages a worker sends to a human; with a coordinator in the
   team each now has one more addressee. Not exercised here (host only).
