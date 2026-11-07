# Plan 00249 research: what the bus stores, what Tuwunel can delete, what reads old history

Three parts: the host measurement (section A), Tuwunel v1.9.4's storage and retention
mechanisms from its source (section B, sections 1-10 and the list of what it cannot do), and
what in this repository reads old room history (section C). [PLAN.md](PLAN.md) holds the design
and the decisions; this file holds the evidence behind them.

## A. Host measurement

Taken read-only on the host on 2026-11-06 by the team's host agent. Team names are anonymised:
team-A is the largest, then B, C and D; E and F are two small test teams. `triage.bash` in this
folder repeats the measurement.

| Team | Team directory | db   | of which `db/archive` (RocksDB's WAL archive) | backups |
| ---- | -------------- | ---- | --------------------------------------------- | ------- |
| A    | 359M           | 268M | 222M (35 files)                               | 91M     |
| B    | 144M           | 113M | 99M (37 files)                                | 31M     |
| C    | 130M           | 104M | 90M (28 files)                                | 27M     |
| D    | 68M            | 57M  | 51M (21 files)                                | 11M     |
| E, F | about 8M each  | 2M   | 156K                                          | 6M      |

- `/var/lib/agent-bus` is 716M in all. `db/media` exists and is empty on every team. The
  installer's state tars are about 12K each, 7 per team: negligible.
- The filesystem is btrfs, 1.6T with 425G free (72% used).
- Backups: 7 daily backup-engine metas per team, a week's span. Their summed file sizes, oldest
  to newest: A 23.5 to 36.8 MiB (10.7 MiB in the last day), B about 8 to 9.9, C 5.6 to 8.6, D
  flat at 4.2, E and F about 0.2.
- **The key finding: `db/archive` is 75-90% of every team's database and nothing prunes it.**
  It holds WAL files back to each team's first day. On team A it grows by up to about 36 MiB a
  day. The live SST data is small: about 46M on A, 6-14M on the others.
- The rendered `tuwunel.toml` sets only `database_backup_path`, `database_backups_to_keep = 7`
  and `rocksdb_allow_fallocate = false` among the storage keys.
- E and F are throwaway test teams of another plan, removed by that plan's own down script: not
  a retention matter.
- The filesystem being btrfs matters: Tuwunel's maintenance doc (`docs/maintenance.md#L75-L93`)
  says a preallocated WAL can pin far more disk than `du` shows, and that removing or
  rewriting the archived files is the only way to get it back. `triage.bash` reports both the
  apparent size and the blocks on disk.

So an event TTL alone does not bound storage: the WAL archive (section B.10) is the bulk, and it
grows until it reaches a hard-coded 1 GiB per team.

## B. Tuwunel v1.9.4

Source: shallow clone of tag v1.9.4 (commit f473d9a4a8342514302884041e0ccfea1ad503fc).
Every Tuwunel citation `path#Ln` is a permalink under
`https://github.com/matrix-construct/tuwunel/blob/v1.9.4/` (so `src/core/config/mod.rs#L230`
is `https://github.com/matrix-construct/tuwunel/blob/v1.9.4/src/core/config/mod.rs#L230`).
RocksDB citations are under `https://github.com/facebook/rocksdb/blob/v11.8.1/`. Tuwunel pins
rust-rocksdb `0.52.0` / librocksdb-sys `0.48.0+11.8.1` from matrix-construct's fork
(`Cargo.toml#L441-L443`, `Cargo.lock#L4076-L4078`). That fork's RocksDB submodule is
matrix-construct/rocksdb@d8a89161, and its `db/wal_manager.cc` is byte-identical to upstream v11.8.1.

### B.1 Config keys that bear on storage growth (src/core/config/mod.rs)

| Key                                                               | Default            | Effect                                                                                                            | Cite                     |
| ----------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------------- | ------------------------ |
| `database_path`                                                   | `/var/lib/tuwunel` | DB root; media lives in `<database_path>/media`                                                                   | `#L136`, `#L5824`        |
| `database_backup_path`                                            | unset              | enables the managed RocksDB BackupEngine                                                                          | `#L212-L221`             |
| `database_backups_to_keep`                                        | 1                  | backups kept after each new backup; \<=0 is an error                                                              | `#L223-L230`, `#L5834`   |
| `save_unredacted_events`                                          | true               | keeps the original JSON of every redacted event                                                                   | `#L3714-L3721`           |
| `redaction_retention_seconds`                                     | 5184000 (60 d)     | how long those originals are kept; 0 = never expire                                                               | `#L3723-L3730`, `#L6239` |
| `delete_rooms_after_leave`                                        | false              | experimental: delete a room when the last local user leaves; not every leave path triggers it                     | `#L3926-L3938`           |
| `forget_forced_upon_leave`                                        | (bool)             | force `/forget` on leave (MSC4267); a membership flag, not a storage purge                                        | `#L1296-L1300`           |
| `media_startup_check`                                             | true               | startup consistency check of the media dir                                                                        | `#L2857-L2868`           |
| `prune_missing_media`                                             | false              | with the startup check, drop DB rows for media files gone from disk                                               | `#L2883-L2892`           |
| `media_create_unused_expiration_time`                             | 86400              | an unused pending MXC (create-then-upload) expires                                                                | `#L653-L659`, `#L5914`   |
| `media_storage_providers`                                         | `["media"]`        | where media is stored (local dir by default)                                                                      | `#L3039-L3049`, `#L6241` |
| `url_preview_cache_ttl`                                           | 86400              | TTL of the URL-preview cache column                                                                               | `#L3276-L3287`           |
| `startup_netburst` / `startup_netburst_keep`                      | true / 50          | outbound federation queue trimmed to 50 per destination at startup; -1 keeps all ("may consume significant disk") | `#L3413-L3433`, `#L6145` |
| `rocksdb_compaction`                                              | true               | automatic compaction; "never" disable it                                                                          | `#L2535-L2544`           |
| `rocksdb_compaction_prio_idle` / `rocksdb_compaction_ioprio_idle` | false / true       | compaction thread priority only                                                                                   | `#L2523-L2533`           |
| `rocksdb_compression_algo`                                        | `zstd`             | compression                                                                                                       | `#L2369-L2385`           |
| `rocksdb_bottommost_compression`                                  | true               | extra compression of the bottom level                                                                             | `#L2419-L2428`           |
| `rocksdb_max_log_files`                                           | 3                  | RocksDB info LOG files, not the WAL                                                                               | `#L2361-L2367`           |
| `rocksdb_allow_fallocate`                                         | true               | WAL preallocation; set false on btrfs/ZFS                                                                         | `#L2576-L2590`           |
| `admin_execute`                                                   | `[]`               | admin commands run at startup (also `--execute`)                                                                  | `#L3505-L3518`           |
| `admin_execute_errors_ignore`                                     | false              | if false, a failing startup command stops the server                                                              | `#L3520-L3526`           |
| `admin_signal_execute`                                            | `[]`               | admin commands run on SIGUSR2                                                                                     | `#L3528-L3536`           |

None of these keys exists at v1.9.4: `retention`, `forget_forgotten_rooms`, `media_retention`,
`rocksdb_periodic_*`, nor any key that sets a per-room or global message lifetime. I grepped
`src/core/config/mod.rs` for each name.

### B.2 `m.room.retention` (MSC1763)

Not implemented. The project's own compliance table marks MSC1763 ❌ with the note "no
m.room.retention support; /retention/configuration endpoint absent"
(`docs/development/compliance/msc.md#L703`). The source has no `m.room.retention` or
`RoomRetention` anywhere. Every `retention` hit in `src/` is about redacted originals
(`src/service/rooms/retention/mod.rs`), task records (`src/service/tasks/mod.rs#L29`) or the
URL-preview cache. Nothing on the server enforces message lifetimes.

### B.3 Admin commands that delete data (src/admin/)

- `!admin rooms delete <room_id> [-f|--force]` (`src/admin/room/mod.rs#L91-L97`,
  `src/admin/room/delete.rs#L7-L17`). It deletes the **whole room**: it kicks local users, removes
  aliases, and unpublishes the room (`src/service/rooms/delete/mod.rs#L89-L145`). It then wipes
  threads, search tokens, extremities, relations, counts, receipts, the state hash, **all PDUs**
  (`timeline.delete_pdus`, `src/service/rooms/delete/mod.rs#L221-L227`,
  `src/service/rooms/timeline/pdus.rs#L46`) and the short room ID. Federation is not required,
  so it works on local-only rooms. It has no cutoff: the room does not survive. It refuses the
  admin room (`delete.rs#L8-L10`). `--force` only widens the erasure of local users'
  left-state (`delete/mod.rs#L147-L148`).
- `!admin rooms prune-empty [-f]`: runs the same delete for every room with no local joined or
  invited user (`src/admin/room/prune_empty.rs#L7-L44`).
- `!admin rooms purge-user <user|regex> [--regex] [--sole-member] [--dry-run]`: deletes every
  room the matched user is joined to (`src/admin/room/mod.rs#L122-L144`).
- `!admin rooms moderation ban-room|ban-list-of-rooms|unban-room|list-banned-rooms`: these
  evict users and block the room. They are not a history purge
  (`src/admin/room/moderation/mod.rs#L17-L47`).
- `!admin users redact-event <event_id>` and `redact-recent <user> <room> <count>`: redact
  messages from local users (`src/admin/user/mod.rs#L273-L289`). See section 8 for what this
  frees.
- `!admin query raw del <map> <key>`, `clear <map> --confirm`, `compact ...`: raw RocksDB
  operations marked "DANGER!!!" (`src/admin/query/raw/mod.rs#L148-L193`).
- Server backups: `!admin server backup-database`, `list-backups`, `verify-backup [id]`,
  `delete-backups <keep>`, `checkpoint-database [--map M] [--path P] [--log-size N]`
  (`src/admin/server/mod.rs#L84-L119`).
- **No admin-room command purges history before a cutoff.** The only caller of
  `timeline.purge_history` is the HTTP route in section 4 (I grepped `.purge_history(`).

### B.4 Synapse-compatible admin HTTP API

The Synapse admin API exists, and it **includes purge_history**. The routes are registered
unconditionally in `src/api/router.rs#L52-L57`, and the room routes are at `#L196-L219`:

- `POST /_synapse/admin/v1/purge_history/{room_id}` (body: `purge_up_to_event_id` or
  `purge_up_to_ts`, plus `delete_local_events`), `POST .../purge_history/{room_id}/{event_id}`,
  and `GET /_synapse/admin/v1/purge_history_status/{purge_id}`
  (`src/api/client/admin/rooms/purge_history.rs#L18-L79`). A timestamp boundary resolves to
  the last event before `ts` (`#L108-L125`). The purge runs as a background task
  (`#L130-L151`). The caller must be a server admin (`#L26`,
  `src/api/client/admin/mod.rs#L66-L75`). The project marks it ✅
  (`docs/development/compliance/synapse-admin.md#L105-L106`).
- What it deletes (`src/service/rooms/timeline/purge.rs#L22-L98`): it walks the room's events
  in stream order before the boundary. **It skips state events, and it skips events from local
  senders unless `delete_local_events` is true** (`#L57-L61`). For each event it deletes
  `pduid_pdu`, `eventid_pduid`, `eventid_outlierpdu` and `roomid_tscount_pducount`
  (`#L70-L77`). It removes the event from search, deletes its relations, and drops the
  retained original (`#L79-L92`). Forward extremities are left untouched, and an error can stop
  the purge partway (`#L22-L27`). The room stays alive. Every sender in a local-only bus is
  local, so the request must set `delete_local_events: true`. That field's default comes from
  the external `synapse-admin-api` crate, which is not in this tree, so I could not verify it
  here. Synapse's default is false.
- Room delete: `DELETE /_synapse/admin/v1/rooms/{room_id}` (synchronous) and
  `DELETE /_synapse/admin/v2/rooms/{room_id}` (background), with `purge` and `block`. Synapse's
  `force_purge` is not mapped (`src/api/client/admin/rooms/delete_room.rs#L12-L70`).
- Media: `POST /_synapse/admin/v1/media/delete`, `.../media/{server}/delete`,
  `POST /_synapse/admin/v1/purge_media_cache`, `DELETE /_synapse/admin/v1/users/{id}/media`,
  `DELETE /_synapse/admin/v1/media/{server}/{id}` (`src/api/router.rs#L221-L239`).

### B.5 Media

Media files sit on the filesystem at `<database_path>/media`
(`src/service/media/mod.rs#L1045-L1051`). They are content-addressed by sha256
(`#L1019`). Only the metadata is in RocksDB. Other storage providers can be configured
(`src/core/config/mod.rs#L3039-L3049`). Backups and checkpoints do not include media
(`docs/backups.md#L10-L14`). Cleanup tools:

- `!admin media delete-range <dur> --older-than|--newer-than [--yes-i-want-to-delete-local-media]`:
  by file mtime. It deletes remote media only unless the flag is given
  (`src/admin/media/mod.rs#L42-L65`).
- `delete-all-from-user <local user>` (`#L67-L71`), `delete-all-from-server <server>`
  (`#L73-L81`), `delete --mxc`, `delete-by-event`, `delete-list` (`#L23-L40`).
- HTTP `media/delete` deletes **local** media older than `before_ts` by mtime, above
  `size_gt`, and keeps profile media by default (`src/service/media/mod.rs#L722-L728`,
  `src/api/client/admin/media/delete_media_by_date_size.rs#L10-L28`). `purge_media_cache`
  deletes remote media by mtime (`src/api/client/admin/media/purge_media_cache.rs#L11-L28`).
- No automatic, time-based media expiry exists, except for unused pending MXCs (section 1).

### B.6 RocksDB compaction

Compaction runs automatically and is on by default (`src/core/config/mod.rs#L2535-L2544`,
`src/database/engine/db_opts.rs#L68`). The docs advise against running it by hand or on a
timer (`docs/maintenance.md#L38-L43`). Every column gets level compaction with a RocksDB `ttl`
of 21 days (`src/database/engine/descriptor.rs#L98-L101`, `src/database/engine/cf_opts.rs#L45`).
In RocksDB, a level-compaction `ttl` forces compaction of SST files older than the ttl, so
tombstones from a purge are compacted out within about 21 days even when write pressure is
low. Manual trigger: `!admin query raw compact [-m map…] [--start/--stop] [--parallelism N] [--exhaustive]` (`src/admin/query/raw/mod.rs#L167-L193`, `src/database/map/compact.rs#L56`).
Flush: `!admin query raw flush` (`#L195-L196`). No `rocksdb_periodic_*` key exists.

### B.7 Backups

The backups use RocksDB's **BackupEngine** (`src/database/engine/backup.rs#L3`, `#L178-L183`).
`backup()` calls `create_new_backup_flush(&db, flush=true)` on a writable DB (`#L35-L40`). It
then calls `purge_old_backups(database_backups_to_keep)` (`#L54-L57`). A purge failure is only
logged (`#L16-L17`). The options are `BackupEngineOptions::new(path)`, the RocksDB defaults, so
SST files are shared. The docs' manual-restore procedure confirms this: it copies `.sst` files
from `$DATABASE_BACKUP_PATH/shared_checksum` (`docs/backups.md#L106-L111`). Backups are
therefore **incremental**: each backup adds only SSTs that are new since the previous one.
Backups only happen when `server backup-database` runs (via the admin room, `admin_execute` or
`admin_signal_execute`; the only caller is `src/admin/server/backup_database.rs#L9`). There is
no built-in timer.

Consequence for history deletion: after a purge, the deleted rows still exist in the old SSTs
that the retained backups reference. Disk use in the backup path drops only once every backup
holding those SSTs has been purged, that is after `database_backups_to_keep` newer backups taken
after compaction, or after `delete-backups <n>`. Until then the live DB shrinks (after
compaction) but the backup repository does not. `checkpoint-database` uses hard links and has
no retention of its own (`docs/backups.md#L124-L176`).

### B.8 Redaction and storage

`redact_pdu` (`src/service/rooms/timeline/redact.rs#L36-L115`) first saves the original via
`retention.save_original_pdu` (`#L58-L61`). It then de-indexes search, deletes the typed
relation, and **rewrites the PDU row in place** with the redacted JSON (`raw_put` on
`pduid_pdu`, `src/service/rooms/timeline/mod.rs#L161-L202`). With the default
`save_unredacted_events = true`, the original content is kept in `eventid_originalpdu`. A
background worker deletes it hourly once `redaction_retention_seconds` (60 days by default)
has passed (`src/service/rooms/retention/mod.rs#L24-L76`). If `redaction_retention_seconds = 0`
the worker never cleans up (`#L49`), and the originals are kept forever. Redaction therefore
frees content only after 60 days (or at once if `save_unredacted_events = false`). The event
skeleton always remains. Originals can be read with `!admin debug get-retained-pdu`
(`src/core/config/mod.rs#L3716`). A Synapse-style `POST /_synapse/admin/v1/user/{id}/redact`
also exists (`src/api/client/admin/users/redact.rs#L47`).

### B.9 Sizes and counts, and running admin commands non-interactively

- `!admin query raw count [map] [prefix]`: record count, with an optional key prefix
  (`src/admin/query/raw/mod.rs#L127-L134`, `count.rs#L14-L25`). The prefix accepts `\xHH`
  escapes (`mod.rs#L234-L246`), so you can count one room's `pduid_pdu` keys by its 8-byte
  shortroomid, which `!admin query short short-room-id` returns
  (`src/admin/query/short/mod.rs#L18`). `keys-total`, `vals-total`, `keys-sizes` and
  `vals-sizes` report byte totals per map or prefix (`raw/mod.rs#L91-L125`). `maps` lists the
  column names (`#L30-L31`).

- `!admin debug database-stats [property] [-m map]`: a RocksDB property, by default
  `rocksdb.stats` (`src/admin/debug/mod.rs#L308-L314`, `database_stats.rs#L16`).
  `debug database-files [map] [--level]` lists SSTs (`mod.rs#L319-L325`).
  `server memory-usage` covers memory only (`src/admin/server/mod.rs#L78-L79`). Some
  statistics require a higher `rocksdb_stats_level` (`src/core/config/mod.rs#L2546-L2559`).

- Ways to run a command non-interactively:

  - a message in the admin room, whose reply lands in the room;
  - `--execute "<cmd>"` on the command line (`src/main/args.rs#L118-L120`) or `admin_execute`;
  - `admin_signal_execute` on SIGUSR2 (`src/service/admin/execute.rs#L84-L111`);
  - `--console` with a TTY (`src/main/args.rs#L113-L116`);
  - `-O key=value` overrides config (`#L40-L42`).

  Output from execute or signal goes to the console (stdout/stderr) when built with `console`,
  and to the log (`info!`/`error!`) otherwise (`src/service/admin/execute.rs#L128-L154`). Its
  only consumer is the process output or the journal. The docs pattern for a one-shot run is
  `--maintenance --execute "server shutdown"` (`docs/backups.md#L89-L97`).

### B.10 RocksDB WAL archive

(a) **No Tuwunel config key controls the WAL archive.** The values are hard-coded in
`src/database/engine/db_opts.rs`:

- `WAL_size_limit_MB` = **1024** (`#L59`).
- `max_total_wal_size` = 512 MiB (`#L60`).
- `WAL_ttl_seconds` is never set, so it stays at the RocksDB default of 0
  (`include/rocksdb/options.h#L1116`). There is no `wal_ttl` anywhere in `src/`.
- `recycle_log_file_num` is never set: default 0 (`options.h#L978`).
- `manual_wal_flush` = true (`db_opts.rs#L34`).
- `track_and_verify_wals_in_manifest` = true (`#L104`).
- `wal_recovery_mode` comes from `rocksdb_recovery_mode`, default 1 = TolerateCorruptedTailRecords
  (`db_opts.rs#L92-L98`, `src/core/config/mod.rs#L2430-L2465`).
- `rocksdb_allow_fallocate` only adds `allow_fallocate=false` (`db_opts.rs#L62-L65`).

The only raw options string passed to RocksDB is that fallocate one. tuwunel.toml offers no
pass-through for other RocksDB options.

(b) **Why WALs are archived:** RocksDB archives an obsolete WAL instead of deleting it whenever
`WAL_ttl_seconds > 0 || WAL_size_limit_MB > 0` (`db/db_impl/db_impl_files.cc#L766-L768`,
`options.h#L1099-L1102`). Tuwunel's hard-coded `WAL_size_limit_MB = 1024` turns archiving
on. No Tuwunel feature reads the archive:

- `src/` never calls `GetUpdatesSince` or any WAL iterator.
- Backups flush first (`backup.rs#L36-L40`).
- `checkpoint-database --log-size` defaults to 0, which means flush and needs no WAL
  (`src/admin/server/mod.rs#L99-L101`).
- The secondary-mode resync uses `try_catch_up_with_primary` (`src/database/engine.rs#L158-L169`),
  which tails the primary's live WALs.

The maintenance doc says it plainly: obsolete WALs "are moved to `database_path/archive` and
reaped only once the archive passes 1 GB" (`docs/maintenance.md#L81-L83`).

(c) **RocksDB's own purge:** `WalManager::PurgeObsoleteWALFiles` runs from
`DBImpl::PurgeObsoleteFiles` (`db_impl_files.cc#L834`). With only the size limit set, it acts
at most every 600 s (`db/wal_manager.h#L132`, `db/wal_manager.cc#L156-L168`). It deletes empty
archived WALs (`wal_manager.cc#L225-L240`). It then keeps
`floor(WAL_size_limit_MB MiB / largest archived file size)` files and deletes the oldest ones
beyond that (`#L246-L284`). **Below 1 GiB of archive, nothing is ever deleted.** So on a
database whose total size is under about 1 GiB, the archive keeps every WAL since day one,
which matches the 75-90% host measurement. A restart changes nothing, because the limit is
fixed in code. Setting a key plus a restart is impossible at v1.9.4: no key exists. Fixing it
inside Tuwunel needs a code change (set `WAL_ttl_seconds`, or a lower size limit, or both to 0
so obsolete WALs are deleted at once), which is an upstream issue or PR.

Deleting archive files by hand: the RocksDB code treats archived WALs as already obsolete:

- with WAL tracking on, the MANIFEST records `DeleteWalsBefore` when WALs become obsolete at
  flush (`db/memtable_list.cc#L1015-L1017`);
- RocksDB itself deletes them with a plain `DeleteDBFile`, with no MANIFEST edit
  (`wal_manager.cc#L229`, `#L274`);
- the only in-memory state is `read_first_record_cache_`, which only `GetUpdatesSince` uses.

So removing `archive/*.log` while the server runs looks harmless per this code. But that is
not a documented operation, and I did not find a RocksDB doc that blesses it. The documented,
IaC-friendly reclaim is an offline copy of `database_path` (`docs/maintenance.md#L87-L93`).
Ordinary `rm` of archive files would be a manual operation under this repository's IaC rule.
If it is wanted, a playbook or timer would have to own it.

### B.11 What Tuwunel cannot do (v1.9.4)

- There is no `m.room.retention` / MSC1763: no per-room or server-wide message lifetime, and no
  automatic expiry of history.
- There is no admin-room command to purge history before a cutoff. Only the Synapse-compat HTTP
  `purge_history`, called with an admin access token, can do it.
- `purge_history` never deletes state events. It leaves forward extremities and the
  event-ID/short-ID/state maps in place. It deletes local senders' events only with
  `delete_local_events: true`.
- There is no config key for WAL archive size or TTL. The archive is capped at a hard-coded
  1 GiB, so a small database keeps every WAL it has ever written.
- There is no built-in backup schedule. Backups are incremental, and they keep purged data on
  disk until they rotate out.
- Media has no automatic time-based retention, and media is not part of database backups.
- Redaction does not free content at once when `save_unredacted_events` is true (60-day
  default). It never frees it if `redaction_retention_seconds = 0`.
- There is no `forget_forgotten_rooms`, `media_retention` or `rocksdb_periodic_*` setting.

## C. What in this repository reads old history

Traced through v1 (`helpers/pingbus/`), v2 (`src-go/internal/teamchat/`) and the plans. The one
property everything below rests on: **no consumer reads room state from the timeline.** Trust,
roster, membership, `m.room.create`, power levels, topic, colour and the rooms index come from
`/state` and `get_state` (v1 `helpers/pingbus/syncer.py:335-447`, `:779-807`; v2
`member/teamsync/session.go:112-126`). A purge that keeps the current room state therefore
leaves all of them working. What breaks is whatever reads old *timeline* events.

| Consumer                                                                                                                           | What it reads                                                                                                                                                                            | How far back        | Effect of events older than 14 days going                                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| v1 and v2 trust, roster, membership                                                                                                | room state                                                                                                                                                                               | any                 | none, while state is kept                                                                                                                                                                                                                                                                                        |
| v1 sync cursor (`sync.json`, `inbox.py:16`, `:437`)                                                                                | forward from the token; a gap is filled forward, at most 20 pages of 100 (`syncer.py:733-777`)                                                                                           | since the last sync | fewer events in a long gap, counted as `gap` drops, as today past the bound. There is no handling of a refused `since` token (the only reset is `seat retire`, `cli.py:3005`); a purge does not normally invalidate a stream token, but this is tested in Phase 2                                                |
| v2 catch-up (SY1, `session.go:161-237`, `:347-368`)                                                                                | back to the floor marker or the member's own join, at most 20 pages of 100                                                                                                               | the downtime        | a member down longer than 14 days with no floor marker reports a gap: the same as a long gap today. A refused token backs off and retries (`teamsync.go:192-221`)                                                                                                                                                |
| v2 send recovery (`engine/sender.go:25-29`, `:519-542`), notice `LatestOwn` (`notice/notice.go:23-24`, `:142-167`)                 | back to the last own event, 5 and 10 pages                                                                                                                                               | recent              | none in practice                                                                                                                                                                                                                                                                                                 |
| v1 `history` (`syncer.py:899-938`), v2 `history` (`verbs.go:191`)                                                                  | head backwards, at most 5000 (v1) or 100 (v2) events                                                                                                                                     | recent              | shows less; nothing fails                                                                                                                                                                                                                                                                                        |
| **v1 questions index** (`syncer.py:588-611`, `protocol.py:1606-1697`)                                                              | **the whole questions room**, re-read on every questions batch and on `questions` and `ask-human` (already silently cut at 5000 events: its `complete` flag is ignored, `syncer.py:610`) | the room's start    | **breaks.** An open question whose ask event is purged vanishes from `questions`; human replies to it no longer attach (`attach_question`, `:1632-1663`); `next_number()` is the highest number seen plus one, so numbers are **reused** once the highest is purged; the duplicate check misses purged questions |
| v1 single-event fetches: `sender_of` (`syncer.py:321-332`), `_refetch` (`cli.py:1445-1466`), `ask-human --re` (`cli.py:1280-1292`) | one event by id                                                                                                                                                                          | any                 | a human reply to a purged ping reaches no agent; a purged inbox item becomes a `schema` drop; `--re` to a purged event is refused                                                                                                                                                                                |
| v2 questions index (SPEC-n3f QX1, `:290-345`; n3f-D9, `:834`)                                                                      | from `m.room.create` forwards, on a first promotion, an epoch change, a refused token or a QX7 redaction                                                                                 | the room's start    | **not built yet** (Plan 00209 `PLAN.md:309`, `:315-318`). As designed it breaks the same way as v1 on every rebuild: purged open questions are lost and the numbering falls back to `question_floor`. A seat that keeps its index (in its store, capped at 3 MiB today, 4 MiB under QX6) survives                |
| Restore export RC3 (`admin/serverestore.go:458-544`)                                                                               | main, reports and questions back to the backup's time, capped at 64 MiB                                                                                                                  | the backup's age    | a restore from a backup older than 14 days loses the events between the backup and the cut. Backups are kept 7 days, so this does not arise unless that is raised                                                                                                                                                |
| Team history export (Plan 00178)                                                                                                   | the room from its first event, `dir=f`                                                                                                                                                   | the room's start    | plan only, "Not Started"; an export can only ever hold 14 days. Its purpose (keep history outside the server) becomes the way to keep anything longer                                                                                                                                                            |
| v1 to v2 migration M3 export (Plan 00209 `DESIGN-v2-D-migration.md:948-958`) and OD-D6 archive (`DESIGN-v2.md:358`, D `:960-966`)  | every event; Element browsing the v1 rooms in full                                                                                                                                       | the room's start    | **conflicts with the design**: both assume full history. Under the owner's 14-day cap the export holds 14 days and the archive shows 14 days                                                                                                                                                                     |
| Element (humans scrolling back)                                                                                                    | `/messages` backwards                                                                                                                                                                    | any                 | sees 14 days                                                                                                                                                                                                                                                                                                     |
| Acceptance scripts (`00161`, `00194`, `00203`, `00211`)                                                                            | events they just posted                                                                                                                                                                  | minutes             | none                                                                                                                                                                                                                                                                                                             |

Client copies sit outside the server's retention: Element's local store, pingbus's state files,
v2 teamchat's store. A purge does not reach them; each is bounded by its own cap or by the client.

Paths are relative to `helpers/pingbus/` for v1 and `src-go/internal/teamchat/` for v2.

## D. Cross-network membership (Plan 00246) and federation

Plan 00246 Task 8.8 lets a member on another machine join a team: it is a **client** of the
team's one homeserver, over TLS with the team's CA pinned, its token issued over ssh. No design
in Plans 00246 or 00209 puts a second homeserver in a room: federation is off by design
(`allow_federation = false` in both renderers, `helpers/agent_bus/render.py` and
`src-go/internal/teamchat/host/render.go`; Plan 00209 `research-remote-seat.md` lines 21 and
83: "There is no federation ... one server and no other origin"). So there is one server-side
copy of each room and the purge on that server is the whole of server-side retention.

What a remote member adds is a client-side copy on another machine: its teamchat store (capped,
as above) or an Element store. Those are outside the server's retention in the same way as a
local member's, and the plan does not reach them.

If a future design ever federated a room to a second homeserver, each server would hold its own
copy and enforce only its own retention. `purge_history` acts on the local copy only and leaves
the forward extremities in place (B.4), so a federated peer could serve purged events back by
backfill. Such a design would have to run the same retention on every server in the room; the
plan records that as a condition on any federation change (PLAN.md decision D11).
