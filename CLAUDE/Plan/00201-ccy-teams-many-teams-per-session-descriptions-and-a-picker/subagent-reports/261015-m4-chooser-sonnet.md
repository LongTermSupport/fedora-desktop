# Plan 00201 M4 report: the shared chooser, one team

Expectations covered: S-B1 to S-B15 without multi-select, S-B4 (with the exclude fix already
in `teams_for`, now shared with `choices`), S-B5 to S-B14, Q10. CCY 3.97.6 (3.97.5 is taken by
another fix); container version unchanged.

## What was built

- `helpers/agent_bus/declare.py`: `choices --repo` (`CHOICE team covers|widen|excluded|unreadable description`), `add-repo --team --repo` (one-line list rewritten, block list appended, read
  back or refused). `teams_for` and `choices` share one `_standing` rule.
- `src-go/internal/declare`: `Choices`, `AddRepo`, shared description parser.
- `src-go/internal/teams/chooser`: `choose-teams --type=podman|host [LIST]`. List screen (fzf),
  blocked rows with reasons and 3 bounded rounds, scope question (Add / Join without adding /
  Leave out), one `ApplyTeams` for all adds, create from the list, seat chooser, Esc handling.
- `choose.PickSeat` (extracted from `Seat`), `agentbus.SeatChooseAs` (`--type`),
  `create.Wizard.RunForChooser` (no own session offer, no stdout), `host.Payload` and
  `host.NewDiverted` plus `teams.NewAppFor` so a play's stdout cannot reach the payload.
- Launcher: `ccy_launch_teams` calls `choose-teams` when attended and the list is empty or has a
  team alone; `ccy_seats_resolve` and `for-checkout --choose` deleted; bash ratchet shrank
  (`scripts/bash-limits.allowlist` updated by `--update`).

## Tests seen fail, then pass

Python `ChoicesTest`, `AddRepoTest` (14 of 15 red, then 99 green); Go compile-red then green for
declare, agentbus, host, wizard (`RunForChooser`), app, chooser; `scripts/test-ccy-teams.bash`
and `scripts/test-ccy-teams-command.bash` green. The bash suite and launcher were edited
together, so its red was not observed separately.

## Deviations and readings (fail-fast reading chosen)

1. A fourth standing, `unreadable`, for a team whose scope is vaulted; it cannot be taken.
2. `choices` prints the description as declared's JSON/`-`/`!vaulted`/`!invalid` field, not raw.
3. `add-repo` on an excluded repository exits 78; a `repos:` line with a comment exits 78.
4. A bare `--teams` with nobody to ask is refused by `agent-bus seat check` on the empty list
   (64), not by a launcher message, to stay inside the ratchet.
5. The scope question names the repository but not the team's current scope in words.
6. `--type=host` is accepted and passed to the seat chooser; cc is not wired (M6).
7. The Add/Join/Leave loop is written over a slice but the list is single-select until M5.
