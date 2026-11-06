# Plan 00249: agent bus retention

**Status**: Not Started (plan only; owner decisions D1-D15 open, the 14-day maximum is decided)
**Created**: 2026-11-06
**Owner**: joseph
**Priority**: High

## Overview

Every agent team runs its own Tuwunel homeserver, and nothing deletes anything: room history is
kept forever. The owner (2026-11-06): "how much history does it retain, and where is it being
stored? ... we need to have some form of TTL or retention period and then stuff should be cleared
out or we're going to just explode storage." Then, as a decision: storage must be constrained
urgently, with **a maximum retention of 14 days for every room**, configurable per team
(default 14, and 14 as the ceiling unless the owner raises it).

The host measurement ([RESEARCH.md](RESEARCH.md) section A) changes where the space is. The live
data is small; **75-90% of every team's database is `db/archive`, RocksDB's archive of old
write-ahead-log files**, which Tuwunel turns on with a hard-coded 1 GiB limit and no config key
(RESEARCH.md B.10). Nothing reads that archive. So Phase 1 bounds the WAL archive first (the
fastest real win), then enforces the 14-day event cap through Tuwunel's Synapse-compatible
`purge_history` admin route, which is the only cutoff purge Tuwunel has (B.4). Both run from a
per-team systemd timer that the playbook installs; nothing is done by hand.

This plan is the full review of homeserver storage: database, WAL archive, backups, state tars,
media, compaction and disk headroom. v1 (`agent-bus`, the teams running now) comes first; v2
(`teamchat`, landing under Plan 00246) gets the same in Phase 3.

## Goals

- No team's WAL archive holds more than a day of WAL files, held there by a timer.
- No room of any team serves an event older than its team's `retention_days` (default and
  ceiling 14), held there by the same timer.
- `retention_days` is declared per team in `agent_bus_teams`, validated fail-fast, and applied
  by the play.
- Disk use per team and the free space under it are measured on every run, and a breach fails
  the unit where `ccy-teams` and `systemctl --failed` show it.
- Everything that reads old history keeps working, or what it loses is stated and accepted by
  the owner (RESEARCH.md section C).

## Non-Goals

- Client-side copies (Element's store, pingbus's state files, teamchat's store): each has its
  own bound, and a server purge does not reach them.
- Patching or forking Tuwunel. A config key for the WAL archive is asked for upstream (Phase 4).
- Throwaway test teams: they are removed by their own plan's down script.
- Keeping history longer than 14 days anywhere (an export or an archive): Plan 00178 stays plan
  only (D13).

## Context & Background

- Research and evidence: [RESEARCH.md](RESEARCH.md): A the measurement, B what Tuwunel v1.9.4
  can and cannot do, cited at the tag, C every reader of old history and what it loses, D
  cross-network membership.
- Measurement script: [triage.bash](triage.bash) (with its leg `probe-storage.bash`), read-only,
  host-only. Re-run after each phase.
- Related plans: 00161 (the v1 bus), 00203 (health and backups), 00211 (reports and questions
  rooms), 00209 (v2 design, SPEC-n3f QX1, migration M3 and OD-D6), 00246 (v2 landing and
  cross-network), 00178 (history export, plan only).

### What Tuwunel v1.9.4 can and cannot do (RESEARCH.md B)

| Need                        | Tuwunel                                                                                                                                                                                          |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Message lifetime config     | **none**: no `m.room.retention` (MSC1763 unsupported), no retention key                                                                                                                          |
| Purge history before a time | **only** `POST /_synapse/admin/v1/purge_history/{room}` with a server admin token; skips state events; skips local senders unless `delete_local_events: true`; background task with status route |
| Delete a whole room         | `!admin rooms delete`, HTTP `DELETE /_synapse/admin/v1/rooms/{room}`; no cutoff                                                                                                                  |
| WAL archive                 | hard-coded `WAL_size_limit_MB = 1024`, no TTL, **no key**: a small database keeps every WAL since day one                                                                                        |
| Compaction                  | automatic; each column has a 21-day RocksDB `ttl`, so purged rows are compacted out within about 21 days                                                                                         |
| Backups                     | RocksDB BackupEngine, incremental (shared SSTs), only when asked; purged rows stay in the backups until they rotate out                                                                          |
| Media                       | files under `<database_path>/media`, not in backups; deletion by age through admin HTTP; no automatic expiry                                                                                     |
| Redaction                   | rewrites in place but keeps the original 60 days (`redaction_retention_seconds`)                                                                                                                 |

## Design

### The per-team key

- **Where:** `retention_days` in the team's `agent_bus_teams` entry (host_vars). The play copies
  the entry whole into the team file (`play-agent-bus.yml`, "Render each team file"), the
  installer validates it and keeps it as `/var/lib/agent-bus/<team>/team.json`. Tuwunel has no
  key for it, so it is not rendered into `tuwunel.toml` and its change restarts nothing.
- **Validation (fail fast):** `helpers/agent_bus/teamfile.py` gains it as an optional key: a JSON
  integer, not a boolean, from 1 to `RETENTION_CEILING_DAYS = 14`; absent means 14. Anything
  else refuses the team file with `retention_days: <value> is not a whole number of days from 1 to 14`, and the play stops on that team. The Go tool reads `team.json` and refuses the same
  values again, so a hand-edited file cannot slip through.
- **Applying a change:** edit the entry, run the play. The installer rewrites `team.json`; the
  next timer run uses the new value. Lowering it purges more on that run; raising it cannot bring
  anything back.
- **Per room kind:** one value per team covers main, reports and questions (and the admin room,
  D15). An optional per-kind override, each no higher than the team's value, is possible later
  (D2).

### The timer and the tool

- A Go command, `agent-bus-retention` (`src-go/cmd/agent-bus-retention`,
  `src-go/internal/busretention`), built and installed by `play-agent-bus.yml` through
  `tasks/build-go-command.yml`, with a row in `scripts/go-binary-suites.tsv`.
- `agent-bus-retention@.service` (oneshot, `User=agent-bus`, the homeserver unit's sandbox with
  `ReadWritePaths=/var/lib/agent-bus/%i` and loopback-only IP access) and
  `agent-bus-retention@.timer` (daily, `RandomizedDelaySec`, after the backup timer's window).
  The play installs both and enables the timer for each present team, and disables it for each
  absent one: `agent-bus-install` is on the bash ratchet allowlist and must not grow.
- One run, in order, every step fail-fast:
  1. **purge**: for each team room (`room_id`, `room_id.reports`, `room_id.questions`), `POST purge_history` with `purge_up_to_ts = now - retention_days` and `delete_local_events: true` sent explicitly (its default could not be verified, B.4), then poll the status route
     until `complete`; `failed` or a timeout fails the run. The admin token is read from
     `secrets/admin.token` by the tool itself, never put in argv, the environment or a log.
  2. **questions guard (Phase 1 only, D5):** the questions room's cutoff is the earlier of the
     time cutoff and the newest question's ask event, so v1's question numbering (highest seen
     plus one) can never reuse a number.
  3. **WAL archive**: delete each file in `db/archive/` last modified more than 24 hours ago.
     RocksDB treats every archived WAL as obsolete and deletes them itself the same way, with
     no MANIFEST edit (B.10); the real-Tuwunel test below proves it on the pinned binary (D4).
  4. **measure**: sizes of the db, the archive, backups, media and state tars, and the free
     space of the filesystem, written to `/var/lib/agent-bus-applied/retention/<team>.json`
     (no secret); over a threshold (D9) the run exits non-zero after writing it.
- `agent-bus-retention report --team T` is read-only: each room's oldest event age and count,
  and the record above. `acceptance.bash` and later triage use it.

### Backups, compaction, media

- Backups stay at 7 (D8). Purged rows leave the backups after the 7 backups that hold their SSTs
  rotate out.
- No forced compaction (D7): the 21-day column `ttl` compacts purged rows out. So an event
  leaves the API at 14 days and the disk by at most 21 days after its SST was written, plus 7
  days in the backups. The WAL archive pruning is what removes the bulk now.
- Media is empty on every team today. Phase 2 deletes local media older than the team's
  `retention_days` through the admin media route in the same run.

### Cross-network and federation

Plan 00246's remote member is a client of the team's one homeserver; no design federates a room,
so one server copy is purged and retention holds (RESEARCH.md D). Any future federation would
have to run the same retention on every server in the room (D11).

## Tasks

### Phase 1a: bound the WAL archive (first deploy, on its own)

- [ ] ⬜ **Task 1.1**: Tests first, then `agent-bus-retention wal-prune --team T`: the archive
  walk over an `fs.FS` fake (old files deleted, recent kept, a non-file or a link refused, an
  unreadable directory fails), and the measure step.
- [ ] ⬜ **Task 1.2**: Real-Tuwunel test (`tuwunel` build tag, the pinned binary): write events
  until WALs are archived, delete the archive while the server runs, write again, restart, and
  check the database opens and every event reads back.
- [ ] ⬜ **Task 1.3**: The unit and timer files, and the play: build and install the command,
  install the units, enable the timer per present team, disable per absent team.
- [ ] ⬜ **Task 1.4**: `deploy.bash` (the play, then `acceptance.bash`) and `acceptance.bash`
  (timer enabled and its last run succeeded, no archive file older than 24 hours plus the timer
  period, the sizes reported; `COVERAGE: n of m`). Queue the plan in `meta-deploy.list`.

### Phase 1b: the 14-day event cap

- [ ] ⬜ **Task 1.5**: `retention_days` in `teamfile.py` (tests first: absent, 1, 14, 0, 15, a
  string, a float, `true`), in `localhost.yml.dist`'s commented example and in
  `docs/agent-bus.md`.
- [ ] ⬜ **Task 1.6**: Tests first, then the purge step against a fake homeserver: the request
  body (cutoff, `delete_local_events: true`), status polling, `failed`, a timeout, a 4xx, a room
  file missing, the questions guard, the token never in argv or output.
- [ ] ⬜ **Task 1.7**: Real-Tuwunel test: purge with a cutoff after a run of local messages; the
  room's state, membership and power levels are intact, `/messages` before the cutoff is empty,
  posting and syncing still work, and a sync token from before the purge still syncs.
- [ ] ⬜ **Task 1.8**: The `report` verb, and acceptance extended: every room's oldest event no
  older than `retention_days` (plus the timer period), a bad `retention_days` refused by the play.

### Phase 2: the rest of v1

- [ ] ⬜ **Task 2.1**: v1 question-number floor: the tool writes a state event in the questions
  room with the next free number before purging, and pingbus's `next_number()` takes the larger
  of that and the highest seen plus one. Once every member kit carries it, drop the guard (D5).
- [ ] ⬜ **Task 2.2**: Media older than `retention_days` deleted in the run (D14).
- [ ] ⬜ **Task 2.3**: Disk alerting: the record's problems on the team's `ccy-teams` row; the
  thresholds of D9.
- [ ] ⬜ **Task 2.4**: `ccy-teams` shows and edits `retention_days` with the other team fields.
- [ ] ⬜ **Task 2.5**: Per-kind overrides, only if D2 asks for them.
- [ ] ⬜ **Task 2.6**: The admin room, if D15 includes it.

### Phase 3: v2 (teamchat)

- [ ] ⬜ **Task 3.1**: `retention_days` in v2's team declaration with the same rule, the purge
  and WAL pruning in `teamchat-admin` on its own timer, the question floor raised before each
  purge so a QX1 rebuild keeps its numbering; coordinated with Plans 00246 and 00209.
- [ ] ⬜ **Task 3.2**: Record in Plan 00209 that M3's export and the OD-D6 archive hold at most
  14 days of history (D12).

### Phase 4: upstream

- [ ] ⬜ **Task 4.1**: An issue on Tuwunel's tracker asking for config keys for `WAL_ttl_seconds`
  and `WAL_size_limit_MB` (D14), with no install detail. When a pinned release has them, set
  them in both renderers and delete the `wal-prune` step.

### Phase 5: close

- [ ] ⬜ **Task 5.1**: `triage.bash` re-run after each phase; the anonymised numbers in the
  journal.
- [ ] ⬜ **Task 5.2**: `qa-reviewer` over the plan's diff; findings fixed.

## Owner decisions

The 14-day maximum is decided (2026-11-06). Each item below carries the recommendation.

| #   | Decision                                      | Recommendation                                                                                                                                                                     |
| --- | --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | TTL per room kind                             | main **14** days, reports **14**, questions **14**: the ceiling for all, until per-room counts (the `report` verb) show a room worth cutting shorter                               |
| D2  | Different periods per room kind in one team   | not in Phase 1: one `retention_days` per team; an override per kind, never above the team's value, only if asked                                                                   |
| D3  | Where the ceiling lives                       | a constant (`RETENTION_CEILING_DAYS = 14`) in the validator, raised only by a reviewed commit                                                                                      |
| D4  | WAL archive: delete while running or stopped  | while running, files older than 24 hours, daily: the code shows RocksDB does the same, Task 1.2 proves it; stopping each homeserver daily buys nothing                             |
| D5  | Questions room in Phase 1                     | keep from the newest question's ask event (the guard), so numbers are never reused, until Task 2.1's floor ships; the room may then exceed 14 days only when no question was asked |
| D6  | Open questions older than the TTL             | lost from `questions` and their late replies unattached: accept; a question open 14 days is stale                                                                                  |
| D7  | At-rest bound                                 | no forced compaction: gone from the API at 14 days, from the live disk by the 21-day compaction `ttl`, from backups 7 days later                                                   |
| D8  | `database_backups_to_keep`                    | stay at 7                                                                                                                                                                          |
| D9  | Disk alert thresholds                         | a team's directory over 1 GiB, or the filesystem under 10% free, fails the run and shows on `ccy-teams`                                                                            |
| D10 | A v1 change while v1 is feature-frozen        | allowed for this plan only, by the owner's urgent decision                                                                                                                         |
| D11 | Federation                                    | stays off; any design that federates a room must run the same retention on every server in it                                                                                      |
| D12 | v1-to-v2 migration (M3 export, OD-D6 archive) | accept that they carry at most 14 days                                                                                                                                             |
| D13 | Plan 00178 export before each purge           | no: it would rebuild the storage the cap removes                                                                                                                                   |
| D14 | Media, and the upstream issue                 | delete local media past the TTL (Phase 2); file the WAL-key issue on the public tracker with generic text                                                                          |
| D15 | Tuwunel's own admin room                      | include it in the purge once `report` shows its size                                                                                                                               |

## Dependencies

- Depends on: Plan 00161 (v1 bus), Plan 00211 (side rooms).
- Coordinates with: Plan 00246 (v2 landing, cross-network), Plan 00209 (v2 design).

## Success Criteria

- [ ] After Phase 1a, every team's `db/archive` holds at most a day and a timer period of WAL,
  and `triage.bash` shows the database shrink by the archive's share.
- [ ] After Phase 1b, `report` shows no event older than `retention_days` in any room, and the
  bus works (send, receive, `questions`, `history`).
- [ ] A bad `retention_days` stops the play with a message naming the team and the key.
- [ ] QA passes (`./scripts/qa-all.bash`); `qa-reviewer` finds nothing open.

## Risks & Mitigations

| Risk                                                | Impact | Probability | Mitigation                                                                 |
| --------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------- |
| Deleting archived WALs corrupts a database          | H      | L           | Task 1.2 on the pinned binary first; a daily backup exists; 24-hour margin |
| `delete_local_events` ignored, so nothing is purged | M      | M           | sent explicitly; Task 1.7 and acceptance check the oldest event            |
| A purge breaks v1 question numbering                | M      | M           | the guard (D5) until the floor (Task 2.1)                                  |
| A purge invalidates a member's sync token           | H      | L           | Task 1.7 syncs with a pre-purge token                                      |
| Purged content lingers in SSTs and backups          | L      | H           | stated bound (D7); WAL archive pruning removes the bulk                    |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00249-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan, research and `triage.bash`: this plan's first commit.
