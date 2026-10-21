# Plan 00211 Phase 1: handoff 4

The fourth Phase 1 implementer stopped at its context budget. Handoffs 1 to 3 hold the earlier
design; read this first.

## Done

- **Merged** the Phase 1 branch (tip 55f64e19) into this worktree's branch, conflicts in
  `CLAUDE/Plan/README.md` and `meta-deploy.list` kept both sides.
- **Tasks 1.6, 1.7, 1.11** (a5486643): `cli` `report`, `ask-human`, `thread`, `questions`,
  `history --room`; the `HUMAN` line's appended ROOM field; the questions-room `REPLY`
  command; `_refetch(seat, ctx, item)` and the read receipt use the item's room;
  `envelope` (8-field HUMAN, `room:`/`question:`/`revises:`, the largest-entry test covers a
  questions HUMAN); `protocol.check_thread_text`; `Syncer.verify_side` public. Protocol doc
  §9 ("In the questions room"), §12 (rooms.json, reports.json, the coordinator `human_text`
  rule), §13 (four rows, `history --room`, the status `ROOM` line), §15 (ROOM, QUESTION,
  REPLY_TO, examples). Every pingbus test module passed before the commit; qa-changed green.
  Container 2.78.3, CCY 3.99.13.3.
- **Task 1.8 rest** (the skill): `SKILL.md` gained the three rooms, the ROOM field, the
  envelope's room lines, a "Reports and questions" section (SPEC §10.1) and the exit-code
  notes; `test_plugin_contract` pins the rules and the four commands. Committed in the commit
  after a5486643 (container 2.78.4, CCY 3.99.13.4) if qa-changed passed; check `git log`.

## Left

1. **Task 1.12**: `acceptance.bash` (`--fake` plus `--stage 1`: S1 the host
   `pingbus version` is the new one, S2 every declared LXC member's in-container
   `pingbus version` is, a down container refused) and `deploy.bash --stage 1` (the whole
   `play-agent-bus.yml`, the ccy play's pingbus staging, then `acceptance.bash --stage 1` as
   its last leg; `--check` skips acceptance and says so; closing message asks the owner to
   restart the teams' sessions). Follow `CLAUDE/PlanScriptStandards.md`; Plan 00194's
   `acceptance.bash` is the `--fake`/`check_leg`/`reject_leg` model. "The new version" needs a
   marker: `pingbus version` prints TOOL_VERSION 0.1.0 unchanged, so either bump
   `cli.TOOL_VERSION` (container bump again) or probe `pingbus report --help` exits 0. Decide
   and journal. `ansible-playbook --syntax-check` only.
2. Journal handoffs 3 and 4's work through `mkplan.bash --journal` (not done).
3. Then `subagent-reports/261021-phase1-complete.md` (every Phase 1 task, the deploy order:
   spike first, then deploy 1 after the Phase 1 merge, and what the owner runs).
