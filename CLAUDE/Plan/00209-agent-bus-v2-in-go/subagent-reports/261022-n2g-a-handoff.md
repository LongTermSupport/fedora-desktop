# n2g part A handoff (Plan 00209, SPEC-n2g.md)

Worktree `/workspace/untracked/worktrees/bus-v2-n2g`, branch `feat/bus-v2-n2g`. The brief is
"Part A" in `261022-n2g-handoff.md`. The builder rules are `/workspace/untracked/scratch/n1-common.txt`.
Each step below was committed with qa-changed green and golangci clean, with and without
`--build-tags tuwunel`.

## Done

| Step | Commit      | What                                                                                                                    |
| ---- | ----------- | ----------------------------------------------------------------------------------------------------------------------- |
| A1   | `4917ee36d` | paths' names; `teamlock.Guard`, `Unguarded`, `ErrNoGuard`; `restore.Guard`, `PendingError`, `ErrPending` (exit 70); RG3 |
| A2   | `e3bbad22c` | `units.State`, `Manager.State`, `Kill`, `Start`, `Stop`; `checkUnit` takes the two restore units                        |
| A3   | `18b1e8e83` | `host.Status.Refresh(ctx, team, first, events []string)`; every caller updated, health's tick included                  |
| A3   | `865a2face` | `host.Serve` (SM3); `host.BaseSandbox()` and `host.TeamSandbox(t)` (n2g-D17), pinned against C 3.2.3's text             |

The lead has been sent each API by message.

### The exported API as built

- **paths** (`paths/restore.go`, all absolute; a backup number `n int64 <= 0` panics):
  - `RestoreRecord(t)`, `RestoreEpochFile(t)` and `RestoreConfig(t)`, in `root/<t>/`;
  - `BackupDir(t)`, `BackupTar(t, n)` and `BackupLastRun(t)`;
  - `RestoreLostFile(t, n)`, in `ctl/<t>/`;
  - `HomeserverPrivateBackups(t)`, `HomeserverPrivateBackupMeta(t)` and
    `HomeserverPreviousDatabase(t)`;
  - `HomeserverRestoreDatabase(t, n)`, the unit's view, for `database_path`;
  - `HomeserverPrivateRestoreDatabase(t, n)`, root's view;
  - `RestoreUnit(t)`, `RestoreCtlUnit(t)` and `RestoreUnitCredential(t)`.
- **teamlock:** `type Guard func(paths.TeamID) error`, `Unguarded`, `ErrNoGuard`, and
  `Take(ctx, dir, team, verb, wait, clock, guard)`, whose guard is the last argument.
- **restore:** `Guard(root) teamlock.Guard`, `ErrPending`, and `PendingError{Team}`.
  `admin.ExitCodeOf` maps `ErrPending` to 70. Also `doc.go`.
- **host:**
  - `Deps.Guard`;
  - `Serve(ctx, d, team) error`, which fails as a `*host.Error` with Verb `serve`;
  - `BaseSandbox() []string`, a copy with `%i`;
  - `TeamSandbox(t) []string`.
- **units:**
  - `State{Load, Active, InvocationID, NRestarts int64, Result}`;
  - `(*Manager).State(ctx, unit)`;
  - `Kill(ctx, unit, whom string, signal int32)`;
  - `Start(ctx, unit)` and `Stop(ctx, unit)`.

### Decisions to record in the final report `261022-n2g-a.md`

- `units.State` carries `Load` (LoadState) beyond the spec's four fields, for RS3's "poll until
  not loaded". A unit that cannot be loaded gives `{Load: not-found, Active: inactive}`.
- `host.Serve` always daemon-reloads. What systemd has loaded cannot be read from the files, and
  a reload of unchanged units changes nothing. It checks the bound addresses (step 9) as well as
  readiness, on units.json's port and listen set, and it does not enable the unit.
- `BaseSandbox` is a function returning a copy rather than a variable. `TeamSandbox`
  substitutes `%i`, since a transient unit's properties are not expanded (n2g-D18).
- `StateDirectoryMode` and `IPAccounting` are in the sandbox list, since they fall inside
  "DynamicUser to InaccessiblePaths". Only `LoadCredential` and `Environment` are left out.
- The production wiring `admin/wire/wire.go` (C's file) got `Guard: restore.Guard(root)`: one
  line and a test. A nil guard would otherwise break every production apply.

## Left: A4, the child ops (RC1-RC8)

The research is done; no A4 code is written.

- **Import constraint.** `ctl` imports `admin` (`ctl.Config.Opener() admin.Opener`), so `admin`
  cannot call `ctl`. The child's restore file operations must reach `ctl` through the `Opened`
  seam. The proposed shape is new fields on `admin.Opened`, which `ctl.Open` fills:
  - `Restore` is an interface: `Swap(token, rooms, roster []byte)` (RC2), `Finish()` (RC4),
    `Revert()` (RC5) and `WriteLost(n, data)` (RC3, 0600, atomic). All are fd-relative on
    ctl's held `dir`;
  - `Staged(port int, token string) (*matrix.Admin, error)` is an admin client on
    `127.0.0.1:<port>` with an explicit token, for RC1 and RC8 when `port` and `token` are given.
- **`dir.create` refuses an existing name.** RC2's "write over" needs an atomic replace:
  a temporary, then `renameat`, then a directory fsync. The link to `.previous` goes first, by
  `linkat`, only when `.previous` is absent.
- **The ops in `admin/serve.go`** go into `argKeys`, with new `keyPort`, `keyToken`, `keyRooms`,
  `keyRoster`, `keyRetire`, `keyRevoke`, `keyRevokeHumans`, `keyLock`, `keyBackup` and
  `keySinceMS`. Put them in a new file `admin/serverestore.go`, with `serverestore_test.go`
  written first.
- **`Answer`** gains `Export *Export{Count, FirstMS, EndMS int64; Truncated bool}` and
  `Epoch *Epoch{RestoreEpoch, Rev int64}`:
  - add them to `answerFits` (each only with its op) and to `validateOps`. A restore op's answer
    carries no handle, credential, main room, view or notice;
  - add them to `encodeOps` and `decodeOps`, with keys `export` and `epoch`, decoded strictly.
- **`checkStarted` (RC7)** also accepts a group list of exactly `[own gid]`. `Process` needs
  `Getgid`: check `admin/process.go`.
- **The matrix calls:**
  - `Kick`, `SetDeactivated`, `ResetPassword` and `SetLocked`;
  - `PutState` of `proto.Roster`, from `apply.DeriveRoster`;
  - `GetStateEvent` for RC8's direct state read;
  - `Messages` (backwards) for RC3's export, under the 64 MiB cap, keeping the newest.
- **Tests run on fakehs** (`matrix/fakehs`). See `admin/world_test.go`'s world and its
  `inProcessChild`.

A5: nothing in A.

## Part C's `restore.Child`

From n2g-c. C writes it in `restore/deps.go` on `feat/bus-v2-n2g-restore`, and A4's admin type
must satisfy it structurally:

- `Secure(ctx, team, unit []string, a SecureArgs) ([]string, error)`, run through the secure
  unit argv `unit` (RU2) and judged as ExecChild judges;
- `Check(ctx, team, unit []string, a CheckArgs) (Epoch, error)`: a nil `unit` means the live
  server, and Port 0 with Token "" means null;
- `Swap(ctx, team, a SwapArgs) ([]string, error)`;
- `Export(ctx, team, a ExportArgs) (Export, error)`;
- `Finish(ctx, team, teamFile []byte) ([]string, error)`;
- `Revert(ctx, team, teamFile []byte) ([]string, error)`.

The argument and result types:

- `SecureArgs{TeamFile []byte; Port int; Token string; Rooms proto.RoomIDs; Roster apply.RosterFile; Retire, Revoke, RevokeHumans, Lock []string}`;
- `CheckArgs{TeamFile, Port, Token}`;
- `SwapArgs{TeamFile, Token, Rooms, Roster}`;
- `ExportArgs{TeamFile; Backup, SinceMS int64}`;
- `Export{Count, FirstMS, EndMS int64; Truncated bool}`;
- `Epoch{RestoreEpoch, Rev int64}`.

A failed answer comes back as `*admin.ChildError`. C also asks `host` for two exports, neither
built nor answered yet:

- `ReadUnitsVersion(root, team) (paths.ServerVersion, error)`, for RU1's binary;
- `RegKeyOf`, for RS7's bootstrap.
