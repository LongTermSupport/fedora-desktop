# Plan 00209: agent bus v2 in go

**Status**: In Progress (landing on F44 is Plan 00246; the plan of record for v2, `teamchat`, a separate system beside v1; N0's
design is integrated in [DESIGN-v2.md](DESIGN-v2.md), reviewed and fixed in three rounds, with its last check open; a dedicated build
session does the work to [BUILD-BRIEF.md](BUILD-BRIEF.md))
**Created**: 2026-10-19
**Owner**: joseph
**Priority**: High

## Overview

The agent team bus (v1) is Python (the admin tool, `pingbus`, the LXC helper, `declare.py`,
`element.py`), bash (the installer, the root wrapper) and Go (`ccy-teams`), glued by marker lines,
exit codes and `sudo`. Opening `ccy-teams` takes 9-23 s on the host because every bus read is a
serial Python process, and every running session starts a Python process for its status line and
for each hook. LXC members are the most fragile part: EL9 guests have Python 3.9.

v2 is the bus rebuilt in Go as a separate system named `teamchat`, designed from the requirements
and not from v1. It installs beside v1, touches nothing v1 uses, and is inert with no team. Teams
move to it one at a time, by a migration the owner chooses to run. v1 is retired only when every
team has moved, and that is raised with the owner then.

## The owner's directions

1. "the python version is NOT a golden archetype. its a prototype and first draft. this go version
   DOES NOT have to stick to python approaches etc. FIX THINGS IMPROVE THINGS MAKE IT EXCELLENT"
2. "i dont even care about staying compatible - now is the time for breaking changes and really
   making this system PROPERLY DECENT"
3. "already running teams like yours msut keep workiing", "teams v2 can be a different thing"
4. "It's time to properly lock into Go - ditch the python and bash legacy and push hard into Go. We
   need fast tools, fast tests and CI, clean and robust" (2026-10-30). Finishing v2 and moving the
   teams onto it comes before further v1 hardening; a v1 defect is fixed in v1 only when it stops
   a team now, and is otherwise carried as a v2 requirement. `bus/pycompat` and its python3
   oracle tests exist only to read v1's files, and go with v1.
5. "go for breaking changes on the ccy teams stuff - I am the single user, as long as your plays
   handle updating the desktop teams then we're good. Push hard, break stuff - move forwards"
   (2026-10-30). This answers OD-CUTOVER: every team moves by one step, which the plays run, and
   v1 is then retired. Direction 3's "already running teams must keep working" now means the
   plays carry each team across, not that v1 and v2 run side by side.
6. Where v2 is going (2026-10-30, answering gate A): "what we've built so far, which only works on
   local host, is not the intended end result": one homeserver per team, which "might be hosted
   locally ... on another server ... somewhere in the cloud", and agents "spread across multiple
   machines, possibly in multiple different networks", reaching it over "some kind of vpn or mesh
   network"; "not the intention that the home server would be on the internet but some kind of
   private networking is implied". The irreversible choices stand "as long as all of these points
   are not locking us into things that are going to prevent that going forwards" (Task 0.6).
7. **v1 is feature-frozen; cross-network membership is v2 only** (2026-11-02): "It goes in v2 and
   lets get v2 done - carrying two versions is nuts"; "if we have v2 imminent then v1 should
   feature freeze apart from bug fix". No v1 feature work starts, the remote-seat items
   included; a v1 change is a bug fix or nothing. On networks: "pingbus should not invent its
   own wireguard overlay ... it should simply support working across multiple routable ip
   ranges - how they are routed is not its concern", and "the team should have explicit configs
   for networks it allows team members to be on expressed as normal CIDR". So v2 has no overlay
   (Plan 00220's overlay is dropped from the bus): a team declares its member networks as a
   list of CIDRs, the one source for its listen addresses, `allow_from`, the unit's IP filter,
   the firewall rules and the seat URL check; a seat is keyed on the homeserver URL; transport
   is TLS with the team's own CA pinned in the bundle (plain HTTP only on loopback); a remote
   seat's token is issued over ssh; and a seat handle never carries a real hostname. These
   replace Task 0.6's "plain HTTP only inside a mesh tunnel".

Clarified with direction 3: **v2 is a separate system.** None of its wire, files, locks, CLI or
process model has to stay compatible with v1, so there is no compatibility contract and no parity
oracle against the Python. The owner's standing instruction "DO NOT GATE ON ME" stands: decisions
are defaulted and may be overturned (Decisions, below).

**Plan of record.** [DESIGN-v2.md](DESIGN-v2.md) is the design: the shape, the seam contract, the
names, the cost, the owner decisions and the trace of v1's requirements, over four chapters (A
protocol and rooms, B member client, C host side, D coexistence and migration).
[DESIGN.md](DESIGN.md), the earlier staged in-place replacement of v1, is superseded and stays as a
record; its requirements R1-R50 are traced in DESIGN-v2.md section 8. Plan 00213 keeps v1's
status-line work.

## Goals

- `ccy-teams` opens in about 0.1 s whatever the number of teams, with no sudo, no Python and no
  process per checkout, team or person (C 3.13, H-C7); it shows both systems in one board.
- No Python and no bash on any v2 path: Go in `src-go`, deployed by the plays.
- No root on any read and no sudoers file; every change to a team's accounts, roles, humans,
  security fields, backups and restores needs the owner's password (DC4); which session holds which
  seat is guarded by the owner's uid and the container mounts (DC19).
- v2 touches nothing v1 uses, proven by a static gate and a VM-lab footprint diff (D 4).
- Each team moves with an acceptance and a rollback, and nothing is lost silently (D 5, 6).
- Every v1 requirement and hard-won lesson carried where it still applies (DESIGN-v2.md section 8).
- v1 retired (directions 4 and 5): the plays move every desktop team to v2 in one step, then remove
  v1's Python and bash and `bus/pycompat`. The cut-over and removal tasks are still to be written.

## Non-Goals

- Compatibility with v1's wire, files, locks or CLI.
- Relay mode, until the hooks-daemon plugin API (#119) ships; it is then specified on v2.
- Importing v1 room history (OD-D4: an export, the v1 archive and a link).
- Plan 00197's seat broker, and reworking the board's git, tmux and podman probes.

## Decisions

Every decision is in [DESIGN-v2.md section 7](DESIGN-v2.md#7-the-owner-decisions), each
**defaulted; the owner may overturn it**, and the build follows the default until then. The
coordinator hears of each one flagged there (IRREVERSIBLE, CONTRADICTS or VISIBLE) before the
milestone that acts on it.

| ID           | Decision                                                                                                                                                | State                                                                                                                                                                                                              |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| D4           | no relay mode in v2; specified on v2 when the plugin API ships                                                                                          | accepted by the coordinator                                                                                                                                                                                        |
| D15          | a keeper (`teamchat run`) and a link (DESIGN.md called it the worker) in every member, the resident cost measured (B 2.14)                              | accepted by the coordinator                                                                                                                                                                                        |
| TID          | a team has a stable id (at most 22 characters) fixed at creation; its name is display only; `server_name` and handles carry the id and survive a rename | accepted with gate A (Task 0.5): built on; to the owner in the coordinator's digest                                                                                                                                |
| INT3         | a v1 name over 22 characters, or one of A 2.2's reserved ids, migrates under a short id declared with the move, and stays its display name              | defaulted; to the coordinator (not among the Task 0.5 answers)                                                                                                                                                     |
| VISIBLE      | OD-A10, BD9, BD15, BD16, DC4, DC16, DC17, DC18, DC19, DC21, OD-D6, INT4, INT5, INT7, and DC8                                                            | accepted (Task 0.5); DC17 with its go-deps allowlist rows and govulncheck clean; DC8 comes back to the coordinator if H-C5 or H-C15 fails; DC16's and DC19's stated cost is INT6's, below                          |
| IRREVERSIBLE | OD-A7, DC1, DC5, DC15, OD-D1 (`teamchat`), OD-D5, and TID above                                                                                         | confirmed by the owner 2026-10-30 (gate A met), on direction 6's condition: Task 0.6 checks none of them prevents a remote homeserver or members on other machines                                                 |
| INT6         | owner-uid processes holding a pool seat's password have power over that account (DC16, DC19)                                                            | the owner chose the default 2026-10-30 (gate B met): owner-uid processes may hold a pool seat's password; [DESIGN-v2.md 7.1](DESIGN-v2.md#71-int6s-narrower-alternative-a-pool-seat-with-no-password) is not built |
| n2f-D12      | the admin token goes to whatever holds the team's loopback port: only `team apply` checks the holder ([SPEC-n2f.md](SPEC-n2f.md) section 11)            | owner only, before the first install (gate A): accepted as residual in the inert code; the alternative is the admin client over a unix socket only the server and `teamchat-ctl` reach, which removes the race     |
| others       | OD-A1-A6, OD-A8, OD-A9, OD-A11, BD1-BD8, BD10-BD14, BD17, BD18, DC2, DC3, DC6, DC7, DC9-DC14, DC20, OD-D2-D4, OD-D7, INT1, INT2                         | defaulted, not flagged                                                                                                                                                                                             |

DESIGN.md's D1-D20 belonged to the in-place design and are superseded with it.

## Requirements carried from other plans

| From                                      | What                                                                                                                                                                       | Carried by                                                                                                               | Milestone |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | --------- |
| Plan 00201 Task 2.16                      | rename a team end to end                                                                                                                                                   | the stable team id (TID): a rename edits `name` and runs the play; nothing restarts, nothing is re-issued (C44, C 3.5.2) | N4        |
| Plan 00211                                | reports, questions, threads and asking a human, the side rooms and their drift check (its [SPEC.md](../00211-agent-bus-reports-and-questions-rooms/SPEC.md), as behaviour) | A 4.3, 6.3, 10; B6                                                                                                       | N1-N3     |
| Plan 00194 Task 2.0e                      | a scheduled restart posts no goodbye                                                                                                                                       | B28's goodbye: the N3 spec skips `left` when ccy's CMD exits 75 (RS2), turned on with Task 4.2                           | N3        |
| Plan 00194 Task 2.0g                      | an addressed reply that is only the quote fallback is ignored as `empty` and logged by no one                                                                              | A 7.5 (`empty`, the unheard count) and B18-B24: the N3 spec decides whether `empty` counts as unheard                    | N3        |
| Plan 00194 Tasks 2.1, 2.1b, 2.2           | the supervisor-plugin delivery path                                                                                                                                        | specified on v2 when #119 ships (D4)                                                                                     | later     |
| Plan 00194 SPEC-inject E1-E26, Plan 00203 | delivery into a session; hot control                                                                                                                                       | B18-B24, B31-B35                                                                                                         | N3        |
| Plan 00213                                | the status line's own TTL cache, the 25 s refresh, stderr shape, empty stays empty                                                                                         | B27, B 2.9                                                                                                               | N3        |

Tasks M0 folded in here from other plans, and what the clean break does to each:

- **00213 Task 3.1** (the v1 hooks in Go): not built. v1's hooks stay Python until v1 retires;
  v2's are `teamchat hook` (B 2.8). `feat/bus-v2-m2-hooks` is superseded.
- **00213 Task 3.2** (what stays in Python): answered by [RESEARCH-python-scope.md](RESEARCH-python-scope.md).
- **00201 Task 2.8** (the relay spool's `REPLY` line): superseded, relay mode is not carried (D4).

## Tasks

Each milestone's acceptance includes an adversarial review to MERGE, the `qa-reviewer` agent over
its diff, and the host checks it names, run through meta-deploy. A Go package merges to F44 with
its first caller.

### Phase 0: N0, the design

Tasks 0.1 to 0.4 and 1.1 to 1.4a, in full: [DONE-n0-n1.md](DONE-n0-n1.md).

- [x] ✅ **Task 0.1**: Research v1; DESIGN.md, reviewed and reworked once.

- [x] ✅ **Task 0.2**: The frame and the four area designs A-D, each reviewed adversarially.

- [x] ✅ **Task 0.3**: The integration into [DESIGN-v2.md](DESIGN-v2.md); DESIGN.md marked superseded.

- [x] ✅ **Task 0.4**: The N0 review, three rounds and a narrow check, verdict MERGE; its third
  minor is carried to N1 (Task 1.7).

- [x] ✅ **Task 0.5**: The coordinator's answers on the flagged decisions, 2026-10-21, recorded in
  the State column of [DESIGN-v2.md section 7](DESIGN-v2.md#7-the-owner-decisions) and in
  Decisions, above:

  - **Accepted:** OD-A10, BD9, BD15, BD16, DC4, DC16, DC17 (`godbus/dbus/v5` and
    `go.yaml.in/yaml/v3`, with go-deps allowlist rows; govulncheck must stay clean), DC18, DC19,
    DC21, OD-D6, INT4, INT5, INT7, and DC8 (if H-C5 or H-C15 fails, it comes back to the
    coordinator).
  - **Irreversible, built on as defaulted:** OD-A7, DC1, DC5, DC15, OD-D1 (`teamchat`), OD-D5 and
    TID. N1-N4 build on them; they are inert beside v1 and cheap to change until something is
    installed. **Gate A:** none of them goes past the point of no return without the owner's own
    word: no host install that writes a name or path an owner would see (the `teamchat` name in
    units or paths; Tasks 1.4, 2.5, 3.3, 4.1) and no migration of any team (Task 6.2; the coordinator's
    "N5 migration" is this plan's Phase 6, N6) until the owner has confirmed them. A merge to
    F44 reaches the host at the next play run or ccy rebuild, so the gate binds at the merge: Go
    packages and binaries may merge, but the play tasks, units and ccy image change that would
    install a `teamchat` name merge only after the owner confirms. The coordinator puts them to
    the owner as one digest.
  - **Owner only:** INT6 (owner-uid processes holding a pool seat's password have power over that
    account), built as defaulted. **Gate B:** the first deploy that gives any process a pool
    seat's password waits for the owner's confirmation. That is the first pool seat added on the
    host: Task 2.5's H-C13 to H-C15 (H-C15 without its held-ccy-session clause) if they run on
    the host, otherwise Task 4.4 (H-C8, that clause, and the pilot's ccy seat); Task 5.1's host
    `cc` members carry the gate too. The narrower alternative the
    owner may choose instead, a pool that keeps no password, is
    [DESIGN-v2.md section 7.1](DESIGN-v2.md#71-int6s-narrower-alternative-a-pool-seat-with-no-password).

- [ ] 🔄 **Task 0.6**: The owner's gate A and gate B answers (2026-10-30): A2 default (INT6
  as built), A3 confirmed, on direction 6's condition. Check the design against direction 6
  before N1 builds on it: a team's homeserver on this host, on another machine or in the cloud,
  reached over a private network (VPN or mesh), with members on several machines and networks.
  List each choice in DESIGN-v2.md (the IRREVERSIBLE ones first, then R3's loopback-plus-listen
  and no-TLS, the host-held pool bundle and seat store, `site`, the migration's remote members)
  that assumes the server and its members share one host, say whether it blocks direction 6 or
  only limits the first build, and change the design where it blocks.

  - [x] ✅ The check (`subagent-reports/261030-task-0.6-remote-homeserver.md`): **none of the
    irreversible choices blocks direction 6.** The team id, `server_name` (settable per team,
    never resolved through DNS with federation off), handles and roster carry no machine.
    `<site>` is an owner-chosen label. What assumes one host (R3's no-TLS loopback, firewalld
    `allow_from`, bundle issue by local sudo, the board's local status files, liveness from
    local seat locks) only limits the first build.
  - [ ] ⬜ Its four cheap design changes, into DESIGN-v2.md before N1. Team ids unique across
    every homeserver host the owner runs, not one `host_vars` file. Where a member's `site`
    comes from, one per machine. Plain HTTP only inside a mesh tunnel, with an optional TLS
    field reserved in bundle v1, so a cloud host needs no second bundle version. SSH named as
    the later channel for issuing a seat to a remote machine.

### Phase 1: N1, foundations

- [x] ✅ **Task 1.1**: `internal/teamchat/paths`, the one source of every name and path (D 3.3).
- [x] ✅ **Task 1.2**: The static gate `teamchat-keeps-off-v1` with its v1 name list (D 4.1), in
  `qa-changed.bash` and `qa-all.bash`.
- [x] ✅ **Task 1.3**: `teamchat/proto`: the wire, the roster type, the verdict and room-trust
  check, kinds, references, limits, the envelope, the shared corpus (A 2-13).
- [x] ✅ **Task 1.4**: A's host checks HC-A1 to HC-A7, judged in the container against the
  pinned Tuwunel 1.9.4; the Element checks are owner steps after gate A (Task 0.5).
- [x] ✅ **Task 1.4a**: The design-text fixes Task 1.4's checks called for.
- [x] ✅ **Task 1.5**: The matrix client and `fakehs` (`internal/teamchat/matrix`), reviewed to
  MERGE and run against the pinned Tuwunel 1.9.4 (M44); it merged with its first callers.
- [x] ✅ **Task 1.6**: GO-2026-6809: the Go toolchain bump; the Go gate passes on
  `spike/v2-merge-up` with the matrix client in.
- [ ] ⬜ **Task 1.7**: The two minors carried out of N0: the re-add mode the store must record (B 2.6
  step 01 (c); `261021-n0-r3-fix.md` minor 6), and the LXC tick's false notice for a launch-failed
  run once a later run has rewritten the lock file (C 3.11.2 step 6; `261021-n0-r3-check.md`
  minor 3).

### Phase 2: N2, the host side

- [ ] 🔄 **Task 2.1**: The N2 spec from area C; `teamchat-admin` with caller classes and the
  `teamchat-ctl` child (C 3.3); DC17's dependencies with their go-deps allowlist rows.
  Built as inert code (slice n2a; both DC17 dependencies landed: the YAML module with n2b,
  `godbus` with n2f): [BUILT-n2.md](BUILT-n2.md).

- [ ] 🔄 **Task 2.2**: The homeserver instance per team (C 3.2): the pinned Tuwunel, the hardened
  `teamchat-server@<team>` unit carrying D's `InaccessiblePaths=`, checked by D's gate (seam 49).
  Done, as inert code: each team's `config.toml` and drop-in, rendered by `host/render.go` and
  written by `team apply`. Not done: the pinned Tuwunel's install, the
  `teamchat-server@<team>` unit template and D's gate over it (seam 49), which are play tasks
  and wait for gate A (Task 2.5).

- [ ] 🔄 **Task 2.3**: Install and apply (C 3.5), the rooms engine from A's specs (C 3.6), the
  roster, members, humans and roles; the service-account localparts `conduit` (HC-A6) and
  `tuwunel` reserved (A 2.3) (seam 18).
  Slices n2a (the rooms engine), n2b (the approval gate, `team apply` step 10), n2d (the member
  operations, `role apply`, `team apply` steps 1-9 and 11, `team remove`) and n2f (the
  production side of every seam, the `health` and `element` verbs) are built as inert code; what
  each covers and leaves out: [BUILT-n2.md](BUILT-n2.md). Owner residual: n2f-D12 (Decisions, above).
  Not built: the play's install tasks and units (gate A), and the member verbs' `-tags tuwunel`
  round trip.

- [ ] 🔄 **Task 2.4**: Seats and bundles in the seat store (C 3.7), the declarations (C 3.8),
  Element profiles (C 3.10), health and the status records (C 3.11), backups and restore (C 3.12).
  Slices n2b (the declarations of C 3.8), n2c (the seat store), n2e (status records, the health
  tick, the disconnected notice, the seat sweep, Element profiles) and n2g (backups and the
  restore engine) are built as inert code, with real-server evidence under `-tags tuwunel`;
  what each covers and leaves out: [BUILT-n2.md](BUILT-n2.md). Not built: the host_vars writer
  (ccy-teams', Task 4.3), the units and timers (gate A), and `restore` in production, which stays
  refused (`restore.ErrUnproven`, n2g-D8, until H-C5 and H-C15).

  **Six N2 packages had no caller; slice n2f gives four of them one** (by import:
  `go list -deps ./cmd/teamchat-admin`). A Go package merges to F44 with its first caller
  (above), so the N2 stack stays on the plan branch, off F44, until the last two land.

  - `health`: called. `teamchat-admin health` (`admin/wiring.go`, `admin/wire`); its run by
    `teamchat-health.{service,timer}` (C 3.11) is play work under gate A (Task 2.5).
  - `health/view`: called, by the child's `team.view` op (`admin/serveview.go`).
  - `notice`: called, by the child's `member.disconnected` op (`admin/serveview.go`). ccy-teams'
    `seats sweep` is its second caller, still to come.
  - `element`: called. `teamchat-admin element check|apply` (`admin/wiring.go`); the optional
    Element play that runs it is gate A.
  - `declare/hostvars`: still waits, on ccy-teams, its only importer, when its flows read and
    write the v2 declarations in host_vars (HV4's writer; Task 4.3).
  - `sweep`: still waits, on ccy-teams' `seats sweep` (Task 4.3), run by the owner's
    `teamchat-seats.{service,timer}` (C 3.7.5; user units under gate A, Task 2.5).

- [ ] ⬜ **Task 2.5**: Acceptance: C 3.14's tests; the M44 real-Tuwunel test, and Tuwunel's answers
  for a locked and a deactivated account recorded into `fakehs` (host or VM lab); VM-lab scenario
  `teamchat-coexist` (P1) to its N2 stage, a host-only team cycle (D 4.3); the install on the host with the footprint before and after (P2); host
  checks H-C1 to H-C6 and H-C9 to H-C12 (H-C7 and H-C8 are Task 4.4's); H-C13 to H-C15 (DC16,
  DC8) in the VM lab, or on the host once gate B is cleared, except H-C15's clause that a held
  ccy session works again after ccy-teams' hot reissue, which needs N3 and N4 and is Task
  4.4's; restore stays refused (DC8) until that clause passes too.

  - **Gate A** (Task 0.5): the play tasks and units of Tasks 2.2 to 2.4 (and of any later task,
    such as 3.3) merge to F44, and the install on the host runs, only after the owner has
    confirmed OD-A7, DC1, DC5, DC15, OD-D1, OD-D5 and TID. The VM-lab parts of this task do not
    wait.
  - **Gate B** (Task 0.5): no pool seat is added on the host (H-C13 to H-C15, less H-C15's
    held-ccy-session clause, where they run on the host) until the owner has confirmed INT6 or
    chosen [DESIGN-v2.md section 7.1](DESIGN-v2.md#71-int6s-narrower-alternative-a-pool-seat-with-no-password)
    in its place. The VM lab is not a deploy and does not wait.

### Phase 3: N3, the member client, LXC members first

- [x] ✅ **Task 3.1**: The N3 spec from area B. It says that net/http may resend a dropped `/sync`
  (a GET retried on some connection errors), and that this is safe because a sync with a since
  token is idempotent within one database, and across a restore because no answer is persisted
  before B 2.6 step 08's epoch check (H-C14); and it carries 00194's 2.0e and 2.0g
  (Requirements carried).
  Done: [SPEC-n3.md](SPEC-n3.md), three Opus review rounds (`subagent-reports/261022-n3-spec-review*.md`,
  fix logs `-fixes-1.md`, `-fixes-2.md`); round 3's one major is applied as the reviewer wrote it
  (report `subagent-reports/261022-n3-spec.md`). The slices, in order (n3b and n3c may be built at
  once after n3a):

  - **n3a**: `cmd/teamchat`, the CLI, the member files' codecs, the locks, the kit's layout and
    the guest operations: an LXC guest's side;
  - **n3b**: `teamchat-admin lxc apply` and `lxc restart` (C 3.9), the session unit as code, the
    L3 probe and the LXC disconnected notice's child op: an LXC member placed by `lxc apply`;
  - **n3c**: `run`, the keeper, the link, the store, the sync engine (SY7 is the resent `/sync`
    and its restore tests on fakehs), receiving, the ends and hot control;
  - **n3d**: the sends, the outbox, TIMEOUTs and the forge check: a whole LXC exchange with
    `--plain`;
  - **n3e**: delivery into Claude Code, the hooks, the status line, the skill: an LXC Claude Code
    member end to end;
  - **n3f**: the side rooms of Plan 00211, from a SPEC-n3 addendum reviewed before it starts.

  2.0e: the switch (RS2) is CMD exit 75 alone and is turned on with its host half (Task 4.2);
  2.0g: `empty` counts as unheard (UH1). Gate A binds the play, kit and unit placement; N3's LXC
  seats are token seats, so gate B binds only the ccy hot reissue's host run (Task 4.4).

- [ ] 🔄 **Task 3.2**: The `teamchat` binary: CLI, member state, the keeper (`run`) and the link, locks, sync,
  delivery, hooks, status line, plugin and skill (B 2.1-2.12). Built, fakes only, none installed:
  n3a-n3d, n3e (hooks, e2e, PT1 seeds, kill rows); open: PT1 end/removal/repeat, n3f (n3f-a
  built: on `feat/bus-v2-n3f-a-int` G6 and G7, G4 and G5 (the records and the filter), and G1
  whole (the question index: fold and caps, the strict codec and load, the build record,
  `RedactedSlot`, the property tests; `feat/bus-v2-n3f-a-codec`), and G2 (the store's seven
  keys, `MaxStore` 4 MiB; `feat/bus-v2-n3f-a-store`) awaiting its review; n3f-b to n3f-e open,
  see BUILT-n3.md).

  - n3a (CLI, codecs, locks, kit layout, guest ops), n3c (the keeper, the link, receiving, the
    ends) and n3d (the sends, the outbox, TIMEOUTs, the forge check) are built, reviewed and
    merged into the branch; their coverage and reviews are in [BUILT-n3.md](BUILT-n3.md).

- [ ] 🔄 **Task 3.3**: LXC members: guest operations (B 2.13), LXC apply from the host (C 3.9),
  one `teamchat-session-<...>.service` per seat (n3b-D2: a member is (team, container, checkout,
  seat), any number per container); the container's own config stays with its own play (seam 52).
  **Gate A** (Task 0.5): its play tasks and units merge to F44 only after the owner's
  confirmation.

  - The guest side of B 2.13 (GU1-GU5, SA1) is built in n3a; the host side (C 3.9) is n3b's.
  - **n3b built** and merged as inert code: `teamchat-admin lxc apply` and `lxc restart`, the
    session unit as code, the L3 probe. No play task or unit is installed (gate A). Coverage,
    reviews, the install build's CL2 gate and the tagged analyzer stages: [BUILT-n3.md](BUILT-n3.md).

- [ ] ⬜ **Task 3.4**: Acceptance: B 2.15's tests, the resident-cost bars by
  `acceptance-member-cost.bash` (B 2.14), an LXC member on a real team in the VM lab;
  `teamchat-coexist` at its N3 stage, which adds the LXC member and its exchange (D 4.3).

- [ ] ⬜ **Task 3.5**: Delivery debouncing (issue #129): incoming items are held for a quiet
  window, 30 s by default with a ceiling, and delivered as one envelope. A halt ping and a
  human's direct message bypass the hold. Specified against delivery into a session (B18-B24)
  before it is built on the keeper's delivery path; plan only until Plan 00246's Task 8.8 is done.

### Phase 4: N4, ccy members and the board

- [ ] ⬜ **Task 4.1**: The v2 kit in the ccy image (D 3.5), with the CCY version bump. **Gate A**
  (Task 0.5): the image change carrying the kit merges to F44 only after the owner has confirmed
  the irreversible decisions, since any later CCY bump would deploy it.
- [ ] ⬜ **Task 4.2**: The launcher: the session directory mounts, refusing a checkout that contains
  the seat store (seam 53); removing the session record before the release, which runs `teamchat seat disconnected` (seam 54, 27); resolving each team's system, OD-D2's refusal (seam 55).
  For 00194's 2.0e (SPEC-n3 RS3), the launcher skips its `seat disconnected` for the run it
  relaunches and records it in `held/notice.json`, and the same change turns RS2's switch on.
- [ ] ⬜ **Task 4.3**: ccy-teams on both systems: the board from the status records (C 3.13, D 3.9),
  the flows (C45), creating a team and renaming it (00201 Task 2.16).
- [ ] ⬜ **Task 4.4**: Acceptance: H-C7 and H-C8, and H-C15's held-ccy-session clause (Task 2.5); the v2-native pilot `teamchat-pilot` with one ccy
  session and one LXC member, its cost measured (P3); `teamchat-coexist` run whole, with the ccy
  seat (D 4.3). **Gate B** (Task 0.5): the pilot's ccy seat and H-C8's are pool seats, so
  neither is added on the host until the owner has confirmed INT6 or chosen
  [DESIGN-v2.md section 7.1](DESIGN-v2.md#71-int6s-narrower-alternative-a-pool-seat-with-no-password).

### Phase 5: N5, other member kinds

- [ ] ⬜ **Task 5.1**: Host `cc` members on v2 (seam 56), refused until then (D 3.9). **Gate B**
  (Task 0.5): a host `cc` member's seat is a pool seat; none is added before the owner's
  confirmation.
- [ ] ⬜ **Task 5.2**: Docker and VM guests, after LXC (B 2.13), when a team needs one.

### Phase 6: N6, migration

- [ ] ⬜ **Task 6.1**: The N6 spec from area D; `internal/teamchat/migrate/v1`, the only package
  that names v1 (D 5.5), and D 4.1 part 2's analyzer over `migrate/v1` (GoProjectRules.md,
  red/green); `teamchat-admin migrate <step>` M0-M8, the fence, the carry plan, the
  commit and rollback (D 5); `ccy-teams migrate` and `migrate-declared.bash` (D 5.10, seam 55).
- [ ] ⬜ **Task 6.2**: Acceptance: VM-lab scenarios `teamchat-migrate` and
  `teamchat-migrate-rollback` (P1); the migration pilot (P4); the first real team, the owner's
  choice (P5); then teams one at a time at the owner's pace. **Gate A** (Task 0.5): no team is
  migrated, the pilot included, until the owner has confirmed OD-A7, DC1, DC5, DC15, OD-D1, OD-D5
  and TID; the VM-lab scenarios do not wait.
- [ ] ⬜ **Task 6.3**: When no team is v1-live, raise v1's retirement with the owner (its own
  plan). That plan also collapses the second owners v2's landing accepted (decision 5 on pull
  request 124, Plan 00246 Task 8.1): `lxccmd` beside the v1 owner of `lxc-ls`, `lxc-info` and
  `lxc-attach`, `teamchat/units` beside the v1 owner of `systemctl`, and the `dynamicNames` row
  for a run-time program inside a wrapper, so each program has one owner again.

### Phase 7: v1 while it runs

- [ ] 🔄 **Task 7.1**: Stage 1 for v1's ccy-teams, in-process reads (`SPEC-view.md`), on
  `feat/bus-v2-s1-declare`; its open time measured against F44 b18e91f5.
- [ ] ⬜ **Task 7.2**: The branches holding 35acceab (`feat/bus-v2-m1-proto*`, `m1-state*`,
  `m1-matrix`, `m2-hooks*`, `s1-declare`) are not pushed or merged until rebuilt onto e918261b, or
  dropped where v2 supersedes them. s1-declare: a new branch at e918261b, F44 b18e91f5 merged,
  then its own commits cherry-picked from ce87e241 and d342732b on, skipping the f460abb7 merge.
- [ ] ⬜ **Task 7.3**: Plan 00213's status-line golden pins the Python's exact stdout; it is
  loosened to what callers parse when the Go status line takes over from the Python one.
- [ ] ⬜ **Task 7.4** (follow-up from Plan 00211 Task 2.8): the board shows an offline member
  live: a ccy seat's stopped session and a stopped LXC container both read `offline`, from the
  live state, not the last apply's record. Not done in v1 because Task 7.1's acceptance compares
  `ccy-teams --list` bytes with the build before stage 1; v2's board follows the owner's rule.

### Phase 8: landing v2 on F44

Moved to [Plan 00246](../00246-agent-bus-v2-landing-and-cross-network/PLAN.md) on the owner's
decision of 2026-11-03, with its task numbers: the landing of `spike/v2-merge-up` (Tasks 8.1 to
8.7 and 8.9, pull request 124) and cross-network membership of direction 7 (Task 8.8).

## Success Criteria

- [ ] The N0 review is at MERGE, and each milestone's review and `qa-reviewer` pass.
- [ ] With v2 installed and no team, nothing is resident and v1's footprint is unchanged (P2, H-C11).
- [ ] A ccy-teams open meets C 3.13's bar on the host (H-C7).
- [ ] The member resident-cost bars hold (B 2.14), and the pilot's cost is recorded (P3).
- [ ] No sudoers file, no root on a read, and no Python started by v2 on the host or in a member.
- [ ] Every team that moves passes its acceptance (A1-A8, D 6.1), with no duplicate on a re-run.

## Delivery & Milestones

- The in-place design on `feat/agent-bus-v2-design`, revised on `feat/agent-bus-v2-design-2`; M0
  on `feat/bus-v2-m0-plan`.
- N0's area designs on `feat/bus-v2-n0-{a-protocol,b-member,c-host,d-migration}`, integrated on
  `feat/bus-v2-n0`.
