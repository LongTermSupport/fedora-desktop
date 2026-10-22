# n2g part B handoff (backup package done; the verbs and RT1 left)

Worktree `/workspace/untracked/worktrees/bus-v2-n2g-backup`, branch `feat/bus-v2-n2g-backup`, cut
from A1 (4917ee36d). Rules: `/workspace/untracked/scratch/n2g-part-common.txt` with part b; the
brief is "Part B" of `261022-n2g-handoff.md`. Report to write at the end: `261022-n2g-b.md`.

## Done (three commits, each qa-changed green)

- 521727896: `backup/tar.go` (BK1-BK3).
- e086d3c3e: `backup/meta.go` (Deps, the trust rule's tree, Tuwunel's numbers) and
  `backup/take.go` (Take, the last run, BT1-BT6, BK4).
- 13186533f: `backup/list.go` (States for HB1, List for BV3) and `backup/units.go` (BU1).

Coverage of `backup` is 93.9%; golangci clean.

## Exported API of `teamchat/backup`

- `Deps{Root; Units; Ready; Clock; CtlUID, StateOwnerUID uint32}` (SM1).
- `UnitState{ActiveState, InvocationID string; NRestarts int64; Result string}`;
  `Units{State(ctx, unit) (UnitState, error); Kill(ctx, unit, whom string, signal int32) error}`;
  `Ready{ServerVersion(ctx, port) error}`; `Clock{Now; Sleep(ctx, d) error}`.
- `Take(ctx, Deps, held *teamlock.Lock, keep []int64) (Result, error)`;
  `Result{Backup, TimeMS int64; Pruned []int64}`.
- `*Error{Team, Step, Err}` with `StepSources`, `StepReady`, `StepSignal`, `StepWait`, `StepWrite`,
  `StepVerify`, `StepPrune`; `ErrNotRunning` (verb exit 70) and `ErrLost` (verb exit 69, RS15).
- `Manifest`, `Sources`, `Contents`; `Encode(Manifest, Sources)`; `Decode(raw, team, n)`.
- `Run{Result string; AtMS, Backup int64; Error string}` with `Value()`;
  `LastRun(Deps, team) (Run, bool, error)`.
- `States(Deps) health.Backups`; `List(Deps, team) (Listing, error)`;
  `Listing{Backups []Entry; LastRun *Run}` with `Value(team) proto.Value` (BV3's exact document);
  `Entry{ID, TimeMS int64; Usable bool}`.
- `TimerUnit`, `ServiceUnit` (text constants).

## Decisions taken (record in the report)

1. Root's own files (team.json, config.toml, the tars, last-run.json, `backups/`) are checked as
   owned by the running euid (0 on a host), so no extra Deps field; as teamlock and seat do.
2. The sources are read first (step `sources`), before the ready check, so a missing source
   costs no signal; the port and server name come from `team.json` (equal to units.json's port
   at step 6, since both are written at step 11 of the same apply).
3. A changed invocation or a unit not active right before the signal is `ErrLost` (69), as is a
   unit lost by the 60 s deadline (the state is read again at the timeout); not running at the
   first read, or not answering, is `ErrNotRunning` (70).
4. A tar that fails its read-back is removed, so HB1's "both halves" never counts a bad tar.
5. BV3's `time_ms` is the meta's time, else the valid tar's manifest time, else null.
6. The token grammar is copied from ctl's unexported `isToken` (paths has no admin.token name;
   propose `paths.CtlAdminToken` and an exported ctl check at merge).
7. last-run.json's error is the error made one line (newlines to "; ", other control/format
   characters to "?", cut to 512 bytes), so it passes `status.OneLine`.

## Left

1. **The verbs** in `admin` (prefer a new file `admin/backupverbs.go` + `_test.go`; add only the
   `"backup"` and `"backups"` rows to `rootRuns()` in `verbs.go`):
   - `System` gains `Backup *backup.Deps` (nil: fail "backups are not wired yet", exit 1).
   - `backup --team T`: `teamlock.Take(ctx, sys.Host.LockDir, team, "backup", 10*time.Minute, sys.Host.Clock, sys.Host.Guard)`, `backup.Take(..., nil)`, then
     `sys.Host.Status.Refresh(ctx, team, false)` on success and failure (errors joined), release;
     print `{"v":1,"team":T,"backup":n,"time_ms":t,"pruned":[...]}`. Exit: `ErrNotRunning` 70,
     `ErrLost` 69, else `ExitCodeOf` (busy 75, RG2 70, else 1).
   - `backup --all`: `health.Stamps{Root: sys.Host.Root}.Stamped(ctx)`, each team as above,
     stderr `<team> backup <n>` or `<team> failed: <error>`, nothing on stdout, exit 1 if any
     failed (n2g-D5).
   - `backups --team T`: no lock, `backup.List`, print `Listing.Value(team).Compact()`.
   - BV4: `--all` with `--team`, neither, or anything else is 64. Run already gives 77 for a
     non-root class before any read (`admin/run.go`); add a test running `backup`, `backups`,
     `restore`, `restore --lost` as the ctl uid and another uid: 77, nothing read.
   - `admin/run_test.go`'s `TestEveryVerbEveryClass` expects the unwired message for built
     verbs; check it still holds.
2. **RT1** (`-tags tuwunel`, `FAKEHS_TUWUNEL_BIN`): a real server with `admin_signal_execute`,
   SIGUSR2 by `unix.Kill`, the wait reads the meta number, the tar pair validates. See
   `matrix/tuwunelhs` for how the repo starts a real Tuwunel.
3. Run golangci with and without `--build-tags tuwunel`, qa-changed, commit each step.
4. Write `261022-n2g-b.md`.

## Merge glue (for the coordinator and C7)

- `admin/wire/wire.go` (C's): `host.Deps.Backup` becomes a closure over `backup.Take(ctx, deps, held, nil)` (BT7, removes `ErrBackupNotBuilt`); health's `Backups` becomes
  `backup.States(deps)` and `health.NoBackups` is then deleted (HB1, HR4) with its test in
  `health/files_test.go`. Both were left untouched here because wire is C's; the lead was told.
- `backup.Units` needs an adapter over A2's `units.Manager` (`State` with InvocationID,
  NRestarts, Result; `Kill(unit, whom, signal)`).
- When A3 adds the `events` argument to `Status.Refresh`, the backup verb's call gains `nil`.
