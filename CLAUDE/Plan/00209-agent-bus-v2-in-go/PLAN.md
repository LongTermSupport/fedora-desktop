# Plan 00209: agent bus v2 in go

**Status**: In Progress (the plan of record for v2, `teamchat`, a separate system beside v1; N0's
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

## Non-Goals

- Compatibility with v1's wire, files, locks or CLI.
- Relay mode, until the hooks-daemon plugin API (#119) ships; it is then specified on v2.
- Importing v1 room history (OD-D4: an export, the v1 archive and a link).
- Retiring v1: its own plan, raised with the owner when no team is v1-live (D 7.2).
- Plan 00197's seat broker, and reworking the board's git, tmux and podman probes.

## Decisions

Every decision is in [DESIGN-v2.md section 7](DESIGN-v2.md#7-the-owner-decisions), each
**defaulted; the owner may overturn it**, and the build follows the default until then. The
coordinator hears of each one flagged there (IRREVERSIBLE, CONTRADICTS or VISIBLE) before the
milestone that acts on it.

| ID           | Decision                                                                                                                                                | State                                                                                                                                                                                                                                                          |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D4           | no relay mode in v2; specified on v2 when the plugin API ships                                                                                          | accepted by the coordinator                                                                                                                                                                                                                                    |
| D15          | a keeper (`teamchat run`) and a link (DESIGN.md called it the worker) in every member, the resident cost measured (B 2.14)                              | accepted by the coordinator                                                                                                                                                                                                                                    |
| TID          | a team has a stable id (at most 22 characters) fixed at creation; its name is display only; `server_name` and handles carry the id and survive a rename | accepted with gate A (Task 0.5): built on; to the owner in the coordinator's digest                                                                                                                                                                            |
| INT3         | a v1 name over 22 characters, or one of A 2.2's reserved ids, migrates under a short id declared with the move, and stays its display name              | defaulted; to the coordinator (not among the Task 0.5 answers)                                                                                                                                                                                                 |
| VISIBLE      | OD-A10, BD9, BD15, BD16, DC4, DC16, DC17, DC18, DC19, DC21, OD-D6, INT4, INT5, INT7, and DC8                                                            | accepted (Task 0.5); DC17 with its go-deps allowlist rows and govulncheck clean; DC8 comes back to the coordinator if H-C5 or H-C15 fails; DC16's and DC19's stated cost is INT6's, below                                                                      |
| IRREVERSIBLE | OD-A7, DC1, DC5, DC15, OD-D1 (`teamchat`), OD-D5, and TID above                                                                                         | accepted with gate A (Task 0.5): N1-N4 build on them; no host install that writes an owner-visible name or path, and no migration of any team, until the owner confirms                                                                                        |
| INT6         | owner-uid processes holding a pool seat's password have power over that account (DC16, DC19)                                                            | owner only, with gate B (Task 0.5): built as defaulted; the first deploy that gives any process a pool seat's password waits for the owner, who may choose [DESIGN-v2.md 7.1](DESIGN-v2.md#71-int6s-narrower-alternative-a-pool-seat-with-no-password) instead |
| others       | OD-A1-A6, OD-A8, OD-A9, OD-A11, BD1-BD8, BD10-BD14, BD17, BD18, DC2, DC3, DC6, DC7, DC9-DC14, DC20, OD-D2-D4, OD-D7, INT1, INT2                         | defaulted, not flagged                                                                                                                                                                                                                                         |

DESIGN.md's D1-D20 belonged to the in-place design and are superseded with it.

## Requirements carried from other plans

| From                                      | What                                                                                                                                                                       | Carried by                                                                                                               | Milestone |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | --------- |
| Plan 00201 Task 2.16                      | rename a team end to end                                                                                                                                                   | the stable team id (TID): a rename edits `name` and runs the play; nothing restarts, nothing is re-issued (C44, C 3.5.2) | N4        |
| Plan 00211                                | reports, questions, threads and asking a human, the side rooms and their drift check (its [SPEC.md](../00211-agent-bus-reports-and-questions-rooms/SPEC.md), as behaviour) | A 4.3, 6.3, 10; B6                                                                                                       | N1-N3     |
| Plan 00194 Task 2.0e                      | a scheduled restart posts no goodbye (waits on hooks-daemon #127)                                                                                                          | B28's goodbye: the N3 spec skips `left` for a restart the supervisor records, once #127 ships                            | N3        |
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

- [x] ✅ **Task 0.1**: Research v1 (`RESEARCH-*.md`); [DESIGN.md](DESIGN.md), reviewed by Fable and
  reworked once (`subagent-reports/261019-*`); M0's bookkeeping (`subagent-reports/261021-m0-*`).
- [x] ✅ **Task 0.2**: The frame ([DESIGN-v2-FRAME.md](DESIGN-v2-FRAME.md)) and the four area
  designs A-D, each reviewed adversarially (`subagent-reports/261021-n0-*`).
- [x] ✅ **Task 0.3**: The integration: [DESIGN-v2.md](DESIGN-v2.md), the chapters edited at their
  seams (section 3), 17 contradictions settled (section 5), the team id and INT3; DESIGN.md marked
  superseded (`subagent-reports/261021-n0-integration.md`).
- [x] ✅ **Task 0.4**: The N0 review: the four integrated adversarial reviews (1 blocker, 37 majors,
  FIX-FIRST), the four-theme fix pass (`subagent-reports/261021-n0-merge.md`), verification in two
  scopes (`261021-n0-verify-ab.md`, `261021-n0-verify-cd.md`; 10 new majors), round 2
  (`261021-n0-r2-*.md`), the re-check (`261021-n0-recheck.md`; 5 majors), the qa-reviewer pass
  (`261021-n0-qa-reviewer.md`), round 3 (`261021-n0-r3-fix.md`) and the narrow check, verdict MERGE
  (`261021-n0-r3-check.md`); its two launch-line minors are fixed and its third is carried to N1
  (Task 1.7).
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

### Phase 1: N1, foundations

- [x] ✅ **Task 1.1**: `internal/teamchat/paths`, the one source of every name and path (D 3.3).
  Built on `feat/bus-v2-n1-paths`, 100% coverage. Review round 3 was FIX-FIRST; its five findings
  are fixed in cb3736fa, and a narrow check of them is MERGE (`261021-n1-paths.md`,
  `261021-n1-paths-check.md`).
- [x] ✅ **Task 1.2**: The static gate `teamchat-keeps-off-v1` with its v1 name list (D 4.1), in
  `qa-changed.bash` and `qa-all.bash` before the first v2 package merges. The merge into
  `feat/bus-v2-n1` adds to `allowed.tsv` the three rows for the matrix replay test
  (`261021-n1-paths.md`), and that merge is gated on `qa-teamchat-separate` exit 0.
- [x] ✅ **Task 1.3**: `teamchat/proto`, test first: the wire, the roster type, the verdict and
  room-trust check, kinds, references, limits, the envelope, the shared corpus (A 2-13), and the
  room specs exported as data (seam 13). Built on `feat/bus-v2-n1-proto`, reviewed to MERGE in
  two rounds; its 57 decisions and their design-text fixes are in
  `subagent-reports/261021-n1-proto.md`.
- [x] ✅ **Task 1.4**: A's host checks HC-A1 to HC-A7 (host or VM lab), before `proto` fixes the
  rules they decide. The server checks (HC-A1, HC-A2's server half, HC-A4, HC-A6, HC-A7, H-C12
  to H-C14) are judged in the container against the pinned Tuwunel 1.9.4 binary (the asset's
  sha256 is the pin's) by the plan-folder probe `hca_probe.py`: HC-A1 and H-C13 fail on one fact
  each, with design-text fixes proposed (`261021-n1-hostchecks.md`, the run in
  `261021-n1-hostchecks-run.md`). The Element checks (HC-A2's display, HC-A3, HC-A5) are owner
  steps in A 14's Element row, after gate A. The lasting regression form of HC-A1 and H-C13 is
  the `tuwunel`-tagged Go test (SPEC-matrix M44, `internal/teamchat/matrix/tuwunel_test.go`),
  which runs in the container against the same binary; `hca_probe.py` stays the record of the
  run that decided the design. **Gate A** (Task 0.5): a check that would leave the
  `teamchat` name in a unit or path on the host runs in the VM lab until the owner has confirmed
  the irreversible decisions.
- [x] ✅ **Task 1.4a**: The design-text fixes Task 1.4's checks call for, for the design pass
  (`261021-n1-hostchecks.md` findings 1, 2 and 4): presence never uses `set_presence=offline`
  (A 6.6, A 14, A 15, A 16, B 2.6 step 09) and C 3.2.4 sets both presence timers and
  `server_user_localpart`; DC16's take chosen from the options given, with C 3.7.3 step 3 and C
  15's seats row changed to match; A 16 notes that HC-A2's display, HC-A3 and HC-A5 are owner
  steps after gate A, and that 7.5 and 10.4 in `proto` stay provisional until HC-A3 is run.
  Done: H-C13 makes a take log in, log out, then log in; only the link syncs, with presence
  timers of 120 s and a further 60 s; restore_epoch is checked after each sync answer, before
  anything is saved (`261021-n1-hc-design.md`, narrow check MERGE in
  `261021-n1-hc-design-check.md`).
- [ ] ⬜ **Task 1.5**: The matrix client and `fakehs`, built and reviewed on
  `feat/bus-v2-m1-matrix` (58c7b6e1): rebuilt off 35acceab (Phase 7), placed under
  `internal/teamchat/` (D 3.3), merged with its first caller (N2 or N3), not alone.
  Built and reviewed on `feat/bus-v2-n1-matrix` (9f42a773; review MERGE in round 3, `261021-n1-matrix.md`):
  the move, `LoginDevice`, `Logout`, `DeleteDevice`, room account data, `Redact` and presence, with fakehs
  following the Task 1.4 facts. M44, the tagged test, was run in the container against the
  pinned Tuwunel 1.9.4 binary (`-race`, with `-count=3`) and passes, HC-A1's presence case included,
  once its config carried v2's login limits and `db_pool_workers = 4` (`261021-n1-matrix.md`,
  section "The M44 run"; review in `261021-n1-m44-review.md`). Its only red check is GO-2026-6809 (Task 1.6). It stays on the
  N1 branch until its first caller (N2 or N3) is ready to merge with it.
- [ ] ⬜ **Task 1.6**: GO-2026-6809: the Go toolchain bump, with the coordinator, before the matrix
  client merges.
- [ ] ⬜ **Task 1.7**: The two minors carried out of N0: the re-add mode the store must record (B 2.6
  step 01 (c); `261021-n0-r3-fix.md` minor 6), and the LXC tick's false notice for a launch-failed
  run once a later run has rewritten the lock file (C 3.11.2 step 6; `261021-n0-r3-check.md`
  minor 3).

### Phase 2: N2, the host side

- [ ] ⬜ **Task 2.1**: The N2 spec from area C; `teamchat-admin` with caller classes and the
  `teamchat-ctl` child (C 3.3); DC17's dependencies with their go-deps allowlist rows.
- [ ] ⬜ **Task 2.2**: The homeserver instance per team (C 3.2): the pinned Tuwunel, the hardened
  `teamchat-server@<team>` unit carrying D's `InaccessiblePaths=`, checked by D's gate (seam 49).
- [ ] ⬜ **Task 2.3**: Install and apply (C 3.5), the rooms engine from A's specs (C 3.6), the
  roster, members, humans and roles; the service-account localparts `conduit` (HC-A6) and
  `tuwunel` reserved (A 2.3) (seam 18).
- [ ] ⬜ **Task 2.4**: Seats and bundles in the seat store (C 3.7), the declarations (C 3.8),
  Element profiles (C 3.10), health and the status records (C 3.11), backups and restore (C 3.12).
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

- [ ] ⬜ **Task 3.1**: The N3 spec from area B. It says that net/http may resend a dropped `/sync`
  (a GET retried on some connection errors), and that this is safe because a sync with a since
  token is idempotent within one database, and across a restore because no answer is persisted
  before B 2.6 step 08's epoch check (H-C14); and it carries 00194's 2.0e and 2.0g
  (Requirements carried).
- [ ] ⬜ **Task 3.2**: The `teamchat` binary: CLI, member state, the keeper (`run`) and the link, locks, sync,
  delivery, hooks, status line, plugin and skill (B 2.1-2.12).
- [ ] ⬜ **Task 3.3**: LXC members: guest operations (B 2.13), LXC apply from the host (C 3.9),
  `teamchat-session-<user>.service`; the container's own config stays with its own play (seam 52).
  **Gate A** (Task 0.5): its play tasks and units merge to F44 only after the owner's
  confirmation.
- [ ] ⬜ **Task 3.4**: Acceptance: B 2.15's tests, the resident-cost bars by
  `acceptance-member-cost.bash` (B 2.14), an LXC member on a real team in the VM lab;
  `teamchat-coexist` at its N3 stage, which adds the LXC member and its exchange (D 4.3).

### Phase 4: N4, ccy members and the board

- [ ] ⬜ **Task 4.1**: The v2 kit in the ccy image (D 3.5), with the CCY version bump. **Gate A**
  (Task 0.5): the image change carrying the kit merges to F44 only after the owner has confirmed
  the irreversible decisions, since any later CCY bump would deploy it.
- [ ] ⬜ **Task 4.2**: The launcher: the session directory mounts, refusing a checkout that contains
  the seat store (seam 53); removing the session record before the release, which runs `teamchat seat disconnected` (seam 54, 27); resolving each team's system, OD-D2's refusal (seam 55).
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
- [ ] ⬜ **Task 6.3**: When no team is v1-live, raise v1's retirement with the owner (its own plan).

### Phase 7: v1 while it runs

- [ ] 🔄 **Task 7.1**: Stage 1 for v1's ccy-teams, in-process reads (`SPEC-view.md`), on
  `feat/bus-v2-s1-declare`; its open time measured against F44 b18e91f5.
- [ ] ⬜ **Task 7.2**: The branches holding 35acceab (`feat/bus-v2-m1-proto*`, `m1-state*`,
  `m1-matrix`, `m2-hooks*`, `s1-declare`) are not pushed or merged until rebuilt onto e918261b, or
  dropped where v2 supersedes them. s1-declare: a new branch at e918261b, F44 b18e91f5 merged,
  then its own commits cherry-picked from ce87e241 and d342732b on, skipping the f460abb7 merge.
- [ ] ⬜ **Task 7.3**: Plan 00213's status-line golden pins the Python's exact stdout; it is
  loosened to what callers parse when the Go status line takes over from the Python one.

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
