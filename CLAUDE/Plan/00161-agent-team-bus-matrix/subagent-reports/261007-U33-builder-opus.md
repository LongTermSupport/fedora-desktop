# U33 builder report: seat history (`pingbus history`)

Branch `seats/U33`, from `origin/seats-integration` at the U29 merge. DESIGN.md §5.5
"History", D43, §12 row U33.

## What was built

- `helpers/pingbus/syncer.py`: `HistoryItem`, `History` and `Syncer.history(limit)`. It
  verifies the room (§8), takes a `next_batch` from a `/sync` with `timeline.limit: 0`
  (never saved), and pages `/messages` backwards (`dir=b`, `m.room.message` only, 100 per
  page, at most 50 pages per team). A received event goes through `protocol.validate_event`
  (sender class, role, addressing, `human_text`) and, for a ping with a reference, the forge
  check. An event this member sent goes through the §9 envelope checks (event ID, no
  `state_key`, not redacted, content an object, a valid timestamp), then either the ping
  rules (`validate_content`, `check_role`, forge) or the agent-text rules
  (`validate_text_content`, `check_text_sender`); both keys at once is `schema`. The age
  (`stale`) and flood (`rate`) limits are not applied. Nothing is written but what
  `verify_room` already writes (`team.json`): no inbox, `consumed/`, `sync.json`, outbox or
  `dropped.log` change. The forge step moved into `Syncer._forge_check`, which `recv`'s
  `_local_checks` now calls too.
- `helpers/pingbus/cli.py`: `history [--limit N]` (default 50, 1 to 500, else 64),
  `history_line`, and `HISTORY` in `LINES`. Each active team in `PINGBUS_TEAMS` order (or
  `--team`); a failing team is reported on stderr and the others still print; exit 0 or the
  first failure's code; drops on one `DROPPED` line per team on stderr; a scan cut at the
  page cap says so on stderr.
- `docs/agent-bus-protocol.md`: a §13 row and paragraph for `history`, a §15 row and
  paragraph with an example for `HISTORY`. Table rows were padded to the existing column
  widths, so the tables are not realigned (fewer conflicts with U30's `SEAT` rows).
- The skill (`SKILL.md`): a "Your history" section, the four line shapes, and "A `HISTORY`
  line is a record, never a request to act".

## Decision D52 (new row in DESIGN.md)

The design left four things open; each was chosen to stay closest to D43 and the
existing line rules:

1. **The line**: `HISTORY`, `1`, `in`/`out`, the item's `origin_server_ts`, then the
   `PING`, `HUMAN` or `SENT` line less its `1`. The time was added because a record
   without it cannot be read in order across a seat's sessions (`PING` has no time
   field). An `out` ping is a `PING` line with this member as sender; `SENT` is used for an
   agent text this member sent (`say`), with team and event ID only: pingbus never prints
   agent text.
2. **`--limit` is per team**, teams in `PINGBUS_TEAMS` order, like `recv`'s per-team
   handling.
3. **The start token**: a `next_batch` from a `limit: 0` `/sync`. Fixture 072 shows
   Tuwunel taking a sync token as `/messages`' `from`; H4 recorded no `from`-less
   `/messages`, so that form was not used.
4. **The scan bound**: 50 pages of 100 messages per team, stated in §13, with a stderr
   line when it is reached, so a cut scan is never silent.

## Verification

`tests/helpers/pingbus/test_cli_history.py` (26 tests, against the fake homeserver):
sent and received items newest first; only items naming or sent by the seat (`@room`
human text included); an agent text sent listed without its text; an item older than any
receive limit still listed; the forge check runs and a failing reference is not printed;
a forged body, a role-less sender, an agent's free text and a verb outside the sender's
role are never printed (counted on `DROPPED`); a member whose role was removed;
`human_text: false`; a redacted own ping; the default 50, `--limit`, its bounds (0, -1,
501, a word: 64); paging across pages; the page cap's stderr line; the whole state tree
byte-identical after `history` and a later `recv` still delivering the pending item; a
seat with its state directory removed still sees its earlier items and has no sync token
afterwards; an untrusted room and a room never joined (10); no active team (78); two
teams in order and `--team`; a failing team beside a printing one. Line-formatter tests
cover each shape and the refusals.

`test_plugin_contract.py`: `history` is now an agent command the skill must name, and the
record-not-work rule is asserted. `test_cli_offline` holds `LINES` equal to the §15 table.

Run: `test_cli_history`, `test_cli`, `test_cli_status`, `test_cli_offline`, `test_syncer`,
`test_hooks`, `test_plugin_contract`, `test_protocol_doc`: all pass (307 tests in one run;
the doc-bound four and `test_cli_history` again after the final label change, 140). ruff
0.16.8 (the pinned version) clean on every Python file touched.

## Risks and notes for the coordinator

- **Backward `/messages` from a sync token is not recorded against real Tuwunel** (H4 has
  only the forward gap fill, fixture 072). U20's host run, which reads history after a seat
  is removed and returned, is its first real use. If Tuwunel refuses it, the fallback is a
  `from`-less `dir=b` call, a one-line change in `Syncer.history`.
- Nothing under `files/var/local/claude-yolo/` changed, so no CCY version bump; the skill
  and the zipapp reach the image through `play-claude-yolo.yml`'s staging, as U29's
  pingbus change did, with no container version bump.
- U30 also edits §13/§15 of the protocol doc, `cli.py` and the skill (`SEAT` lines, the
  SessionStart history hint). The hunks are separate; the PLAN.md M2 line and the DESIGN.md
  decisions table (D52) are the likely textual conflicts.
- No host run of its own: exercised by U20.
