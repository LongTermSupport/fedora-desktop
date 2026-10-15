# Plan 00201 M5 report: several teams per session

Expectations covered: S-B3 (`--multi`), S-B6 and S-B10 for several marks, S-B11 (Create marked with
others), S-B12, S-C1, S-C4, S-C5, S-C6, S-C8, S-C9. CCY 3.97.8, container 2.58.

## What was built

- `src-go/internal/teams/chooser`: the list is a multi-select (`ui.ChooseSpec.Multi`), header "Tab
  marks several, Enter takes them (or the highlighted one), Esc leaves". Every mark is judged: blocked
  ones (down, excluded, unreadable) are explained, the valid marks are kept and not offered again,
  the list returns (three rounds at most, then exit 1). Taken teams go through the existing scope
  question (one apply for all Adds) and then the seat chooser in list order. Create marked with other
  teams takes both; cancelling the wizard keeps the other marks.
- `ccy-teams describe-seats --block LIST` prints "This session is on N teams:" and per seat
  `team  seat S (role)  description`. `ccy_seats_take` prints it on stderr after a successful take.
- `helpers/pingbus`: `send`/`say` infer the team (`--re` held by exactly one team's inbox or
  consumed; else every `--to` name fits exactly one verified record, a user ID fitting the team whose
  server it names); otherwise exit 64 with the S-C9 text and one `  TEAM  DESCRIPTION` line per team.
  `--team` wins. SessionStart's first line names the teams when there are two or more.
- `.claude/hooks/handlers/status_line/bus_teams.py` (registered as a plugin in
  `.claude/hooks-daemon.yaml`): `bus: alpha,beta` from `PINGBUS_TEAMS`, nothing when unset or when
  the value is not a valid team list.
- Docs: protocol, agent-bus.md, the pingbus skill, changelog, meta-deploy entry (3.97.8, container
  2.58).

## Red then green

- Go: `teamtest` ampersand-mark test failed, then passed; 9 of 10 chooser multi tests failed against
  the single-select list, all pass; 3 `describe-seats --block` tests failed, then passed.
- Bash: `scripts/test-ccy-teams.bash` 4 checks failed before the lib change, 163 pass after.
- Python: `test_cli_infer` 8 of 10 failed, all pass; the SessionStart test failed, then passed.
- Status line: the pytest module failed to import before the handler existed; 14 pass.

## Deviations and readings (fail-fast reading chosen)

1. A failing `describe-seats --block` stops the launch (the seats are already taken); the block is
   not optional.
2. The block is printed for one team as well ("1 team").
3. A kept mark is removed from the next round's rows, because fzf cannot preselect.
4. `say` has no `--re`; its inference uses `--to` only.
5. A team whose record is untrusted fits nothing in `--to` inference.
6. `--to-coordinator` alone never infers.
7. The status-line handler reads `PINGBUS_TEAMS` from the daemon process's environment, which is the
   session's only when the daemon was started by that session (one daemon per container). It was
   tested with the daemon's classes but not loaded by a running daemon here; restart and health check
   the hooks daemon on the host to confirm it loads.
8. Bash ratchet: `agent-bus-seats.bash` kept at its record by trimming two statements of
   `ccy_seats_take`; the allowlist line is updated by `--update` (code lines 61).
