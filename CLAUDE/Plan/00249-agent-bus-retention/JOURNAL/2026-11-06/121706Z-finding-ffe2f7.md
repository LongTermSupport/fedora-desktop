## 12:17 · finding · —   — Plan written; the WAL archive is the bulk

Plan written from the owner's question about history retention, and three owner decisions
given while it was written: a 14-day maximum for every room (decided), configurable per team
with 14 as default and ceiling, and a check of Plan 00246's cross-network design.

Findings:

- Tuwunel 1.9.4 has no message retention: no `m.room.retention`, no config key. Its only cutoff
  purge is the Synapse-compatible `POST /_synapse/admin/v1/purge_history/{room}`, which skips
  state events and skips local senders unless `delete_local_events: true` (RESEARCH.md B.4).
- The host measurement (anonymised in RESEARCH.md A) shows `db/archive`, RocksDB's WAL
  archive, is 75-90% of every team's database. Tuwunel hard-codes `WAL_size_limit_MB = 1024`
  with no TTL and no key, so a small database keeps every WAL since its first day (B.10).
  Nothing in Tuwunel reads the archive. This is why Phase 1a, the WAL archive, comes before the
  event purge.
- The one reader that a purge breaks is v1's questions index, a replay of the whole questions
  room: purged open questions vanish and question numbers can be reused. The Phase 1 guard and
  the Phase 2 floor event address it (RESEARCH.md C, PLAN.md D5).
- No design federates a room; Plan 00246's remote member is a client of the one homeserver.

Throwaway test teams seen in the measurement are another plan's, removed by its own down
script; they are not a retention matter.

`triage.bash` and its leg `probe-storage.bash` were checked with bash -n and shellcheck, and the
leg was run against a fake tree with a sudo stub (all three modes, exit 0). It has not run on
the host yet; the coordinator queues it.
