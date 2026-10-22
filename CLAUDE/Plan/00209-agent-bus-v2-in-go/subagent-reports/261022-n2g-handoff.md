# n2g handoff: backups and restore (Plan 00209 Task 2.4, C 3.12)

Worktree `/workspace/untracked/worktrees/bus-v2-n2g`, branch `feat/bus-v2-n2g`, cut from
`feat/bus-v2-n2` at f7d832be. The builder rules are `/workspace/untracked/scratch/n1-common.txt`.
Report names: `261022-n2g.md`, `261022-n2g-review.md` (`-2`, `-3`).

## Done

- **Research.** C 3.12 in full, with 3.2.2, 3.2.3, 3.6.1, 3.11.3, 3.14's row, H-C4, H-C5,
  H-C15 and DC8, and the APIs the spec leans on.

- **Tuwunel spike.** The pinned 1.9.4 was run in the container. The digest is
  `untracked/scratch/n2g-tuwunel-spike.md` in this worktree (untracked), and its facts are in
  the spec's preamble.

- **`SPEC-n2g.md`**, reviewed twice:

  - round 1 `261022-n2g-spec-review.md` was FIX-FIRST, with 4 high findings, all fixed;
  - round 2 `261022-n2g-spec-review-2.md` (a fresh Opus, cold) was FIX-FIRST, with 5 high,
    6 medium and 9 low findings, all applied (its Disposition section).

  Both rounds' fixes are committed. Read the spec itself, not the reviews: the spec is the
  truth.

- **n2g-D14 is decided: the offline restore is built** (section 7a, RO1 to RO7). The owner
  confirms only its defaults (the D14 row).

## Open items

- **For the owner:** the n2g-D14 row's four defaults. They are the flag `--offline`, the 30 s
  probe, a pending operation resolved by RM12, and the database that would not start being
  removed.
- **For the owner:** RS13's `stuck` discard. When RS16 finds a state directory the engine did
  not make, the record is dropped and `restore.block` is kept, so the team stays down. No verb
  repairs that directory. It can only arise from tampering, and a verb for it is not built
  (YAGNI).
- **H-C5 must also confirm** that `TimeoutStartSec=15min` covers H-C1's largest database, and
  that the staging unit sends READY under `--restore-backup` (RU1).
- **n3a (fakehs `RestoreTo`, SPEC-n3 gap 06)** may model the raise as atomic. RE1 states why
  that holds, and RS3's verify and RT3 prove it. No change to SPEC-n3 is needed.

## The build, in three parts

**Order:** part A's step A1 is committed first, on this branch. Parts B and C are then each
cut from A1's commit in their own worktree (`bus-v2-n2g-backup` and `bus-v2-n2g-restore`).
Part A goes on with A2 to A5 here, in parallel with them. Each part builds TDD (test first, seen
red), at 90% per package, with `-race` and golangci clean, with and without
`--build-tags tuwunel`. Each step is committed when green. The coordinator merges A, then B,
then C. C's last step (C7, the wiring and RT) runs after the merge of all three.

**Package ownership**, to avoid conflicts:

- A owns `paths`, `teamlock`, `units`, `host`, `ctl`, and the `admin` child files (`child.go`,
  `serve*.go`, `execchild.go`).
- B owns `backup` and the `backup`/`backups` verbs in `admin/verbs.go`.
- C owns `restore` (except A's `guard.go` and `doc.go`), the `restore` verb, `health`'s HB2 and
  HB3, and `admin/wire`.
- `admin/run.go`'s verb table already lists all three verbs. Only the `runs` map rows are
  added: B adds `backup` and `backups`, C adds `restore`.

### Part A: foundations, units, host, the guard, and the child ops

- **A1 (first, small).** In `paths`, every name of the spec's `paths` line, registered in
  `separation_test.go`'s `sampleNames` and in the zero/derivation tests, as n3a's PA1 did. In
  `teamlock`, the `Guard` type `func(paths.TeamID) error`, `Unguarded`, and `Take(..., guard)`
  refusing nil (RG1). In `restore`, `doc.go`, `guard.go` (`Guard(root) teamlock.Guard`,
  `PendingError`, exit 70, RG2), and every RG3 caller updated:

  - `host.Apply` and `host.Remove` through the new `Deps.Guard`;
  - `admin`'s `memberVerb` (`verbs.go:156`);
  - `health`'s `TeamLocks` with `Unguarded`.

  Tests: a nil guard is refused; the guard runs after the flock; its error comes after the
  release; a link named `restore.json` counts.

- **A2. `units` (SD10).** `Kill(unit, whom, signal)` by `KillUnit`. The state read gains
  `InvocationID` (hex), `NRestarts` and `Result`, decoded strictly. `checkUnit` accepts
  `teamchat-restore@<t>.service` and `teamchat-restore-ctl@<t>.service`. The `host.UnitState`
  and `health.UnitState` adapters stay as they are, and a new `units.State` struct carries the
  new fields for `backup` and `restore`. Seam: the existing `Conn` fake.

- **A3. `host`.**

  - `Serve(ctx, d Deps, team) error` (SM3), refactored out of `applying` (`converge7`'s start
    path and `ready`), reading `units.json`. `applying` then calls it.
  - `BaseSandbox` is C 3.2.3's identity, hardening and deny lines as one `[]string`
    (n2g-D17). A test pins it against C's text, without the lifecycle lines.
  - `Status.Refresh` gains `events []string`, and every caller is updated.

- **A4. The child ops (RC1 to RC8, RC6, RC7)** in `admin` and `ctl`:

  - `restore.secure`, `restore.swap`, `restore.export`, `restore.finish`, `restore.revert`
    and `restore.check`, served by the child;
  - `Answer` gains `Export` and `Epoch`, each decoded strictly and allowed only with its op;
  - `checkStarted` (RC7);
  - `ctl` gains the `*.previous` link-and-write and the revert (RC2, RC5), fd-relative.

  `restore.secure`'s port and token, and `restore.check`'s, open an admin client on
  `127.0.0.1:<port>`. Tests run on fakehs: secure leaves the RT2 rows (on fakehs), export's cap
  keeps the newest, check reads a planted epoch, and a repeat of swap or revert is idempotent.
  The Go interface that `restore` declares for these ops (`restore.Child`) is written by C;
  A's `admin` type satisfies it structurally, and C's tests use a fake.

- **A5. RT.** Nothing in A. RT2's `restore.secure` rows run in C7 once all three parts are
  merged.

### Part B: the `backup` package and the two backup verbs

- **Packages:** `teamchat/backup` (new); `admin` (the `backup` and `backups` verbs); `host`'s
  `Deps.Backup` call site (BT7: n2f-D6's refusal goes); and `health` (HR4's `NoBackups` is
  removed, and `backup.States` satisfies `health.Backups`).
- **Exported:**
  - `Deps` (SM1);
  - `Take(ctx, Deps, held *teamlock.Lock, keep []int64) (Result, error)`;
  - `Result{Backup, TimeMS int64; Pruned []int64}`;
  - `Encode` and `Decode` (the one tar parser, BK3);
  - `Manifest`;
  - `States(Deps) health.Backups`;
  - `List` (BV3);
  - `LastRun` (BT5);
  - `TimerUnit` and `ServiceUnit` (BU1).
- **Expectations:** BK1 to BK4, BU1, BT1 to BT7, HB1, and BV1 to BV4 (with RS15's exit rule for
  BV1).
- **Seams:**
  - SM1's `Units` (a fake with `State` and `Kill`, recording calls);
  - `Ready` (a fake answering or failing);
  - `Clock` (a fake whose `Sleep` advances it);
  - the homeserver tree is a temp directory under the trust rule, with `StateOwnerUID` set to
    the test's uid;
  - `admin.Process` for BV4's 77.
- **Tests that pin the spike's hazards:** BT1's three no-`Kill` cases. RT1 (`-tags tuwunel`,
  `FAKEHS_TUWUNEL_BIN`) can run in B: it needs only a real server, `SIGUSR2` sent by
  `unix.Kill` in place of D-Bus, and the tar pair.
- **Depends on A** only for A1's `paths` and `teamlock`. B declares its own `Units` interface
  with A2's methods, so A2 is not waited for.

### Part C: the restore engine, the verb, health, and the wiring

- **Packages:** `teamchat/restore` (all but `guard.go` and `doc.go`); `admin` (the `restore`
  verb: `--team`, `--backup`, `--resume`, `--discard`, `--lost`, `--offline` and
  `--roster-from-backup`); `health` (HB2's `Restores` interface and the tick's `restore`
  field; HB3, no restart while a record exists); and `admin/wire` (RW1, RW2).
- **Exported:**
  - `Deps` (SM4);
  - `Request`;
  - `Run`, `Resume` and `Discard`;
  - `Merge(current, backup apply.RosterFile, ...) (Merged, error)` and `MergeFromBackup` (RM1
    to RM12);
  - `Validate` (RV1 to RV7);
  - `Record` with `EncodeRecord` and `DecodeRecord`;
  - `EpochFloor` (`restore-epoch.json`);
  - `StagingArgv(t, n, restoring bool)` and `SecureArgv` (RU1, RU2);
  - `Records(Deps) health.Restores` (HB2);
  - `Child`, the interface A's `admin` satisfies;
  - `RemoveTree` and the fd-relative `FS`;
  - `ErrUnproven` (RW2).
- **Steps:**
  - **C1.** The record codec, `EpochFloor`, and `Merge` with all of RM, as pure functions with
    table tests. `EpochFloor`'s epoch is written at RS6 step 1, and its `min_backup` at RS8
    step 2;
  - **C2.** The unit argvs RU1 and RU2, as pure functions whose properties are asserted
    (`Restart=no`, no other `Restart` line, the `BaseSandbox` lines, no secret in argv);
  - **C3.** `FS` over the trust rule: `openat2`, `RemoveTree`, `renameat2` and `Fsync`/`Syncfs`.
    RS16's table and RS17 are pure over `FS`. Tests: a planted link survives `RemoveTree`; every
    RS16 row and refusal;
  - **C4.** `Validate` (RV), RV7's start of an `inactive` unit included;
  - **C5.** The engine (RE1, RS1 to RS18, RO1 to RO7) over fakes: every fault, and SM5's
    `Goexit` kill at every call with a resume after it. Also RS18's `failed` after `Stop`, RO7's
    crash loop, and the call-order tests of RS3 (verify, then `Syncfs`, then `staged`) and RS6
    (`Fsync` before `start`). Also: the kill between `start` and the block's removal (RS12);
    a resume under `--roster-from-backup` at `swap`, `start` and `clean`, which RV4 does not
    refuse; the verify's wait for the staging unit to unload (RS3); and a revert from `start`
    then `--discard`, whose answer names the served events (RS11, RS13);
  - **C6.** The `restore` verb, with RS14, RS15 and the 64 cases, then health's HB2 and HB3;
  - **C7 (after A, B and C are merged).** `wire` (RW1, with `host.Deps.Backup` closing over
    `backup.Deps`; RW2's `ErrUnproven`, so the verb exits 70 before the lock); then RT2, RT3 and
    RT4 with `-tags tuwunel`, using
    `FAKEHS_TUWUNEL_BIN=/workspace/untracked/worktrees/bus-v2-n1-hostchecks/untracked/scratch/tuwunel-dl/tuwunel-1.9.4`.
    RT runs the staging server as a plain process on a loopback port, with no systemd.
- **Seams:** SM4 in full (each a fake). `restore.Child` is a fake answering scripted ops. The
  units fake gives scripted state sequences (`activating`, `failed`, a changed
  `InvocationID`).

## After the build

- **QA.** `./scripts/qa-changed.bash` fully green (go1.26.10 is in the container).
- **Code review.** An Opus adversarial loop over the code, at most 3 rounds, then a narrow
  check. Commit each review file.
- **PLAN.md and the report.** PLAN.md Task 2.4 records backups and restore as inert code
  (restore refused in production, DC8), in the same commit as the last code. Then write the
  report `261022-n2g.md`.

## Gotchas

- **Tuwunel's quirks.**
  - A `SIGUSR2` before Tuwunel's handler kills it (not PID 1) or is dropped (PID 1), so check
    readiness and the invocation first (BT1).
  - `--restore-backup` wipes a target directory that is not empty, so the path must be fresh
    (RV2, RS2) and the staging unit must never restart (RU1, RS3).
  - The restored server keeps serving, so it is stopped (RS3, RS18) and checked stopped before
    the renames (RS6).
- **`checkStarted`** requires `PATH` alone, `/` and no groups. systemd adds variables and the
  `initgroups` groups, hence `env -i` and RC7.
- **The bootstrap after a restore** cannot re-run `host.Apply`. `team.json` lacks the LXC and
  `Address` parts of the input, so a re-render would differ. Use `host.Serve` (SM3) and then
  the child's `team.apply` with `Input{Team: team.json}`, which only reads `in.Team`
  (`apply.Run`).
- **`rev`:** `planRoster` writes `NextRev(event, file)` and stores it in the file, so after
  RC1's write (merged `rev`, file equal) the bootstrap writes no second event unless the
  content changes. RS7's check is `>=` for that reason.
- **Health record fields.** `status.Record.Restore` exists and is kept "as written" by the tick
  (`health.base`). HB2 makes the tick fill it, with `pre` 0 for null.
- **Hooks and tooling.**
  - `sed` is blocked; use `awk 'NR>=a && NR<=b'` or Read.
  - Pipes to `head` or `tail` are blocked.
  - The TDD hook wants each test file written before its source file.
- **Import cycles.**
  - `health` must not import `restore` or `backup`; they satisfy health's interfaces
    structurally. `backup` may import `health`, for `BackupState`.
  - `restore` must not import `admin`. It declares `Child`, and `admin` implements it.
  - `restore` imports `backup` for `Take` (SM4) only through a function value in `Deps`, so
    part C does not wait for part B.
