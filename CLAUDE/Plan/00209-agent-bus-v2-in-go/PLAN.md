# Plan 00209: agent bus v2 in go

**Status**: In Progress (landing on F44 is Plan 00246; the plan of record for v2, `teamchat`, a separate system beside v1; N0's
design is integrated in [DESIGN-v2.md](DESIGN-v2.md), reviewed and fixed in three rounds, with its last check open; a dedicated build
session does the work to [BUILD-BRIEF.md](BUILD-BRIEF.md); what N0 and N1 built: [DONE-n0-n1.md](DONE-n0-n1.md))
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

In full, quoted: [OWNER-DIRECTIONS.md](OWNER-DIRECTIONS.md). In short:

1. The Python is a prototype, not an archetype: fix things, make it excellent.
2. No compatibility with v1 is owed; now is the time for breaking changes.
3. Running teams keep working; v2 can be a different thing.
4. Lock into Go (2026-10-30): finishing v2 comes before v1 hardening; `bus/pycompat` goes with v1.
5. Break the ccy teams stuff (2026-10-30): the plays move every team in one step, then v1 retires.
6. One homeserver per team, members on several machines and networks (2026-10-30, Task 0.6).
7. v1 is feature-frozen; cross-network membership is v2 only, by declared CIDRs, TLS with the
   team CA, a remote seat's token over ssh, no overlay (2026-11-02).

**v2 is a separate system**: no compatibility contract and no parity oracle against the Python.
"DO NOT GATE ON ME" stands: decisions are defaulted and may be overturned.

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
  v1's Python and bash and `bus/pycompat`. The cut-over and removal are Tasks 6.4 and 6.5.

## Non-Goals

- Compatibility with v1's wire, files, locks or CLI.
- Relay mode, until the hooks-daemon plugin API (#119) ships; it is then specified on v2.
- Importing v1 room history (OD-D4: an export, the v1 archive and a link).
- Plan 00197's seat broker, and reworking the board's git, tmux and podman probes.

## Decisions

Every decision is in [DESIGN-v2.md section 7](DESIGN-v2.md#7-the-owner-decisions), each
**defaulted; the owner may overturn it**. The table of IDs and states, and the requirements
carried from Plans 00194, 00201, 00203, 00211 and 00213: [DECISIONS.md](DECISIONS.md). Gate A
(the irreversible choices) was met 2026-10-30 on direction 6's condition; gate B (INT6) was met
the same day with the default. Owner residual: n2f-D12.

## Tasks

Each milestone's acceptance includes an adversarial review to MERGE, the `qa-reviewer` agent over
its diff, and the host checks it names, run through meta-deploy. A Go package merges to F44 with
its first caller. Long task bodies, unchanged: [TASKS-in-full.md](TASKS-in-full.md).

### Phase 0: N0, the design

Tasks 0.1 to 0.6 and 1.1 to 1.4a, in full: [DONE-n0-n1.md](DONE-n0-n1.md).

- [x] ✅ **Task 0.1**: Research v1; DESIGN.md, reviewed and reworked once.

- [x] ✅ **Task 0.2**: The frame and the four area designs A-D, each reviewed adversarially.

- [x] ✅ **Task 0.3**: The integration into [DESIGN-v2.md](DESIGN-v2.md); DESIGN.md marked superseded.

- [x] ✅ **Task 0.4**: The N0 review, three rounds and a narrow check, verdict MERGE; its third
  minor is carried to N1 (Task 1.7).

- [x] ✅ **Task 0.5**: The coordinator's answers on the flagged decisions (2026-10-21), in the
  State column of [DESIGN-v2.md section 7](DESIGN-v2.md#7-the-owner-decisions) and
  [DECISIONS.md](DECISIONS.md): the accepted ones, the irreversible ones built on behind
  **gate A**, and INT6 behind **gate B**; in full: [DONE-n0-n1.md](DONE-n0-n1.md).

- [ ] 🔄 **Task 0.6**: The owner's gate A and gate B answers (2026-10-30): A2 default, A3
  confirmed, on direction 6's condition; the design checked against direction 6 (a remote
  homeserver, members on several machines). In full: [DONE-n0-n1.md](DONE-n0-n1.md).

  - [x] ✅ The check (`subagent-reports/261030-task-0.6-remote-homeserver.md`): **none of the
    irreversible choices blocks direction 6**; what assumes one host only limits the first build.
  - [ ] ⬜ Its four cheap design changes, into DESIGN-v2.md before N1: team ids unique across
    homeserver hosts; a member's `site`, one per machine; an optional TLS field reserved in
    bundle v1; SSH as the later channel for issuing a seat to a remote machine.

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

What each slice built and left out: [BUILT-n2.md](BUILT-n2.md).

- [ ] 🔄 **Task 2.1**: The N2 spec from area C; `teamchat-admin` with caller classes and the
  `teamchat-ctl` child (C 3.3); DC17's dependencies with their go-deps allowlist rows.
  Built as inert code (slice n2a; both DC17 dependencies landed: the YAML module with n2b,
  `godbus` with n2f).

- [ ] 🔄 **Task 2.2**: The homeserver instance per team (C 3.2), the hardened
  `teamchat-server@<team>` unit (seam 49). Built as inert code; open: the Tuwunel install and
  the unit template (Task 2.5), the health and seats unit texts (S7b).
  [In full](TASKS-in-full.md#task-22).

- [ ] 🔄 **Task 2.3**: Install and apply (C 3.5), the rooms engine (C 3.6), the roster, members,
  humans and roles (seam 18). Slices n2a, n2b, n2d and n2f built as inert code; open: the play's
  install tasks and units, and the member verbs' `-tags tuwunel` round trip.
  [In full](TASKS-in-full.md#task-23).

- [ ] 🔄 **Task 2.4**: Seats and bundles (C 3.7), declarations (C 3.8), Element profiles (C 3.10),
  health and status records (C 3.11), backups and restore (C 3.12). Slices n2b, n2c, n2e and n2g
  built as inert code; `restore` stays refused (n2g-D8). The sealed carry
  ([SPEC-carry.md](SPEC-carry.md), issue #324) is complete in code on `sealed-carry`; open: the
  play's run per host, the `-tags tuwunel` carry test, the drill with SH1, the Q5 lab scenario.
  S5 (issue #296) built in part; `human lock` and `unlock` wait on n2h-D12 and n2h-D20.
  [In full](TASKS-in-full.md#task-24).

- [ ] ⬜ **Task 2.5**: Acceptance: C 3.14's tests, the M44 real-Tuwunel test, VM-lab
  `teamchat-coexist` (P1) to its N2 stage, the install with its footprint (P2), host checks
  H-C1 to H-C6, H-C9 to H-C15 (less H-C15's held-ccy-session clause, Task 4.4's); restore stays
  refused (DC8) until that clause passes. Gates A and B: [in full](TASKS-in-full.md#task-25).

### Phase 3: N3, the member client, LXC members first

What each slice built and its reviews: [BUILT-n3.md](BUILT-n3.md).

- [x] ✅ **Task 3.1**: The N3 spec from area B, [SPEC-n3.md](SPEC-n3.md), three Opus review
  rounds; the slices n3a to n3f, and 2.0e and 2.0g carried. [In full](TASKS-in-full.md#task-31).

- [ ] 🔄 **Task 3.2**: The `teamchat` binary (B 2.1-2.12). Built, fakes only, none installed:
  n3a-n3d; n3e in part; n3f-a to n3f-c2a (pull request 178). Open: n3e's rest (the OF1 row,
  PT1's unreached points, seed 57 in issue 156), n3f-c2a's kill-and-resume and QS7 end-to-end
  tests, n3f-c2b, n3f-c2c, n3f-d and n3f-e; the owner's `sizeAwaiting` question.
  [In full](TASKS-in-full.md#task-32).

- [ ] 🔄 **Task 3.3**: LXC members (B 2.13, C 3.9), one `teamchat-session-<...>.service` per seat
  (n3b-D2). Guest side built (n3a), host side built (n3b), the play side and its review fixes
  built (`installplay/lxc_test.go`); not yet run on a host or in the VM lab.
  [In full](TASKS-in-full.md#task-33).

- [ ] ⬜ **Task 3.4**: Acceptance: B 2.15's tests, the resident-cost bars by
  `acceptance-member-cost.bash` (B 2.14), an LXC member on a real team in the VM lab;
  `teamchat-coexist` at its N3 stage, which adds the LXC member and its exchange (D 4.3).

- [x] ✅ **Task 3.5**: Delivery debouncing (issue #129). Superseded by Plan 00267 Task 7.1, which
  built it as the coalesce window (`src-go/internal/teamchat/member/engine/coalesce.go`), with
  Plan 00267 Task 7.5 the human message debounce. The original text:
  [in full](TASKS-in-full.md#task-35).

### Phase 4: N4, ccy members and the board

- [x] ✅ **Task 4.1**: The v2 kit in the ccy image (D 3.5), with the CCY version bump (3.114.0,
  container 2.81.0; `tasks/teamchat-ccy-kit.yml`). Gate A met (Plan 00246 INSTALL-PATH.md).
- [x] ✅ **Task 4.2**: The launcher (seams 53, 54, 27, 55): `ccy-start-claude` in Go, the
  `ccy-teams internal` verbs, RS3 with RS2's switch on (CCY 3.116.0), `ccy-teams seats sweep`
  and its user units. Decisions L1-L6: [LAUNCH-ccy.md](LAUNCH-ccy.md).
  [In full](TASKS-in-full.md#task-42).
- [ ] ⬜ **Task 4.3**: ccy-teams on both systems: the board from the status records (C 3.13, D 3.9),
  the flows (C45), creating and renaming a team. Built so far: `choose-teams` for ccy and `cc`
  launches. [In full](TASKS-in-full.md#task-43).
- [ ] ⬜ **Task 4.4**: Acceptance: H-C7, H-C8, H-C15's held-ccy-session clause, the pilot
  `teamchat-pilot` (P3), `teamchat-coexist` run whole (D 4.3); gate B is met.
  [In full](TASKS-in-full.md#task-44).
- [ ] ⬜ **Task 4.5** (owner, 2026-11-17: "BUILD IT"): one ccy session holding a v1 seat and a v2
  seat at once. Design first (DESIGN-dual-bus.md), then (a) launch, (b) start, (c) exit and
  restart, (d) restore, test first; (a)'s no-`agent-bus` half is built (CCY 3.122.22).
  [In full](TASKS-in-full.md#task-45).
- [x] ✅ **Task 4.6**: Seat duties (coordinator, worker, host), approved by the owner
  (PROPOSAL-seat-kinds.md, section 7): the approval-gated `duties` field, `teamchat whoami --json`
  and duty on the status rows. Built test first on `v2-seat-duty`, merged into v2 at fe67f23c3;
  [build report](subagent-reports/seat-duty-build-report.md). A duty does not change a host
  `cc --teams` session's deny rules (Task 5.1 step 10).
- [ ] 🔄 **Task 4.7**: The coordinator-retrospective backlog,
  [BACKLOG-coordinator-retro.md](BACKLOG-coordinator-retro.md) (R0-R12, v2 only). R0, the
  questions verbs, is built and merged with its step 7 review fixed; the owner's Lightning check
  HC-Q1 is not yet run. [In full](TASKS-in-full.md#task-47).

### Phase 5: N5, other member kinds

- [ ] 🔄 **Task 5.1**: Host `cc` members on v2 (seam 56), refused until then (D 3.9). **Gate B**
  (Task 0.5) is met: a host `cc` member's seat is a pool seat, and the owner chose INT6 on
  2026-10-30. Design and build order: [DESIGN-host-cc-v2.md](DESIGN-host-cc-v2.md) section 6;
  built, its acceptance (section 8) is the host's, queued in `meta-deploy.list`.
  - [x] ✅ Step 1: the conversation-record class (`project_conversation_record`, `conversation.Locate`).
  - [x] ✅ Step 2: the host kit holds its plugin and settings (`teamchat kit assemble`).
  - [x] ✅ Step 3: `teamchat run --extra-settings FILE` (H4).
  - [x] ✅ Step 4: `cc-claude -- claude <args>` (H5).
  - [x] ✅ Step 5: `ccy-teams internal cc-run`, both systems.
  - [x] ✅ Step 6: the chooser offers teamchat teams to a host launch; the consent names pool handles.
  - [x] ✅ Step 7: `teamchat reach` inside a `--state` session (`<run>/state-root`).
  - [x] ✅ Step 8: docs and plan text.
  - [x] ✅ Step 9: version bumps (CCY 3.124.1, container 2.84.51 on its branch; the v2 batch is
    released as CCY 3.124.7, container 2.84.58, after the v2 review), meta-deploy entries,
    `acceptance-host-cc-v2.bash`, [build report](subagent-reports/task-5.1-build-report.md).
  - [x] ✅ Step 10: cc's deny file keeps a host seat out of tests, builds, lint and QA (Plan
    00263 D3, static half; DESIGN-host-cc-v2.md step 10).
  - [ ] ⬜ Acceptance on the host (design section 8), attended.
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
- [ ] ⬜ **Task 6.4** (owner, 2026-11-17: "close the door on v1"): the cut-over without
  migration, a team at a time, to a fresh v2 team (no history carried, OD-D4), in six checked
  steps; no v1 team is retired before its replacement passes step 4 (the pilot round trip).
  [In full](TASKS-in-full.md#task-64).
- [ ] ⬜ **Task 6.5**: removing v1 (D 7.2 steps 4 to 7) once every `agent_bus_teams` entry is
  absent, as plays that refuse to run while a v1 team is present; needs issue #299 fixed first.
  [In full](TASKS-in-full.md#task-65).

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
- [ ] ⬜ **Task 7.5** (follow-up from the landing review, not blocking v2): Defence Before Fix
  rules for the three classes fixed at landing without one (the floor ids and issue #380, the
  host kit judged by presence, a teamsync test reading live state on a batch alone), or a stated
  reason each cannot be expressed. [In full](TASKS-in-full.md#task-75).

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
