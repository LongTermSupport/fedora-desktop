# n2g integration handoff (Plan 00209 N2 slice n2g)

Worktree `/workspace/untracked/worktrees/bus-v2-n2g`, branch `feat/bus-v2-n2g`. The integrator
stopped at its context budget. This file covers what is done, what is left, and the design
worked out for the RT tests.

## Done (committed)

| Commit      | What                                                                                                                                                                                                                                                                                                                                                                |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (merge 1)   | `feat/bus-v2-n2` merged `--no-ff`; clean                                                                                                                                                                                                                                                                                                                            |
| (merge 2)   | `feat/bus-v2-n2g-backup` (part B) merged; clean                                                                                                                                                                                                                                                                                                                     |
| `62e93ddf3` | `feat/bus-v2-n2g-restore` (C1-C5) merged; `qa/rules/table.go` keeps both dynamicNames rows (`admin UnitChild.Ask`, `restore unitCommand`)                                                                                                                                                                                                                           |
| `c22224a77` | `feat/bus-v2-n2g-restore-verb` (C6) merged. Conflicts in `exit.go` and `verbs.go` resolved, keeping every row. Semantic clashes fixed in the merge: C6's `restoreArgs` became `restoreFlags`; C6's duplicate `maxLost` was dropped (serverestore's const, same 64 MiB, also covers `--lost`); `Refresh(..., nil)` in `backupverbs.go` and `health/restores_test.go` |
| `cd6d78f61` | C7 wiring, below                                                                                                                                                                                                                                                                                                                                                    |

### C7 as built (`cd6d78f61`)

- `admin.System.Backup` is `func() (backup.Deps, error)`, a seam built when a backup verb runs,
  because it reads the teamchat-ctl uid. This replaces part B's `*backup.Deps`. Building the
  System still reads nothing (WR2). If the seam errors, the verbs exit 78 (a test covers this).
- `wire`:
  - `host.Deps.Backup` is `backup.Take` over that seam (BT7). `ErrBackupNotBuilt` is gone, and
    its test was removed.
  - health's `Backups` is `backup.States(backup.Deps{Root, StateOwnerUID: 0})`. HB1 reads only
    root's files and the homeserver tree. `health.NoBackups` and `TestNoBackups` are deleted;
    the "none for long" case in `health_test.go` still covers that problem.
  - `backupUnits` and `restoreUnits` adapt `*units.Manager`.
  - `ctlUID` refuses a uid of 0, a negative uid, or one above 2^32-1.
  - `machine.engine` builds the whole `restore.Deps`:
    - `RestoreChild{Live: ExecChild, Unit: UnitChild{Runner: m.runner, Argv}}`. `m.runner` is
      `execx.OS{}` in production, so this matches the lead's literal, and a test can still fake
      it;
    - `backup.ReadTar` (part B's `readTar`, now exported, with a test) mapped to `restore.Tar`;
    - `host.Serve`;
    - `admin.Bootstrapper(sys)` (new; team apply uses it too);
    - `regKey` (`host.RegKeyOf`; a config without a key is an error);
    - `status.Read`, which answers nil when there is no record;
    - `host.TeamSandbox`, `host.ReadUnitsVersion`, `rand.Reader` and the clock.
  - `System.Restore` answers `restore.ErrUnproven` unless `machine.restoreProven` is set. It is
    never set in production (n2g-D8). Tests set it and reach the engine through the real
    adapter: `admin/wire/store_test.go`.
- `admin.Engine{Deps}` is the Restorer. Discard ignores `held`. `answered` gives the bytes only
  when the run succeeded or when the answer is `Printed`.
- `admin.ExitCodeOf` checks `backup.ErrLost`, `matrix.ErrUnreachable` and `matrix.ErrRateLimited`
  in `verbExit`, before `*ChildError`, so that RS15's 69 is reached. The tail duplicate is
  removed. `TestRestoreLostServerIsSixtyNine` was seen red, then green.

### Verified at `cd6d78f61`

- `go build ./...` passes.
- `go test -race -cover` passes over `./internal/teamchat/... ./internal/execx/... ./cmd/teamchat-admin`, and every package is at 90% or above:
  - admin 95.7, wire 94.6, backup 94.1, restore 94.8, health 94.1;
  - the rest as before.
- The exception is `cmd/teamchat-admin` at 66.7%. That figure was already there: the package is
  unchanged in this slice, and the uncovered lines are its thin `main`.
- golangci-lint reports 0 issues with and without `--build-tags tuwunel`.
- `./scripts/qa-changed.bash --base feat/bus-v2-n2` is green.

## Left

1. **RT2, RT3 and RT4** (`-tags tuwunel`), with
   `FAKEHS_TUWUNEL_BIN=/workspace/untracked/worktrees/bus-v2-n1-hostchecks/untracked/scratch/tuwunel-dl/tuwunel-1.9.4`.
   They are not built. The design worked out so far is below.
2. Run RT1 (`go test -tags tuwunel ./internal/teamchat/backup -run RT1`) on the merged tree. It
   has not been run since the merge.
3. Step 4 of the brief: one Opus adversarial reviewer over `git diff feat/bus-v2-n2..HEAD` plus
   SPEC-n2g.md, writing `261022-n2g-review.md`. The brief lists its focus. Fix what it finds and
   run rounds 2 and 3 only if needed.
4. PLAN.md Task 2.4, in the same commit as the last code, and `subagent-reports/261022-n2g.md`.
   The report needs:
   - the parts, and coverage per package;
   - decisions n2g-D1 onwards;
   - the owner residuals: D14's four defaults, and D8;
   - the review rounds;
   - the design-text fixes: C 3.12.2 step 08's start-block gap, and `restore.check`'s `rooms`
     argument (part A's report has the second).

## Points for the reviewer (open questions)

- `restore/engine.go` `serverLost` wraps every failure of the child against the live server in
  `matrix.ErrUnreachable` (online only). With the new ordering, a `ClassRefused` or
  `ClassConfig` answer from the live child at RS5 or RS7 now exits 69, not 70 or 78. That may be
  wrong. If so, a fix could wrap only the `failed` and `unreachable` classes, or only transport
  errors.
- In `wire`, `backup.States` is given a partial `backup.Deps`. It uses only `Root` and
  `StateOwnerUID`, and a comment says so.
- `status.Read` uses `state.ReadFile`, which has no owner check, to read the status record for
  RM11. The health tick reads the same file the same way.

## RT design (worked out, not written)

- **Where.** In `package admin`, as a tagged file `admin/tuwunel_test.go`. It cannot go in
  `ctl`'s tests: ctl imports admin, so an admin test cannot import ctl. The admin test world
  (`world_test.go`) runs the real verbs (`team approve`, `team apply`, `member add`/`reissue`)
  as root over host fakes, with the real child in process (`inProcessChild` → `ctl-child` →
  `w.open`).
- **The real world.** Factor `newWorld`'s directory setup into a bare constructor, then build
  `newRealWorld(ctx, t)`:
  - a live server `L` from `tuwunelhs.StartWith(ctx, t, server, Options{DatabasePath: root+paths.HomeserverPrivateDatabase(team), Extra: database_backup_path = root+HomeserverPrivateBackups(team), database_backups_to_keep, admin_signal_execute = ["server backup-database"]})`.
    This is the same as `backup/tuwunel_test.go`'s `realTree`;
  - the admin registered with `L.RegKey`, as in `apply/tuwunel_test.go`'s `realAdmin`, and the
    accounts `coord` and `other` created with a password;
  - `w.admin` set to that admin client, and the admin token written to
    `root+paths.CtlState(team)+"/admin.token"` (the backup's source);
  - a new `world.base` field. `world.member` and `world.staged` use it when set. `staged` must
    dial `http://127.0.0.1:<port>` with the given port, as `ctl.Open` does, so the restore
    reaches the staged server `S`;
  - the fakehs cleanup skipped when `w.srv` is nil.
- **`tuwunelhs`.** It needs `Options.Args []string` (additive), which is appended to the
  command, for `--restore-backup N`.
- **Taking backup N.** `backup.Take` with `realUnits` (Kill is `L.Signal(SIGUSR2)`). Ready is a
  wrapper that probes `L.Port`, since team.json says 8448. `CtlUID` and `StateOwnerUID` are
  `selfUID`.
- **RT2.**
  - Before N:
    - apply the team;
    - `member add` seat 3 with a `password` credential (the secret is in the bundle on stdout,
      `decodeDoc`);
    - add a `token` member (`addArgs("next","token")` gives `coord`), which merge leaves alone
      when its CredSeq is at or below B;
    - give alice a known password (`w.admin.ResetPassword`);
    - log seat 3 in with `LoginDevice(..., "TCSEAT")`, which is the pool seat token from N;
    - have the token member `PutRoomAccountData(main, "teamchat.floor", {...X})`.
  - Take N.
  - After N:
    - `member reissue` seat 3, which gives CredSeq > B and a new password;
    - bump alice's `RevokedSeq` above B in the roster file (decode, edit, `Encode`; no verb
      sets it after creation) and reset her password on L;
    - have the token member put floor Y.
  - Plant `root+HomeserverPrivateRestoreDatabase(team,N)/SENTINEL`.
  - Start S on that path with `--restore-backup N`, the same backup path, and the same server
    name. Assert that SENTINEL is gone, and that L still takes seat 3's new password (L is
    unchanged).
  - Build the merged roster with `restore.Merge(current roster file, backup.ReadTar(...).Roster, MergeOptions{Team})`.
  - Call `RestoreChild{Live: inProcessChild{w}, Unit: func([]string) Child { return inProcessChild{w} }}.Secure(ctx, team, restore.SecureArgv(team, self), SecureArgs{TeamFile, Port: S.Port, Token: tar.Token, Rooms: tar.Rooms, Roster: m.Roster, lists from m.Lists})`.
  - Assert on S:
    - seat 3's old and new passwords are both refused;
    - the TCSEAT token from N is refused (whoami gives `M_UNKNOWN_TOKEN`);
    - alice's N-era password is refused;
    - the token member's floor reads X;
    - the roster event's `restore_epoch` is `m.Roster.RestoreEpoch` (use `restore.check` through
      the child, or `GetState` with the tar token).
- **RT3.** After RT2's secure:
  - stop S with SIGTERM (the cleanup of `StartWith`). Two runs are needed, so either give
    tuwunelhs a `Stop`, or start S under its own context;
  - start it plainly (no `--restore-backup`) on the same path. The first state read must give
    the merged `restore_epoch` and `rev`, and a revoked member's old token must be refused;
  - repeat with SIGKILL (`Server.Signal(SIGKILL)`). The cleanup's `stoppedCleanly` then needs to
    accept that kill.
- **RT4.**
  - Make L's database unable to open: stop L, then truncate `CURRENT`;
  - restore N into a fresh path, secure it as in RT2, rename it into place with
    `restore.OS.Rename` from `db.restore-N` to the database name, and start a server there;
  - it must serve with the raised epoch. A fuller version would run `restore.Run` with
    `Offline: true` over fakes of units and systemd-run that start real processes, but the
    spec asks only for "the engine's real-process parts".

## State

The tree is clean at `cd6d78f61`, apart from this handoff. No push, no qa-all.
