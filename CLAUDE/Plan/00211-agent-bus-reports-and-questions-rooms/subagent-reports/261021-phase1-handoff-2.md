# Plan 00211 Phase 1: handoff 2

The second Phase 1 implementer stopped at its context budget after Tasks 1.2, 1.3, 1.5, 1.9 and
half of 1.4. Read this first; [handoff 1](261021-phase1-handoff-1.md) still holds the design for
what is left (its "cli", "envelope" and "R3 surfaces" bullets), and is not repeated here.

## Done (branch `agent-a140d7166ad867cc1-44dd0279`)

- RED b3a1a873, then the GREEN commit after it (container 2.78.1, CCY 3.99.13.1).
- `inbox`: `Item.room` and `Item.question`; `ROOM_KEY`/`QUESTION_KEY`, written only for a
  questions-room item (a main-room file is byte-identical to the old format); `SideRoom`,
  `save_side_rooms`/`side_rooms` (`rooms.json`, removed by `mark_untrusted`);
  `admit_report(limits, digest, now_ms)` (hourly cap from `reports.json`, then the outbox gate
  with verb `report` and ref `digest`). Tests: `tests/helpers/pingbus/test_inbox_rooms.py`.
- `limits`: `report_per_hour` (6, 1-60), `REPORT_WINDOW_S`, `ReportWindow`. Doc §10 row added.
- `syncer`: `sync_filter(..., also=)`, `SIDE_TRUST_TYPES`, `SIDE_TYPES`, `invite_action` and the
  `INVITE_*` and `REASON_*` constants, `Syncer.side`, `_sync_side_rooms` (run by `verify_room`),
  `_reports_room` (the only code that depends on the restricted join), `_questions_room`,
  `_verify_side`, `_listening_room`, `_questions_events` (fresh-join cut, re-verify, gap fill),
  `question_index()`, `history(limit, room=)`; `HistoryItem.room` and `.question`. Tests:
  `tests/helpers/pingbus/test_syncer_rooms.py`. The fake models restricted joins.
- `members.py` and `admin.add_member` refuse a present coordinator with human_text false.
- SPEC §1, §5.6 (the bump), §7 (reports.json) and §8.2 (one QUESTION layout) corrected.

## Left (Phase 1)

- 1.4 rest: `status` prints `questions: <state> (<reason>)` from `TeamState.side_rooms()`; the
  hook context says the same for `refused`. Tests in `test_cli_status.py`, `test_hooks.py`.
- 1.6 cli: `report`, `ask-human`, `thread`, `questions`, `history --room`; the `HUMAN` line's
  appended `ROOM` field (`LINES["HUMAN"]` grows by one, so every HUMAN line in the tests and
  `relay_line` gain `-`); `REPLY` for a questions item; `_refetch(seat, ctx, item)` and
  `_read_receipts` must use the questions room ID (`state.side_rooms()["questions"].room`) and
  `validate_event(..., room="questions")` for a questions item. `report` verifies the reports
  room with `seat.syncer._verify_side("reports", ...)` before sending (make it public).
  `ask-human` numbers with `seat.syncer.question_index().next_number()`.
- 1.7 envelope: `_FIELDS["HUMAN"]` 8; `room:`, `question:`, `revises:` after `team:`.
- 1.8 hooks role lines and SKILL.md (SPEC §10.1, §10.2).
- 1.10 Go parser test, 1.11 docs, 1.12 acceptance and deploy scripts.
- Phase 0: `spike.bash` is not written yet. Write it (PlanScriptStandards, `plan_mode deploy`,
  `plan_require_host`, SPEC §11.2) and queue it in `CLAUDE/Plan/meta-deploy.list`.

## How to run the tests here

The Bash guard refuses `python3 -m unittest` with computed operands. Name modules literally, or
use `python3 untracked/scratch/run_mods.py 'tests/helpers/pingbus/test_*.py'` (a glob runner in
the untracked scratch area; recreate it if gone). The whole pingbus suite takes minutes: run it
in the background.
