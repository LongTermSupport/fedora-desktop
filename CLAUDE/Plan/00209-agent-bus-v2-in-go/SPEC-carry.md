# SPEC addendum: the sealed carry of a team (issue #324, beside SPEC-n2g)

**Status: specified, with the coordinator's decisions applied (section 12); no question is
open. Nothing here is built.** It sits beside
`CLAUDE/Plan/00209-agent-bus-v2-in-go/SPEC-n2g.md`. Sources: the restore decisions note ("The
finding that settles Q2 and Q7", points 1-5, the residual, Q1, Q5, next actions 3 and 4),
SPEC-n2g (the trust rule, RM1-RM11, RV2, RV3, RS15-RS18, RG1-RG3), and the code of F44
(`src-go/internal/teamchat/...`); every `file:line` below is relative to
`src-go/internal/teamchat/`.

Each expectation is numbered and gets a Go test written first and seen to fail.

**What the carry is.** Three verbs beside `restore`, taking nothing from it: `team seal`
(terminal), `team export` (the sealed export, and the drill export), `team import` (onto an
empty guest), and one check verb, `team carry-check`. It moves a team's *current* state, which
the source never changes again, so no merge runs (RM1-RM12 do not apply), the roster `seq`
continues, `restore_epoch` is unchanged, and no credential is revoked or reissued.

**What it is not.**

- Not a restore. DC8, n2g-D8 and RW2 are unchanged; `restore` stays refused in production. The
  carry is outside DC8 (O7). A nightly `<n>.tar` is never a carry input (SI5).
- Not `--onto-fresh`: the import accepts only an archive whose source was sealed (or a drill
  archive, on a scratch guest, with `--drill`).
- **Gate A.** No play, task, unit file or meta-deploy entry. Where the carry needs the base
  unit to honour a file (SC4), that is a requirement on Task 2.5's template, stated, not built.

**Trust rule (additions to n2g's).**

- The archive is read only from `paths.CarryExport(t)` (below), a regular file owned by uid 0,
  mode 0600, in a directory `paths.CarryDir` owned by uid 0, mode 0700, opened
  `O_RDONLY|O_NOFOLLOW|O_NONBLOCK` and `fstat`ed: root reads only what root placed. The driver
  places it there as root (its copy); a file of any other owner or mode is refused, never
  fixed.
- The archive is streamed, never held whole, so `state.ReadOwnedFile` (`state/state.go:90`)
  cannot serve; `state` gains `OpenOwnedFile(path, uid) (*os.File, error)` with the same checks
  (`openIn`, `read`, `state/state.go:146`, `:166`), returning the opened file.
- Every placed file is created by root, uid 0, before the team's first start on the target.
  The homeserver tree is placed root-owned and is handed to the dynamic uid by systemd at the
  first start (SH1, a host check).
- Root writes under `ctl/<team>/` only through the `teamchat-ctl` child (`carry.place`, SC-C2);
  root makes the directory itself, as apply's first-apply files do (`host/apply.go:562`).
- No error quotes a member's content; a member name is quoted only after it has passed the name
  grammar (SX4).

## 0. Names (CN, in `paths`)

`paths` is the one source of each name below (Q6's verdict). It is an image input: the commit
that adds them carries the CCY version bump.

- **CN1. Records.** `SealRecord(t)` is `AdminState(t)/sealed.json`; `DrillRecord(t)` is
  `AdminState(t)/drill.json`; `CarriedRecord(t)` is `AdminState(t)/carried.json`.

- **CN2. The carry directory.** `CarryDir` is `AdminStateRoot + "/_carry"`. Its name fails the
  team id grammar (`paths/ids.go:71`, `^[a-z][a-z0-9-]{0,21}$`), so every walk of
  `AdminStateRoot` that parses team ids skips it, as it skips `_lxc` and `_sets`
  (`host/apply.go:458-462`). `CarryExport(t)` is `CarryDir/<t>.tar`, `CarryDrillExport(t)` is
  `CarryDir/<t>.drill.tar`, `CarryPart(t, drill)` is the same with `.part` appended,
  `CarryImportRecord(t)` is `CarryDir/<t>.import.json`, and `CarryStaging(t)` is
  `CarryDir/<t>.staging/`.

- **CN3. The homeserver staging name.** `HomeserverCarryStaging(t)` is
  `HomeserverPrivateStateRoot + "/.carry-<t>"`: on the same file system as the target, so its
  placement is one `renameat2(RENAME_NOREPLACE)`; its leading dot fails the team grammar.

- **CN4. A team's state roots.** `TeamStateRoots(t) []string` is the closed list of every path
  that holds state of team `t`, and the one list SI2, the lab's purge checker (Q5) and the tests
  use. From `paths` today it is:

  | Root                                                                                          | Where it is defined      |
  | --------------------------------------------------------------------------------------------- | ------------------------ |
  | `AdminState(t)` `/var/lib/teamchat-admin/<t>`                                                 | `paths/paths.go:262`     |
  | `CtlState(t)` `/var/lib/teamchat-ctl/<t>`                                                     | `paths/paths.go:310`     |
  | `HomeserverState(t)` `/var/lib/teamchat/<t>` (systemd's link)                                 | `paths/paths.go:254`     |
  | `HomeserverPrivateState(t)` `/var/lib/private/teamchat/<t>`                                   | `paths/paths.go:257`     |
  | `TLSDir(t)` `/var/lib/teamchat-tls/<t>`                                                       | `paths/paths.go:300`     |
  | `StatusTeamDir(t)` `/var/lib/teamchat-status/teams/<t>` (stamp, status, approval, lxc-online) | `paths/paths.go:329-341` |
  | `LXCRecord(t)` `/var/lib/teamchat-status/lxc/<t>.json`                                        | `paths/paths.go:344`     |
  | `ServerDropInFile(t)`'s directory                                                             | `paths/host.go:35`       |
  | `TLSDropInFile(t)`'s directory                                                                | `paths/host.go:38`       |
  | `HomeserverCarryStaging(t)`, `CarryStaging(t)`, `CarryImportRecord(t)`                        | CN2, CN3                 |

  The team lock file (`paths.TeamLock`, `paths/paths.go:322`) is not state: it is never
  unlinked (`teamlock/teamlock.go:1-5`) and lives in `/run`. A test holds the list complete: it
  walks every `func(TeamID) string` in `paths` whose result is not under one of the roots above
  and fails naming it, so a new per-team path must join the list or be named in the test's
  exemptions (units, sessions, Element and member-side names).

- **CN5. Constants and the one setting.** `carry.MaxAge` is 72 h and `carry.MaxAgeDrill` is
  24 h (O1); `carry.ExportFormat` is the manifest's `format` integer, 1 today (SX8). These are
  constants of this repository, not flags, not configuration, and not a driver setting.
  `carry.MaxArchive`, the archive cap (O4), is the one declared setting: the inventory variable
  `teamchat_carry_max_archive_bytes`, default 1 GiB (1073741824), which the play writes into
  the host's teamchat-admin configuration and both export and import read. A value that is
  absent, unreadable, zero or negative refuses the verb (exit 78) and names the setting; there
  is no built-in fallback. A source or archive over the cap is refused loudly (exit 70), naming
  the measured size and the cap, both in bytes (SX4, SI).

## 1. The seal (SC, `team seal --team T`)

`carry.Seal(ctx, deps, team)` under the team lock (`host.PlayWait`, 10 min) with the guard of
SG3. Root's class only (77 otherwise, BV4's seam `admin.Process`/`classOf`, `admin/class.go:36`).

- **SC1. Validation, nothing written before it passes** (refusal 70 unless said):
  - the team is applied: its stamp, `team.json`, `server-name` and `units.json` (RV3's
    `readApplied`, `restore/validate.go:156`). Its origin is not `migration`: the origin is
    decided by the presence of the migration's `state.json`, `paths.MigrationDir(t)` +
    `/state.json` (`paths/paths.go:275`), a regular file meaning `migration`, absent meaning
    `v2`, anything else an error (`health.Stamps.origin`, `health/files.go:101-113`, reached by
    `Stamps.Origin`, `:59`). The seal reads that file through that function, never the status
    record: the record's `origin` field is derived from it (`health/health.go:371-377`,
    `health/files.go:132-140`) and is only where an operator sees it (`status.json` of the
    team's status directory). A migration-origin team is refused: D's verbs are not built (as
    n2g-D9; O8);
  - no restore and no leftover of one: RG2's record (`restore/guard.go:32-44`) and RV2's list
    (`restore/validate.go:110`): `db.restore-*`, `db.previous`, `config.restore.toml`,
    `restore.block`, a `*.previous` in `ctl/<team>/`, a loaded restore unit;
  - no `drill.json` (SD5) and no `CarryImportRecord(t)`;
  - the roster (`ReadOwnedFile` as the ctl uid, I-B6) decodes by `apply.DecodeRosterFile`
    (`apply/files.go:262`) and has no `pending` (RV5's wording: the verb that completes it);
  - the homeserver is active and answers `ServerVersion` (the tail, SC2, needs it). Not
    answering is 70, naming `team remove` as the only other way out;
  - an existing `sealed.json` at step `sealed` is no refusal: SC7.
- **SC2. The tail.** The child's new op `carry.tail` (SC-C1) reads, on the live server with the
  current token, for each room of `rooms.json` its newest event (`event_id`,
  `origin_server_ts`), and the main room's roster event (`restore_epoch`, `rev`) as RC8 does
  (`admin/serverestore.go:395`). The event's `rev` must equal `roster.json`'s `rev` and its
  `restore_epoch` the roster's `RestoreEpoch`; otherwise 70 ("the roster event and roster.json
  disagree: run team apply first"). A server lost mid-read is 69 (RS15).
- **SC3. The record, before any stop.** `sealed.json` (0600, `state.WriteFile`, fsynced) is
  written at step `sealing`:
  `{"v": 1, "team": T, "step": "sealing", "tail": [{"room": "<purpose>", "event_id": "...", "ts_ms": n}...], "started_ms": n}`.
  From this write on, SG1 and SG3 refuse every other verb but `team remove`.
- **SC4. Stop and disable.** The TLS terminator, then the homeserver, each stopped then
  disabled, as `team remove` does (`host/remove.go:91-116`, refactored to an exported
  `host.StopTeamUnits`). Each must then be stopped by RS18 (`inactive`, `failed` or not
  loaded); `activating` fails the step. `restore.block` is then written (0600): the base unit's
  start block (RS17), so no start reaches the database from here on. **Requirement on Task 2.5,
  not verified:** the rendered base unit holds `AssertPathExists=!<RestoreBlock>`; until it
  does, the block binds only this package's own starts (RS17) and a hand `systemctl start`
  would reopen the gap. There is no `systemctl mask` (O3).
- **SC5. The facts, read after the stop.** Under the lock nothing else writes them:
  - roster `seq`, `rev`, `RestoreEpoch` (from `roster.json`);
  - `restore-epoch.json`'s `{epoch, min_backup}` (`restore.DecodeEpochFloor`,
    `restore/epoch.go:31`; absent reads `{0, 0}`, as RM10);
  - `server_name` from `AdminState(t)`'s `server-name` file (`paths.ServerNameFile`, root's
    own file by the trust rule; `units.json` holds no server name), and the port and the
    Tuwunel version from `units.json` (`host.DecodeUnits`, `host/records.go`);
  - the software digest (`host.Deps.Software`, `host/apply.go:183`) and the clock.
- **SC6. Off the host's books.** The stamp is removed, then the status record
  (`host.Deps.Status.Remove`, as `host/remove.go:177-183`). With no stamp, `health.Stamps`
  skips the team (`health/files.go:45-47`), so the tick (`health/health.go:165-182`) and
  `backup --all` (`admin/backupverbs.go:77`) never see it again. `approved.json` stays: the
  export carries it (SX2).
- **SC7. Sealed.** `sealed.json` is rewritten at step `sealed`:
  `{"v": 1, "team": T, "step": "sealed", "sealed_ms": n, "server_name": "...", "port": n, "tuwunel": "<version>", "software": "<digest>", "roster": {"seq": n, "rev": n, "restore_epoch": n}, "epoch_floor": {"epoch": n, "min_backup": n}, "tail": [...]}`
  and the answer is that document with `"changed": [...]` (exit 0). A `team seal` of a team
  already `sealed` answers the same document, changes nothing, and exits 0.
- **SC8. Terminal.** No verb removes `sealed.json` but `team remove --purge` (with the whole of
  `AdminState(t)`, `host/remove.go:201-218`). There is no `unseal`, and no flag of any verb
  ignores the record. A test greps the verb table (`admin/verbs.go:99-114`) for every verb and
  runs each on a sealed team: only `team remove`, `team export` and `team seal` do not refuse.
- **SC9. The residual of an aborted teardown.** The seal is terminal, and a sealed team is not
  a state anyone recovers into. If the teardown aborts after the seal (the export or the
  import fails and the driver gives up), the recovery is the operator's rollback store, which
  restores the guest's datasets to before the seal. A sealed but undestroyed guest is never
  the recovery path: nothing in this spec offers `unseal`, a mask, or a restart of a sealed
  team (O3).

## 2. The export (SX, `team export --team T`)

`carry.Export` under the team lock (10 min), with SG4's guard: the team must be `sealed`.

- **SX1. Format: one ustar archive, deterministic.** Written to `CarryPart(t, false)` (0600,
  root) in `CarryDir` (0700, root, made and checked as `backup`'s directory is,
  `backup/take.go:278`), fsynced, then renamed to `CarryExport(t)` and the directory fsynced. A
  single file because the driver moves it off the node and back; uncompressed because RocksDB's
  files are compressed already and a decompressor is attack surface on import. Members are in a
  fixed order (SX2), then by byte order of name within a tree; every header carries uid 0,
  gid 0, no user or group name, the mode of SX3, and mtime `sealed_ms / 1000`. No PAX, GNU,
  link, device or FIFO records. So two exports of one sealed team are byte-identical: a test
  exports twice and compares, and that test is also the proof that the state is cold.

- **SX2. Members, in order.**

  1. `carry.json`, the manifest (SX5);
  2. `admin/`: `team.json`, `server-name`, `config.toml`, `units.json`, `restore-epoch.json`
     (when present), `tls/ca.crt`, `tls/ca.key` (as `host.CABackupFiles` reads them,
     `host/tls.go:191-200`, which judges the pair by `loadCA`), `backups/<n>.tar`,
     `backups/last-run.json`;
  3. `ctl/`: `roster.json`, `rooms.json`, `admin.token`, `guest/<seat>` files
     (`paths.GuestTokenDir`, `paths/paths.go:319`), and any `restore-<N>-lost.jsonl` (evidence
     a past restore left, RS14);
  4. `status/approved.json` (O2: carried);
  5. `hs/`: the whole of `HomeserverPrivateState(t)`, that is `db/` and Tuwunel's `backups/`
     (`meta/`, `private/<n>/`, `shared_checksum/`), read fd-relative by the trust rule
     (`restore.OS`'s `openBeneath`, `restore/fs.go:249`). It is carried whole; there is no
     `db/`-only form (O4).

  Not carried, because `team apply` makes each again on the target (SA): `firewall.json`, the
  stamp, `status.json`, `lxc-online.json`, the TLS leaf, both drop-ins. Never present on a
  sealed team (SC1): `restore.json`, `config.restore.toml`, `db.restore-*`, `db.previous`.
  Present but not carried: `restore.block` (SC4's start block; the target starts without it)
  and `drill.json` (absent by SC1). `sealed.json` is not a member: its content is the
  manifest's `seal` object. `LXCRecord(t)` is not carried (C 3.9, not built).

  **Why Tuwunel's `backups/` travels with `db/`:** `restore-epoch.json`'s `min_backup` refers
  to numbers in `meta/` (RM11, RV4). Carried without `meta/`, the target's numbering would
  restart at 1 and every new backup could fall below a carried `min_backup`, refusing every
  later restore of it. With the tree, numbering continues and BT4's pairs stay pairs. The cost
  is the size of Tuwunel's backups, measured at about 4 MB on a real team (O4).

- **SX3. Modes.** Files 0600, except `admin/tls/ca.crt` 0644 (`host/tls.go:20-23`) and
  `status/approved.json` 0640 (`declare/gate.go:28`); directories 0700. The source's modes are
  checked against these when read; a source file of another mode is refused (70), never
  normalised, since the target would place what it found.

- **SX4. Names and caps.** Every name is relative, of the grammar
  `^[A-Za-z0-9_][A-Za-z0-9._+-]{0,127}(/[A-Za-z0-9_][A-Za-z0-9._+-]{0,127})*$`, never `.` or
  `..`, and fits ustar's 100 + 155 split. Caps: the manifest 1 MiB; each `admin/` and `ctl/`
  file the cap its own reader uses (`fileMax`, `host/apply.go:84`; BK2's for the roster, rooms
  and token); a `*-lost.jsonl` 64 MiB (RS14, `carry.LostCap`); `hs/` members none singly; the
  archive `carry.MaxArchive` (O4, CN5). A source over a cap fails the export, naming the file,
  its measured size and the cap. On import, a lost export travels to `ctl/<team>/` as the bytes
  after `carry.place`'s request, never inside it: the child's request cap (64 KiB, CC3) is
  smaller than this cap and is not raised for it (SC-C2, sc-D11).

- **SX5. Integrity: the manifest.**
  `{"format": 1, "kind": "sealed"|"drill", "team": T, "server_name": "...", "seal": <SC7's fields from sealed_ms on>, "members": [{"name": "...", "type": "file"|"dir", "mode": "0600", "size": n, "sha256": "<hex>"}...]}`.
  `format` is `carry.ExportFormat` (SX8). `members` lists every member after the manifest, in
  archive order; a directory has no size or digest. The exporter makes two passes: one that
  digests every source, which builds the manifest, and the write, which digests again and fails
  the export if any digest or size differs (the source changed: it was not cold).

- **SX6. The answer** is
  `{"v": 1, "team": T, "kind": "sealed", "path": "<CarryExport(t)>", "size": n, "sha256": "<hex of the whole archive>", "sealed_ms": n}`.
  The whole-archive digest is what the driver records and gives the import (SI1): it binds the
  copy that comes back to the export that was made. It is not authenticity: anyone holding the
  archive holds the CA key and the token anyway (the residual, SI7 and SC9).

- **SX7. Re-export and interruption.** A `.part` left by a kill is removed (`unlinkat`, after
  `Lstat` shows a regular file) and the export starts again; the result is byte-identical
  (SX1). An existing `CarryExport(t)` is replaced only by an export whose digest equals it; a
  different digest means the sealed state changed, which is a failure (1), naming both digests.

- **SX8. A versioned manifest (O9).** The manifest's `format` is an integer, `carry.ExportFormat`
  (1 today), and it changes whenever a member, a field or the order of SX2 changes in a way an
  older import would misread. The export always writes the current value. There is no refusal
  on the age of the tool, and the software digest stays a record only; the format is the one
  compatibility check between an exporter and an importer. The import's side is SI13.

## 3. The drill export (SD, `team export --team T --drill`)

The live team cannot be sealed to rehearse (point 5). The drill is a short stop, a cold copy, a
restart.

- **SD1. Validation:** SC1's, without the seal clause: applied, not migration, no restore or
  leftover, no import record, no `pending`, serving.
- **SD2. Record first.** `drill.json` (0600) at step `stopping`, with SC2's tail, before any
  stop. While it exists SG1 and SG3 refuse every verb but `team export --drill` (its resume).
- **SD3. Stop, copy, restart.** SC4's stop (no disable, no block), SC5's facts, then SX1 to SX5
  with `kind: "drill"` and `drilled_ms` in place of `sealed_ms`, into `CarryDrillExport(t)`.
  Then `host.Serve` (`host/serve.go:17-34`: start, ready, loopback bound, terminator) and the
  record removed. The answer is SX6's with `kind: "drill"` and `drilled_ms` in place of
  `sealed_ms`. A copy that fails after the stop (a source SX3 or SX4 refuses, found only once
  the team is cold) still serves the team and removes the record, then fails with the copy's
  error and class. SX7's equal-digest rule is the sealed export's only: the team is live
  between drills, so a drill export replaces the last one.
- **SD4. Interruption.** A kill leaves `drill.json` and, perhaps, the team stopped. The health
  tick then reports the unit not active and does not restart it: it restarts only an active
  unit whose probe missed (`health/probe.go:50-84`). Recovery is `team export --team T --drill`
  again: with `drill.json` present it skips SD1's serving clause, removes any `.part`, copies
  again, serves, and removes the record. A failure of `Serve` leaves the record and answers
  "the team is down: team export --team T --drill again after fixing <error>" (1).
- **SD5. Effect on others.** `backup --all` waits up to 10 min for the lock
  (`admin/backupverbs.go:98`), then reports the team failed (75 inside, exit 1 for the run,
  BV2). A drill export never touches `sealed.json`; a sealed team refuses `--drill` (70).

## 4. The import (SI, `team import --team T --sha256 <hex> [--drill]`)

`carry.Import` takes the team lock (wait 0: nothing else may hold a team that has no state;
busy is 75) with SG5's guard. Nothing is written before SI1 to SI7 and SI13 pass.

- **SI1. The input.** Exactly `CarryExport(t)` (or `CarryDrillExport(t)` with `--drill`),
  opened by `state.OpenOwnedFile` with uid 0. `--sha256` is required (64): the whole-archive
  digest is computed while streaming, and a mismatch fails the import after staging, which is
  then removed (SI10). A path argument does not exist: the inbox is fixed.

- **SI2. An empty guest for T.** Every root of `TeamStateRoots(t)` (CN4) is absent (`Lstat`:
  a link counts), except the three carry names, which must be absent too on a fresh import. The
  team's server, TLS and restore units are not active: inactive, or not loaded; `failed` and
  `activating` refuse. Their load state is not judged, because an instance of an installed
  template (SI3 requires the templates) reports `loaded` though it was never started. Each present root is
  named in the refusal (70): "team T has state on this guest; the import runs only onto a guest
  where T was never applied".

- **SI3. The guest can serve it.** Refusals (70), each naming what is missing:

  - the base units are installed (`ServerUnitTemplateFile`, `TLSUnitTemplateFile`; apply's
    render needs both, `host/apply.go:483-491`): the play has run;
  - the Tuwunel binary of the manifest's version is installed (`paths.ServerBinary`,
    `paths/paths.go:216`), regular and root-owned: apply's step 6 may start that version for
    its backup (`host/apply.go:745-755`), and the carried database was last written by it;
  - no other team's `units.json` records the manifest's port (apply's own refusal,
    `host/apply.go:452-479`, run here so it fails before anything is placed).

- **SI4. Kind and flag agree.** `kind: "sealed"` without `--drill`; `kind: "drill"` only with
  `--drill`; any other pairing is 70. With `--drill`, the guest holds no stamped team
  (`health.Stamps.Stamped`) and no `AdminState` directory of a team that is not a drill import
  (Q1's refusal).

- **SI5. A carry, nothing else.** The first member must be `carry.json`. A first member named
  `backup.json` is refused (70) as "a nightly backup is restore's input (DC8), not a carry": a
  BK1 pair has no seal record. Any other first member, a manifest that does not decode strictly
  (once SI13 has passed), or a team other than T, is 70.

- **SI6. The seal agrees with what it seals.** Read from the archive, before placement:

  - `ctl/roster.json` decodes for T (`apply.DecodeRosterFile`), has no `pending`, and its
    `seq`, `rev` and `RestoreEpoch` equal the manifest's `seal.roster`;
  - `admin/restore-epoch.json` (absent is `{0, 0}`) equals `seal.epoch_floor`;
  - `admin/team.json` decodes (`declare.DecodeTeamFile`) for T, and its `server_name`,
    `admin/server-name` and the manifest's are one;
  - `admin/units.json` decodes for T (`host`'s `decodeUnits`, exported as `host.DecodeUnits`)
    and its version and port are the manifest's;
  - `admin/tls/ca.crt` and `ca.key` pass `host.CheckRestoreCA` (`host/tls.go:221-224`) against
    the target, where no CA exists, so it judges the pair alone (`backupCA`, `:250-282`);
  - `status/approved.json` decodes (`declare.DecodeApproved`, `declare/gate.go:457`);
  - `ctl/admin.token` passes `ctl`'s token rule.

  Any disagreement is 70 ("the seal record disagrees with <member>") and nothing is placed.

- **SI7. Age (O1).** `now - sealed_ms` at most `carry.MaxAge` (72 h), or for a drill export
  `now - drilled_ms` at most `carry.MaxAgeDrill` (24 h), and `sealed_ms` not more than 5 min
  ahead of the clock. Older is 70, naming the age and the limit. The limits are constants, not
  flags. The age is checked again just before SI9's commit point (step 5): **the driver must
  finish the import inside the window, or the import aborts (70) and the driver rolls back
  with `--discard`.** This is the residual's control; the other is the driver deleting the
  export.

- **SI8. Stage.** The record `CarryImportRecord(t)` (0600) is written at step `staging`
  `{"v": 1, "team": T, "kind": "...", "sha256": "...", "step": "staging", "placed": []}`. The
  archive is then streamed member by member: each digest and size checked against the manifest
  as it is written, each refused by the BK3 checks generalised (`backup/tar.go:231-366`:
  framing, header checksum, type, mode, uid/gid, mtime, names, no data after the end blocks),
  plus: a member not listed, listed twice or out of order; a directory listed as a file. `hs/`
  lands in `HomeserverCarryStaging(t)` (made 0700 root, entries created fd-relative under a
  root-owned fd of `HomeserverPrivateStateRoot`, `O_NOFOLLOW`, as the trust rule's homeserver
  clause); `admin/`, `ctl/` and `status/` land in `CarryStaging(t)`. Files are written 0600 (or
  SX3's mode), uid 0, gid 0, fsynced; each directory fsynced after its entries; then the whole
  archive digest is compared with `--sha256`. The record goes to `staged`.

- **SI9. Place**, step `placing`; each placed root is appended to the record's `placed` before
  its rename, so a resume knows what is its own:

  1. `HomeserverCarryStaging(t)` renamed to `HomeserverPrivateState(t)` (`renameat2`
     `RENAME_NOREPLACE`, `restore/fs.go:137`), then the parent fsynced. It stays root-owned;
     no `chown` to a dynamic uid that does not exist yet (SH1). `HomeserverState(t)`, the link,
     is not made: systemd makes it at the first start;
  2. `CtlState(t)` made 0700, owned by the ctl uid and gid (`mkdirOwned`,
     `host/dirs.go:25`, as `host/apply.go:562`), then the child's `carry.place` (SC-C2) writes
     `roster.json`, `rooms.json`, `admin.token`, `guest/*` and any lost export with its
     atomic writer, the lost exports streamed from `CarryStaging(t)` (SC-C2, sc-D11);
  3. `status/approved.json` saved by `declare.FileApprovals.Save` (`declare/gate.go:579-581`)
     with the owner group's gid (`System.OwnerGroup`), as `team approve` writes it, after
     `MkdirStatus` (`host/dirs.go:33`);
  4. `CarriedRecord(t)` written into the staged admin directory:
     `{"v": 1, "team": T, "kind": "...", "sha256": "...", "sealed_ms": n, "imported_ms": n, "roster": {...}, "tail": [...]}`,
     the tail being the manifest's `seal.tail`, which section 6 checks (sc-D13);
  5. the staged admin directory renamed to `AdminState(t)`, then `AdminStateRoot` fsynced. This
     rename is the commit point: `AdminState(t)` holds `server-name` and the CA, so from here
     `team apply` would see a team that is not first (`host/apply.go:312-316`);
  6. what is left of `CarryStaging(t)` (the manifest copy, `ctl/` and `status/`) removed, then
     the record, then `CarryDir` fsynced (sc-D12).

  The CA is thus in place before any `team apply` (point 3), and `host.restorable`
  (`host/tls.go:227-247`) on any later call finds the same pair and writes nothing.

- **SI10. Failure and interruption.** Every clean-up is an explicit step (SM5's rule: nothing in
  a `defer` but the lock's release), so a kill at the k-th FS call is a test row.

  - before `staged`, or a digest mismatch: both staging names removed (`RemoveTree`
    fd-relative, `restore/fs.go:151`), then the record; "import not done" (1);
  - `--resume` at `staging` does the same and starts again from SI1; at `staged` it re-checks
    the staged tree's digests against the manifest copy in `CarryStaging(t)` and goes to SI9;
    at `placing` it repeats SI9 from the first entry not in `placed`, taking an already-renamed
    root (target present, staging absent) as done and any other mix as 70 naming both paths;
  - `--discard` at `staging` or `staged` removes both staging names and the record; at
    `placing` it removes, by `RemoveTree`, exactly the roots in `placed`, and only when each is
    a no-follow directory, then the staging names and the record, so the guest is empty for T
    again (SI2 then passes). The archive in the inbox is never removed by the import.

- **SI11. The answer** is
  `{"v": 1, "team": T, "kind": "...", "sealed_ms": n, "roster": {"seq": n, "rev": n, "restore_epoch": n}, "placed": ["<root>"...], "next": ["team carry-check --team T", "team apply"]}`
  for a sealed import (the check first, O5), and `"next": ["team carry-check --team T"]` for a
  drill.

- **SI12. Nothing in the roster changes.** The import writes `roster.json` byte for byte; no
  merge, no `seq` raise, no `restore-epoch.json` write. A test compares the placed roster and
  epoch file with the source's bytes.

- **SI13. The export format is known (O9).** Before SI2 to SI7 judge anything else, the import
  reads the manifest's `format` alone (a lenient read of that one field, the manifest being
  the first member and capped by SX4). A `format` the import does not know, absent, not an
  integer, or not equal to `carry.ExportFormat`, is 70, naming both versions: "the export is
  format <n>, this teamchat-admin imports format <m>". Nothing is staged or placed. There is
  no automatic migration between formats, and no refusal on the age of the tool.

## 5. After the import (SA): what `team apply` keeps and makes

**The operator runs** `team carry-check` for T first (a sealed import too, O5), then the
teamchat play's `team apply` for T (the play feeds the declaration on stdin,
`admin/verbs.go:502-520`), through `meta-deploy.bash` on the host. No `team approve`, since the
approval is carried (O2). Not `restore`, not `member reissue`. On a drill import the operator
runs `team carry-check` only, and `team apply` refuses (SG2).

**Ordering on the rebuilt guest.** The play applies every declared team, and a `team apply` of T
before the import would make a fresh team (a new CA, `host/tls.go:55-59`), after which SI2 refuses
the import for ever. So the first play run on the rebuilt guest must exclude T
(`teamchat_only_teams`, `paths.OnlyTeamsVar`, `paths/paths.go:138`), then the import, then the
play with T. This is the driver's step, and SG6 is its backstop: a first apply of a team declared
as carried is refused until its import has landed (O6).

The proof from source, for an imported team (every file of SX2 present, none of SA's
"made" column):

| Thing                               | On an imported team                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Source                                                                                          |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| The lock and guard                  | taken with `Deps.Guard`; SG1/SG3 pass (no seal, no import record)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | `host/apply.go:274`, `teamlock/teamlock.go:100-112`                                             |
| `server-name`                       | kept; it makes the apply not first, so step 4's files are skipped; a declaration with another name is refused (70)                                                                                                                                                                                                                                                                                                                                                                                                                                                               | `host/apply.go:311-319`, `:556-559`                                                             |
| The approval                        | kept; must hold the declaration, else 70 before any change, and again in the child                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | `host/apply.go:320-326`, `declare/gate.go:174-179`, `:251-254`, `apply/apply.go:130-137`        |
| Port and listeners                  | checked; another team's `units.json` or a foreign listener on the port is 70                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | `host/apply.go:426-479`                                                                         |
| `config.toml`                       | its registration key kept; the file rewritten only when the render differs (new listen addresses or networks on this guest will differ)                                                                                                                                                                                                                                                                                                                                                                                                                                          | `host/apply.go:516-531`, `:790-796`                                                             |
| Server drop-in                      | made (none exists): `decide` answers first start                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `host/apply.go:221-226`, `:797-802`                                                             |
| CA cert and key                     | kept: `loadCA` finds the pair, `makeCA` does not run; `createOnly` could not replace them anyway                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `host/tls.go:51-59`, `:88-93`, `:100-126`                                                       |
| TLS leaf                            | made, signed by the carried CA (no leaf files: "leaf made"); `leafMade` restarts the terminator                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `host/tls.go:60`, `:150-159`, `host/leaf.go:65-85`, `:124-125`, `host/tlsunit.go:94`            |
| `firewall.json` and rules           | made: absent record, so the wanted rules are recorded, added in both configurations, recorded alone                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `host/apply.go:588-620`, `:661-694`                                                             |
| Step 6 backup                       | **needs the carried `units.json`.** With the restart set unchanged it is skipped, as on a guest with other addresses, which the restart set does not hold (sc-D17); changed (a guest whose play pins another Tuwunel, or installs another base unit), apply starts the recorded version through a drop-in made from `units.json` and takes a backup, numbered after the carried `meta/`. **Without `units.json` the apply fails** ("a database exists, and neither a drop-in nor units.json records the Tuwunel version"), which is why SX2 carries it and SI3 checks the binary | `host/apply.go:698-714`, `:718-741` (`:723-724`), `:745-755`                                    |
| Unit started, enabled               | made: start, enable                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `host/apply.go:806-824`                                                                         |
| Loopback bind                       | checked                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | `host/apply.go:882-888`                                                                         |
| Terminator drop-in, unit            | made, started, enabled, bound to the new listen set                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `host/tlsunit.go:73-109`                                                                        |
| Bootstrap (child)                   | runs on the carried database with the carried token: the admin check, the roster load (no `pending`, SI6), rooms and roles converged; a roster write raises `seq` from the carried value                                                                                                                                                                                                                                                                                                                                                                                         | `host/apply.go:912-919`, `apply/apply.go:140`, `:168-179`, `:184-197`, `apply/files.go:422-428` |
| `team.json`                         | rewritten only when the canonical form differs                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | `host/apply.go:933`, `:947-956`                                                                 |
| `units.json`                        | rewritten with this guest's digests, the pinned version, port and listen set                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | `host/apply.go:936-943`                                                                         |
| Stamp                               | made (absent), software and Tuwunel of this guest                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | `host/apply.go:958-986`                                                                         |
| Status record                       | made by the tick's refresh (no record: origin given then)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | `host/apply.go:926`, `health/files.go:122-126`                                                  |
| `restore-epoch.json`                | kept: nothing in `host` reads or writes it (only `restore` and `paths` name it)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `paths/restore.go:14`; grep of `host/`                                                          |
| `restore_epoch` in the roster event | unchanged: apply writes no epoch; the child's roster event carries the roster's                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | `apply/apply.go:429` (`DeriveRoster`)                                                           |
| `lxc-online.json`                   | not made by apply; the tick writes it when LXC members report                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | `health/online.go:138`                                                                          |

- **SA1.** A test runs `host.Apply` over a fake tree that `carry.Import` populated from an
  export of a fake team, with fakes of systemd, firewall, network and the child, and asserts
  each row: what is kept byte for byte (CA pair, `server-name`, `restore-epoch.json`, the
  roster before bootstrap), what is made (leaf, both drop-ins, `firewall.json`, stamp), and that
  no `makeCA` and no `team approve` ran. Built as `carry/applyover_test.go`: the source is a
  team a first `host.Apply` made, sealed and exported; the guest imports it and runs
  `team carry-check`, then `host.Apply` runs the declaration marked `carried_from` (SG6) with
  production's file approvals and the guards `wire` joins. The config and `units.json` are kept
  too (same render), `team.json` is rewritten (the marker), and no backup is taken.
- **SA2.** The same test with `units.json` withheld fails at step `backup` with
  `host/apply.go:724`'s error: the pin that SX2 must carry it.
- **SA3.** The same test on a guest whose play pins another Tuwunel (sc-D17) takes step 6's
  backup on the recorded version, through the drop-in `host.WriteRecordedDropIn` makes from
  `units.json`, before it installs the pinned drop-in, restarts and records the pinned version.
  A guest with other listen addresses takes no backup (the restart set does not hold them) and
  records its listen set in `units.json` and the terminator's drop-in.

## 6. The carry check (SD6, `team carry-check --team T`)

The proof that the copy is whole, keeping the instance on loopback. It is wanted for a drill
import and, as the pre-apply proof, for the import onto the rebuilt bus guest (O5).

- Valid only on a team whose `carried.json` exists and decodes (either kind; absent is 70,
  naming `team import`); a drill-kind team accepts no other lock-taking verb but `team remove`
  (SG2). Its lock guard refuses a restore and every carry record (SG1, SG3). A team already
  applied (its stamp present) is refused (70, naming `team apply`) before anything starts: the
  check would stop a serving team and remove the drop-in apply made (sc-D14).
- Under the team lock (`host.ServeLoopback`, with the pin's memory bound): writes the server drop-in made from `units.json` alone (the
  `recordedDropIn` text, `host/apply.go:745-755`, exported as `host.WriteRecordedDropIn`),
  reloads, starts the server unit, waits ready on the recorded port (`host/apply.go:846-848`)
  and holds its bind to loopback alone (`host/apply.go:891-909`). It writes no firewall rule,
  no TLS drop-in, no leaf, no stamp; the terminator is never started. So the instance is
  reachable only on loopback, and the tick never sees it (no stamp).
- The child's `carry.check` (SC-C3) answers each tail room's newest event and the roster
  event. Every event of `carried.json`'s `tail` (the manifest's `seal.tail`, copied at SI9 step 4,
  sc-D13) must be present, and the roster
  event's `restore_epoch` and `rev` must equal the seal's; otherwise 1, naming the room.
- The unit is then stopped (RS18: the terminator and the homeserver each `inactive` or
  `failed`, neither disabled), for either kind, and also after a start or a `carry.check`
  that failed: `team apply` starts it on its own terms, through the drop-in it makes (SA's
  table). There is no flag to leave it running. The answer is
  `{"v": 1, "team": T, "kind": "...", "rooms": [{"room": "<purpose>", "event_id": "...", "found": b}...], "roster_event": {"rev": n, "restore_epoch": n}, "ok": b}`,
  printed beside the failure (1) when `ok` is false. A start or a `carry.check` that fails
  answers nothing and is its own failure (a server lost is 69). Once both units are stopped,
  the recorded drop-in the start wrote is removed, with its directory when empty, and systemd
  reloaded, after a failed start or check as after a good one: the tree after the check is as
  before it, so SA's "Server drop-in: made (none exists)" holds. A unit RS18 does not find
  stopped keeps its drop-in (no reload), and a later check writes and removes it again (sc-D14).

## 7. Guards and locks (SG, SL)

- **SG1. Dispatcher refusal.** Before any verb runs, `admin` reads `--team T`'s
  `SealRecord(t)`, `DrillRecord(t)` and `CarryImportRecord(t)` (`Lstat`; any one present):
  - sealed (either step): every verb refuses (70) but `team remove`, `team export` (no
    `--drill`) and `team seal`. That covers the lock-free verbs too (`backups`, `units print`,
    `health --team`, `element check`, `restore --lost`, `team approve`), which RG's guard alone
    would not reach;
  - drill record: every verb but `team export --drill` refuses;
  - import record: every verb but `team import --resume|--discard` refuses.
    The refusal names the record and the one way on. Test: the whole verb table on each state.
- **SG2. A drill-kind `carried.json`** refuses `team apply`, member, role, human, lxc and
  backup verbs (70: "a drill import never joins a real team"); allows `team carry-check` and
  `team remove`. Every other verb naming the team refuses too (sc-D16). It is judged twice, as
  SG1 is: in the dispatcher after SG1 (`carry.DrillImported`, carried.json read as root's own
  file and decoded strictly; one that cannot be read or decoded fails the verb), and under the
  lock by `carry.DrillImportGuard`, which `wire` joins to `host.Deps.Guard`, the LXC verbs'
  guard and the restore's, so an import that lands while a verb waits for the lock still
  refuses it. `team remove`'s guard and `team carry-check`'s do not hold it. Test: the whole
  verb table on a drill-kind and on a sealed-kind `carried.json`.
- **SG3. The lock's guard** (`teamlock.Guard`, `teamlock/teamlock.go:84`) repeats SG1's
  judgement once the flock is held, so a seal that lands between the dispatcher's read and a
  verb's lock still refuses it. `wire` sets `host.Deps.Guard` to `teamlock.All(restore.Guard(root), carry.Guard(root))` (`admin/wire/wire.go:129`, and `admin/wire/lxc.go:49`); `teamlock.All`
  is new, refusing on the first guard that refuses, and refusing a nil member as `Take` does.
- **SG4.** `team remove` takes `host.Deps.RemoveGuard` (new; `restore.Guard` and the import
  record clause, without the seal clause) at `host/remove.go:31`, so a sealed team can be
  removed. `team export` takes `carry.SealedGuard` (sealed at step `sealed` required, RG2
  still refusing).
- **SG5.** `team import` takes a guard that requires the import record absent on a fresh
  import and present on `--resume`/`--discard` (as `restore` passes its own, RG3).
- **SG6. A carried team is not applied before its import has landed (O6).** A declaration may
  mark a team as carried: `carried_from`, an optional string of the team's declaration, free
  text naming where the team was carried from, of `description`'s grammar
  (`proto.ValidDescription`: 1 to 120 characters, trimmed, no control or format characters).
  Only its presence has effect. Its canonical form omits it when empty, so every existing
  `team.json`, input and `approved.json` is byte for byte what it was; when set it is written
  last in `team.json` and the input and read back by `DecodeTeamFile` and `DecodeInput`. It is
  not a gated field, so `approved.json` never holds it (sc-D15). `team apply` for such a team, when it is a first apply (no `AdminState(t)`, so no
  `server-name`, `host/apply.go:312-316`), is refused with exit 70 and the message "team T is
  declared as carried: run team import first, then team apply", before any file, unit or
  firewall rule is made, and before the CA could be. Once `carried.json` exists the marker has
  no effect on apply. This is a **declare change** (`declare`'s team grammar and its canonical
  form, and so `team.json`'s round trip), and it is gate A's inventory: the play hands each
  `teamchat_teams` entry to `team apply` whole, so the key reaches the verb with no play
  change. Tests: the refusal and its exit code on a fake host with a marked declaration and no
  import (`host`, `admin`); a marked team that is not first applies (`host`); the same
  declaration applies over a tree `carry.Import` placed (SA1, item 13); an unmarked declaration
  is untouched (its encodings pinned, `declare`).
- **SL1.** Every carry verb holds the team lock for its whole run, released only in the one
  deferred call (SM5). Seal and export wait `host.PlayWait`; import waits 0; carry-check
  waits `host.PlayWait`. No carry verb ever holds two team locks.
- **SL2. The health tick** takes the lock unguarded and without waiting
  (`health/host.go:57-59`); it is unchanged. A tick that gathered a team before its seal and
  reaches `underLock` after it re-reads the stamp under the lock, finds none, and drops the
  team (`health/leaf.go:75-102`, `health/health.go:214-215`): no restart, no leaf renewal. A
  test interleaves a seal between the tick's gather and its restart.

## 8. Health tick and backup timer (SH)

- **SH-T1.** A sealed team: never ticked (no stamp, SC6); `backup --all` skips it; `backup --team T` is SG1's 70. Tests over `health.Stamps` and `backupAll`.
- **SH-T2.** A drill under way: the tick sees the stopped unit as a problem and restarts
  nothing (`health/probe.go:50-84`); the backup timer's take is busy (75 inside `--all`).
- **SH-T3.** An imported team before `team apply`: no stamp, so neither runs.
- Built as tests only, no code change: `health/carry_test.go` (Stamps, as `wire` gives the
  tick, lists neither a sealed nor an imported team and the tick writes no record of them; a
  drill's lock drops the team as busy, and its stopped units are problems, neither restarted
  nor probed) and `admin/carrybackup_test.go` (`backup --all` skips both and takes the stamped
  team; `backup --team` of a sealed team is 70 with no signal; under a drill's lock the team's
  line names the holder and the run ends 1). A `backup --all` that listed an unstamped team
  would still be refused by the lock's guard (SG3) or the drill-import check (SG2), team by
  team: seen when the tests were proven red by making `Stamps` list unstamped teams.
- **SH1. Host check (H-C-style, not verified).** On the pinned systemd, a `DynamicUser=yes`,
  `StateDirectory=teamchat/<t>` unit whose `/var/lib/private/teamchat/<t>` already exists,
  owned by uid 0 down every level, starts with the tree owned by its dynamic uid at every level
  and the `/var/lib/teamchat/<t>` link made. SI9 step 1 relies on it. It joins the drill's
  record clauses (Q3), proven on the scratch guest before the carry (the driver's Task 2.3).

## 9. Exits (SE)

Following RS15 and C 3.3's table (`admin/run.go:23-27`, `admin/exit.go:26-74`):

| Code | When                                                                                                                                                                                               |
| ---- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0    | done; a repeat of `team seal` on a sealed team; an export whose archive already exists byte for byte                                                                                               |
| 1    | a failure in a step (stop, write, rename, digest mismatch, child failure of its own class aside), "import not done", a drill check that finds an event missing                                     |
| 64   | usage: `--sha256` missing or not 64 hex, `--drill` on `team seal`, `--resume` with `--discard`, `--team` missing                                                                                   |
| 69   | the homeserver answered at validation and was lost during the tail read or the check                                                                                                               |
| 70   | every refusal of SC1, SD1, SI2-SI7, SI13, SG1-SG6; the archive's kind and flag disagreeing; an age over `MaxAge` or `MaxAgeDrill`; an unknown export format; a source or archive over `MaxArchive` |
| 75   | the team lock is busy (import: any holder at all)                                                                                                                                                  |
| 77   | not root                                                                                                                                                                                           |
| 78   | the wiring failed; `teamchat_carry_max_archive_bytes` absent, unreadable, zero or negative                                                                                                         |

## 10. The child's carry ops (SC-C)

- **SC-C1. `carry.tail`.** Args `{team_file}`; the live server, the current token. Answers
  `tail: [{room, event_id, ts_ms}...]` for each room of `rooms.json` and
  `epoch: {restore_epoch, rev}` (RC8's reading, `admin/serverestore.go:395`).
- **SC-C2. `carry.place`.** Args `{team_file, roster, rooms, token, guest: [{name, data}], lost: [{name, size, sha256}]}`;
  refused when any target already exists in `ctl/<team>/`; writes each with the atomic writer
  and answers the names written. Decoded strictly by root (RC6's rule: no credential in the
  answer). The request carries the small files; the lost exports' bytes are not in it
  (sc-D11):
  - **Which carries what.** The request is one line of at most 64 KiB (CC3), for this op as for
    every other. The lost exports' bytes follow it on the child's stdin, in `lost`'s order, and
    the stream is exactly the sum of their sizes. Only `carry.place` takes bytes after its
    request: any other op with bytes after its line is refused unserved, and root asks no other
    op with a stream.
  - **Root, before streaming.** Root reads each lost export from `CarryStaging(t)`'s `ctl/`, and
    refuses before sending anything: a name that is not a plain basename of the grammar
    `restore-<N>-lost.jsonl` (so no `/` and no `..`), a name given twice, a staged lost export
    the request does not name, a file opened through a link (`O_NOFOLLOW`, no link anywhere in
    the path), one that is not regular, over SX4's 64 MiB, or not of its size and sha256. The
    stream digests each file again and fails, never ends early, if one changed since.
  - **The child.** It reads no more of the stream than the sizes, and one byte to see its end;
    it streams each lost export into a fresh temporary in `ctl/<team>/`, checking its size and
    sha256, and syncs it. A short, long or wrong stream fails the op with nothing placed and no
    temporary left. Only when every lost export has verified and the stream has ended does it
    write the small files and link each lost export into place (OP4: never over a file).
  - Root never writes `ctl/<team>/` itself: the trust rule's bullet on `ctl/` above stands.
- **SC-C3. `carry.check`.** Args `{team_file, port, tail}`: a client on `127.0.0.1:<port>` with
  the token in `ctl/<team>/`; answers, per tail entry, found or not, and the roster event's
  epoch.

## 11. Decisions where the inputs are silent

| ID     | Decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| sc-D1  | The export is one deterministic uncompressed ustar with a digest manifest first and a whole-archive digest in the answer. Not a directory: it must cross machines as one object whose digest the driver records.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| sc-D2  | The seal removes the stamp and the status record, so the tick and the backup timer stop by construction (`health/files.go:45-47`), not by a new branch in each.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| sc-D3  | The seal writes `restore.block` as its start block (one meaning, "no start", already honoured by RS17); the carry adds no second block file and no `systemctl mask` (O3).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| sc-D4  | `hs/` travels whole, with Tuwunel's `backups/` and root's `backups/` (SX2), for `min_backup`'s sake; there is no `db/`-only form (O4).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| sc-D5  | `units.json` travels; `firewall.json`, the leaf, the drop-ins, the stamp and the status record do not (SA's table).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| sc-D6  | The import's input is one fixed root-owned inbox path, not an argument, so root never opens a path a caller named.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| sc-D7  | `AdminState(t)` is placed last; its rename is the commit point; the import record's guard covers the window before it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| sc-D8  | A drill import never becomes a real team (SG2); its proof is `carry-check` on loopback with no terminator and no firewall.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| sc-D9  | The age limits and the export format are constants of this repository (CN5), never driver settings: the driver cannot widen the window or skip the format check.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| sc-D10 | `approved.json` is carried, and `team apply` of a team declared as carried is refused until its import has landed (SG6), so the order cannot be got wrong.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| sc-D12 | The import's staging and resume, where SI8-SI11 are silent: `CarryStaging(t)` holds `carry.json` (the manifest, the copy SI10's resume reads) and the archive's `admin/`, `ctl/` and `status/` as they are named, so `StagedLost` reads `CarryStaging(t)/ctl/`. Staging makes `HomeserverPrivateStateRoot` and its parent as systemd would (0755 and 0700, root's) only where absent, and leaves them on a discard. Each placed root is appended to `placed` before its step; a resume takes the homeserver and admin roots by SI10's rename rule, and redoes `ctl/<team>/` or the status directory only when it is the last root in `placed` (removed, then made again: the same bytes). `--resume` and `--discard` refuse a `--sha256` or a kind that is not the record's (70). The drill's answer names its time `drilled_ms`, as SD3's does. `--discard` answers `{"v": 1, "team": T, "discarded": [paths]}`.                                                                                                                          |
| sc-D11 | The coordinator's ruling on SC-C2 against the 64 KiB request cap, option (A): the cap stays for every op; `carry.place`'s request names each lost export by name, size and sha256, and root streams their bytes from `CarryStaging(t)` after it, every check done first; the child checks them again and places nothing unless all verify (SC-C2, SX4). Rejected: (B) raising the cap, which widens it for every op; (C) lowering SX4's cap, which drops a capability.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| sc-D13 | `team carry-check`, where section 6 is silent: the tail it checks is in `carried.json`, which SI9 step 4 writes with the manifest's `seal.tail`, because the manifest copy in `CarryStaging(t)` is removed at step 6 and nothing else on the guest holds it. The start is `host.ServeLoopback` (the recorded drop-in, the pin's memory bound, reload, start, ready, loopback alone); the unit is stopped (RS18, both units, no disable) whatever the start or the check did. What becomes of the recorded drop-in is sc-D14's. The answer is section 6's document, printed beside the failure when not ok.                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| sc-D14 | The coordinator's ruling on the drop-in `team carry-check` writes: once RS18 finds both units stopped (after a failed start or check too), the check removes the recorded drop-in, its directory when empty, and reloads systemd, so the tree after the check is as before it and SA's "Server drop-in: made (none exists)" holds for the first `team apply`. A unit not found stopped keeps its drop-in (removing what a running unit was started from would leave systemd's view and the files apart); the next check overwrites and removes it. Because the removal would take an applied team's own drop-in, and the stop would stop a serving team, the check refuses (70) a team whose stamp exists, before anything starts: it runs only between `team import` and `team apply`.                                                                                                                                                                                                                                                    |
| sc-D15 | The coordinator's ruling on SG6's marker: `carried_from` is a string of the team's declaration, free text naming where the team was carried from; only a non-empty value has effect. Its grammar is `description`'s, the declare package's free-text field (`proto.ValidDescription`: 1 to 120 characters, trimmed, no control or format characters). It is omitted from the canonical form when empty, so `team.json`, the input and `approved.json` of every unmarked team are unchanged byte for byte (pinned by a test); when set it is the last key of `team.json` and the input, and round-trips. It is not a gated field (AG1), so the approval neither holds nor judges it. The refusal sits beside the first-apply test, before the approval is read, so a refused apply makes no call at all.                                                                                                                                                                                                                                    |
| sc-D16 | The coordinator's ruling that SG2 is built (item 12b), and where SG2 is silent: a drill import refuses every verb that names the team but `team carry-check` and `team remove`, not only the families SG2 lists (section 6: it "accepts no other lock-taking verb but team remove"; a seal, export, import, restore, approve or rotate-ca of a drill import is as wrong as an apply). It is judged in the dispatcher after SG1 and again under the lock (`carry.DrillImportGuard`, joined to the host's, the LXC verbs' and the restore's guards), since an import can land while a verb waits for the lock. `carried.json` is read as root's own file and decoded strictly; one that cannot be read or decoded fails the verb rather than admitting it. The refusal is `ErrRefused` (70), naming the two verbs that run.                                                                                                                                                                                                                  |
| sc-D17 | SA3 as specified could not be built: it assumed that a guest with other listen addresses changes the restart set, but since X18 the homeserver binds loopback alone, so the rendered config, and with it the restart set (base unit, config, ExecStart), holds no listen address; the listen set is in the reload set's filter and the terminator's drop-in. Seen red: the test with other addresses found no backup taken. SA3 now takes the restart-set change a rebuilt guest can really bring, a play that pins another Tuwunel (the recorded version's binary still installed, SI3), and a second test holds that other addresses alone take no backup. SA's table row for step 6 says so. No code changed.                                                                                                                                                                                                                                                                                                                           |
| sc-D18 | Item 15's RT-style test is one round run twice: `carryRound` (admin's tests) carries alpha through the four verbs over the carry engine as `wire` builds it (`carry.OS`, `carry.StageOS`, `restore.OS`, `CarryChild` and the ctl-child in process; systemd, readiness and the status record are the world's fakes), and wants the homeserver tree byte for byte on the guest, the seal's tail in `carried.json`, every tail event found by the check, and the tail and the roster epoch read back from the imported server served again. Over fakehs (one in-memory server stands for both, so it proves the plumbing, not the database) it runs in every build; over a real Tuwunel (`-tags tuwunel`) the seal's stop of the homeserver unit stops the source server by SIGTERM and the guest's loopback start is Tuwunel on the imported database. The wiring change it found: `machine.carry` looked the teamchat-ctl account up twice and built the engine before checking the second lookup; it now looks it up once and fails first. |

## 12. Decisions

The coordinator answered each question of the draft; each is now a decision. None remains
open.

- **O1. `carry.MaxAge`: decided.** 72 h from the seal, and 24 h for a drill export. A
  fedora-desktop constant (CN5), not configurable by the driver. The driver must finish the
  import inside that window, or it aborts and rolls back (SI7). Reason: long enough for a
  rebuild that waits a night, short enough that a stale export left on a disk is refused, and
  the control must not be one the party who holds the export can loosen.
- **O2. The approval travels: decided.** `approved.json` is carried (SX2, SI9 step 3), so the
  rebuilt guest needs no `team approve` (password sudo, DC4), and the gate still holds the
  declaration to it. Reason: the approval is of the declaration, which is unchanged by the
  move.
- **O3. No mask: decided.** The seal does not `systemctl mask`. The residual (SC9): if a
  teardown aborts after the seal, the operator's rollback store restores the guest's datasets
  to before the seal, so a sealed but undestroyed guest is never the recovery path. Reason: a
  mask is a host effect outside gate A's unit files, and a sealed guest is not meant to be
  recovered into; the rollback store is the one recovery and it predates the seal.
- **O4. Size: decided.** `hs/` is carried whole with Tuwunel's backups, measured at about
  4 MB on a real team. There is no `db/`-only fallback, so `min_backup` is never rewritten and
  RV4's refusal keeps its force after a past `--roster-from-backup`. `carry.MaxArchive` is a
  declared setting, 1 GiB by default, a guard against a runaway archive rather than a size
  limit (CN5). Reason: the cost is small and the
  alternative would change `restore-epoch.json`.
- **O5. `carry-check` on the rebuilt bus guest: decided, wanted.** It is the pre-apply proof
  for a sealed import as for a drill (section 6, SI11, SA). The unit is stopped after it for
  both kinds; `team apply` starts it. Reason: the proof is cheap, on loopback, and finds an
  unreadable copy before the apply makes a leaf and opens the firewall.
- **O6. The play's ordering: decided, yes.** `team apply` refuses a first apply of a team
  declared as carried until its import has landed: SG6, exit 70. It is a declare change and
  gate A's inventory. Reason: a first apply before the import makes a fresh CA and makes
  SI2 refuse the import for ever; the refusal turns a driver's ordering mistake into a
  message.
- **O7. DC8: decided.** The carry is outside DC8; `restore` is unchanged and stays refused in
  production. `team import` therefore has production wiring and no RW2-style refusal. Reason:
  a carry moves a sealed team's current state and merges nothing, which is not what DC8 gates.
- **O8. Migration-origin teams: decided.** SC1 refuses a team of migration origin (as
  n2g-D9), and this does not affect the carry: the infra team checked read-only, and the only
  team on the real bus guest is of v2 origin, with no migration state. The refusal reads the
  authoritative marker, the migration's `state.json` (`health/files.go:101-113`), not the
  status record's derived `origin` field, which is only where an operator sees it. Reason: a
  derived record can lag or be absent, and the file is what decides the origin everywhere
  else.

No question is left open: every question of the draft is a decision above.

- **O9. Tool age: decided.** No refusal on the age of the tool, and the software digest stays
  a record. Instead the manifest carries a versioned `format` integer and the import refuses an
  export whose format it does not know, naming both versions (SX8, SI13). Reason: the format
  is what an importer actually depends on, and it is exact where a build age is not.

## 13. Where the implementation goes, and the commit order

**Packages and files.**

- `paths` (**image input, CCY bump**): CN1-CN5 in a new `paths/carry.go`; the completeness test
  of CN4.
- `state` (**image input, CCY bump**): `OpenOwnedFile`, built on `openIn` and the trust checks
  of `read` (`state/state.go:146`, `:166`), beside `ReadOwnedFile` (`:90`).
- `teamchat/carry` (new, not an image input): the archive writer and streaming parser (built on
  `backup/tar.go`'s `header`, `checksum`, `framing`, `checkHeader`, `:161-366`, generalised to
  directories and large members), the manifest and its format, the three records, `Seal`,
  `Export`, `Import`, `Check`, `Guard`, `SealedGuard`, `Deps` (systemd, the child,
  `restore.FS`-style fd-relative ops extended with `Mkdirat`/`Openat` create, the clock).
- `restore`: export `leftovers` (`restore/validate.go:110`) and `readApplied` (`:156`) for SC1;
  `OS` gains create-beneath (`restore/fs.go:249`, `:261`).
- `teamlock`: `All(...Guard) Guard` beside `Guard` (`teamlock/teamlock.go:84-91`).
- `host`: `StopTeamUnits` from `removing.stopUnit`/`stop` (`host/remove.go:91-116`);
  `WriteRecordedDropIn` from `recordedDropIn` (`host/apply.go:745-767`); `DecodeUnits` from
  `decodeUnits` (`host/records.go:64`); `Deps.RemoveGuard`, used at `host/remove.go:31`; the
  first-apply refusal of SG6 beside the first-apply test at `host/apply.go:312-316`.
- `declare`: the `carried_from` marker of SG6 in the team grammar and its canonical form
  (sc-D15).
- `admin`: the verbs `team seal`, `team export`, `team import`, `team carry-check` in
  `rootRuns` (`admin/verbs.go:99-114`), SG1 in the dispatcher (`admin/run.go`), exits through
  `ExitCodeOf` (`admin/exit.go:26`) with `carry.ErrSealed`, `ErrRefused` joining `verbExit`'s
  70 list (`:55`); the child ops in `admin/child.go` beside `serveRestore`
  (`admin/serverestore.go:84`), with RC6's strict decoding.
- `admin/wire`: `teamlock.All` guards (`wire.go:129`, `lxc.go:49`), `RemoveGuard`, the carry
  deps.
- `health`: no code change; tests only (SL2, SH-T1 to SH-T3).
- Not touched: `proto`, `matrix`, `pki`, `seat`, `clock` (image inputs). If `carry.tail` needs a
  matrix call the client lacks, that is a `matrix` change and a further CCY bump; RC3's
  `/messages` use (`restore.export`) suggests it does not.
- Tracked spec text and plan rows: this file, a Plan 00209 task (or a new plan) owning it, and
  the lab scenario of Q5 asserting `TeamStateRoots` absent after purge.

**Test-first commit order** (each commit: tests red, then code green, recorded in the owning
plan's journal):

01. `paths` names and `TeamStateRoots` with its completeness test; `state.OpenOwnedFile`; the
    CCY version bump in the same commit (both image inputs).
02. `carry` archive: writer and parser, determinism, every hostile archive (links, devices,
    `..`, absolute names, PAX, setuid, wrong mode or owner, extra or missing or reordered
    members, oversize, data after the end, digest mismatch, a BK1 tar as input), and the
    manifest's `format`: `TestImportRefusesUnknownFormat` (a manifest of format 0, 2 and a
    non-integer is refused with exit 70 naming both versions, before anything is staged;
    SX8, SI13).
03. `carry` records: `sealed.json`, `drill.json`, `carried.json`, the import record, strict
    codecs.
04. `teamlock.All`, `carry.Guard`/`SealedGuard`, `host.Deps.RemoveGuard`, SG1 in the
    dispatcher; the verb-table test of SC8 and SG1 (red: every verb runs on a sealed fake).
05. `host` exports (`StopTeamUnits`, `WriteRecordedDropIn`, `DecodeUnits`) as pure refactors,
    existing tests green.
06. Child ops `carry.tail`, `carry.place`, `carry.check` over the fake homeserver.
07. `Seal` and its verb, with the SM5 kill at every FS and systemd call, and SL2's interleaving.
08. `Export` (sealed), determinism and the two-pass cold proof, `.part` recovery.
09. The drill export, its record, `Serve` restart, kill rows.
10. `Import` and its verb: every refusal of SI2-SI7, the age window at the start and again
    before the commit point (SI7), staging, placement, `--resume`, `--discard`, a kill at every
    call.
11. `carry-check`, for both kinds, stopping the unit after it (section 6).
12. SG6: the `carried_from` marker in `declare`, and `TestApplyRefusesCarriedFirstApply` (exit
    70 before any change; applies after the import; an unmarked team untouched). Then, as its
    own commit (item 12b), SG2: a drill import refuses every verb but `team carry-check` and `team remove`, in the
    dispatcher and under the lock (`carry.DrillImportGuard`); the verb-table test on a
    drill-kind `carried.json` (sc-D16).
13. SA1-SA3: `host.Apply` over an imported tree (the proof of next action 4 as a test).
14. Health and backup tests (SH-T1 to SH-T3).
15. Wiring in `admin/wire`; a `-tags tuwunel` test exporting and importing a real database and
    reading the tail back (RT-style). Built (sc-D18): `admin/wire` gives `System.Carry` every
    seam and guard (`TestCarryEngineIsTheHostsAdapters`, `TestCarryEngineStagesAsTheHost`,
    `TestTheGuardsRefuseTheCarryRecords`, `TestTheGuardsRefuseADrillImport`); `carryRound`
    (`admin/carryround_test.go`) runs `team seal`, `team export`, `team import` and
    `team carry-check` over the engine `wire` builds, with fakehs, and
    `TestTuwunelCarryRoundRT` (`admin/carry_tuwunel_test.go`) runs it over a real Tuwunel.
    **Not yet run against a real Tuwunel**: it compiles, vets and lints under the tag and skips
    without `FAKEHS_TUWUNEL_BIN`; the by-hand run (`src-go/CLAUDE.md`, build tags) is pending.
16. Host check SH1, added to the drill's clause list (Q3), run by the driver on the scratch
    guest.
