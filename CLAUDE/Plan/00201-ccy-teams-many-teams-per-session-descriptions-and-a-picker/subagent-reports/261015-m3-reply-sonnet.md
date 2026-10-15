# Plan 00201 Task 2.3: M3 replying (sonnet)

Built exactly SPEC-many-teams.md S-D1 to S-D6 in `helpers/pingbus/cli.py`, test-first.

- **Q8 was taken as the spec recommends** (a `REPLY` line after `HUMAN` and `PEER` items only,
  never after a `PING`, never in `history`), pending the owner's confirmation. Recorded in
  PLAN.md Task 2.3.
- S-D1: `REPLY<TAB>1<TAB>team<TAB>event<TAB>pingbus --team <team> say --to <name>`; name is
  the human localpart, or the agent's short name when unique in the team record, else the full
  handle. New `LINES` entry, protocol doc section 15 row and prose.
- S-D2 to S-D4: `--to` resolves against the verified team record: user ID, exact handle,
  human localpart, reserved localpart (left to the validator, so `admin` still gives
  `refused: target`, exit 4), then short name (exactly one agent). Ambiguous or unknown: exit 64
  naming the candidates, or the team's agents and humans. An unknown name used to be
  `refused: target` (exit 4); it is now exit 64 by design, and
  `test_say_takes_humans_and_agents_with_a_role` was updated.
- S-D5: `send --help` and `say --help` carry an epilog with the forms.
- S-D6: the pingbus skill
  (`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md`)
  documents `REPLY` and the `--to` forms, and says to copy the command.
- Not changed: `say` with no `--to` still exits 64 (a usage error, correct).
- No `files/var/local/claude-yolo` file was touched, so no `CCY_VERSION` bump is required. The
  agent-bus bundle digest changes (expected); the helper and skill reach containers with the
  next container build.

Tests: `tests/helpers/pingbus/test_cli_reply.py` (12 new tests). Red run before the code: 11
failures or errors across the new file and `SayTest`; green after.
