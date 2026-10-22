# n2g part C handoff (restore engine), after C1 to C4

Worktree `/workspace/untracked/worktrees/bus-v2-n2g-restore`, branch `feat/bus-v2-n2g-restore`,
cut from A1 (4917ee36d). Rules: `/workspace/untracked/scratch/n2g-part-common.txt` (part c) and
`n1-common.txt`. Brief: "Part C" of `261022-n2g-handoff.md`. Contract: `SPEC-n2g.md` (sections
5-12, 7a; sections 7 and 7a are the engine, about lines 399-646). Final report: `261022-n2g-c.md`,
covering C1-C5. C6 (the verb, HB2, HB3) is another builder's, in
`feat/bus-v2-n2g-restore-verb`; C7 runs after the merge.

## Done (committed, qa-changed green each time, golangci clean with and without `-tags tuwunel`)

- **C1 (865909c29).** `record.go` (`Record`, `Step*`, `Lost`, `EncodeRecord`, `DecodeRecord`),
  `epoch.go` (`EpochFloor`, `Raised`, `EncodeEpochFloor`, `DecodeEpochFloor`, `MaxEpoch`),
  `merge.go` (`Lists`, `Merged`, `MergeOptions`, `Merge`, `MergeFromBackup`,
  `NoteElementPasswords`), `errors.go` (`ErrRefused`, `RefusedError`, `refusedf`), `reader.go`.
- **C2 (4f86945a2).** `argv.go`: `Staging`, `StagingArgv(s, restoring)`, `SecureArgv(team, self)`,
  `unitCommand(argv, stdin, stdout) execx.Cmd`; a `dynamicNames` row in `src-go/qa/rules/table.go`.
- **C3a (13c4de893).** `fs.go`: `Kind`, `Entry`, `FS`, `OS{Root, StateOwnerUID}` over openat2.
- **C3b (9ce7b977c).** `renames.go`: `Toward` (`Swapped`, `Unswapped`), `Renames(fsys, team, n, toward)` (RS16's table; any other state is a `*StuckError{Team, Listing}` such as
  `db=directory db.previous=absent db.restore-7=file`, nothing renamed), `Startable(fsys, team)`
  (RS17), `restoreName(team, n)`, `nameDB`, `namePrevious`. `FS` gained `RootKind(path)` (Lstat,
  a link is `KindOther`).
- **C4 (01989167f).** `deps.go`: `UnitState{Load, Active, InvocationID, NRestarts, Result}` with
  `NotLoaded()` and `Stopped()` (RS18), `Units{State, Stop}`, `Readiness`, `Status{Refresh(ctx, team, first, events)}`, `Clock{Now, Sleep}`, `Tar{TimeMS, ServerName, TeamFile, Config, Roster, Rooms, Token}`, `Deps` (SM4, every seam), `Request{Team, Backup, Offline, FromBackup}`.
  `validate.go`: `Validated{Team, TeamFile, Config, Tar, Merged}` and `Validate(ctx, d, req)`
  (RV1-RV7, RO1's 30 probes); helpers `readRoot(d, path, limit)`, `readTar`, `readFloor`,
  `metaName(team, n)`, `metaDir(team)`, `stamp(ms)` (UTC RFC 3339), constants `offlineProbes`,
  `probeEvery`, `smallFileMax`, `rosterFileMax`. `child.go`/`child_test.go` are part A4's
  (ead39222c on feat/bus-v2-n2g) byte for byte, so the merge is clean: `Child`, `SecureArgs`,
  `CheckArgs` (with `Live()`), `SwapArgs`, `ExportArgs`, `Export`, `Epoch`.
- **The test world** `world_test.go`: a temp tree under `OS`, a fake for every seam, and one
  ordered effect log `w.calls` with a kill counter (`w.kill`: `runtime.Goexit` at the k-th
  effect, before it acts), `w.fail[name]` and `w.hooks[name]`. Effect names: `write <file>` (the
  record as `write restore.json=<step>`), `remove <file>`, `rename <from> <to>`,
  `removetree <name>`, `fsync`, `syncfs`, `stop <unit>`, `run staging restoring=<bool>`,
  `child secure|swap|export|finish|revert`, `child check staged=<bool>`, `serve`, `bootstrap`,
  `backup`, `status`. Knobs: `unloadAfter` (State reads a stopped staging unit stays loaded),
  `crashLoop` (live unit alternates activating/failed until stopped, and the old database never
  starts), `stopFails`, `answers` (probes that fail before one answers; -1 never), `stagedEpoch`
  (override the staged check's answer), `bootRev`, `tarErr`, `tarServer`. Helpers `w.db()`
  ("live", "restored", ""), `w.record()`, `w.floor()`, `w.has`, `w.read`, `w.put`, `w.mark`,
  `w.callsSince`, `w.index`, `inGoroutine(fn) (returned bool)` in `renames_test.go`. World
  numbers: backup 7 at `backupMS`, M is 9, merged epoch 3 and rev 6, h2 needs a reissue.

Coverage of `restore` is 95.7%.

## The entry point (sent to the lead; C6 codes against it; keep it, or message "main" at once)

- `Run(ctx, d Deps, held *teamlock.Lock, req Request) (Answer, error)`: validates itself.
- `Resume(ctx, d, held, team) (Answer, error)`; `Discard(ctx, d, team) (Answer, error)`.
- `Answer.Encode() []byte` (RS9; a discard is `{"v":1,"team","backup","discarded":true,"notes"}`)
  and `Answer.Printed bool`: with an error, the verb prints the answer on stdout and the error
  (exit 1) — RS8's "restored; clean-up failed" and "status refresh failed".
- Exits: `ErrRefused` 70; teamlock busy 75; else 1. For RS15's 69, wrap `matrix.ErrUnreachable`
  (n2g-c-verb's instruction); define no `ErrServerLost`.
- **The C6 builder (n2g-c-verb, feat/bus-v2-n2g-restore-verb, tip c0e776694) already owns these
  names in package restore; do NOT define them:** `Records{Root, OwnerUID}` (HB2),
  `Recorded(root) teamlock.Guard` (refuses `--resume`/`--discard` with no record; this replaces
  my `NeedRecord`) and `ErrUnproven` (a `*RefusedError`, RW2), in `restore/records.go` and
  `restore/unproven.go`.
- **admin's side:** the verb calls the engine through `admin.Restorer`: `Run(ctx, held, admin.RestoreRequest{Team, Backup, Offline, RosterFromBackup}) ([]byte, error)`, and
  `Resume(ctx, held, team)` and `Discard(ctx, held, team)` with the same returns. Each returns
  the answer JSON (nil when there is none) beside its error, and the verb prints any answer
  before it maps the error. The verb takes the lock, so the engine never takes it. So
  `Discard` takes `held` too, and the wire adapter turns `Answer` into `Encode()` bytes (nil
  when there is no answer), which makes `Printed` unnecessary.

## C5, the engine: the design worked out (write `engine_test.go` first, then `engine.go`)

The record is written with `d.FS.WriteFile(paths.RestoreRecord(t), EncodeRecord(rec))`; every
step change is a write. No clean-up in a `defer`.

- **Run:** `Validate`; RS1 record at `staging` (Roster, Lists from the merge, Offline,
  FromBackup); `stage`; on error RS10; else syncfs, record `staged`; then `fromStaged`.
- **stage (RS2, RS3):** write `config.restore.toml` = config with its one `database_path = ` line
  set to `"` + `paths.HomeserverRestoreDatabase(t, n)` + `"` and its one `address = ` line set to
  `["127.0.0.1", "::1"]`; fail unless exactly one of each and one `database_backup_path = `.
  Then `Kind(team, restoreName)` must be absent (a refusal; on it do NOT remove the plant, only
  the config and the record, so RV2 names it next time). Run `StagingArgv(Staging{Team, N, Version(t), Port: decl.Port, Sandbox: Sandbox(t)}, true)` through `d.Run` with `unitCommand`.
  State(staging): active, keep InvocationID. `Child.Secure(ctx, t, SecureArgv(t, Self), SecureArgs{TeamFile, Port, Token: tar.Token, Rooms: tar.Rooms, Roster: rec.Roster, Retire, Revoke, RevokeHumans, Lock})`. State again: same InvocationID, NRestarts 0, active. Stop it,
  must be Stopped. Poll State on `Clock.Sleep(1s)` up to 10 s until NotLoaded, else fail. Run
  the argv without `--restore-backup`; State (new id); `Child.Check(ctx, t, secureArgv, CheckArgs{TeamFile, Port, Token})` must equal the merged `{RestoreEpoch, Rev}`; State same id,
  NRestarts 0; stop, Stopped. Then `Syncfs`.
- **stopUnit(unit):** State; NotLoaded is done; else Stop, State, must be `Stopped()`.
- **RS10:** stopUnit staging and secure; `RemoveTree(restoreName)`, `Remove(RestoreConfig)`,
  `Remove(RestoreRecord)`; "restore not done: <cause>" wrapping the cause (`%w`, so a refusal
  stays 70). A clean-up failure leaves the record at `staging` with both errors.
- **fromStaged:** RS4 unless Offline, FromBackup or Pre set: `Backup(ctx, held, []int64{N})`,
  rec.Pre, write. RS5 unless Offline: `Export(ExportArgs{TeamFile, N, tar.TimeMS})`, rec.Lost,
  write. Either failing: "restore not done, staged: <err>; restore --team T --resume or
  --discard" (record stays `staged`). Then swap.
- **swap (RS6):** 1 `readFloor`; if merged epoch above it, `WriteFile(RestoreEpochFile, EncodeEpochFloor(floor with Epoch raised))` (failure: "not done, staged"); 2 record `swap`,
  `WriteFile(RestoreBlock, nil)`; 3 stopUnit(live), then live, staging and secure all `Stopped`
  else fail before any rename; 4 `Renames(Swapped)`, `Fsync`; 5 `Child.Swap(SwapArgs{TeamFile, tar.Token, tar.Rooms, rec.Roster})`; 6 record `start`, `Remove(RestoreBlock)`; then start. A
  failure after step 2's record write is RS11 (revert, not from start).
- **start (RS7):** `Startable`; `Serve`; `Child.Check(ctx, t, nil, CheckArgs{TeamFile:...})`:
  epoch equal, rev at least merged; `Bootstrap(host.Bootstrap{Input: declare.Input{Team: decl}, TeamFile, RegKey: d.RegKey(config)})`; failure is RS11 from start (`served: true`); then clean.
- **clean (RS8):** 1 record `clean`; 2 if FromBackup: min_backup = highest decimal regular file
  in `ReadDir(team, metaDir(t))` + 1, written if above; 3 `RemoveTree(namePrevious)`; 4
  `Remove(RestoreConfig)`; 5 `Child.Finish(TeamFile)`; 6 `Remove(RestoreRecord)`; 7
  `Status.Refresh(ctx, t, false, []string{"restored backup N, <count> events lost, export at <paths.RestoreLostFile(t, N)>"})` or RO5's line offline. Failure 2-6: Printed answer +
  "restored; clean-up failed: <err>; restore --team T --resume"; 7: Printed + "restored; status
  refresh failed: <err>".
- **revert (RS11):** 1 record `revert` (+ `Served` when from start); 2 `WriteFile(block)`; 3
  stopUnit(live); 4 `Renames(Unswapped)` (a StuckError: rec.Stuck = listing, write, report with
  the listing, block stays), `Fsync`; 5 `Child.Revert`; 6 `Remove(block)`; 7 unless Offline:
  `Startable`, `Serve`; 8 record `staged` (Pre and Served kept). Errors: 2-6 and 7 "team <t> is
  down: restore --resume or --discard after fixing <error>"; offline success "team <t> is down:
  the restored database did not start: <cause>; restore --resume or --discard"; online success
  "restore not done, reverted: <cause>; restore --team T --resume or --discard".
- **Resume:** record must exist (decode); `readTar` (RV4's resume rule: the tar and the record
  only, no meta, no min_backup) and `readApplied`'s team.json and config; then by step: staging
  RS10 then "not done"; staged fromStaged; swap from RS6 step 2; start: `Remove(block)` then
  start; clean from RS8 step 2; revert from RS11 step 2, then "not done".
- **Discard (RS13):** at staging or staged: stopUnit staging and secure only, `RemoveTree (restoreName)`, `Remove(RestoreConfig)`, `Remove(RestoreRecord)`; notes: "the discarded copy
  was the only copy of backup N" when Pre set and meta/N gone; "the events posted while the
  restored copy served are discarded with it" when Served. At revert with Stuck: remove the
  record only, the block stays (note says so). Otherwise refused naming `--resume`.
- **Answer (RS9):** `{"v":1,"team","backup","offline","pre": M|null,"lost": {count, first_ms, end_ms, truncated, path} | {"exported": false, "since_ms": tar.TimeMS},"reissue","return", "password","unknown","pending": ...|null,"notes","changed"}`. Notes: the record's, plus online
  "events posted between the export's end (\<end_ms>) and the stop are lost and were not
  exported", offline RO5's "offline restore: every event after <stamp> is lost and was not
  exported; no pre-restore backup was taken; the database that would not start was removed".
  `changed` collects the child's and the bootstrap's lines of this run.

## C5 tests planned (each over the world)

1. The happy online run's whole `w.calls` sequence, exactly (RS3's verify, then `syncfs`, then
   `write restore.json=staged`; RS6's `write restore-epoch.json` before `write restore.json=swap`, `fsync` before `write restore.json=start`), the end state (db restored,
   no db.previous, db.restore-7, record, block, config.restore.toml; floor epoch 3), the answer
   and the status event; `w.sleeps` shows the unload wait.
2. SM5 sweep, online and offline (`crashLoop`, `answers: -1`, live unit `activating`): for every
   k up to the happy run's length, kill at k, assert "db absent implies block present" and "db
   restored and no block implies record at start or later", then Resume (and a fresh Run when
   the resume answers "not done" from staging) until the team is restored and clean.
3. Named crash points: the kill between `write restore.json=start` and `remove restore.block`
   (both left; Resume serves); `--roster-from-backup` killed at swap, start and clean, each
   resumed (min_backup 8 written; a later fresh Run of 7 refused); a kill after the epoch write
   (floor 3, record staged; Resume does not raise it again); the epoch file is 0600 and its
   directory holds no temporary.
4. Faults: a verify answering the backup's epoch fails step 05 (RS10 clean); a changed
   InvocationID after secure (hook on `child secure`); a staging unit that stays loaded past
   10 s; a db.restore-7 planted at `write config.restore.toml` refused with no `run`; RS4 or
   RS5 failing leaves `staged` and the team serving; a staging unit active at RS6 refused with
   no `rename`; `bootstrap` failing reverts (`served: true`, `stopFails` so RS18 sees `failed`),
   then Discard names the served events; offline revert leaves the team down at `staged`; a
   plant at `bootstrap` makes the revert's renames stuck (record `revert` with `stuck`, block
   kept), and Discard then removes only the record; `child finish` failing gives a Printed
   answer and a Resume completes; `status` failing gives a Printed answer; Discard refused at
   swap, start and clean; Discard's only-copy note.

## Decisions taken so far (put in the final report)

- The record carries `"from_backup"` and its `lists` hold every list and `pending`; `"stuck"`
  only beside `revert`.
- Merge: a retired handle's `needs` is cleared; a human only in the backup is left out of the
  merged roster and locked; RM12 as in C1's commit.
- `StagingArgv` takes the sandbox and the version as data; `OS.Remove` unlinks only;
  `StateOwnerUID` also owns root's own files for `ReadOwnedFile`.
- RS16's refusal is a `StuckError` (exit 1, not a refusal): only `--discard` repairs it.
- RV2 lists ctl's `*.previous` by `os.ReadDir` names only (never opened). RV7 starts only a
  loaded, `inactive` unit with `NRestarts` 0; with a start that fails, the online refusal quotes
  it. RO1 probes 30 times, one second apart, the first at once.
- `Deps` adds `RegKey func(config) (string, error)` (host's `regKeyOf`, to be exported by A),
  `Sandbox`, `Version` and `Self` to SM4's list.

## Merge glue (for the report)

wire's adapters: units.Manager to `restore.Units` (A2's state type to `UnitState`); hostnet to
`Readiness`; `host.Status` (A3's Refresh with events) to `Status`; `host.Serve`;
`host.BaseSandbox` to `Sandbox`; a host export of the units.json version (`Version`) and of
`regKeyOf` (`RegKey`); `backup.Take` behind `Deps.Backup` and `backup.Decode` (with the read of
`<n>.tar`) behind `Deps.Tar`; admin's `restore.Child` (A4); a `dynamicNames` row wherever admin
runs `SecureArgv`'s argv.
