# Plan 00211 Phase 1: handoff 3

The third Phase 1 implementer stopped at its context budget. Read this first; handoff 1 holds
the cli and envelope design (its "cli" and "envelope" bullets), handoff 2 what the syncer and
inbox offer. Neither is repeated here.

## Done

- **Task 1.4 rest** (RED 1306354e, then GREEN): `inbox.REASON_NO_HUMAN_TEXT` (syncer aliases
  it); `pingbus status` prints `ROOM <team> <purpose> <state> <reason or ->` per side room, in
  `protocol.PURPOSES` order, after the MEMBER/UNEXPECTED lines and only for a trusted team;
  `hooks.TeamView.questions_refused` and the `questions_refused` template, told at
  SessionStart by `side_room_lines`. Tests: `test_cli_status.SideRoomStatusTest`,
  `test_hooks.SessionStartTest`.
- **Task 1.8, the hooks half**: the role templates carry SPEC §10.2's sentences. SKILL.md
  (`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md`, SPEC
  §10.1) is NOT done.
- **Task 1.10**: `src-go/internal/agentbus/parse_test.go` `TestParseRosterSkipsRoomLines`
  (passes on today's parser, which is the pin).
- **Phase 0**: `spike.bash` and `spike_probe.py` written (plan_mode deploy, plan_require_host,
  scratch homeserver under the run dir, removed by `plan_on_cleanup` and a direct call) and
  queued last in `CLAUDE/Plan/meta-deploy.list` with its description. Never run here.
- **Task 1.11, part**: `docs/agent-bus.md` gained "The reports and questions rooms";
  `docs/agent-bus-protocol.md` gained the §2 rows and content keys and §8.1. Still owed: §9
  (questions-room receive path, room-aware re-validation, edits, attachment rules), §12 (the
  coordinator `human_text` rule), §13 (the new commands), §15 (the HUMAN `ROOM` field, the
  status `ROOM` line, `QUESTION`, `REPLY_TO`).
- **RED for 1.6/1.7** (committed failing): `tests/helpers/pingbus/test_cli_rooms.py` (report,
  ask-human, thread, questions, the HUMAN `ROOM` field, REPLY command, `history --room`) and
  the questions-room cases in `test_envelope.py` (its `human()` helper now writes the 8-field
  HUMAN line and passes `question_headline=`/`revises=` to `entry_from_line`).

## Left

1. GREEN for 1.6 and 1.7 to make the RED tests pass (cli: `report`, `ask-human`, `thread`,
   `questions`, `history --room`; `human_line` appends ROOM; `reply_line` for a questions item;
   `_refetch(seat, ctx, item)` and `_read_receipts` use the questions room ID and
   `validate_event(..., room="questions")`; make `Syncer._verify_side` public for `report`.
   envelope: `_FIELDS["HUMAN"]` 8, ROOM grammar `-|questions:Q<n>|questions:-`, `Entry.room`,
   `.question` (rendered `Q<n> "<_render(headline)>"` or `none`), `.revises`, the three lines
   after `team:`). Every existing HUMAN line in the tests (test_cli\*, test_relay\*, the hooks
   and envelope suites) gains `\t-`; `LINES["HUMAN"]` gains the ROOM field. Check
   `envelope.ENTRY_MAX_BYTES` still bounds an entry with a 120-character headline.
2. SKILL.md (SPEC §10.1) and the rest of the docs above.
3. Task 1.12: `acceptance.bash` (`--fake`, stage 1 S1-S2) and `deploy.bash --stage 1`.
4. Container bump per commit touching `helpers/pingbus/` (patch digit):
   `python3 untracked/scratch/bump.py` (untracked; recreate it if gone: it bumps the Dockerfile label, both claude-yolo variables and the changelog heading).
5. Journal this handoff's work through `mkplan.bash --journal`, and tick PLAN.md tasks.
