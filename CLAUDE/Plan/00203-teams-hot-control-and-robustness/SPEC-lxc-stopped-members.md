# SPEC: LXC members whose containers are not running

Plan 00203 Task 2.6. The owner's requirement: play-agent-bus.yml "can't be so brittle that it
has to have LXC agents running. It's got to be tolerant of agent sessions not running at the
time of Ansible plays." After a reboot most containers are stopped (autostart is off on most
of them), so a stopped container is the ordinary state, not a fault.

## The fault this replaces

`run_present` (helpers/agent_bus_lxc/members.py) called `_guest_running` for every declared
present member on every run, including members joined long ago, and raised `MemberError` (exit
78\) when any container was not RUNNING. One stopped container failed the whole play, so no other
team change could be applied: adding a different member through `ccy-teams lxc-add` included.
The absent phase did the same for a member being taken off a team.

## Terms

- **Reachable**: the container exists and lxc-info says RUNNING. Only a reachable container is
  entered (lxc-attach).
- **Known handle**: the member's handle, known on this host without entering the container: the
  entry's own `host`, `seat` and `repo` when it gives all three; else the handle the member
  record (L8) says this member was joined as, if the record's checkout is the entry's and the
  record's handle agrees with whatever `host`, `seat` or `repo` the entry gives.
- **Joined**: the known handle is an active (not parked) member in `agent-bus list <team>`.

## Expectations

- **L1** A present member whose container is not reachable and which is joined is **skipped**:
  nothing is done to it (no kit, env file, config check or unit), the play does not fail, and
  stdout carries `SKIPPED<TAB><team><TAB><user>@<container><TAB><why>`, the why naming the
  container's state and that the first run with it up refreshes it.
- **L2** A present member whose container is not reachable and which is not joined (no known
  handle, or the handle is parked or not listed) is **deferred**: nothing is done to it, the play
  does not fail, and stdout carries `DEFERRED<TAB><team><TAB><user>@<container><TAB><why>`, the
  why naming the container's state and saying to start it and run the play (or ccy-teams apply)
  again, or not to declare it yet.
- **L3** A deferred or skipped member never blocks another: every reachable member is joined
  and refreshed as before, in the same run.
- **L4** A seat a skipped member holds (its known handle) is claimed, so a reachable member
  whose seat is derived never takes it. A deferred member with no known handle may hold an
  active numbered seat that no entry can name while it is stopped; in its team the seat
  derivation passes over such a seat (derive_seat's `unheld_ok`) instead of refusing it as a
  possible lost bundle, so the new member gets the next free number.
- **L5** A container that does not exist still fails (exit 78): the play joins existing
  containers and never makes one, so that is a wrong entry, not a stopped container. Every
  failure of a reachable member (gh, route, allow_from, suggest-handle, add-member, config
  check, service) still fails the play, as before.
- **L6** An absent member whose container is not reachable: its known handle, if active, is
  parked from the host side (so it loses the team at once); the bundle and env file inside the
  container are left for the first run with it up, which removes them (idempotent: the absent
  phase removes a bundle it finds and parks only an active handle). Stdout carries a `DEFERRED`
  line saying what is left. A user left in no team whose container is not reachable keeps its
  unit until that run.
- **L7** After the members' lines, when anything was skipped or deferred, the helper prints one
  `LXC-SUMMARY<TAB><text>` line naming each, and the play prints the skipped and deferred lines
  in a task of their own, so a run that deferred something says so without `-v`.
- **L8** The member record: `/var/lib/agent-bus-applied/lxc/<team>.json`, written by both
  phases for every team declared present that has an entry in the run (0640 in a 0750
  directory, root and the stamps' group, as the health record: no secret). One item per entry
  of the team: container, user, state (present or absent), status (`joined`, `stopped`,
  `deferred`, `left`), handle (or null), checkout, why, and `since` (when the item took its
  current status and handle, kept while they stay). A phase updates its own entries and keeps
  the other phase's; an item no entry declares any more is dropped. The record is rewritten
  only when it differs, and a rewrite is a `CHANGED` line; a run that changes nothing reports
  no change. It is written only after the phase succeeded. A team not declared present has no
  record (it is removed).
- **L9** `ccy-teams apply` (and every flow that applies a team, lxc-add included), after a
  successful play, reads each applied team's record and says each deferred member with its why,
  and each skipped one; the apply still succeeds.
- **L10** The board shows the record: a deferred present member has a row of its own (kind
  `lxc`, state `deferred`, WHERE `deferred: <why>`), a deferred absent member a row with state
  `deferred` saying it is leaving, and a joined member whose container was stopped has
  `container stopped at the last apply` added to its row's WHERE. An unreadable record is said
  on the team's row, never skipped.
- **L11** `ccy-teams lxc-add`, before its summary, lists the declared members of the team whose
  containers are not running, saying the play skips or defers them and that they do not block
  this add. It never starts a container. After the apply, a new member the record shows as
  deferred is said to be deferred, not joined.
