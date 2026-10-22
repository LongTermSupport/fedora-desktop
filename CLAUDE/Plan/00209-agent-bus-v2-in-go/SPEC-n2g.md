# SPEC: N2 slice n2g, backups and restore (Plan 00209 Task 2.4, C 3.12)

Built on n2a to n2f (`SPEC-n2a.md` to `SPEC-n2f.md`). Sources: area C's 3.12.1 and 3.12.2, with
3.2.2 (files), 3.2.3 (the base unit), 3.4 (locks), 3.5.2 step 6, 3.6.1 (the roster), 3.11.2 step
4 and 3.11.3 (the status record), 3.14's "backups and restore" row, H-C4, H-C5, H-C15, DC8 and
DC14. The facts about the pinned Tuwunel 1.9.4 come from a spike run in the container
(`untracked/scratch/n2g-tuwunel-spike.md` of the slice's worktree, not tracked):

- `SIGUSR2` with `admin_signal_execute = ["server backup-database"]` makes backup `<n>` in about
  0.2 s: `backups/meta/<n>` (a text file whose first line is the backup's time in unix seconds),
  `backups/private/<n>/`, and `backups/shared_checksum/`, created on demand, 0700/0600.
- Numbers start at 1 and never repeat. `database_backups_to_keep` prunes the oldest, removing
  its `meta/<n>` and `private/<n>/`.
- A `SIGUSR2` before the handler is installed kills the process when it is not PID 1 (the
  spike). Under the base unit's `PrivatePIDs=yes` it is PID 1, and the signal is dropped (H 5),
  which shows only as BT2's timeout. So root signals only a homeserver that is active and
  answers, in the same invocation it probed (BT1).
- `tuwunel --restore-backup <n>` restores into the configured `database_path` and then goes on
  serving. A missing `<n>` exits 1 (`Backup #<n> not found; available: ...`). It needs the
  same `database_backup_path` as the server that made the backup.
- A target directory that is not empty is wiped without a word. So the staging path must be
  new, and the staging unit must never restart, since a restart would run the restore again
  over the secured copy (RU1, RS3).
- The live database stays byte-identical, and the restored server's next backup continues the
  shared numbering.

Each expectation below is numbered and gets a Go test written first and seen to fail.

**What this slice is.** The internal backup function (step 6 of `team apply` included) and the
verbs `backup` and `backups`; the health tick's real backups and restore record; the restore
engine of C 3.12.2 with its record, resume and discard, its child operations and its transient
units rendered as code; the offline restore for a homeserver whose database will not start
(RO, n2g-D14); and the guard every lock-taking verb runs.

**What it is not (owner gates).**

- **Gate A.** No play, task, unit file, timer, tmpfiles entry or meta-deploy entry. The backup
  units' text (BU1) and the base unit's sandbox (n2g-D17) exist as code only, as n2d's
  drop-ins do.
- **Gate B.** Nothing gives a process a pool seat's password.
- **DC8.** The production wiring refuses `restore` (exit 70) until H-C5 and H-C15 have passed,
  as PLAN.md Task 2.5 says (n2g-D8). The engine is built and proven over fakes. The parts that
  need no systemd run against the pinned Tuwunel (RT).

**Trust rule.**

- Root never decides from a file another account can write, except `teamchat-ctl`'s files in
  `ctl/<team>/`, which it reads by I-B6 (owner checked).
- Every file root reads is opened `O_RDONLY|O_NOFOLLOW|O_NONBLOCK`, `fstat`ed, regular, and
  capped. That is `state.ReadFile`, and `ReadOwnedFile` for root's own files (uid 0).
- The homeserver's tree (`/var/lib/private/teamchat/<t>/`) is owned by the dynamic uid at
  every level below `/var/lib/private/teamchat`, so `O_NOFOLLOW`, which guards only the last
  component, is not enough there. Every path in it is resolved by
  `openat2(RESOLVE_NO_SYMLINKS|RESOLVE_BENEATH|RESOLVE_NO_XDEV)` from a root-owned fd of
  `/var/lib/private/teamchat` (`O_DIRECTORY|O_NOFOLLOW`, `fstat`ed uid 0). Every entry is
  then listed, read, renamed or removed relative to the fd so found.
- A tree root removes there (`db.previous`, `db.restore-<N>`) is removed level by level: each
  directory is opened no-follow relative to its parent's fd, and each entry is removed by
  `unlinkat`. A link is removed, never followed (`teamchat/restore`'s `RemoveTree`; a test
  plants a link to a root-owned directory and finds it intact).
- Root writes under `ctl/<team>/` only through the `teamchat-ctl` child.
- A FIFO is never opened.
- Every program runs through `execx`, with no secret in argv.

Packages:

- `teamchat/backup` (new): the tar, Tuwunel's numbers, the internal backup, the last-run record,
  the health adapter.
- `teamchat/restore` (new): the record, the merge, validation, the engine, the unit argv, the
  fd-relative renames, the guard.
- `admin`: the verbs and the child's ops.
- `ctl`: the child's swap files.
- `units`: `KillUnit`, and the restore unit's name.
- `health`: real backups, the restore record, no restart while a restore is recorded.
- `host`: the start and readiness steps exported, `Deps.Guard`.
- `teamlock`: a guard on every `Take`.
- `admin/wire`.

## 0. Seams (SM)

Every effect goes through one of these, and each has a fake in the package's tests.

- **SM1. `backup.Deps`:**
  - `Root` (the host prefix, "" on a host);
  - `Units` (`State`, which gives `ActiveState`, `InvocationID`, `NRestarts` and `Result`, and
    `Kill`, SD10);
  - `Ready` (`hostnet.Readiness`'s `ServerVersion` on the team's port);
  - `Clock` (`Now`, and `Sleep` for BT2's poll);
  - `CtlUID`.
- **SM2. `host.Deps.Backup`** stays `func(ctx, held *teamlock.Lock) error`. `wire` closes over
  `backup.Deps` and passes `keep` nil (BT7).
- **SM3. `host.Serve(ctx, d host.Deps, team paths.TeamID) error`** is the start of C 3.5.2
  steps 7 and 8 for a team already applied: reload when needed, start, then wait ready (60 s)
  on `units.json`'s port and listen addresses. It is refactored out of `applying`, which then
  calls it. It reads `units.json` and never renders.
- **SM4. `restore.Deps`:**
  - `Root`, `CtlUID`;
  - `Units` (SM1's, plus `Stop`);
  - `Run` (`execx.Runner`, for RU1's and RU2's `systemd-run`);
  - `Child` (the RC ops, declared in `restore`, implemented in `admin`);
  - `FS` (the state directory's `Open`, `Stat`, `Rename` (`renameat2` `RENAME_NOREPLACE`) and
    `RemoveTree`, all fd-relative by the trust rule, with a fake over a temp directory);
  - `Backup` (`backup.Take`), `Serve` (SM3) and `Bootstrap` (`host.Deps.Bootstrap`);
  - `Status` (`host.Status`), `Rand` and `Clock`.
- **SM5. A kill** is modelled by a fake that calls `runtime.Goexit` at its k-th call. The
  engine never cleans up in a `defer`: every clean-up is an explicit step, so a `Goexit` leaves
  the disk as a kill would. The lock's release is the one deferred call, and a real kill
  drops the `flock` too. For every k, the test asserts what is on disk, then runs `--resume`
  and asserts the team's end state. This is the handoff's "a kill at every step".

## 1. The backup tar (BK, C 3.12.1 step 3 and 4, I-B5, I-B6)

- **BK1. Members.** `root/<team>/backups/<n>.tar` is a ustar archive of exactly six regular
  files, in this order:

  - `backup.json`, the manifest
    `{"v": 1, "team": "<t>", "backup": <n>, "time_ms": <meta time x 1000>, "server_name": "<s>"}`
    (n2g-D1);
  - `team.json` and `config.toml` from `root/<team>/`;
  - `roster.json`, `rooms.json` and `admin.token` from `ctl/<team>/`.

  Each is mode 0600, uid and gid 0, and carries the manifest's time. There are no directories,
  links, PAX or GNU records.

- **BK2. Caps.** Each member and the whole archive have a cap:

  - the manifest and the token, 4 KiB each;
  - `team.json` and `config.toml`, 64 KiB each;
  - the roster and the rooms record, 1 MiB each;
  - the archive, 4 MiB.

- **BK3. One parser** (I-B5) is the backup's read-back and the restore's validation. It refuses:

  - any type but a regular file, and any name outside BK1 or out of order, or given twice;
  - a size over its cap, or one that differs from the bytes;
  - a setuid, setgid or sticky bit, or a mode other than 0600;
  - a uid or gid other than 0, a user or group name, or a member time other than the
    manifest's;
  - PAX records or xattrs, and data after the end blocks.

  The content must be valid too:

  - the manifest strictly, with its team the one asked for;
  - `team.json` by `declare.DecodeTeamFile`, its id the team and its `server_name` the
    manifest's;
  - `roster.json` by `apply.DecodeRosterFile` and `rooms.json` by `apply.DecodeRooms`, both
    for the team;
  - the token as `ctl`'s rule (printable ASCII, no space, at most 4096 bytes);
  - `config.toml` as UTF-8 with no NUL, kept only as evidence (C 3.12.2).

  No error quotes a member's content. The hostile tars of 3.14 are tests: links, devices,
  `..`, setuid, extra members and oversize.

- **BK4. Sources** are read by the trust rule:

  - `root/<team>/` files by `ReadOwnedFile` with uid 0;
  - `ctl/<team>/` files by `ReadOwnedFile` with the `teamchat-ctl` uid (I-B6).

  A missing source fails the backup, naming it.

- **BU1. The backup units as data.** `backup.TimerUnit` and `backup.ServiceUnit` hold the
  text of C 3.12.1's `teamchat-backup.timer` (`OnCalendar=daily`, `RandomizedDelaySec=1h`,
  `Persistent=true`) and `teamchat-backup.service` (`Type=oneshot`,
  `ExecStart=/usr/local/sbin/teamchat-admin backup --all`, `TimeoutStartSec=30min`, the health
  unit's sandbox with C's `ReadWritePaths=` and no `CAP_SYS_PTRACE`). A test pins each
  directive. Nothing installs them (gate A); Task 2.5's play renders them.

## 2. The internal backup (BT, C 3.12.1)

`backup.Take(ctx, deps, held *teamlock.Lock, keep []int64) (Result, error)` is the internal
function. Its callers are `team apply` step 6, `backup` and the restore's step 6. It never takes
a lock: the held lock is its argument (C 3.4).

- **BT1. Ready first, in one invocation.** `Take` reads the unit's `InvocationID`, then the
  unit must be `active` and `ServerVersion` must answer (H 5, H-C4). If not, the backup fails
  ("the homeserver is not running"). Right before the signal it reads the state again. An
  `InvocationID` that changed, or a unit no longer `active`, fails the backup ("the homeserver
  restarted"): a crash restart under `Restart=always` could otherwise take the signal before
  its handler. `Take` never starts the unit: that is `team apply`'s `startForBackup`. Tests:
  the fake records no `Kill` when the unit is not active, when `ServerVersion` fails, or when
  the second read gives another invocation.
- **BT2. The signal.** Before signalling, `Take` reads the highest number in
  `HomeserverPrivateState(t)/backups/meta/`, resolved by the trust rule. It counts only regular
  files with decimal names, so Tuwunel's temporary names are skipped. A missing directory is
  none. It then sends `SIGUSR2` with D-Bus `KillUnit(unit, "main", 12)` (DC17, SD10). It polls
  once a second on `Clock.Sleep`, up to 60 s, for a higher number whose meta file's first line
  is a unix time, and takes the highest such number. A meta file is read relative to the meta
  dir's fd, no-follow, regular, capped at 64 KiB. A timeout says "no backup appeared in 60 s
  (a signal before Tuwunel's handler is dropped)".
- **BT3. The pair.** The tar of BK1 for that number is written with `state.WriteFile` (0600)
  into `root/<team>/backups/`. That directory is made 0700, root-owned, and checked like the
  admin root. The tar is read back with BK3, and the backup fails if it does not validate.
- **BT4. Prune.** After the pair is written, every `<m>.tar` with no `meta/<m>` and `m` not in
  `keep` is removed. Only names `^[1-9][0-9]*\.tar$` are removed; other entries are left. `keep`
  is the restore's `N` (n2g-D10).
- **BT5. The last run** is `root/<team>/backups/last-run.json` (0600):
  `{"v": 1, "result": "ok"|"failed", "at_ms": <clock>, "backup": <n>|null, "error": "<line>"|null}`.
  Every `Take` writes it, whether it succeeds or fails; a write that fails is joined to the
  answer (n2g-D3).
- **BT6. Errors** name the team and the step: ready, signal, wait, sources, write, verify,
  prune. A deadline names 60 s.
- **BT7. `team apply` step 6** calls `Take` with the held lock and no `keep`. n2f-D6's refusal
  is gone.

## 3. The health adapter (HB, C 3.11.2 step 4, 3.11.3)

- **HB1. `backup.States`** is `health.Backups`:

  - `Newest` is the highest `n` with both `meta/<n>` and `<n>.tar` present (C's "both halves");
  - its `TimeMS` is the meta time;
  - `LastRun` is `last-run.json`'s result, or `none` when there is no record.

  An unreadable record or directory is an error. HR4's `NoBackups` is removed.

- **HB2. `restore.Records`** is a new `health.Restores`. With `restore.json` present it gives
  `status.Restore{step, backup, pre}` and a problem:

  - `staged`: "restore staged", fix `restore --team T --resume` or `--discard`;
  - `staging`, `swap`, `start` or `revert`: "restore interrupted", fix
    `restore --team T --resume`;
  - an offline record adds "(offline)" to the problem.

  The tick copies it into the record's `restore` field, with `pre` 0 for null, since
  `status.Restore.Pre` is an integer. With no `restore.json` the field is null.

- **HB3.** The tick never restarts a team that has a `restore.json`; the problem stands
  instead (C 3.12.2: the health tick's restart is refused).

## 4. The verbs `backup` and `backups` (BV, C 3.3, 3.13)

- **BV1. `backup --team T`.** It takes the team lock (wait 10 min, with the guard of RG1), runs
  `Take`, refreshes the status record, and prints
  `{"v": 1, "team": "T", "backup": <n>, "time_ms": <t>, "pruned": [<m>...]}`. A failure is
  printed by C 3.3's table: busy 75, refused 70, anything else 1.
- **BV2. `backup --all`** is the timer's. It covers every stamped team (`health.Stamps`), one
  after another, each as BV1. It prints one line per team on stderr: `<team> backup <n>` or
  `<team> failed: <error>`. It prints nothing on stdout, and exits 1 if any team failed
  (n2g-D5).
- **BV3. `backups --team T`** takes no lock and reads only. It prints
  `{"v": 1, "team": "T", "backups": [{"id": n, "time_ms": t, "usable": bool}...], "last_run": <last-run.json or null>}`,
  newest first. `usable` means both halves are present and the tar validates by BK3.
- **BV4.** `--all` and `--team` exclude each other. Anything else is 64. The verbs are root's
  class (77 otherwise). DC4 holds because v2 installs no NOPASSWD rule: an owner reaches them
  only by password sudo (n2g-D7). The seam is `admin.Process` (`classOf`). A test runs
  `backup`, `backups` and `restore` (with `--lost` too) as the ctl uid and as another uid, and
  gets 77 with nothing read.

## 5. The merged roster (RM, C 3.12.2 "The merged roster")

`restore.Merge(current, backup apply.RosterFile) (Merged, error)` is pure. `B` is the backup
roster's `seq`, and "after N" means a `seq` above `B`.

- **RM1.** For each prefix, `counters` is the larger of the two rosters' values.

- **RM2.** A handle `retired` in either roster is `retired`, and goes in `retire` (kick from every
  room, deactivate). A handle in the backup roster but not in the current one is `retired` too
  (n2g-D11).

- **RM3.** A handle in the current roster but not the backup's is `lost` and goes in `return`
  (the board names `member return`). Nothing is revoked, since the restored database has no
  account for it.

- **RM4.** A handle `active` in the current roster and present in the backup's, in any state
  that is not `retired`, with `cred_seq > B`, gets `needs: reissue` and goes in `revoke` and
  `reissue`. Tests cover `active`, `parked` and `lost` in the backup.

- **RM5.** Every `active` handle with `cred` `password` gets `needs: reissue`, whatever its
  `cred_seq`, and goes in `revoke` and `reissue` (K13).

- **RM6.** A handle that is `parked` or `lost` in the current roster, and present in the backup's,
  goes in `revoke`: the restored account holds an older credential.

- **RM7.** Humans:

  - one with `revoked_seq > B` gets `needs: password` and goes in `password`;
  - if the backup roster has that human too, it also goes in `revoke_humans`;
  - one only in the current roster gets `needs: password` and nothing is revoked (n2g-D11);
  - one `removed` in the current roster goes in `lock`;
  - a password a human changed from Element after `N` is no roster write, so root cannot see
    it. The answer carries one fixed line saying so, and nothing is guessed (n2g-D19).

- **RM8.** Every other field is the current one: `coordinator`, `coordinator_wanted`, each
  handle's state, kind, `human_text` and creds. `pending` is null.

- **RM9.** Refusals:

  - a `claimed` handle in either roster, naming it;
  - either roster's team other than the restore's.

- **RM10.** `seq` is `max(current.seq, B) + 1` and `rev` is `current.rev + 1`; the roster event
  written in step 5 carries exactly this `rev` (n2g-D12). `restore_epoch` is
  `max(current.restore_epoch, H) + 1`, where `H` is the high-water in
  `root/<team>/restore-epoch.json` (0 when absent, read by `ReadOwnedFile` uid 0, strict
  `{"v": 1, "epoch": n}`). At A 5.1's maximum the merge refuses (n2g-D20).

- **RM11. `--roster-from-backup`** (the current roster unreadable):

  - The backup roster is the base. Every handle in the status record's members raises its
    prefix's counter to at least its seat number, and is `lost` when the base lacks it; none is
    revived.
  - `restore_epoch` is a value from the injected random source in `[0, 500000000)` that differs
    from the backup's and from `H`, and is drawn again until it does. C 3.12.2's "status
    record's copy" does not exist, since `status.Record` holds no epoch; `H` stands in for it,
    and is the stronger bound.
  - LXC records (C 3.9, not built) and the pool's `seat.json` files (owner-writable, so the trust
    rule keeps root from deciding from them) are not read (n2g-D13).
  - The answer names what is unknown.

- **RM12. A pending operation under `--offline`** (RO4) is resolved in the safe direction, and
  the answer names it:

  - `team.apply` is cleared, since step 9's bootstrap is that apply;
  - a member `retire` makes its handle `retired`, in `retire`;
  - any other member operation sends its handle to `revoke`. It is `needs: reissue` when
    `active` in both rosters, and `lost` otherwise.

  Without `--offline`, a pending operation is RV5's refusal.

## 6. Validation (RV, step 01)

Nothing is written before all of these pass.

- **RV1.** No `restore.json`, except under `--resume` or `--discard`.
- **RV2.** No leftover, each named if found:
  - a `db.restore-*` or `db.previous` entry in the state directory;
  - `config.restore.toml` or `restore.block` in `root/<team>/`;
  - a `*.previous` in `ctl/<team>/` (listed, not opened);
  - a loaded `teamchat-restore@<t>` or `teamchat-restore-ctl@<t>` unit.
- **RV3.** The team is applied (its stamp, `team.json` and `server-name`). Its status origin is
  not `migration`; D's verbs and `carried_at` are not built, so `--accept-carry-loss` is not in
  this build (n2g-D9).
- **RV4.** `<N>.tar` is read and passes BK3, and the manifest's `server_name` equals
  `server-name`. On a fresh restore, `meta/<N>` exists too. A resume validates `<N>.tar` and
  the record only: `M`'s prune may have removed `meta/<N>`, whose copy is now
  `db.restore-<N>`. `<N>.tar` survives a resume, because BT4 keeps it and the guard refuses
  every other backup while the record exists.
- **RV5.** Neither roster has a `pending`. A current one's refusal names the verb that completes
  it: `team apply` for `team.apply`, and `member <verb> --team T --handle <h>` for a member op.
  Under `--offline`, the current roster's `pending` is RM12's instead.
- **RV6.** The approval holds the applied `team.json` (`declare.Hold`), so step 9's bootstrap
  cannot be refused by the gate: "an approval is not applied: run team apply first".
- **RV7.** Without `--offline`, the live homeserver must be active and answer `ServerVersion`,
  since step 6 and step 7 need it. A refusal names `--offline` and what it loses (RO1).

## 7. The engine (RS, steps 02-10)

`restore.Run(ctx, deps, req)` runs under the team lock the verb took with RG1's restore guard.
The record `root/<team>/restore.json` (0600, `state.WriteFile`, so it is fsynced) is
`{"v": 1, "team": T, "backup": N, "offline": bool, "pre": M|null, "step": "...", "roster": <merged file>, "lists": {...}, "lost": {"count", "first_ms", "end_ms", "truncated"}|null}`
(n2g-D10). It holds what a resume needs, since the tar in memory dies with the process. Its
steps are `staging`, `staged`, `swap`, `start` and `revert`.

- **RE1. The epoch invariant** (A 5.1, B 2.6 step 08, C13). No request a member can send ever
  reaches a restored database whose roster event holds an epoch other than the merged one.
  SPEC-n3's SY6 and SY7, and n3a's fakehs `RestoreTo` (n3 gap 06, the rollback and the raise
  in one step), rely on this. It holds because:

  - the staging server is reachable only from its own network namespace (RU1, H-C5, H-C15);
  - RC1 writes the roster event before the staging stop;
  - after the stop, the state directory is synced (RS3), so the write is on disk;
  - the staging unit never restarts (RU1, RS3), so the secured copy is never restored over;
  - the block covers every rename (RS6, RS11);
  - RS7 reads the served epoch before anything else is done.

  The epoch never repeats a value a database has served (RM10's high-water, n2g-D20).

- **RS1. Record** (step 03): before anything is created, the record is written at `staging`.

- **RS2. Stage** (step 04): `config.restore.toml` is written (0600). It is the current
  `config.toml` with exactly its one `database_path = ` line set to `db.restore-<N>` in the
  state directory and its one `address = ` line set to `["127.0.0.1", "::1"]`; a config without
  exactly one of each fails. The rendered `database_path` must differ from the live one, and
  `database_backup_path` must be present and unchanged (the spike's (d)). Right before the
  staging unit is started by RU1, `db.restore-<N>` is checked absent fd-relative (`ENOENT`), so
  a path planted after RV2 is refused before Tuwunel can wipe it.

- **RS3. Secure** (step 05). The staging unit's `InvocationID` is read. The secure unit (RU2)
  is run with `restore.secure` (RC1). After it answers, the staging unit's state must show the
  same `InvocationID` and `NRestarts=0`; otherwise step 05 fails. The staging unit is then
  stopped (units `Stop`, SD8 extended) and must be inactive or unloaded (`--collect`). The
  state directory is then `syncfs`ed through its fd: the stopped process's writes are in the
  page cache, and the sync makes them survive a power loss before the swap. Only then is the
  record set to `staged`. RC1 writes the roster event last, and RocksDB recovers a prefix of
  its log, so a roster event that is on disk proves the revocations before it are too. RT3
  proves it on the pinned Tuwunel.

- **RS4. Pre-restore backup** (step 06): `backup.Take(held, keep=[N])`; the record's `pre` is set
  to `M`. Under `--resume` it is skipped when `pre` is set. It is skipped under `--offline`
  (RO3).

- **RS5. Export** (step 07): `restore.export` (RC3) runs against the live homeserver, and its
  count, first, end and truncation are recorded. A resume from `staged` runs it again, so the
  export covers what the live database served meanwhile. It is skipped under `--offline`
  (RO3).

- **RS6. Swap** (step 08), in this order:

  1. `restore-epoch.json` is written (`state.WriteFile`) with the merged epoch, if that is
     above the one it holds (RM10's high-water, n2g-D20);
  2. the record goes to `swap`, and `restore.block` is written;
  3. the live unit is stopped. The live unit and both staging units must then be inactive or
     unloaded, or the step fails before any rename;
  4. the renames of RS16, toward `swapped`;
  5. the child runs `restore.swap` (RC2);
  6. the record goes to `start`, and the block is removed.

- **RS7. Start** (step 09) uses `host.Serve` (SM3), behind RS17's check. Before anything else,
  the child's `restore.check` (RC8) reads the served roster event. Its `restore_epoch` and
  `rev` must be the merged ones; otherwise step 09 fails (RE1). Then comes the normal
  bootstrap, the child's `team.apply` with `Input{Team: team.json}`, the approval and the
  config's registration key.

- **RS8. Clean** (step 10), in this order:

  1. `db.previous` is removed by `RemoveTree` (the trust rule);
  2. `config.restore.toml` is removed;
  3. the child runs `restore.finish` (RC4);
  4. the record is removed;
  5. the status record is refreshed with the event "restored backup N, <count> events lost,
     export at <path>" (`host.Status.Refresh` takes events), or RO5's event when offline.

- **RS9. The answer** is
  `{"v": 1, "team": T, "backup": N, "offline": bool, "pre": M|null, "lost": {"count", "first_ms", "end_ms", "truncated", "path"}|{"exported": false, "since_ms": <N's time>}, "reissue": [...], "return": [...], "password": [...], "unknown": [...], "pending": <RM12's resolution>|null, "notes": [...], "changed": [...]}`.
  `notes` holds RM7's fixed line about Element passwords, and RO5's line when offline.

- **RS10. Failures in steps 03 to 05.**

  1. The staging units are stopped (a unit that is not loaded counts as stopped).
  2. `db.restore-<N>` and `config.restore.toml` are removed, then the record.
  3. The answer is "restore not done" (1).

  If the clean-up itself fails, the record stays at `staging` with both errors.

- **RS11. Failures in steps 06 to 09.** Whatever step 8 swapped is swapped back. A failure in
  step 06 or 07 has swapped nothing, so the record stays `staged` and the team serves. From
  step 08 on, in this order:

  1. the record goes to `revert` (fsync), before anything is touched;
  2. the block is written again;
  3. the live unit is stopped and must be inactive;
  4. the renames of RS16, toward `unswapped`;
  5. the child runs `restore.revert` (RC5);
  6. the block is removed;
  7. the old database is started (RS17) and waited for; under `--offline` it is not started
     (RO6);
  8. the record is set to `staged`, with `pre` kept.

  A failure of the swap-back leaves the record at `revert` and the block in place. It is
  reported as "team <t> is down: restore --resume or --discard after fixing <error>" (1).

- **RS12. `--resume`** reads the record and goes on from it:

  - `staging`: RS10, then "not done";
  - `staged`: from RS4;
  - `swap`: forward. The block is ensured and the live unit stopped, then RS16 toward
    `swapped`, the control swap repeated, then RS7 and RS8;
  - `start`: RS7 and RS8;
  - `revert`: RS11 from its step 2, then "not done", with the record at `staged`.

- **RS13. `--discard`** works at `staging` or `staged` only. It stops the units, and removes
  `db.restore-<N>` (by `RemoveTree`), `config.restore.toml` and the record. It leaves
  `restore-epoch.json`. At `swap`, `start` or `revert` it is refused, naming `--resume`.

- **RS14. `--lost --backup N`** prints `ctl/<team>/restore-<N>-lost.jsonl`. The file is read by
  the trust rule as the `teamchat-ctl` uid, capped at 64 MiB. Each line must be one JSON object,
  and is re-encoded compact, so no control byte reaches a terminal.

- **RS15. Exits.** A refusal is 70. A busy lock is 75. A failure is 1, or 69 for an unreachable
  homeserver. `--lost` takes no lock and writes nothing.

- **RS16. The renames** are made in the state directory's fd by `renameat2(RENAME_NOREPLACE)`.
  Before each one, the directory is read (`d` for `db`, `p` for `db.previous` and `r` for
  `db.restore-<N>`, each a no-follow directory or absent):

  | On disk | Toward `swapped`                 | Toward `unswapped`                  |
  | ------- | -------------------------------- | ----------------------------------- |
  | `d r`   | `db` to `db.previous`, then next | done                                |
  | `p r`   | `db.restore-<N>` to `db`         | `db.previous` to `db`               |
  | `d p`   | done                             | `db` to `db.restore-<N>`, then next |

  Every other state (all three, one alone, none, or an entry that is not a directory) is
  refused, naming what is there, and the record is left as it is. A repeat is harmless. Every
  row and every refusal is a test, with a kill between the two renames in both directions.

- **RS17. No start without a database.** Every start of the live unit by this package (RS7,
  RS11 step 7) is preceded by a check that `db` is a no-follow directory in the state
  directory, and that `restore.block` is absent. Otherwise the start is refused, so no path of
  the engine can make Tuwunel create an empty database and serve it.

## 7a. The offline restore (RO, n2g-D14)

The case backups exist for is a homeserver whose database will not start. The staged copy is
secured without the live server, so every credential guarantee holds. What the live server
gives is only `M` (an undo of a restore whose "before" does not start) and the export (which
cannot be made). `--offline` gives up those two, and nothing else.

- **RO1. The flag.** `restore --team T --backup N --offline`. It is refused (70) when the live
  homeserver answers `ServerVersion` at any probe within 30 s ("the homeserver serves: restore
  without --offline"), so a serving team cannot lose its export by mistake. Without the flag, a
  team that does not answer is RV7's refusal. That refusal names `--offline` and its loss:
  "every event after \<N's time> is lost and not exported; there is no pre-restore backup".
- **RO2. Recorded.** The record holds `"offline": true`. `--resume` and `--discard` take no
  `--offline` (64), and the record decides.
- **RO3. Skipped:** RS4 (`pre` stays null) and RS5 (`lost` stays null). `N` is not pruned,
  since no backup is taken.
- **RO4. Pending.** The current roster's `pending` is resolved by RM12, not refused. The
  backup roster's `pending` is still refused (RV5).
- **RO5. The loss, stated.** The answer's `lost` is `{"exported": false, "since_ms": <N's time>}`,
  and `notes` holds "offline restore: every event after <time> is lost and was not exported; no
  pre-restore backup was taken; the database that would not start was removed". The status
  record's event says the same.
- **RO6. The swap-back** under `--offline` does not start the old database, which did not
  start before. The team stays down with the record at `staged`, reported as "team <t> is
  down: the restored database did not start: <error>; restore --resume or --discard" (1).
- **RO7. The same engine.** Everything else (RE1, RS1 to RS3, RS6 to RS17, RG) is unchanged.
  The block written at RS6 also ends the crash loop that `Restart=always` drives. Tests:
  - the engine over fakes with a unit that crash-loops (states alternate `activating` and
    `failed`), each step and each kill as for the online path;
  - RO1's refusal with a server that answers on the third probe;
  - RT4 on the pinned Tuwunel.

## 8. The child's restore ops (RC, C 3.2.2, 3.3)

- **RC1. `restore.secure`.** Args: `{team_file, port, token, rooms, roster, retire, revoke, revoke_humans, lock}`.

  - The child opens an admin client on `127.0.0.1:<port>` with the given `token`, the tar's
    token, which the restored database knows. It waits up to 60 s for `versions`.
  - **retire:** every handle in `retire` is kicked from each room in `rooms` where it is joined
    or invited, then deactivated.
  - **revoke:** every handle in `revoke` and every human in `revoke_humans` gets
    `ResetPassword` (logout of every device) with a random password that is never stored.
  - **lock:** every human in `lock` gets `SetLocked(true)`.
  - **roster event:** written last. It is `proto.Roster` derived from the roster, `team_file`
    and `rooms` (`apply.DeriveRoster`), validated by A's decoder, and written with `PutState`.
    The staged event's `rev` must be below the roster's, or the op fails.
  - It writes no room account data (B 6 C13), and answers its lines only.

- **RC2. `restore.swap`.** Args: `{team_file, token, rooms, roster}`. For each of `admin.token`,
  `rooms.json` and `roster.json` in `ctl/<team>/`:

  - when `<name>.previous` is absent, the current file is linked to it;
  - the new content is then written with the atomic writer.

  A repeat is idempotent.

- **RC3. `restore.export`.** Args: `{team_file, backup, since_ms}`, on the live homeserver with
  the current token.

  - Every event of the team's rooms with `origin_server_ts > since_ms` is read with `/messages`
    backwards from the end, then written in time order to `restore-<N>-lost.jsonl` (0600,
    atomic).
  - The export stops at 64 MiB with `truncated` set. Reading backwards, it keeps the newest
    events, and `first_ms` is the oldest one kept.
  - It answers `export: {count, first_ms, end_ms, truncated}` (n2g-D15).

- **RC4. `restore.finish`.** Removes the `*.previous` files through the held directory fd.

- **RC5. `restore.revert`.** For each `<name>.previous`, writes its content over `<name>`, then
  removes it. A repeat is idempotent.

- **RC6. Root decodes the answers strictly** (CC6, CO3). An `export` comes only with
  `restore.export`, and `epoch` only with `restore.check`. Every restore op's answer carries
  no handle, credential, main room, view or notice.

- **RC8. `restore.check`.** Args: `{team_file}`, on the live homeserver with the current
  token. It reads the main room's roster event by a direct state request, decodes it by A's
  decoder, and answers `epoch: {restore_epoch, rev}` (RS7, RE1).

- **RC7. The secure child's start.** `checkStarted` also accepts a supplementary group list
  that holds only its own gid, which systemd's `initgroups` for `User=` produces (n2g-D16).
  Everything else is unchanged. The environment is cleared by `/usr/bin/env -i PATH=...` in the
  unit's command.

## 9. The units (RU, SD10)

- **RU1. The staging unit** is `teamchat-restore@<t>.service` (`paths.RestoreUnit`), run as
  `/usr/bin/systemd-run --unit=<it> --collect --service-type=notify --quiet`.
  - Its properties are each line of `host.BaseSandbox`, the base unit's `[Service]` sandbox of
    C 3.2.3 held once as data (n2g-D17). These are the identity, hardening and deny lines
    (`DynamicUser` to `InaccessiblePaths`, with `UMask`, `IPAddressDeny=any` and
    `SocketBindDeny=any`). They are not the lifecycle lines (`Type`, `NotifyAccess`,
    `WatchdogSec`, `Timeout*`, `Restart*`), nor `LoadCredential` and `Environment`.
  - Its own lifecycle is `Restart=no`, `NotifyAccess=main` and `TimeoutStopSec=330`. A restart
    would run `--restore-backup` again over the secured copy and wipe it (the spike's (b)). A
    test asserts the argv has `Restart=no` and no other `Restart` line.
  - It runs as `DynamicUser=yes`, `User=teamchat-<t>` and `StateDirectory=teamchat/<t>`, with
    `PrivateNetwork=yes`.
  - Its credential is
    `LoadCredential=config.toml:/var/lib/teamchat-admin/<t>/config.restore.toml`, and
    `TUWUNEL_CONFIG=/run/credentials/<it>/config.toml` (n2g-D18).
  - It may reach `IPAddressAllow=127.0.0.1/32 ::1/128` and `SocketBindAllow=tcp:<port>`.
  - Its command is the binary that `units.json`'s version names, with `--restore-backup <N>`.
- **RU2. The secure unit** is `teamchat-restore-ctl@<t>.service`, run as
  `systemd-run --pipe --wait --collect --quiet`. Its properties are
  `JoinsNamespaceOf=<staging unit>`, `PrivateNetwork=yes`, `User=teamchat-ctl`,
  `Group=teamchat-ctl`, `WorkingDirectory=/`, `NoNewPrivileges=yes`, `IPAddressDeny=any` and
  `IPAddressAllow=127.0.0.1/32 ::1/128`. Its command is
  `/usr/bin/env -i PATH=/usr/bin:/bin <self> ctl-child`, with the request on stdin.
  - The answer is judged as `ExecChild` judges it: capped, exit and answer agreeing.
  - Both argvs are pure functions, so the properties are asserted in tests (3.14).
- **RU3.** Every `systemd-run` goes through `execx` with `CleanEnv` and `PATH=/usr/bin:/bin`,
  `LANG=C`.
- **SD10.** `units.Manager` gains `Kill(unit, whom, signal)` (`KillUnit`). Its state read gains
  `InvocationID` (16 bytes, shown as hex), `NRestarts` and `Result`, each decoded strictly.
  `checkUnit` accepts the server and the two restore unit names, and nothing else.

## 10. The guard (RG, C 3.12.2 "A kill or power loss")

- **RG1.** `teamlock.Take` takes a `Guard` (`func(paths.TeamID) error`), and nil is refused. The
  guard runs once the flock is held. Its error is returned after the lock is released.

- **RG2. `restore.Guard(root)`** refuses (exit 70, `restore.PendingError`) when anything named
  `restore.json` exists in `root/<team>/` (`Lstat`, so a link counts too). The refusal names
  `restore --team T --resume` and `--discard`.

- **RG3.** The guard is passed by:

  - `host.Apply` and `host.Remove`, through `Deps.Guard`;
  - every member and role verb;
  - `backup`.

  `restore` passes its own: a fresh restore refuses as RG2, and `--resume` and `--discard`
  need the record. The health tick passes `teamlock.Unguarded` and refuses only its restart
  (HB3).

## 11. Wiring (RW)

- **RW1.** `wire` builds `backup.Deps` from the units `Manager` (with `Kill`), `hostnet.Readiness`,
  the clock and the ctl uid. `host.Deps.Backup` is `backup.Take`, and health gets
  `backup.States` and `restore.Records`.
- **RW2.** `System.Restore` is a seam. In production it answers `restore.ErrUnproven` ("restore
  is not enabled: host checks H-C5 and H-C15 have not passed (DC8)"), so the verb exits 70
  before it takes the lock (n2g-D8). Tests build the engine over fakes.

## 12. Real Tuwunel (RT, `-tags tuwunel`, `FAKEHS_TUWUNEL_BIN`)

- **RT1.** A real server with `admin_signal_execute`, given `SIGUSR2`, yields a meta number that
  `backup`'s wait reads, and the tar pair validates.

- **RT2.** `--restore-backup N` into a fresh `db.restore-<N>` serves the restored database while
  the original is untouched. `restore.secure`, run against it, leaves:

  - a member reissued after `N` refused;
  - a human reset after `N` refused;
  - a pool seat's token from `N` refused;
  - every `teamchat.floor` as `N` holds it;
  - the roster event with the raised `restore_epoch`.

  It also restores into a target planted non-empty and finds the plant gone, which pins the
  hazard that RS2's check defends.

- **RT3. The epoch survives the stop** (RE1). After RT2's secure, the staged server is stopped
  once by SIGTERM and once by SIGKILL. Each time it is started plainly (no `--restore-backup`)
  on the same path. The first answered state request gives the merged `restore_epoch` and
  `rev`, and a revoked member's old token is refused.

- **RT4. Offline** (RO). The live database is made unable to open (its `CURRENT` file
  truncated). The engine's real-process parts then restore `N` into a fresh path, secure it,
  rename it into place, and the server serves the restored database with the raised epoch.

## 13. Decisions where the design is silent or wrong

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n2g-D1  | The tar carries a manifest `backup.json` (team, number, time, server name), so a tar renamed to another number or copied from another team is refused, and the restore knows `N`'s time without reading the homeserver's files.                                                                                                                                                                                                                                                                                                                                     |
| n2g-D2  | Root reads Tuwunel's numbers from `meta/`, resolved by `openat2` with no symlinks from a root-owned fd (the trust rule). A compromised homeserver could hide a number, which costs its own team's tars only (C 3.2.2: it can forge its team anyway).                                                                                                                                                                                                                                                                                                                |
| n2g-D3  | The last run is a root-only record, `backups/last-run.json`, written by every run, timer or not. The design names "the record" without a format.                                                                                                                                                                                                                                                                                                                                                                                                                    |
| n2g-D4  | The backup never starts a homeserver. Only `team apply` does, for step 6.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| n2g-D5  | `backup --all` runs the teams in sequence and reports on stderr only, as `health --all` does.                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| n2g-D6  | The backup's own JSON answer and the `backups` list are this slice's formats; ccy-teams reads them (C 3.13).                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| n2g-D7  | DC4 is honoured by the caller class and the absence of any NOPASSWD rule. The code does not inspect `SUDO_*`, which a root caller sets as it likes.                                                                                                                                                                                                                                                                                                                                                                                                                 |
| n2g-D8  | Restore is refused in production until H-C5 and H-C15 pass (DC8, Task 2.5).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| n2g-D9  | A migration-origin team's restore is refused until D's verbs exist, so `--accept-carry-loss` waits for them.                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| n2g-D10 | `restore.json` holds the merged roster, the lists and the export's result, so a resume never recomputes a merge (a second merge would raise `seq` and the epoch again). The pre-restore backup keeps `N.tar` from its prune, because the swap needs its token and rooms.                                                                                                                                                                                                                                                                                            |
| n2g-D11 | A handle only in the backup roster is retired. A human only in the current roster needs a password but is not revoked, since it has no account to revoke.                                                                                                                                                                                                                                                                                                                                                                                                           |
| n2g-D12 | The secured roster event's `rev` is exactly the live `rev` + 1, which root fixes, so no `rev` comes back from the child.                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| n2g-D13 | `--roster-from-backup` raises counters only from the status record. LXC records are not built, and the pool is owner-writable.                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| n2g-D14 | The design's restore needs a serving homeserver (`M` and the export), yet the case backups exist for is a database that will not start. The offline restore (RO) is built. Every credential guarantee comes from the staged copy, which needs no live server; only `M` and the export are given up, and the answer says so. **The owner confirms only the defaults:** the flag's name `--offline`; the 30 s "not answering" probe; a pending operation resolved by RM12 rather than refused; and the database that would not start removed at RS8 rather than kept. |
| n2g-D15 | The export is capped at 64 MiB, with `truncated` in the answer.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| n2g-D16 | The child's start check allows its own gid as the one supplementary group (systemd `User=`).                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| n2g-D17 | The base unit's sandbox lines are one Go list. The play's template must be rendered from it (gate A, Task 2.5), so the staging unit cannot drift from the base unit. The list holds identity, hardening and deny lines only; the lifecycle lines stay per unit, since the base unit's `Restart=always` would let a staging restart wipe the secured copy.                                                                                                                                                                                                           |
| n2g-D18 | The staging unit names its credential path in full: a transient unit's command line expands no `%d`. H-C5 confirms it.                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| n2g-D19 | A password a human changed from Element after `N` is no roster write, so root cannot know it. The answer carries one fixed line saying the old password is back for any such human; nobody is revoked on a guess.                                                                                                                                                                                                                                                                                                                                                   |
| n2g-D20 | `root/<team>/restore-epoch.json` is the highest epoch any restore put in place, written at RS6 before a secured database can serve, and kept by `--discard`. Without it, an attempt that served E+1, was swapped back and discarded would let the next restore merge E+1 again from another database, and a member paused across both would keep a stale `since` (H-C14).                                                                                                                                                                                           |
