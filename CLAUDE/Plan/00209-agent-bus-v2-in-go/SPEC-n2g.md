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
- A `SIGUSR2` before the handler is installed kills the process (H 5 says a PID 1 drops it). So
  root signals only a homeserver that is active and answers.
- `tuwunel --restore-backup <n>` restores into the configured `database_path` and then goes on
  serving. A missing `<n>` exits 1 (`Backup #<n> not found; available: ...`).
- A target directory that is not empty is wiped without a word, so the staging path must be new.
- The live database stays byte-identical, and the restored server's next backup continues the
  shared numbering.

Each expectation below is numbered and gets a Go test written first and seen to fail.

**What this slice is.** The internal backup function (step 6 of `team apply` included) and the
verbs `backup` and `backups`; the health tick's real backups and restore record; the restore
engine of C 3.12.2 with its record, resume and discard, its child operations and its transient
units rendered as code; and the guard every lock-taking verb runs.

**What it is not (owner gates).**

- **Gate A.** No play, task, unit file, timer, tmpfiles entry or meta-deploy entry. The backup
  units' text exists as code only (BK10), as n2d's drop-ins do.
- **Gate B.** Nothing gives a process a pool seat's password.
- **DC8.** The production wiring refuses `restore` (exit 70) until H-C5 and H-C15 have passed,
  as PLAN.md Task 2.5 says (n2g-D8). The engine is built and proven over fakes. The parts that
  need no systemd run against the pinned Tuwunel (RT).

**Trust rule.**

- Root never decides from a file another account can write.
- Every file root reads is opened `O_RDONLY|O_NOFOLLOW|O_NONBLOCK`, `fstat`ed, regular, and
  capped. That is `state.ReadFile`, and `ReadOwnedFile` for root's own files (uid 0).
- A directory root lists or changes inside the homeserver's tree (`/var/lib/private/teamchat/<t>/`,
  owned by the dynamic uid) is opened `O_DIRECTORY|O_NOFOLLOW`, and every entry is reached
  relative to that fd.
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

## 2. The internal backup (BT, C 3.12.1)

`backup.Take(ctx, deps, held *teamlock.Lock, keep []int64) (Result, error)` is the internal
function. Its callers are `team apply` step 6, `backup` and the restore's step 6. It never takes
a lock: the held lock is its argument (C 3.4).

- **BT1. Ready first.** The unit must be `active` and `ServerVersion` must answer (H 5, H-C4).
  If not, the backup fails ("the homeserver is not running"). It never starts the unit: that is
  `team apply`'s `startForBackup`.
- **BT2. The signal.** Before signalling, `Take` reads the highest number in
  `HomeserverPrivateState(t)/backups/meta/`: only regular files with decimal names; a missing
  directory is none. It then sends `SIGUSR2` with D-Bus `KillUnit(unit, "main", 12)` (DC17,
  SD10). It polls once a second, up to 60 s, for a higher number whose meta file's first line
  is a unix time. A meta file is read relative to the meta dir's fd, no-follow, regular,
  capped at 64 KiB.
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
  - `staging`, `swap` or `start`: "restore interrupted", fix `restore --team T --resume`.

  The tick copies it into the record's `restore` field. With no `restore.json` the field is null.

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
  only by password sudo (n2g-D7).

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
- **RM4.** A handle `active` in both with `cred_seq > B` gets `needs: reissue`, and goes in
  `revoke` and `reissue`.
- **RM5.** Every `active` handle with `cred` `password` gets `needs: reissue`, whatever its
  `cred_seq`, and goes in `revoke` and `reissue` (K13).
- **RM6.** A handle that is `parked` or `lost` in the current roster, and present in the backup's,
  goes in `revoke`: the restored account holds an older credential.
- **RM7.** Humans:
  - one with `revoked_seq > B` gets `needs: password` and goes in `password`;
  - if the backup roster has that human too, it also goes in `revoke_humans`;
  - one only in the current roster gets `needs: password` and nothing is revoked (n2g-D11);
  - one `removed` in the current roster goes in `lock`.
- **RM8.** Every other field is the current one: `coordinator`, `coordinator_wanted`, each
  handle's state, kind, `human_text` and creds. `pending` is null.
- **RM9.** Refusals:
  - a `claimed` handle in either roster, naming it;
  - either roster's team other than the restore's.
- **RM10.** `seq` is `max(current.seq, B) + 1` and `rev` is `current.rev + 1`; the roster event
  written in step 5 carries exactly this `rev` (n2g-D12). `restore_epoch` is
  `current.restore_epoch + 1`; at A 5.1's maximum the merge refuses.
- **RM11. `--roster-from-backup`** (the current roster unreadable):
  - The backup roster is the base. Every handle in the status record's members raises its
    prefix's counter to at least its seat number, and is `lost` when the base lacks it; none is
    revived.
  - `restore_epoch` is a value from the injected random source in `[0, 500000000)` that differs
    from the backup's.
  - LXC records (C 3.9, not built) and the pool's `seat.json` files (owner-writable, so the trust
    rule keeps root from deciding from them) are not read (n2g-D13).
  - The answer names what is unknown.

## 6. Validation (RV, step 01)

Nothing is written before all of these pass.

- **RV1.** No `restore.json`, except under `--resume` or `--discard`.
- **RV2.** No leftover, each named if found:
  - a `db.restore-*` entry in the state directory;
  - `config.restore.toml` or `restore.block` in `root/<team>/`;
  - a `*.previous` in `ctl/<team>/` (listed, not opened).
- **RV3.** The team is applied (its stamp, `team.json` and `server-name`). Its status origin is
  not `migration`; D's verbs and `carried_at` are not built, so `--accept-carry-loss` is not in
  this build (n2g-D9).
- **RV4.** `<N>.tar` is read and passes BK3, and the manifest's `server_name` equals
  `server-name`. `meta/<N>` exists.
- **RV5.** Neither roster has a `pending`. A current one's refusal names the verb that completes
  it: `team apply` for `team.apply`, and `member <verb> --team T --handle <h>` for a member op.
- **RV6.** The approval holds the applied `team.json` (`declare.Hold`), so step 9's bootstrap
  cannot be refused by the gate: "an approval is not applied: run team apply first".
- **RV7.** The live homeserver is active and answers: step 6 and step 7 need it. A team whose
  homeserver cannot serve is refused in this build, naming the gap (n2g-D14, to the owner).

## 7. The engine (RS, steps 02-10)

`restore.Run(ctx, deps, req)` runs under the team lock the verb took with RG1's restore guard.
The record `root/<team>/restore.json` (0600, `state.WriteFile`, so it is fsynced) is
`{"v": 1, "team": T, "backup": N, "pre": M|null, "step": "...", "roster": <merged file>, "lists": {...}, "lost": {"count", "end_ms", "truncated"}|null}`
(n2g-D10). It holds what a resume needs, since the tar in memory dies with the process.

- **RS1. Record** (step 03): before anything is created, the record is written at `staging`.

- **RS2. Stage** (step 04): `config.restore.toml` is written (0600). It is the current
  `config.toml` with exactly its one `database_path = ` line set to `db.restore-<N>` in the
  state directory and its one `address = ` line set to `["127.0.0.1", "::1"]`; a config without
  exactly one of each fails. The staging unit is then started by RU1.

- **RS3. Secure** (step 05): the secure unit (RU2) is run with `restore.secure` (RC1). After it
  answers, the staging unit is stopped (units `Stop`, SD8 extended) and the record set to
  `staged`.

- **RS4. Pre-restore backup** (step 06): `backup.Take(held, keep=[N])`; the record's `pre` is set
  to `M`. Under `--resume` it is skipped when `pre` is set.

- **RS5. Export** (step 07): `restore.export` (RC3) runs against the live homeserver, and its
  count and end are recorded.

- **RS6. Swap** (step 08), in this order:

  1. the record goes to `swap` and `restore.block` is written;
  2. the live unit is stopped;
  3. in the state directory's fd, `db` becomes `db.previous`, then `db.restore-<N>` becomes `db`,
     each with `renameat2(RENAME_NOREPLACE)`;
  4. the child runs `restore.swap` (RC2);
  5. the record goes to `start`, and the block is removed.

  Each rename is checked against the directory first, so a repeat is harmless.

- **RS7. Start** (step 09) uses `host.Serve`: start, wait ready (60 s), and the bound addresses
  of `team.json`. Then comes the normal bootstrap, the child's `team.apply` with
  `Input{Team: team.json}`, the approval and the config's registration key.

- **RS8. Clean** (step 10), in this order:

  1. `db.previous` is removed fd-relative, never following a link;
  2. `config.restore.toml` is removed;
  3. the child runs `restore.finish` (RC4);
  4. the record is removed;
  5. the status record is refreshed with the event "restored backup N, <count> events lost,
     export at <path>" (`host.Status.Refresh` takes events).

- **RS9. The answer** is
  `{"v": 1, "team": T, "backup": N, "pre": M, "lost": {"count", "end_ms", "truncated", "path"}, "reissue": [...], "return": [...], "password": [...], "unknown": [...], "changed": [...]}`.

- **RS10. Failures in steps 03 to 05.**

  1. The staging units are stopped (a unit that is not loaded counts as stopped).
  2. `db.restore-<N>` and `config.restore.toml` are removed, then the record.
  3. The answer is "restore not done" (1).

  If the clean-up itself fails, the record stays at `staging` with both errors.

- **RS11. Failures in steps 06 to 09.** Whatever step 8 swapped is swapped back:

  1. the block is written again;
  2. the unit is stopped;
  3. `db` goes back to `db.restore-<N>` and `db.previous` to `db`;
  4. the child runs `restore.revert` (RC5);
  5. the block is removed;
  6. the old database is started and waited for;
  7. the record is set to `staged`, with `pre` kept.

  A failure of the swap-back leaves the record and the block in place, and is reported as "team
  <t> is down: restore --resume or --discard after fixing <error>" (1).

- **RS12. `--resume`** reads the record and goes on from it:

  - `staging`: RS10, then "not done";
  - `staged`: from RS4;
  - `swap`: forward. The block is ensured, the live unit stopped, the renames not made are
    made, the control swap repeated, then RS7 and RS8;
  - `start`: RS7 and RS8.

- **RS13. `--discard`** works at `staging` or `staged` only. It stops the units, and removes
  `db.restore-<N>`, `config.restore.toml` and the record. At `swap` or `start` it is refused,
  naming `--resume`.

- **RS14. `--lost --backup N`** prints `ctl/<team>/restore-<N>-lost.jsonl`. The file is read by
  the trust rule as the `teamchat-ctl` uid, capped at 64 MiB. Each line must be one JSON object,
  and is re-encoded compact, so no control byte reaches a terminal.

- **RS15. Exits.** A refusal is 70. A busy lock is 75. A failure is 1, or 69 for an unreachable
  homeserver.

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
  - The export stops at 64 MiB with `truncated` set.
  - It answers `export: {count, end_ms, truncated}` (n2g-D15).

- **RC4. `restore.finish`.** Removes the `*.previous` files through the held directory fd.

- **RC5. `restore.revert`.** For each `<name>.previous`, writes its content over `<name>`, then
  removes it. A repeat is idempotent.

- **RC6. Root decodes the answers strictly** (CC6, CO3). An `export` comes only with
  `restore.export`, and every restore op's answer carries no handle, credential, main room,
  view or notice.

- **RC7. The secure child's start.** `checkStarted` also accepts a supplementary group list
  that holds only its own gid, which systemd's `initgroups` for `User=` produces (n2g-D16).
  Everything else is unchanged. The environment is cleared by `/usr/bin/env -i PATH=...` in the
  unit's command.

## 9. The units (RU, SD10)

- **RU1. The staging unit** is `teamchat-restore@<t>.service` (`paths.RestoreUnit`), run as
  `/usr/bin/systemd-run --unit=<it> --collect --service-type=notify --quiet`.
  - Its properties are each line of `host.BaseSandbox`, the base unit's `[Service]` sandbox of
    C 3.2.3 held once as data (n2g-D17).
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
- **SD10.** `units.Manager` gains `Kill(unit, whom, signal)` (`KillUnit`). `checkUnit` accepts
  the server and restore unit names.

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

## 13. Decisions where the design is silent or wrong

| ID      | Decision                                                                                                                                                                                                                                                                 |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| n2g-D1  | The tar carries a manifest `backup.json` (team, number, time, server name), so a tar renamed to another number or copied from another team is refused, and the restore knows `N`'s time without reading the homeserver's files.                                          |
| n2g-D2  | Root reads Tuwunel's numbers from `meta/` relative to a no-follow dir fd. A compromised homeserver could hide a number, which costs its own team's tars only (C 3.2.2: it can forge its team anyway).                                                                    |
| n2g-D3  | The last run is a root-only record, `backups/last-run.json`, written by every run, timer or not. The design names "the record" without a format.                                                                                                                         |
| n2g-D4  | The backup never starts a homeserver. Only `team apply` does, for step 6.                                                                                                                                                                                                |
| n2g-D5  | `backup --all` runs the teams in sequence and reports on stderr only, as `health --all` does.                                                                                                                                                                            |
| n2g-D6  | The backup's own JSON answer and the `backups` list are this slice's formats; ccy-teams reads them (C 3.13).                                                                                                                                                             |
| n2g-D7  | DC4 is honoured by the caller class and the absence of any NOPASSWD rule. The code does not inspect `SUDO_*`, which a root caller sets as it likes.                                                                                                                      |
| n2g-D8  | Restore is refused in production until H-C5 and H-C15 pass (DC8, Task 2.5).                                                                                                                                                                                              |
| n2g-D9  | A migration-origin team's restore is refused until D's verbs exist, so `--accept-carry-loss` waits for them.                                                                                                                                                             |
| n2g-D10 | `restore.json` holds the merged roster, the lists and the export's result, so a resume never recomputes a merge (a second merge would raise `seq` and the epoch again). The pre-restore backup keeps `N.tar` from its prune, because the swap needs its token and rooms. |
| n2g-D11 | A handle only in the backup roster is retired. A human only in the current roster needs a password but is not revoked, since it has no account to revoke.                                                                                                                |
| n2g-D12 | The secured roster event's `rev` is exactly the live `rev` + 1, which root fixes, so no `rev` comes back from the child.                                                                                                                                                 |
| n2g-D13 | `--roster-from-backup` raises counters only from the status record. LXC records are not built, and the pool is owner-writable.                                                                                                                                           |
| n2g-D14 | **To the owner.** The design's restore needs a serving homeserver (`M` and the export). A team whose database will not start cannot be restored in this build. The proposal is a flag that skips `M` and the export, with the loss stated.                               |
| n2g-D15 | The export is capped at 64 MiB, with `truncated` in the answer.                                                                                                                                                                                                          |
| n2g-D16 | The child's start check allows its own gid as the one supplementary group (systemd `User=`).                                                                                                                                                                             |
| n2g-D17 | The base unit's sandbox lines are one Go list. The play's template must be rendered from it (gate A, Task 2.5), so the staging unit cannot drift from the base unit.                                                                                                     |
| n2g-D18 | The staging unit names its credential path in full: a transient unit's command line expands no `%d`. H-C5 confirms it.                                                                                                                                                   |
