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

| ID     | Decision                                                                                                                                                | State                                                                                                                                           |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| D4     | no relay mode in v2; specified on v2 when the plugin API ships                                                                                          | accepted by the coordinator                                                                                                                     |
| D15    | a keeper (`teamchat run`) and a link (DESIGN.md called it the worker) in every member, the resident cost measured (B 2.14)                              | accepted by the coordinator                                                                                                                     |
| TID    | a team has a stable id (at most 22 characters) fixed at creation; its name is display only; `server_name` and handles carry the id and survive a rename | defaulted, the coordinator agreed; it goes to the owner in the coordinator's next digest                                                        |
| INT3   | a v1 name over 22 characters migrates under a short id declared with the move, and stays its display name                                               | defaulted; to the coordinator                                                                                                                   |
| others | OD-A1-A11, BD1-BD18, DC1-DC21, OD-D1-D7, INT1, INT2, INT4-INT7                                                                                          | defaulted; the flagged ones (OD-A7, OD-A10, BD9, BD15, BD16, DC1, DC4, DC5, DC15-DC19, DC21, OD-D1, OD-D5, OD-D6, INT4-INT7) to the coordinator |

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
- [ ] ⬜ **Task 0.5**: The coordinator's answers on the flagged decisions (Decisions, above).

### Phase 1: N1, foundations

- [ ] ⬜ **Task 1.1**: `internal/teamchat/paths`, the one source of every name and path (D 3.3).
- [ ] ⬜ **Task 1.2**: The static gate `teamchat-keeps-off-v1` with its v1 name list (D 4.1), in
  `qa-changed.bash` and `qa-all.bash` before the first v2 package merges.
- [ ] ⬜ **Task 1.3**: `teamchat/proto`, test first: the wire, the roster type, the verdict and
  room-trust check, kinds, references, limits, the envelope, the shared corpus (A 2-13), and the
  room specs exported as data (seam 13).
- [x] ✅ **Task 1.4**: A's host checks HC-A1 to HC-A7 (host or VM lab), before `proto` fixes the
  rules they decide. The server checks (HC-A1, HC-A2's server half, HC-A4, HC-A6, HC-A7, H-C12
  to H-C14) are judged in the container against the pinned Tuwunel 1.9.4 binary (the asset's
  sha256 is the pin's) by the plan-folder probe `hca_probe.py`: HC-A1 and H-C13 fail on one fact
  each, with design-text fixes proposed (`261021-n1-hostchecks.md`, the run in
  `261021-n1-hostchecks-run.md`). The Element checks (HC-A2's display, HC-A3, HC-A5) are owner
  steps in A 14's Element row, after gate A. The regression form is the `tuwunel`-tagged Go
  test once the matrix client merges.
- [ ] ⬜ **Task 1.5**: The matrix client and `fakehs`, built and reviewed on
  `feat/bus-v2-m1-matrix` (58c7b6e1): rebuilt off 35acceab (Phase 7), placed under
  `internal/teamchat/` (D 3.3), merged with its first caller (N2 or N3), not alone.
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
  roster, members, humans and roles; the service-account localpart reserved by HC-A6 (seam 18).
- [ ] ⬜ **Task 2.4**: Seats and bundles in the seat store (C 3.7), the declarations (C 3.8),
  Element profiles (C 3.10), health and the status records (C 3.11), backups and restore (C 3.12).
- [ ] ⬜ **Task 2.5**: Acceptance: C 3.14's tests; the M44 real-Tuwunel test, and Tuwunel's answers
  for a locked and a deactivated account recorded into `fakehs` (host or VM lab); VM-lab scenario
  `teamchat-coexist` (P1) to its N2 stage, a host-only team cycle (D 4.3); the install on the host with the footprint before and after (P2); host
  checks H-C1 to H-C12.

### Phase 3: N3, the member client, LXC members first

- [ ] ⬜ **Task 3.1**: The N3 spec from area B. It says that net/http may resend a dropped `/sync`
  (a GET retried on some connection errors), and that this is safe because a sync with a since
  token is idempotent; and it carries 00194's 2.0e and 2.0g (Requirements carried).
- [ ] ⬜ **Task 3.2**: The `teamchat` binary: CLI, member state, the keeper (`run`) and the link, locks, sync,
  delivery, hooks, status line, plugin and skill (B 2.1-2.12).
- [ ] ⬜ **Task 3.3**: LXC members: guest operations (B 2.13), LXC apply from the host (C 3.9),
  `teamchat-session-<user>.service`; the container's own config stays with its own play (seam 52).
- [ ] ⬜ **Task 3.4**: Acceptance: B 2.15's tests, the resident-cost bars by
  `acceptance-member-cost.bash` (B 2.14), an LXC member on a real team in the VM lab;
  `teamchat-coexist` at its N3 stage, which adds the LXC member and its exchange (D 4.3).

### Phase 4: N4, ccy members and the board

- [ ] ⬜ **Task 4.1**: The v2 kit in the ccy image (D 3.5), with the CCY version bump.
- [ ] ⬜ **Task 4.2**: The launcher: the session directory mounts, refusing a checkout that contains
  the seat store (seam 53); removing the session record before the release, which runs `teamchat seat disconnected` (seam 54, 27); resolving each team's system, OD-D2's refusal (seam 55).
- [ ] ⬜ **Task 4.3**: ccy-teams on both systems: the board from the status records (C 3.13, D 3.9),
  the flows (C45), creating a team and renaming it (00201 Task 2.16).
- [ ] ⬜ **Task 4.4**: Acceptance: H-C7 and H-C8; the v2-native pilot `teamchat-pilot` with one ccy
  session and one LXC member, its cost measured (P3); `teamchat-coexist` run whole, with the ccy
  seat (D 4.3).

### Phase 5: N5, other member kinds

- [ ] ⬜ **Task 5.1**: Host `cc` members on v2 (seam 56), refused until then (D 3.9).
- [ ] ⬜ **Task 5.2**: Docker and VM guests, after LXC (B 2.13), when a team needs one.

### Phase 6: N6, migration

- [ ] ⬜ **Task 6.1**: The N6 spec from area D; `internal/teamchat/migrate/v1`, the only package
  that names v1 (D 5.5); `teamchat-admin migrate <step>` M0-M8, the fence, the carry plan, the
  commit and rollback (D 5); `ccy-teams migrate` and `migrate-declared.bash` (D 5.10, seam 55).
- [ ] ⬜ **Task 6.2**: Acceptance: VM-lab scenarios `teamchat-migrate` and
  `teamchat-migrate-rollback` (P1); the migration pilot (P4); the first real team, the owner's
  choice (P5); then teams one at a time at the owner's pace.
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
