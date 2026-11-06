# Plan 00248 design: every team start is the launch summary

Line numbers are of F44 `2d4aa6842`. The route and step names (R1-R10, L0-L16, T1-T6) are those
of [RESEARCH-team-start-today.md](RESEARCH-team-start-today.md), which maps today's prompts.

## 1. The idea

There is already one screen that shows every default and launches on Enter:
`ccy-teams launch-summary` (`teams/chooser/summary.go`, Plan 00201 Task 2.33), called by
`lib/launch-summary.bash`. Today it shows the teams only for a plain `ccy` with saved teams
(R4). Every other team start settles the teams first, question by question, in
`ccy-teams choose-teams` before tmux (L1), and then calls the summary with `--skip-teams`.

This design makes **the summary the one place a team launch is settled**, for every route.
`choose-teams`' questions (team list, scope, seat, role) stay, but each becomes what one line
of the summary opens when the person picks it, which is how the summary already uses `settle`
and `PickSeat` (`summary.go:325-356`). No second screen is built: Plan 00223's
`ccy-teams quick-launch` (its C1, C2, C6) is the same screen again, designed before Task 2.33
existed, and is superseded (decision D1).

## 2. The screen

Rows, in order (the first row Enter lands on is in bold):

| Row                                                          | Default and source                                                                                                         | Picking it                                                                                                                   |
| ------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| **Launch with these choices**                                | Enter, unless a line is marked NEEDS AN ANSWER (then Enter lands on the first marked line, as today, `summary.go:269-281`) | launches                                                                                                                     |
| `Change token (T, expires D, 5h 40% 7d 12%)`                 | `LAST_TOKEN`; expiry from `internal/tokens`; usage from the gate's cache (no fetch)                                        | the token chooser (`ccy_summary_ask token`, `lib/launch-summary.bash:45-56`), back to the screen                             |
| `Change SSH key (K)`                                         | `LAST_SSH_KEYS`                                                                                                            | the SSH menu, back to the screen (as today)                                                                                  |
| `Change network (N)`                                         | `LAST_NETWORK`                                                                                                             | as today: cleared, asked at its own step L12 (D8)                                                                            |
| `Change team T (seat S, worker)` per team                    | section 3                                                                                                                  | scope if needed (`settle`), then seat and role (`PickSeat`), back to the screen                                              |
| `Add a team...` (new)                                        | none                                                                                                                       | the team list of `choose-teams` (`take`, `list.go:29`), then `settle`; each team taken gets a line with section 3's defaults |
| `Leave team T out` (new, one per team)                       | none                                                                                                                       | the team is dropped from this launch (the code is `summary.drop`, `summary.go:358`)                                          |
| `Attach the session holding S@T` (new, only when it applies) | section 4                                                                                                                  | ends the summary with an attach (D4)                                                                                         |

The heading names the other sessions of this checkout that are running (D7). No model row:
there is no model choice at launch today; Plan 00207 adds a mark on a team line when its seat's
role needs a better model than the session's (D9).

## 3. Where each team default comes from

| Launch form                       | Teams shown                                                                                                                                                                    | Seat                                                                                                             | Role                                                                           |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| plain `ccy`                       | the saved `LAST_TEAMS`, as today                                                                                                                                               | the saved seat if free; held: section 4                                                                          | the seat's role now (`choose.Probe`); changed since the save: marked, as today |
| `ccy --teams` (bare)              | the saved teams (D5); none saved: the one team that covers the checkout and is up (the rule of `list.go:30-33`); otherwise none, and `Add a team...` is marked NEEDS AN ANSWER | as above; a team with no saved seat: agent-bus's `DEFAULT` (`Choices.Default`, already `PickSeat`'s default row) | as above; a new seat: worker (`choose.go:323-326`)                             |
| `ccy --teams TEAM[,TEAM]`         | the named teams only; saved seats of other teams are not shown (the command line named the teams)                                                                              | the team's saved seat if free, else `DEFAULT`                                                                    | as above                                                                       |
| `ccy --teams SEAT@TEAM`           | as given                                                                                                                                                                       | as given; held: marked NEEDS AN ANSWER, not moved (the command line named it, D4)                                | as above                                                                       |
| no saved file (first launch here) | as the bare or named form, without saved seats                                                                                                                                 | `DEFAULT`                                                                                                        | as above                                                                       |

A team that does not cover the checkout is marked (scope), as today; its line asks the scope
question (`widen.go:31-77`) with Add as the default and sudo for the play run (T2, T2s). That
question and its sudo stay: adding a repository to a team's trust is a decision with an
outward effect, and the sudo password cannot be defaulted.

With no saved file (D6), the token line's default is the unexpired token with the most usage
left in the gate's cache (`internal/tokens` pool, the same read as the token line), and the SSH
and network lines say `asked next` and are asked at their usual steps (L5, L12): their
discovery lives in bash (`lib/ssh-handling.bash:426`, `claude-yolo:2977-3070`) and porting it
is Plan 00191's work, not this plan's.

## 4. When a default no longer holds

| Case                                                       | What the screen does                                                                                                                                                                                                                                                                                                                                                                                 |
| ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Saved seat held by a running session                       | the line proposes `DEFAULT` instead, saying so: `Change team T (seat 3, worker; seat 2 is held by session X)`. Not marked: Enter accepts the move, shown, never silent (D4)                                                                                                                                                                                                                          |
| Saved seat held by a **detached** session of this checkout | as above, plus an `Attach the session holding 2@T` row; picking it ends the summary with `attach NAME`. The summary runs inside the new session's tmux (D2), where `ccy_tmux_attach` (`lib/tmux-session.bash:116`, an `attach-session`) cannot be used, so the Go switches this client to the holder's session (`internal/tmuxccy`) and the launcher ends its own empty session with nothing started |
| A seat named on the command line is held                   | marked NEEDS AN ANSWER; picking the line opens the seat chooser                                                                                                                                                                                                                                                                                                                                      |
| No free seat (agent-bus offers no `DEFAULT` that is free)  | marked; the line opens the seat chooser, whose `new seat` row makes one at L15 (sudo)                                                                                                                                                                                                                                                                                                                |
| Seat taken between the screen and `seat take` (a race)     | refused at L15 (exit 75) as today; the next launch's screen proposes the next free seat (Plan 00223 C9: no host-side hold exists before the container)                                                                                                                                                                                                                                               |
| Role changed since the save                                | marked, as today (`summary.go:222`)                                                                                                                                                                                                                                                                                                                                                                  |
| Team gone, down, excluding the checkout, vaulted scope     | marked, as today (`summary.go:205-211`); picking the line leaves it out                                                                                                                                                                                                                                                                                                                              |
| Token used up (cached usage)                               | the token line is marked NEEDS AN ANSWER; Enter lands on it and opens the token chooser; the other lines are kept                                                                                                                                                                                                                                                                                    |
| Token expired, or no file of that name                     | marked; the token chooser                                                                                                                                                                                                                                                                                                                                                                            |
| Cache stale: the token is used up at the launch            | the bash gate at L7 (`claude-yolo:1534`, which fetches) stays the one authority and offers the chooser, as today                                                                                                                                                                                                                                                                                     |
| agent-bus cannot list a team's seats                       | the line is marked with agent-bus's reason (as today, `summary.go:218`); a failure to read the teams at all stops the screen with its code (fail fast)                                                                                                                                                                                                                                               |
| No gum, or no ccy-teams                                    | as today: the `[Y/n]` prompt (L4n) for saved settings, and `choose-teams` for `--teams` (which itself needs gum and says so)                                                                                                                                                                                                                                                                         |
| Nobody at the terminal (restore, restart, headless)        | unchanged: no screen; the saved seats are taken only from `--teams` in the session's record                                                                                                                                                                                                                                                                                                          |

## 5. How it fits the code (DRY, and Go)

**One screen, one team model.** `summary.go`'s `launchTeam`, `assess`, `row` and `change` become
a small reusable team line (its own file in `teams/chooser`, or a package if the session add
needs it from `teams/picker`), used by the launch summary and by the session add's one screen
(R9, section 6). The seat default is read in one place: `choose.Probe` grows to return the
`DEFAULT` and the holder alongside `taken` and `role`, still the one place that names the bus
(so the v2 swap of Plan 00209 changes only it).

**The token line's usage** is read by a cache-only function moved from
`ccylaunch/usable.go:120` (`Front.TokenUsability`) into `internal/tokens`, called by both the
launcher's front and `ccy-teams` (Plan 00223 C11; the token-usage gate it waited for is merged).

**All new logic is Go** (CLAUDE.md "Tooling Is Go"). The bash changes are moves and removals,
and `claude-yolo` (allowlist row `scripts/bash-limits.allowlist:50`) and
`lib/launch-summary.bash` must not grow; if they would, the build stops and the owner decides
(as Plan 00223 C15). Expected bash changes:

- **L1 defers to the summary.** `ccy_launch_teams` (`claude-yolo:865-885`) no longer calls
  `choose-teams` when a person is at a terminal with gum and `ccy-teams`; the given words are
  checked by `agent-bus seat check` (attended mode keeps a team named alone, as today) and the
  summary resolves them. Without gum it calls `choose-teams` as today.
- **The summary runs for team launches with no saved file** too (D6), from the same block.
- **`--skip-teams` goes**: `lib/launch-summary.bash:64` and `72`, and the flag in `summary.go`.
  The summary is given the `--teams` words as given (`--given=`), and the Go does the filtering
  of section 3.
- **The chosen seats reach the record and a restart** by the steps that exist:
  `ccy_teams_canonical_args` (`lib/agent-bus-seats.bash:96-112`, today run at
  `claude-yolo:889`) after the summary when `--teams` was given, and the `record-seats` and
  `CCY_ORIG_ARGS` append of `claude-yolo:1282-1285` when it was not.
- **Q3 (L9) is not asked** after an accepted summary with a team (D7): one condition beside
  `CCY_NO_CONTAINER_PROMPTS` (`claude-yolo:1794-1797`), scoped so the zombie menu (L8) is kept.

**Where the screen runs: inside tmux, where it runs today** (D2). Plan 00223 put its screen in
the outer run before tmux so that the detached-session picker (L3) and the tmux record would see
canonical seats. Moving the summary out of the inner run would undo Task 2.33's placement and
cost a hand-off across the re-exec (Plan 00223 C17, C21, C22: three decisions of their own).
Kept inside, the seats are settled after L3, so for `ccy --teams` and `ccy --teams TEAM` L3 is
not scoped by seat: it offers the detached sessions of this checkout, as it does for a plain
`ccy` today (D3). The summary's `Attach` row covers the case that scoping existed for: a saved
seat held by a detached session.

## 6. The routes outside the launcher

- **R6, the picker's "Start a ccy session in DIR?"** (`picker/offers.go:396`, `413`). The offer
  is unprompted, so its yes/no (default No, decision A12) stays; the yes runs `ccy --teams`,
  which is now one summary.
- **R7, a team row's "Start a ccy session in TEAM"** (`picker/menu.go:128`). The seat chooser of
  `seats.NewSession` (`seats/seats.go:187`) is removed and the action runs `ccy --teams TEAM`:
  the summary proposes the seat, and offers its role, by the one rule of section 3 (D10). The
  refusals `NewSession` makes before it (not a project folder, an LXC checkout, homeserver
  down, seats unreadable: 156-181) stay.
- **R8, the create wizard's "Start a ccy session ... as a member of TEAM?"**
  (`create/wizard.go:1146`): unchanged; its yes now lands on one summary.
- **R9, adding a team to a running session** (`picker/sessions.go:225-264`,
  `picker/addrunning.go:53-90`). After the team (or the session) is chosen, one screen built on
  the same team line: `Add S to T`, `Change seat (lowest free: 3)`, `Change role (worker)`;
  Enter runs `ccy-teams session add S T/SEAT --role=... --yes`, so `hot`'s own confirm
  (`hot/add.go:81-99`, `hot.go:664`) is not asked a second time (D11). The `--yes` already
  exists for the no-terminal case (`hot.go:651-660`).
- **R10, `cc --teams`**: out of scope (`cc` has no Quick Launch). It can call the same summary
  later.

## 7. Steps, today against this design

Counted as in the research: a screen or prompt that waits for a person, the typical case of
each route (several teams on the machine, an existing seat, a teammate running). Steps that
stay because they cannot be defaulted (a scope widen and its sudo, a new seat's sudo, a
passphrase) are listed under the route in the research and are the same on both sides.

| Route                                       | Today                            | Design                                            |
| ------------------------------------------- | -------------------------------- | ------------------------------------------------- |
| R1 `ccy --teams`, ran here before           | 5 (T1, T3, T4, L4, L9)           | 1 (L4)                                            |
| R2 `ccy --teams TEAM`                       | 4 (T3, T4, L4, L9)               | 1                                                 |
| R3 `ccy --teams SEAT@TEAM`                  | 2 (L4, L9)                       | 1                                                 |
| R4 plain `ccy`, saved team                  | 2 (L4, L9)                       | 1                                                 |
| R5 first launch here, `ccy --teams`         | 7 (T1, T2, T2s, T3, L5, L7, L15) | 6 (T1 from `Add a team...`, T2, T2s, L4, L5, L15) |
| R6 picker, "Start a ccy session in DIR?"    | 6                                | 2                                                 |
| R7 picker, team row "Start a ccy session"   | 5 (menu, action, seat, L4, L9)   | 3 (menu, action, L4)                              |
| R8 after create, "Start a ccy session ...?" | 5                                | 2                                                 |
| R9 add a team to a running session          | 4 (team, seat, role, confirm)    | 2 (team, Enter)                                   |
| (L3, a detached session here)               | +1 on every route above          | +1, unscoped for R1 and R2 (D3)                   |
