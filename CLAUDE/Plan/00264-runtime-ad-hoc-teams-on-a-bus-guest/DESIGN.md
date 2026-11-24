# Plan 00264 design: runtime ad hoc teams on a bus guest

A design for the owner to decide on. Nothing here is built. It answers the owner's question Q61:
"starting an ad-hoc team must not need a whole IaC action; teams fluid, cheap to create and tear
down", with several teams per bus.

**Terms.** The *bus guest* is the bus server host (Plan 00246
[INSTALL-PATH.md](../00246-agent-bus-v2-landing-and-cross-network/INSTALL-PATH.md) "Terms") when it
is a guest machine that this repository's play provisions. A *member machine* is any machine whose
sessions take seats in a team. A *declared team* is one in `teamchat_teams` in host_vars. A
*runtime team* is one created on the bus guest by a `teamchat-admin` verb with no host_vars entry.
Placeholders: team-A, team-B, the bus guest, the member machine, the owner. Code paths are under
`src-go/internal/` unless given in full.

## 1. The team lifecycle today

### 1.1 Declared, approved, applied by the play

A team exists because host_vars declares it ([DESIGN-v2-C-host.md
3.8](../00209-agent-bus-v2-in-go/DESIGN-v2-C-host.md)): id, display `name`, `port` (declared per
team, "there is no range"), `server_name`, `networks`, `url`, `humans`, `coordinator`, `trust`,
`duties`, `state`, `purge`. [`playbooks/imports/play-teamchat.yml`](../../../playbooks/imports/play-teamchat.yml)
then, in order (its header, steps 1 to 6):

1. builds `teamchat-admin` and `teamchat` on the controller and installs them root-owned;
2. installs the pinned Tuwunel, the base units (`teamchat-server@`, `teamchat-tls@`, health and
   backup timers), the resolver files and the tmpfiles entry, printed by `teamchat-admin units list|print` (`teamchat/admin/unitsverb.go`);
3. on an enforcing host, installs the `teamchat` SELinux module
   ([`files/usr/local/share/teamchat/teamchat.cil`](../../../files/usr/local/share/teamchat/teamchat.cil))
   and labels **each present team's declared port** `teamchat_port_t`, one `seport` task per team;
4. `team approve` for the teams `teamchat_approve_teams` names (the review of the inventory change
   is the approval, DC18);
5. `lxc apply --phase absent`, `team remove [--purge]` for absent entries, `team apply` for present
   ones, `lxc apply --phase present`;
6. enables the health and backup timers exactly while some team is **declared** present (C40).

`ccy-teams create` (`teams/create/wizard.go`) is today's "start a team": it writes the declaration
and runs that play under the play lock (`teams/ops/playlock.go`). That is the whole IaC action the
owner wants out of the path.

### 1.2 What the host verbs make

All are root verbs of `teamchat-admin` (caller classes in `teamchat/admin/class.go`; the verb table
`rootRuns` in `teamchat/admin/verbs.go`), each under the team lock (`teamchat/teamlock`, DC11).

| Verb                              | Code                                         | What it creates or removes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| --------------------------------- | -------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `team approve`                    | `teamchat/declare/gate.go`, `host/dirs.go`   | `approved.json` (the team's security fields, existence included) in the status directory; makes the status directories                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `team apply`                      | `teamchat/host/apply.go`                     | step 2 refusals (server_name fixed at first apply, the approval held by `declare.Hold`, networks routed, the port not recorded by another team's `units.json` and not listening); step 4 first-apply files: `root/<team>/` (config with the registration secret, `server-name`), `ctl/<team>/`; the team CA and leaf (`host/tls.go`, CA made once); firewall rich rules per effective network, recorded in `firewall.json` first (`hostnet/firewall.go`); a backup before a restart; the server drop-in, `teamchat-server@<team>` and `teamchat-tls@<team>` started and enabled; readiness; loopback-only bind; bootstrap in the `teamchat-ctl` child (admin account, rooms, `ctl/<team>/roster.json`); then `team.json`, `units.json`, the stamp and the status record, last |
| `team remove [--purge]`           | `teamchat/host/remove.go`                    | refuses while the LXC record holds a seat not yet left; stops and disables the terminator then the homeserver; removes the recorded firewall rules; deletes the drop-in and the stamp; re-renders the health filter; deletes the status record. `--purge` also deletes the homeserver state, `ctl/<team>/`, the approval and `root/<team>/`, last                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `team seal` / `export` / `import` | `teamchat/carry/seal.go`, `export.go`        | carry a team to another host (SPEC-carry)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `team rotate-ca`                  | `teamchat/host/rotate.go`, `admin/rotate.go` | a new CA and leaf; every seat must then be reissued and placed again                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `member add\|return\|reissue`     | `teamchat/admin/verbs.go` `memberVerb`       | an issued seat (`seat.Issued`, `teamchat/seat/records.go`): the bundle with `base_url` and `tls_ca`, plus a password or token                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |

Every change verb is run by root, reached by the owner through password sudo (C10). Reads need no
privilege: the status record `/var/lib/teamchat-status/teams/<team>/status.json`
(`teamchat/status/status.go`), the stamp and `approved.json` beside it (`teamchat/paths`).

### 1.3 How a member machine learns of a team and takes a seat

- A seat reaches a member machine only as an issued bundle, piped into `teamchat seat place-issued --checkout DIR` (`teamchat/member/cli/place.go`, `teamchat/member/placeissued/`).
  The bundle carries the address (`base_url`) and the trust (`tls_ca`) pinned for that seat. There
  is no other trust import.
- A session's teams are `control/<session>/desired.json` in the owner's seat store
  (`teamchat/member/state/desired.go`, DESIGN-v2-C-host.md 3.7.5), written by the owner side
  (`teams/teamchatlaunch`, `teams/hot`, `teams/membership`; for LXC, `teamchat/host/lxc/apply.go`).
- The seat sweep (`teams/seatsweep/seatsweep.go` over `teamchat/sweep`) releases a seat whose
  session is over or that `desired.json` no longer lists. It never asks whether the team exists.
  Its release logs the seat out on the server; against a server that is gone or has a new CA the
  logout fails every pass and the seat stays held for ever (issue #386).
- `ccy-teams seats abandon` (`teams/seatsweep/abandon.go`, branch `ci/batch-386-397-398`) releases
  a team's seats without the logout, but only when `knownBy` finds no local source that still
  knows the team: (a) the team's status record on this machine (`paths.StatusFile`), and (b) any
  session's `desired.json` listing the team (`paths.Store.DesiredFile`). An unreadable source
  refuses (exit 78). Its doc comment says these are the sources a runtime-teams design reuses;
  section 5.4 does.

There is no tombstone, no "team removed" record, anywhere.

## 2. What creating a team at runtime requires on the bus guest

The finding that shapes everything: **`team apply` is already a runtime verb.** It is root Go,
idempotent, fail-fast, judged under a lock, and it makes every per-team resource itself. The play
only hands it a declaration. What ties creation to IaC is four things around it: where the
declaration comes from, how it is approved, the per-team SELinux port label, and the timers.

| Step                         | Today                                                           | Runtime, safely and idempotently?                                                                                                                                                                                                                            | Needs IaC                                                                                    |
| ---------------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| Declaration                  | host_vars entry, via the play                                   | Yes: the request is the declaration (JSON, the same `declare.Input` decode). `root/<team>/team.json` already keeps the last applied one, so a re-apply needs no host_vars                                                                                    | No                                                                                           |
| Id and name                  | declared                                                        | Yes: validated by `paths.ParseTeamID`; refused if any team (declared or runtime, installed or tombstoned) holds it                                                                                                                                           | No                                                                                           |
| Port                         | declared per team, no range                                     | Yes, if allocated from a range: lowest port in the range that no `units.json` records (`host.PortRecordedBy`) and nothing listens on, under one host-wide **allocation lock** (the team lock is per team, so two creates could otherwise pick the same port) | The **range** itself (a host var) and its reservation                                        |
| SELinux port label           | one `seport` per declared team, in the play                     | Not without root running `semanage` per create, which is slow and edits policy at runtime                                                                                                                                                                    | **Yes**: label the whole range `teamchat_port_t` once                                        |
| Approval (`approved.json`)   | `team approve`, by the play or password sudo                    | Yes, written by the runtime verb under the rules of section 3                                                                                                                                                                                                | The policy that pre-approves (section 3)                                                     |
| Homeserver instance          | template unit from the play, drop-in by `team apply`            | Yes: the drop-in, `DynamicUser` state directory and start are `team apply`'s                                                                                                                                                                                 | The base units (already)                                                                     |
| CA and TLS                   | `team apply` (`host/tls.go`)                                    | Yes: CA made once, never replaced; leaf renewed by the health tick                                                                                                                                                                                           | No                                                                                           |
| Firewall rich rules          | `team apply` (`hostnet/firewall.go`), recorded in firewall.json | Yes: per network, in the zone that routes it, recorded before the change                                                                                                                                                                                     | The **zones** and their reject target (INSTALL-PATH.md X30 section 1)                        |
| Roster, rooms, admin account | bootstrap in the `teamchat-ctl` child                           | Yes                                                                                                                                                                                                                                                          | No                                                                                           |
| Status record, stamp         | `team apply` step 11                                            | Yes. Adds `origin: runtime` (section 6)                                                                                                                                                                                                                      | No                                                                                           |
| Health and backup timers     | enabled while a team is **declared** present                    | A runtime verb could enable them, but then IaC and runtime fight over the same unit state                                                                                                                                                                    | **Yes**: enabled on a bus guest that allows runtime teams, whether or not a team is declared |
| Element profile              | its own play                                                    | Out of scope here: a member-side concern                                                                                                                                                                                                                     | No                                                                                           |

### 2.1 Privileges and the least-privilege route

`team apply` must run as root (system files, units, firewall, C9). The question is only how a
non-root requester reaches it. Three routes:

- **Password sudo** (as every change verb today, C10). No new privilege at all. The owner types a
  password per create.
- **A narrow NOPASSWD sudoers rule** for one verb. sudoers matches argv, which is weak for a verb
  whose input is a JSON document on stdin; it also reverses DC4's stance ("no reader with a
  sudoers rule") and gives every process of the requesting account root's entry point.
- **A request spool and a root oneshot** (recommended). A directory
  `/var/lib/teamchat-requests/` (tmpfiles, laid down by IaC): `incoming/` group-writable by a
  `teamchat-requesters` group, sticky, so a file's owner uid is its requester, read through an
  `O_NOFOLLOW` open and `fstat`; `done/` root-only. A `teamchat-requests.path` unit starts
  `teamchat-requests.service`, which runs `teamchat-admin team requests` as root: each request is
  decoded strictly, judged against the policy, and applied or parked for the owner. No sudo, no
  daemon, no socket; the root code is the existing Go binary. The requester learns its verdict
  from `done/<id>.json` (group-readable). This mirrors how the design already moves state across
  privilege: files written by one side, read by the other (DC4, 3.7.5).

What IaC must lay down for the recommended route: the group, the spool directories (tmpfiles), the
path and service units (printed by `teamchat-admin units print`, so the text stays Go's), the
policy file `/etc/teamchat/runtime-teams.json`, the port range and its SELinux label. A missing
policy file means runtime teams are off: every request is refused with exit 78 and its reason.

## 3. Approve

Creating a team is a security change by DC18: existence is a gated field, with `humans`,
`coordinator`, `networks`, `url`, `trust` and LXC members. Three ways to approve:

1. **Owner only, per team.** Every request waits until the owner runs `sudo teamchat-admin team approve --request <id>` (password). Safe; not cheap: an agent team cannot start one while the
   owner is away.
2. **Coordinator request, owner approval.** As 1, but a coordinator agent may file the request,
   and the board shows it pending. Still owner-gated for every team.
3. **Policy quota** (recommended, with 2 as its escalation). The owner approves a **policy** once,
   in IaC (the review of the host_vars change is the approval, exactly as `teamchat_approve_teams`
   is today): how many runtime teams may run, the port range, which networks a runtime team may
   name (for example the LXC bridge and one member network), and that `humans`, `trust` and `url`
   are empty or fixed. A request inside the policy is applied with no password: the root oneshot
   writes `approved.json` from it, recording `approved_by: policy <digest>` and the requester's
   uid. A request outside it (a human, a wider network, a trust setting) is parked as pending and
   needs route 1.

**Audit.** Every request keeps its document, the requester's uid, the verdict, the policy digest it
was judged against and the CHANGED lines in `done/<id>.json`, root-owned, never deleted by the
verbs (pruned by count, like the status events, `status.MaxEvents`). The status record gains an
`origin` of `runtime` and the request id. The board lists runtime teams with who asked.

## 4. Tear down

### 4.1 Remove and purge

A runtime team is removed by a request of kind `remove` (by its requester, or anyone in the group
when the policy says so) or by the owner directly. It runs today's `team remove`, unchanged, plus
the notice and tombstone below. `--purge` semantics are today's. For a runtime team the default
should be purge, since nothing declares it and an unpurged runtime team's `root/<team>/` is state
nobody will return to; that default is an owner decision (Q4). The port returns to the range when
`units.json` goes, which is with the purge; a plain remove keeps the port recorded, so the same
team can be brought back on it.

### 4.2 Releasing members' seats cleanly (the issue #386 gap)

The gap: after a remove, and worse a purge and re-create with a new CA, members' seats cannot be
released because the release's logout can never succeed. The fix is two signals, one live and one
lasting.

1. **A closing notice while the server still runs.** Before `team remove` stops the homeserver, it
   posts a host lifecycle notice `team-closing` in the main room (a new kind in area A's protocol,
   [DESIGN-v2-A-protocol.md](../00209-agent-bus-v2-in-go/DESIGN-v2-A-protocol.md), so it is A's to
   specify) and waits a bounded grace for the linked members to leave. A linked member's link
   reports it to its session; the owner side (the sweep, the only writer of `desired.json` besides
   the launcher) drops the team from `desired.json` with a farewell of reason `team-closing`, and the
   normal release runs, its logout succeeding because the server is still up. Seats still held at
   the end of the grace fall to step 2.
2. **A tombstone that outlives the team.** `team remove` replaces the status record with
   `/var/lib/teamchat-status/teams/<team>/removed.json`: the team id, its `server_name`, the
   fingerprint of the CA it had, the time, and whether it was purged. It is deleted only when a
   team with that id is applied again, which writes a status record with the new CA's fingerprint.
   The notice in step 1 carries the same fields; a member machine's link writes them into its own
   seat store (`<store>/tombstones/<team>.json`) so a member machine that is not the bus guest has
   the tombstone too.

The sweep then releases, without the logout (the `seat.Abandon` path issue #386 added), any held
seat whose bundle's CA fingerprint equals a tombstone's. That is precise: a re-created team has a
new CA, so a tombstone of the old one never matches a seat of the new one.

A member machine that was offline for the whole grace never receives the notice. Its link then
fails the pinned TLS handshake (a new CA) or cannot connect at all. Whether that alone may release
a seat is Q7; the design's default keeps `ccy-teams seats abandon` as the manual route there,
since a TLS failure alone cannot tell a re-created team from an attack or a network fault.

### 4.3 Rename

The team id never changes (DESIGN-v2-C-host.md 3.8: a changed `team` is a removal and a new team).
A runtime **display** rename is a request of kind `rename` that re-applies the recorded
`team.json` with a new `name` (C44): no new port, CA or seat. An id change is not offered: it is
create team-B, move the members by issuing new seats, remove team-A (with its tombstone). Carry
(`team seal`/`export`/`import`) keeps the id and is for moving a team between hosts, not renaming.

## 5. Several teams per bus

### 5.1 Limits and ports

- One homeserver per team stays (DC1); each costs roughly 100-135 MB resident at the default cache
  (DESIGN-v2-C-host.md 3.2.1, H-C1), held by `MemoryHigh=`. The policy's `max_runtime_teams`
  bounds the total; the root oneshot refuses a create past it, naming the running ones.
- The port range is a host var, `teamchat_runtime_ports: {first, last}`, its size at least
  `max_runtime_teams`. Declared teams keep their declared ports; `declare check` and the play refuse
  a declared port inside the range, so the two never compete.
- A create also refuses when free memory, read from `/proc/meminfo`, is below the policy's floor:
  the refusal names the floor and the figure, never a silent start that the kernel later throttles.

### 5.2 Naming

Runtime ids live in the same id space as declared ones (`^[a-z][a-z0-9-]{0,21}$`, `ParseTeamID`).
An id is refused while any team, declared, runtime, or tombstoned and not purged, holds it. Whether
runtime ids carry a fixed prefix, so a host_vars author can never pick one by accident, is Q8.

### 5.3 Isolation

Unchanged from v2 and not weakened by runtime creation: a dynamic uid, a CA, a port, firewall rules,
an admin token and a roster per team (DESIGN-v2-C-host.md 3.2.1, 3.2.2). The one new shared thing
is the spool; a request file is data, decoded strictly with a size cap, and the root code that
reads it is the same that reads the play's JSON today.

### 5.4 Never "unknown" while live: the known-team sources

`knownBy` must never judge a live runtime team unknown. The design keeps its two sources and makes
the runtime path feed them, then adds the tombstone as a third, explicit answer:

| Source                                         | A live runtime team                                                                                                                                       | A removed team                                      |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------- |
| status record on this machine                  | present: the runtime create writes it in `team apply` step 11, under the team lock, before the request's verdict says applied; no bundle exists before it | absent (replaced by the tombstone)                  |
| any session's `desired.json`                   | listed once a session has joined                                                                                                                          | dropped by the sweep on the notice or the tombstone |
| tombstone (`removed.json`, or the store's own) | absent                                                                                                                                                    | present: `knownBy` answers "removed", not unknown   |

Two rules follow. `knownBy` reads the status record's **presence**, never its currency: a record
older than `status.Lifetime` (a stopped health timer) still means known. And the tombstone is
consulted first: a tombstoned team is "removed", which `seats abandon` and the sweep accept with no
`--allow-known-team`, while a team with a status record or a listing is known, as now. On a member
machine that is not the bus guest there is no status record, so `desired.json` and the store's
tombstones are the sources there; that is no weaker than today, since a seat is only ever placed
from a bundle the host issued after `team apply` finished.

## 6. What stays in IaC, and how IaC and runtime coexist

**Stays in IaC:** the bus guest itself and its provisioning; the packages and the pinned Tuwunel;
`teamchat-admin`, `teamchat` and the kit; the base units, the request path and service units, the
tmpfiles entry with the spool; the `teamchat-requesters` group and its members; the firewalld zones
with their reject target; the port range and its SELinux label; the policy file; the timers' enable
rule; and every declared team.

**Coexistence.** The play **ignores** runtime teams' declarations and **converges their software**:

- The play's team steps act only on host_vars entries, as now. A runtime team is not absent from
  host_vars in the play's sense; it is not in it. Today's play never removes an undeclared team
  (only `state: absent` entries), so this already holds and is kept as a tested rule.
- The timers step changes from "a team is declared present" to "a team is declared present or the
  policy allows runtime teams", so a play run never stops the health timer under a runtime team.
- After a base unit or member program change, the play already re-applies every declared team
  (play-teamchat.yml "Apply Every Present Team Because a Base Unit or the Member Program
  Changed"). It also runs `teamchat-admin team reapply --origin runtime`, which re-applies each
  runtime team from its own `root/<team>/team.json`, so no runtime team runs stale units.
- ccy-teams' APPLIED view (DESIGN-v2-C-host.md 3.13) shows a stamp with no declaration as
  "decide". A stamp with `origin: runtime` is shown as "runtime" instead, so drift stays visible
  and a runtime team is not mistaken for a leftover.
- **Adoption** is explicit, never implicit: `ccy-teams adopt team-A` splices the recorded
  `team.json` into host_vars (the existing byte-splice writer) and the next apply sets the origin to
  declared. A host_vars entry whose id is a running runtime team's is refused by `declare check`
  until it is adopted, so a play can never silently take a runtime team over (Q6).

**Reprovisioning the guest.** A rebuilt guest has no runtime teams: their state and backups lived
on the guest. The recommended default is that runtime teams are ephemeral by definition. Before a
planned rebuild the owner may carry a runtime team off (`team seal`/`export`) or adopt it. The
members of a team lost to a rebuild have no tombstone; that is the Q7 case. Q5 asks whether
ephemeral is acceptable.

## 7. Failure modes and fail-fast behaviour

| Failure                                   | Behaviour                                                                                                                                                     |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| No policy file, or runtime teams off      | every request refused, exit 78, verdict names the missing file                                                                                                |
| Malformed or oversized request            | refused with the decode error in its verdict; never skipped, never retried silently                                                                           |
| Requester not in the group                | the spool's mode refuses the write; a file of another owner in `incoming/` is refused by uid                                                                  |
| Port range exhausted, or team cap reached | refused, naming the teams that hold the range                                                                                                                 |
| Two creates at once                       | the allocation lock serialises them; a bounded wait, then a refusal naming the holder (C12)                                                                   |
| `team apply` fails part way               | today's behaviour: the CHANGED lines up to the failure, the named step, exit code; the verdict records it; the team is in "failed", a re-request converges it |
| Remove while an LXC seat has not left     | today's refusal (`host/remove.go` `lxcSeats`), in the verdict                                                                                                 |
| Members do not leave within the grace     | the remove proceeds; their seats are released by the tombstone                                                                                                |
| The path unit's service fails             | the unit is failed, which the health tick reports in the status record; nothing is retried in a loop                                                          |
| A play run during a runtime create        | the play lock and the team locks already order them; the play does not touch the runtime team                                                                 |

## 8. How it is tested

- **Unit, with fakes**, in the packages that change: the request decode and the policy judgement;
  the port allocator over a fake root with recorded `units.json` and fake listeners (`host` already
  has fakes, `host/fakes_test.go`); the verdict records; `team remove` writing the tombstone and
  posting the notice through a fake child; `knownBy` and the sweep over a store with tombstones,
  status records and `desired.json` files in every combination of the table in 5.4; the play's
  "runtime teams untouched" rule as a Go test over the play's team-step selection inputs where it
  can be, else in the lab.
- **The VM lab** (`src-go/cmd/vmtest`, the Plan 00246 scenarios): on a bus guest provisioned by
  the play with a policy, a member on a second VM. Create team-A by request; join a seat; create
  team-B beside it and show the two cannot reach each other's ports; remove team-A and show the
  member's seat released by the notice; purge and re-create team-A with the member offline, bring
  it back, and show the tombstone releases the old seat and the new seat places; rerun the play and
  show both runtime teams untouched and re-applied after a base unit change; SELinux enforcing
  throughout.

## 9. Options

| Option                                | What                                                                                                                     | For                                                                                                         | Against                                                                                                                    |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| O1 Password sudo runtime verbs        | `sudo teamchat-admin team create` with a JSON document; port from the range; everything else as `team apply`             | smallest build; no new privilege; matches C10 exactly                                                       | the owner types a password for every team, so an agent team cannot start one alone; not "cheap" in the owner's sense       |
| O2 Policy-bounded spool (recommended) | requests in a spool, a root oneshot applies those inside an IaC-approved policy, parks the rest for the owner's password | cheap within the policy; no sudoers, no daemon; every request audited; the owner's gate moves to the policy | more to build (spool, units, policy, allocator); the policy must be drawn tight enough that "inside" is genuinely safe     |
| O3 NOPASSWD sudoers verb              | a sudoers line for one `teamchat-admin` verb                                                                             | little to build                                                                                             | argv matching is a weak boundary for a stdin document; reverses DC4; any process of the account reaches root's entry point |
| O4 Keep IaC, make the play cheaper    | a scoped `teams` tag run per create                                                                                      | nothing new                                                                                                 | still a whole IaC action, which is what Q61 rejects                                                                        |

**Recommended: O2**, with the notice and tombstone of 4.2 built first, since they close issue #386
for declared teams too and every option needs them.

## 10. Questions for the owner

1. **Who may request** a runtime team: the owner only; the owner and coordinator agents (members of
   `teamchat-requesters`); or any member account on the bus guest?
2. **What the policy pre-approves**: which networks, and must `humans`, `trust` and `url` be empty
   for a request to pass without your password?
3. **Limits**: the maximum number of runtime teams, the port range, the free-memory floor.
4. **Remove default** for a runtime team: purge, or a plain remove that keeps its state? And the
   bound on the closing notice's grace.
5. **Reprovision**: are runtime teams ephemeral (lost with the guest), or must a rebuild carry them?
6. **Adoption**: may a runtime team be adopted into host_vars, and should a host_vars entry that
   names a running runtime team's id be refused until adopted (recommended) or adopt it silently?
7. **Offline members**: may a pinned-CA TLS failure alone release a seat, or does the manual
   `seats abandon` stay the only route when no notice or tombstone reached the machine
   (recommended)?
8. **Ids**: a fixed prefix for runtime team ids, or the shared id space with refusal of any id in
   use?
9. **Timers**: enabled on every bus guest that allows runtime teams, even with no team running?
