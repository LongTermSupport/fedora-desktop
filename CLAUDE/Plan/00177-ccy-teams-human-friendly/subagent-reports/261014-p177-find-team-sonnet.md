# Plan 00177 Phase 7 (Tasks 7.1, 7.2): ccy --teams finds the team, Quick Launch remembers it

Branch `fix/ccy-teams-find-team`. Not pushed, not merged, `qa-all.bash` not run.

## What was built

- Go: new package `src-go/internal/teams/find` and the subcommand
  `ccy-teams for-checkout [--choose]`. It reads the checkout's origin (`gitx`), reduces it to
  `owner/repo` (`declare github-remote`) and asks `declare teams-for` (the same matching the
  picker's "no team covers this checkout" uses). Teams go to stdout, one per line; everything
  else to stderr. One match: printed and said. Several: all printed, or with `--choose` the
  human picks (gum, the existing `ui` adapter) and the choice is printed; nobody to ask (no
  terminal, no gum) fails naming them. None, not a checkout, or no GitHub origin: plain
  message naming `ccy-teams` as the way to create one, exit 1. Esc: 130. Usage mistakes: 64.
  `teamtest.Script` gained a `CHOOSE:<text>` gum answer. `usage.txt` documents it. No
  `picker` or `login` file touched.
- Launcher (CCY 3.96.0): a bare `--teams` (last argument, followed by another option, or
  `--teams=`) leaves `TEAMS_LIST` empty and the team logic, now `ccy_launch_teams`, asks
  `ccy-teams for-checkout --choose`, then continues as a team alone (seat chooser). The old
  "--teams needs a team" 64 is gone. Quick Launch saves `LAST_TEAMS` (the canonical seats),
  shows `Team:` in the summary, and on acceptance (interactive, headless or restore) launches
  into them via the same function and appends `--teams <seats>` to the restart replay
  arguments. An explicit `--teams` wins and the summary shows it. `CONFIG_VERSION` is 2, so an
  older file is discarded with the existing format message.
- Docs: `--help`, `docs/agent-bus.md` (step 3 and the launch block), `docs/ccy.md`,
  `docs/ccy-changelog.md`, `usage.txt`, PLAN.md task notes.

## Decisions to know

- The saved value is the seats (`dev1@team-a`), not the bare team: headless and restore cannot
  run the seat chooser, and a seat still held by a running session is refused by
  `agent-bus seat take` with its usual message.
- Quick Launch is asked after tmux insulation, so the team is applied late (the tmux session
  record has no `--teams` for that launch). A reboot restore takes the saved config unasked
  and so gets the team back from `LAST_TEAMS`; the same restore also gives a team to an old
  teamless session of a project whose latest launch used one (the same as it already does for
  token and network).
- The function-count budget: `ccy_launch_teams` is a new function, offset by inlining
  `save_launch_config` into its single call site (its parameter shuffling is gone).

## Follow-up: a restore never gives a team (CCY 3.96.1)

- A session restore no longer reads `LAST_TEAMS`; it takes `--teams` only from the session's
  own record, so a teamless session restores teamless.
- When Quick Launch applies the saved seats, the launcher calls the new
  `ccy-teams record-seats <session> <seats>` (Go: `registry.Store.SetTeams`), which replaces
  `--teams` in that session's registry record exactly as an explicit launch would carry it; a
  restore then replays the seats from the record. No record or no session name is a no-op.
- Red-first tests: `TestSetTeams*` (registry), `TestRecordSeats*` (find), and in
  `test-ccy-teams.bash` the restore-stays-teamless, records-into-the-session, explicit-wins
  and no-saved-team cases. The earlier "restore takes the saved team" decision above is
  superseded. Ratchet: 2839 lines (base 2840), 12 functions, 2749 statements (base 2749).

## Follow-up 2: the session lookup fails loudly (CCY 3.96.2)

The coordinator was right: dropping `|| exit 1` on the tmux session lookup traded fail-fast for
the ratchet. Fixed by moving the lookup to Go, not by removing bash code elsewhere:
`ccy-teams record-seats <seats>` now finds the current ccy session itself
(`tmuxccy.Tmux.CurrentSession`, from the inherited TMUX; "" outside a ccy tmux session, where
there is nothing to record). tmux refusing to name the session is an error ("could not tell
which ccy session this is: \<tmux's reason>"), exit 1, and the launcher's `|| exit 1` stops the
launch. Red tests first: `TestCurrentSession*` (tmuxccy), `TestRecordSeatsThatCannotTellTheSessionFailsNamingIt`
(find), and a bash case where a failing `record-seats` stops the launch. The bash call is now
`ccy-teams record-seats "$CCY_SEATS" || exit 1`, with no command substitution, so the launcher
is at 2839 lines and 2748 statements (base 2840 and 2749); nothing was removed elsewhere.

## Bash-limits ratchet

`claude-yolo`: code lines 2840 (base 2840), functions 12 (12), statements 2745 (base 2749).
Nothing grew; no statements were folded onto one line. `qa-bash-limits.bash` passes.

## Checks run

- `scripts/qa-go.bash`: GO-OK (fmt, vet, `go test -race`, coverage 94.8%, golangci-lint,
  project rules).
- shellcheck on the touched scripts shows only the existing SC1091/SC2086 infos (`qa-changed`
  runs it clean).
- Suites: test-ccy-teams (157, with new cases: bare `--teams` variants, find failure codes,
  explicit flag never asks, Quick Launch save/summary/launch/restore/override/format),
  test-ccy-teams-command (323), test-ccy-agent-bus, test-ccy-lifecycle, test-ccy-network-disconnect,
  test-ccy-restart-request, test-ccy-flag-check, test-ccy-container-version-hook: all pass.
- `qa-changed.bash` reports two suites as "proved nothing" (`test-planlib.bash`,
  `test-ccy-info-flags.bash`): they pass but print no `passed: <n>` line. Neither touches this
  change; not investigated.

## Not covered

The attended path (real gum chooser through the real launcher) has no suite: the launcher
suites run with no terminal. The Go side is covered through the fakes; the owner's host test is
the proof of the interactive path.
