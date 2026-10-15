# Plan 00201 M6 report: `cc --teams` on the shared flow

Expectations covered: S-E1 to S-E6, M5 review finding F6 (launch block for cc), and the M5 F1
check that the host SessionStart hook writes the per-session teams record. CCY 3.97.10,
container 2.58 (unchanged: no image content changed).

## What was built

- `src-go/internal/teams/chooser/consent.go`: for `--type=host` only, after the seats are known
  (given list or chosen) and before the list is printed, the Element consent. A record that
  exists answers it. Otherwise: the words naming every handle, `gum confirm` defaulting to No,
  and on yes the record `accepted <UTC time>` (folder 0700, file 0600, O_EXCL, O_NOFOLLOW). A
  decline or Ctrl-C prints `Not started. Nothing was recorded; cc --teams asks again next time.`
  and exits 1. A symlinked folder or record, or a record that is not a plain file, exits 78.
- `ccy-teams describe-seats --sentence LIST`: `You are H1 in alpha and H2 in beta.` for cc's
  system prompt; a seat of the checkout that does not exist fails (78) and prints nothing.
- `cc-teams.bash` rewritten: ccy's list grammar (`cc_teams_split_args`), `cc_teams_choose`
  (chooser on a terminal, then `agent-bus seat check`, `--resolved` without one),
  `cc_teams_take` (`seat take --type=host`, then the launch block and the sentence on stderr).
  `cc_teams_seat` and `cc_teams_accept` are gone; the ratchet record fell from 170 to 113 code
  lines, 217 to 170 statements, and lost its `ui` mark. `cc` tests `CC_TEAMS_GIVEN`.
- Docs: changelog 3.97.10, `docs/agent-bus.md` cc section, `CLAUDE/QA.md` row, usage text,
  meta-deploy text, PLAN.md Task 2.6.

## Host SessionStart check (M5 F1)

cc passes `--plugin-dir $CC_TEAMS_KIT/plugin/pingbus`, the kit copy of the same plugin
(`agent-bus-install` installs it from `files/opt/claude-yolo/optional/agent-bus`), and runs
claude with `PATH="$CC_TEAMS_KIT:$PATH"`. Its `hooks.json` runs `pingbus hook session-start` in
the session's environment, which `seat exec` has given `PINGBUS_TEAMS`, so the per-session file
under `/tmp/pingbus-sessions-<uid>` is written on the host exactly as in a container; the daemon
reads the same default path. `scripts/test-cc-teams.bash` pins the three links. The plugin
registers no SessionEnd hook, so the record is never removed on the host (nor in ccy); it is
keyed by session id and harmless, noted for M7.

## Red then green

- Go chooser: 4 of the consent tests failed with the consent un-wired (the rest never ran: one
  test panicked on an empty gum log), all 10 pass once wired. `describe-seats --sentence`: the
  new tests did not compile before the code, pass after. `go test ./internal/teams/...` and
  golangci-lint clean.
- Bash: `scripts/test-cc-teams.bash` aborted at the first case against the old library
  (`CC_TEAMS_GIVEN: unbound variable`); against the new one 57 pass, 0 fail. Two real bugs were
  found by it on the way: `--resolved` must follow the list, and a count of the command array
  had to be read inside the subshell that built it.

## Readings and deviations (fail-fast reading chosen)

1. **No terminal, host seat, no record**: cc cannot ask, so it checks `--resolved` and takes the
   seats; pingbus then refuses the session at exec until an attended launch has recorded the
   consent. The old cc asked on `/dev/tty`. The refusal is loud but the seat exists by then.
2. **Handles in the consent** come from `agent-bus seat choose <team> --type=host`. A seat that
   command does not offer (for example `9@alpha` for a number not listed) cannot be named, so it
   exits 78 with the seats to choose from; this only bites while the consent is unrecorded.
3. **An existing consent folder that others can read (not 0700)** is refused with `chmod 700`
   guidance instead of being chmod-ed, because the Go linter bans the chmod and the spec says
   0700\. The old bash chmod-ed it.
4. A Ctrl-C at the consent question is treated as a decline (exit 1), not 130.
5. Prompt wording changed from "a member of the agent team T" to `You are H in T [and ...]. Post to your teams only with pingbus, as yourself; ...` (S-E5). It still names no Element term.
6. The `bus: A,B` handler tests stay outside every gate (M5 F7); the host run must look at it.
