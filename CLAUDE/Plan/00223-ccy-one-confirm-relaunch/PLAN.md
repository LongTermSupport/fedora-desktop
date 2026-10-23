# Plan 00223: ccy one confirm relaunch

**Status**: Not Started (plan only; decisions C1-C12 open, coordinator defaults stated)
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

## Overview

The owner: "regards teams - currently we require multiple confirmations if we want to keep
everything the same / the teams stuff should be merged with the token/ssh keys stuff / it
should be possible to just restart with all the preset stuff still selected / maybe the team
seat being the only one that needs checking as if we have multiple sessions in same repo we
can't try to use the same seat / but we can definitely make the ccy launch process less
confirms when accepting same team, seat and worker/coordinator (and label future) maybe"

Today, launching `ccy` again with nothing changed still asks Quick Launch's `[Y/n]`, then the
existing containers menu whenever a teammate runs in the same repo; through `ccy --teams` it
first asks the team, the seat and the role and then Quick Launch as well. A saved seat that
another session now holds is found only at the very end, when `agent-bus seat take` refuses it.
The full inventory, with file and line for each question, is
[DESIGN.md section 1](DESIGN.md#1-what-launching-again-asks-today).

This plan replaces those with **one Quick Launch screen**: token, SSH keys, network, each team's
seat, role and (once Plan 00221 lands) label, and the other sessions here, with Enter to launch.
Only the seat is rechecked; a held seat is offered the next free one or the full seat chooser; a
token with no usage left goes to the token chooser. The screen is a `ccy-teams` subcommand the
bash launcher calls, and the launcher does not grow. ("Relaunch" in the folder name is the
owner's word; in the code it means the restart path, so the design avoids it: DESIGN, Words.)
Design and expectations R1-R20: [DESIGN.md](DESIGN.md).

## Goals

- Launching again with nothing changed is one answer (Enter), for plain `ccy` and for
  `ccy --teams` with saved seats (R1, R13, DESIGN 2.3).
- The saved seat is checked before the image and container work; a held one is never launched
  into from the screen, and the next free seat or the chooser is offered (R3-R7); a seat lost to
  a launch racing this one is offered again, not refused, on a terminal (R19).
- The token line shows expiry and cached usage; a used-up or expired token defaults to the token
  chooser and keeps every other setting (R8-R10), through the sibling's token-usage gate.
- Q3 (`[c/s/m/q]`, other containers of this project) is not asked after an accepted screen
  (R11); the container checks, Q2 (zombies) included, run before it (R12).
- `Answer every question again` includes the team chooser, with a `No team` row (R18).
- Unattended launches behave exactly as today (R14); the saved file's format is unchanged
  (R15) and has one reader, the bash (R16).

## Non-Goals

- Restarts, restores and headless runs: they already ask nothing.
- Labels themselves (Plan 00221), the tree board (Plan 00221), worker tool restrictions
  (Plan 00222).
- `cc --teams` on the host: it has no Quick Launch; it can call the same screen later.
- Defining "used up": the token-usage gate's (DESIGN section 3).
- Porting the rest of the launcher to Go (Plan 00191).
- A host-side seat reservation (decision C9), and changing the zombie menu itself (Q2), the
  compose questions (Q5) or the passphrase (Q7).

## Dependencies

- **The token-usage gate** (a sibling worker, `ccy-token-usable`, `internal/ccylaunch` F15):
  **merged into F44 is the hard gate for every build task** (Task 0.2). Both edit `claude-yolo`,
  its allowlist row, `CCY_VERSION` and the Quick Launch difftest goldens. The screen reads its
  `Usability` from the cache; the bash gate stays the authority after the screen.
- **Plan 00221** (labels): either order. This plan adds `SeatView` with an empty `Label`;
  00221 fills it (DESIGN section 4). The requirement must be recorded in 00221 (Task 0.3).
- **Plan 00146 Task 3.3**: rewrites `.last-launch.conf` after a token switch; unaffected (R15).
- **Plan 00209** (v2): supplies its own seat probe when a team migrates (DESIGN section 5).

## Tasks

Every build task is test first: the failing test is committed or shown before the code
(src-go/CLAUDE.md; red and green noted in the journal).

### Phase 0: Decisions and base

- [ ] ⬜ **Task 0.1**: The owner answers C1-C12, or the defaults stand; record each in the
  journal and adjust the tasks.
- [ ] ⬜ **Task 0.2**: Hard gate: the token-usage gate is merged into F44 and this branch is
  rebased or merged onto it. Re-read its final `TokenUsability` signature and exit codes; test
  first, move its cache-only read into `internal/tokens` with its tests, `Front.TokenUsability`
  calling it, so `ccy-teams` never imports `internal/ccylaunch` (C11; DESIGN 2.4); correct
  DESIGN section 3 if anything moved.
- [ ] ⬜ **Task 0.3**: Through the coordinator, ask Plan 00221's owner seat to record in 00221's
  Dependencies that it fills `SeatView.Label` (DESIGN section 4); note the answer in the journal.

### Phase 1: Seats, sessions and the token line

- [ ] ⬜ **Task 1.1**: Test first, the seat probe (`seatProbe`, v1 over
  `agentbus.SeatChooseAs`): for each `SEAT@TEAM` given, its state and the team's DEFAULT; held,
  free, new, a held DEFAULT, no DEFAULT, and an agent-bus failure (stops, R17).
- [ ] ⬜ **Task 1.2**: Test first, the structured `SeatView{Team, Seat, Handle, State, Role, Label}` in `internal/teams/choose`, and `describer.describe` rebuilt on it; the existing
  `describe-seats` tests and `scripts/test-ccy-teams-command.bash` pass unchanged (same bytes).
- [ ] ⬜ **Task 1.3**: Test first, the other sessions of this checkout (R11) from
  `internal/teams/discover`, cross-checked with the project's running containers through
  `internal/engine` and the `ccy-session` label; a container no record names; a lookup failure
  stops (R17).
- [ ] ⬜ **Task 1.4**: Test first, the token line (R8-R10) from the arguments the bash passes
  (R16), `internal/tokens` expiry and the gate's cache-only `Usability`: usable, used up,
  expired, missing, no cache, usage disabled.

### Phase 2: The screen

- [ ] ⬜ **Task 2.1**: Test first, the screen model: the lines of DESIGN 2.2; every R4, R6,
  R8-R11 case as a table test, with and without colour (`NO_COLOR`).
- [ ] ⬜ **Task 2.2**: Test first, the rows and default (R2, R4, R9): which row is first in each
  case; accelerators drawn beside their rows; heading crumbs asserted with `Script.HeadingAt`;
  Esc and Ctrl-C are Quit (R20).
- [ ] ⬜ **Task 2.3**: Test first, `Choose a seat...` (R7): `choose.PickSeat` for the picked
  teams, then back to the screen with the new seats; leaving the chooser returns to the screen
  unchanged.
- [ ] ⬜ **Task 2.4**: Test first, `ccy-teams quick-launch` (C1): the arguments of R16, the
  payload on stdout (answer, then the seat list), diagnostics on stderr; each answer, a closed
  stdout, no terminal (exit 64, R20), and a `--seats-only TEAM` form for R19.
- [ ] ⬜ **Task 2.5**: Test first, `choose-teams --offer-none` (R18): a `No team` row; choosing
  it prints the single stdout line `-` with exit 0 (never a team list: team names start with a
  letter); leaving the chooser still prints nothing (DESIGN R18). Bash side in Task 3.2.

### Phase 3: The launcher

- [ ] ⬜ **Task 3.1**: Difftest scenarios first (`internal/ccylaunch/difftest`), with the
  `ccy-teams` fake (already in `fake_test.go`) given `quick-launch` and `--offer-none` answers in
  `scenarios.json`: launch accepted; seat held then next free; token used up (cached) then
  chooser; teammates running (no Q3); the container preflight before the screen (R12); bare
  `--teams` and `--teams TEAM` with saved seats, with a named team that has no saved seat, and
  with no saved file, checking the canonical arguments and `record-seats` (DESIGN 2.3);
  `Answer every question again` with teams, and its `No team` answer dropping `--teams` (R18); a 75 from `seat take` on a terminal then
  the next free seat (R19); each unattended path unchanged (R14). Goldens for these are recorded
  from the bash after Task 3.2, each difference from today's golden explained in the journal.
- [ ] ⬜ **Task 3.2**: In `claude-yolo`, per DESIGN 2.4: the attended display and `[Y/n]` loop of
  1237-1343 replaced by the call and a `case` on the answer, every unattended and headless line
  listed there kept; the container preflight block 1788-1848 moved whole before the Quick Launch
  block for every launch (R12); Q3 skipped after `launch`; the deferral test at 892 (no saved
  file read there) and the deferred team step and canonical-args rewrite of DESIGN 2.3; line
  880's `-` meaning no team and `ccy_teams_canonical_args` dropping `--teams` for an empty list
  (R18); the R19 retry inline at 3876, adding no function. The allowlist row set to what
  `qa-bash-limits` measures, which must not exceed today's row (lines, statements, functions). `CCY_VERSION` bump and `docs/ccy-changelog.md`.
- [ ] ⬜ **Task 3.3**: `scripts/test-*` suites that pin Quick Launch's text updated in the same
  commit, with the reason.

### Phase 4: Docs, deploy, review

- [ ] ⬜ **Task 4.1**: docs/ccy.md (Quick Launch, sessions with teams), docs/agent-bus.md (the
  Getting started line about launching again), the launcher's `--help`, `ccy-teams --help`
  (`quick-launch`, `--offer-none`).
- [ ] ⬜ **Task 4.2**: The meta-deploy entry for `play-claude-yolo.yml` (which already builds
  `ccy-teams` and installs the launcher) in `CLAUDE/Plan/meta-deploy.list`, in the same commit
  as the code.
- [ ] ⬜ **Task 4.3**: Adversarial review of the diff until MERGE, then `qa-reviewer`.
- [ ] ⬜ **Task 4.4**: On the host: launch a team worker again in a repo where a teammate runs
  (one Enter, no Q3); launch into a saved seat another session holds (next free seat offered
  before the image work); start two launches into the same saved seat at once (the second is
  offered the next free seat at take, R19); launch on a used-up token (token chooser, seats
  kept); `ccy --teams` with saved seats (one Enter).

## Decisions

All are coordinator defaults; **the owner may change any of them**.

| ID  | Decision                                | Default                                                                                                                                                                                                                     |
| --- | --------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| C1  | Name and home of the screen             | `ccy-teams quick-launch`: that binary has `PickSeat`, the describer, `discover` and agent-bus already, and is built by `play-claude-yolo.yml`; no new binary. Not "relaunch" (DESIGN, Words)                                |
| C2  | The screen's form                       | rows with the default first and accelerators drawn (R2), not a bare `[Y/n]` read; `y`/Enter and `n` keep their meaning                                                                                                      |
| C3  | Held seat                               | offer agent-bus's DEFAULT as the first row, plus the chooser (R4, R5); never pick it silently                                                                                                                               |
| C4  | Role on a moved seat                    | the new seat's own role; a coordinator's role stays with its held seat; no promotion from the screen (R6)                                                                                                                   |
| C5  | Other sessions of this project (Q3)     | absorbed: an accepted screen means "start beside them"; Q3 kept on "Answer every question again" (R11)                                                                                                                      |
| C6  | `ccy --teams` with saved seats          | the team step is deferred past the Quick Launch block whenever it would ask (a test at 892 that reads no saved file); the screen does not move earlier; the Go screen offers only the named teams' saved seats (DESIGN 2.3) |
| C7  | Token usage on the screen               | cache only (`fetch=false`); the bash gate at the token's resolution fetches and stays the one authority (R8, DESIGN 3)                                                                                                      |
| C8  | `Answer every question again` and teams | includes the team chooser with a `No team` row when there were saved teams or `--teams` (R18)                                                                                                                               |
| C9  | Two launches racing for one seat        | known gap in v1 (no host-side hold exists before the container's keeper); mitigated by offering the next free seat on take's 75 on a terminal (R19). A true reservation waits for v2                                        |
| C10 | Esc or Ctrl-C at the screen             | Quit, nothing started; a departure from today's "end of input takes the saved settings" for a terminal only (R20)                                                                                                           |
| C11 | Where the cache-only usage read lives   | moved into `internal/tokens` after the gate merges, shared by the launcher front and `ccy-teams` (DESIGN 2.4)                                                                                                               |
| C12 | Container preflight order               | the whole block 1788-1848 (root, rootless, stale cleanup, prompt setting, zombies) moves before the Quick Launch block for every launch, one order for all (R12)                                                            |

## Success Criteria

- [ ] Launching again with nothing changed, plain `ccy` or `ccy --teams`, is one Enter, with a
  teammate running in the same repo too.
- [ ] A saved seat held by another session is shown as held before any image or container work,
  and the next free seat or the chooser is offered; a seat lost to a racing launch is offered
  again on a terminal, not refused.
- [ ] A used-up or expired saved token defaults to the token chooser, and the other saved
  settings are kept.
- [ ] Restore, headless and no-terminal launches are unchanged (difftest goldens).
- [ ] `claude-yolo`'s counted lines and statements do not grow; its allowlist row is what the
  gate measures.
- [ ] `qa-changed.bash` passes on each commit; review at MERGE; `qa-reviewer` passes.

## Delivery & Milestones

- Plan and design: branch `agent-ae04210a0169536be-fb911524`.
