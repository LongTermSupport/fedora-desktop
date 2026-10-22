# n2g part C handoff (restore engine), after C1, C2 and C3a

Worktree `/workspace/untracked/worktrees/bus-v2-n2g-restore`, branch `feat/bus-v2-n2g-restore`,
cut from A1 (4917ee36d). Rules: `/workspace/untracked/scratch/n2g-part-common.txt` (part c) and
`n1-common.txt`. Brief: "Part C" of `261022-n2g-handoff.md`. Contract: `SPEC-n2g.md` (sections
5-12, 7a; read it, it is the truth). Final report: `261022-n2g-c.md`. C7 runs after the merge of
A, B and C: do C1-C6 only and say so.

## Done (committed, qa-changed green each time, golangci clean with and without `-tags tuwunel`)

- **C1 (865909c29).** `restore/record.go`: `Record`, `Step*`, `Lost`, `EncodeRecord`,
  `DecodeRecord`. `restore/epoch.go`: `EpochFloor{Epoch, MinBackup}`, `Raised`,
  `EncodeEpochFloor`, `DecodeEpochFloor`, `MaxEpoch` (999 999 999, A 5.1).
  `restore/merge.go`: `Lists`, `Merged`, `MergeOptions{Team, High, Offline}`,
  `Merge(current, backup, o)`, `MergeFromBackup(backup, []status.Member, o, rand io.Reader)`,
  `NoteElementPasswords`. `restore/errors.go`: `ErrRefused`, `RefusedError` (admin must map it
  to 70 in `admin/exit.go`'s `verbExit`). `restore/reader.go`: the strict JSON reader helpers.
- **C2 (4f86945a2).** `restore/argv.go`: `Staging{Team, Backup, Version, Port, Sandbox}`,
  `StagingArgv(s, restoring) ([]string, error)` (checks the sandbox: identity lines present, no
  Restart\*/Timeout\*/Type/NotifyAccess/WatchdogSec/LoadCredential/Environment/ExecStart, no `%`),
  `SecureArgv(team, self)`, `unitCommand(argv, stdin, stdout) execx.Cmd` (CleanEnv, PATH, LANG).
  `src-go/qa/rules/table.go` got a `dynamicNames` row for `restore.unitCommand` (the seam rule
  cannot see a run-time program after `systemd-run --`).
- **C3a (13c4de893).** `restore/fs.go`: `Kind` (Absent, Directory, File, Other), `Entry`, the
  `FS` interface (Kind, ReadDir, Rename, RemoveTree, Fsync, Syncfs on the state directory by
  relative names; WriteFile, Remove on root paths) and `OS{Root, StateOwnerUID}` over openat2.

Coverage of `restore` is 98%.

## Decisions taken so far (put in the final report)

- The record carries `"from_backup": bool` (RS8 step 2 needs it on a resume) and its `lists`
  object holds `retire, revoke, revoke_humans, lock, reissue, return, password, unknown, notes`
  and `pending` (RM12's resolution line or null), so the RS9 answer is rebuilt on a resume. An
  optional `"stuck"` is allowed only beside `revert`.
- Merge: a retired handle's `needs` is cleared. A human only in the backup roster is left out of
  the merged roster and goes in `lock` (spec silent). A removed human goes in `lock` only. RM12:
  a non-member op (team.apply, role.apply) is cleared with `noteApplyWaits`; a member op on a
  handle the backup lacks, or one the merge retired, is left as the merge has it.
- `StagingArgv` takes the sandbox as data (A's `host.BaseSandbox` rendered for the team, wired
  in C7) and the version and port (port from `team.json`'s `Port`; the version from units.json,
  which needs a host export in C7).
- `OS.Remove` unlinks only (never a directory). `StateOwnerUID` also stands for the owner of
  root's own files where the engine reads them with `ReadOwnedFile` (0 in wire, the test uid in
  tests).

## Left

1. **C3b.** `restore/renames.go`: RS16's table over `FS` (`d r`/`p r`/`d p` toward swapped and
   unswapped, loop until done, any other state refused with a one-line listing such as
   `db=directory db.previous=absent db.restore-7=file`, which becomes the record's `stuck`);
   RS17's `startable` check (`db` a directory, `restore.block` absent by Lstat). Tests: every row
   in both directions (the `p r` rows are the kill between the two renames), every refusal, over
   `OS` on a temp tree (`osTree` in `fs_test.go`).
2. **C4.** `restore/validate.go`, RV1-RV7 (see the spec). Reads root files with
   `state.ReadFile`/`ReadOwnedFile`; the backup tar through a Deps seam (part B's `backup.Decode`
   is not on this branch): declare `Tar func(team, n) (Tar, error)` returning manifest time and
   server name, team.json bytes, config, `apply.RosterFile`, `proto.RoomIDs`, token.
3. **C5.** `restore/deps.go` and `restore/engine.go`: `Deps` (SM4) with local interfaces, since
   A's are not on this branch: `Units{State(ctx, unit) (UnitState, error); Stop(ctx, unit)}` with
   `UnitState{Load, Active, InvocationID, NRestarts, Result}` mirroring A2's `units.State`
   (committed on feat/bus-v2-n2g as e3bbad22c; `Load` "not-found" is "not loaded");
   `Ready{ServerVersion(ctx, port)}`; `Run execx.Runner`; `Child` (below); `FS`;
   `Backup func(ctx, held *teamlock.Lock, keep []int64) (int64, error)`;
   `Serve func(ctx, team) error`; `Bootstrap func(ctx, host.Bootstrap) ([]string, error)`;
   `Approvals declare.Approvals`; `StatusRecord func(team) (*status.Record, error)`;
   `Status{Refresh(ctx, team, first bool, events []string) error}` (A3's new signature);
   `Rand`, `Clock{Now, Sleep}`; `Sandbox func(team) []string`; `Version func(team) (paths.ServerVersion, error)`; `Self string`. Then `Run`, `Resume`, `Discard` (RS1-RS18,
   RO1-RO7) and the SM5 `runtime.Goexit` kill at every fake call with a resume after it, plus
   the crash-point tests the lead named: the kill between `start` and the block removal, a
   resume under `--roster-from-backup` at swap, start and clean, the verify's wait for the
   staging unit to unload, and a revert from `start` then `--discard` naming the served events.
   **The epoch raise (RS6 step 1, restore-epoch.json) must be atomic and durable and tested**
   (`FS.WriteFile` is `state.WriteFile`, fsynced; test it lands before the record's `swap`).
4. **C6.** The `restore` verb in admin (`--team`, `--backup`, `--resume`, `--discard`, `--lost`,
   `--offline`, `--roster-from-backup`; RS14, RS15, the 64 cases; add only the `restore` row to
   `admin/run.go`'s runs map; map `restore.ErrRefused` to 70 in `admin/exit.go`), then health's
   HB2 (`Restores` interface in health Config, the tick fills `status.Record.Restore`, pre 0 for
   null; `restore.Records(Deps) health.Restores`) and HB3 (no restart while `restore.json`
   exists; the problem stands). Health must not import restore.
5. Report `261022-n2g-c.md`, with the merge glue: wire's adapters from units.Manager to
   `restore.Units`, `host.Status`, `host.Serve`, `host.BaseSandbox`, a host export of the
   units.json version and of `regKeyOf` (asked of part A by message), `backup.Take` and
   `backup.Decode` behind `Deps.Backup` and `Deps.Tar`; admin's `restore.Child` adapter (A4) and
   a `dynamicNames` row for wherever admin runs `SecureArgv`'s argv.

## The Child interface (sent to part A by message; A4's admin type satisfies it)

```go
type Child interface {
	Secure(ctx context.Context, team paths.TeamID, unit []string, a SecureArgs) ([]string, error)
	Check(ctx context.Context, team paths.TeamID, unit []string, a CheckArgs) (Epoch, error) // unit nil: the live server
	Swap(ctx context.Context, team paths.TeamID, a SwapArgs) ([]string, error)
	Export(ctx context.Context, team paths.TeamID, a ExportArgs) (Export, error)
	Finish(ctx context.Context, team paths.TeamID, teamFile []byte) ([]string, error)
	Revert(ctx context.Context, team paths.TeamID, teamFile []byte) ([]string, error)
}
type SecureArgs struct { TeamFile []byte; Port int; Token string; Rooms proto.RoomIDs; Roster apply.RosterFile; Retire, Revoke, RevokeHumans, Lock []string }
type CheckArgs struct { TeamFile []byte; Port int; Token string }
type SwapArgs struct { TeamFile []byte; Token string; Rooms proto.RoomIDs; Roster apply.RosterFile }
type ExportArgs struct { TeamFile []byte; Backup, SinceMS int64 }
type Export struct { Count, FirstMS, EndMS int64; Truncated bool }
type Epoch struct { RestoreEpoch, Rev int64 }
```

## Gotchas met

- The error-hiding hook blocks any `x, _ := f()` in a Write, even of a non-error second value.
- A Bash command with `;` is blocked (safe-mode prelude): chain with `&&` only; commit with
  `git commit -F untracked/scratch/msg.txt`.
- `new(expr)` is the modernize form (go 1.26); gofumpt runs by `golangci-lint fmt`.
- Writes must stay inside the worktree (`untracked/scratch/` there).
