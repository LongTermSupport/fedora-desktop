# Plan 00223: ccy one confirm relaunch

**Status**: Not Started (plan only; decisions C1-C20 open, coordinator defaults stated)
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
existing containers menu whenever a teammate runs in the same repo, and a detached-session picker
first whenever a teammate's session is detached; through `ccy --teams` it also asks the team, the
seat and the role. A saved seat that another session now holds is found only at the very end,
when `agent-bus seat take` refuses it. The full inventory, with file and line for each question,
is [DESIGN.md section 1](DESIGN.md#1-what-launching-again-asks-today).

This plan replaces those with **one Quick Launch screen**: token, SSH keys, network, each team's
seat, role and (once Plan 00221 lands) label, and the other sessions here, with Enter to launch.
Only the seat is rechecked; a held seat is offered the next free one, the full seat chooser, or
attaching to the detached session that holds it; a token with no usage left goes to the token
chooser. The screen is a `ccy-teams` subcommand the bash launcher calls before tmux; it hands
over only the answer and the seats, and the token, keys and network stay on today's
saved-settings path; the launcher does not grow. ("Relaunch" in the folder
name is the owner's word; in the code it means the restart path, so the design avoids it:
DESIGN, Words.) Design and expectations R1-R20: [DESIGN.md](DESIGN.md).

## Goals

- Launching again with nothing changed is one answer (Enter), for plain `ccy` and for
  `ccy --teams` with saved seats (R1, R13, DESIGN 2.3).
- The saved seat is checked before the image and container work; a held one is never launched
  into from the screen, and the next free seat, the chooser or an attach is offered (R3-R7); a
  launch that loses a seat to a racing one is refused as today, and its next launch's screen
  offers the next free seat (R19, C9).
- The token line shows expiry and cached usage; a used-up or expired token defaults to the token
  chooser and keeps every other setting (R8-R10), through the sibling's token-usage gate.
- Q3 (`[c/s/m/q]`, other containers of this project) is not asked after an accepted screen, and
  the detached-session picker has nothing new to offer a team launch after it (R11).
- `Answer every question again` includes the team chooser, with a `No team` row (R18).
- Every question is answered before tmux; the inner run gets the answer (`--settled`) and the
  seats (`--teams`), and takes the token, keys and network from the saved file as today, so
  records, restores and restarts are today's (DESIGN 2.1).
- Unattended launches behave exactly as today (R14); the saved file's format is unchanged
  (R15) and has one reader, the bash (R16).

## Non-Goals

- Restarts, restores and headless runs: they already ask nothing.
- Labels themselves (Plan 00221), the tree board (Plan 00221), worker tool restrictions
  (Plan 00222).
- `cc --teams` on the host: it has no Quick Launch; it can call the same screen later.
- Defining "used up": the token-usage gate's (DESIGN section 3).
- Porting the rest of the launcher to Go (Plan 00191).
- A host-side seat reservation (decision C9); moving or changing the container preflight and
  zombie menu (Q2, decision C12), the compose questions (Q5) or the passphrase (Q7); changing
  `lib/tmux-session.bash` or `lib/agent-bus-seats.bash` (`lib/session-registry.bash` gets only
  the `--settled` classification, Task 3.2).

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

- [ ] ⬜ **Task 0.1**: The owner answers C1-C20, or the defaults stand; record each in the
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
- [ ] ⬜ **Task 1.3**: Test first, the sessions of this checkout (R11) from
  `internal/teams/discover` and `internal/tmuxccy` (attached or detached), cross-checked with the
  project's running containers through `internal/engine` and the `ccy-session` label; a
  container no live session names; `--session NAME` left out; the containers read on the
  `--engine` given (C19), podman and docker, and a docker read refused for lack of group access
  stopping with the engine's words; a lookup failure stops (R17).
- [ ] ⬜ **Task 1.4**: Test first, the token line (R8-R10) from the arguments the bash passes
  (R16), `internal/tokens` expiry and the gate's cache-only `Usability`: usable, used up,
  expired, missing, no cache, usage disabled.

### Phase 2: The screen and its answer

- [ ] ⬜ **Task 2.1**: Test first, the screen model: the lines of DESIGN 2.2's R1-R11; every R4,
  R6, R8-R11 case as a table test, with and without colour (`NO_COLOR`).
- [ ] ⬜ **Task 2.2**: Test first, the rows and default (R2, R4, R9): which row is first in each
  case; the `Attach` row only for a detached holder; accelerators drawn beside their rows;
  heading crumbs asserted with `Script.HeadingAt`; Esc and Ctrl-C are Quit (R20).
- [ ] ⬜ **Task 2.3**: Test first, `Choose a seat...` (R7): `choose.PickSeat` for the picked
  teams, then back to the screen with the new seats; leaving the chooser returns to the screen
  unchanged.
- [ ] ⬜ **Task 2.4**: Test first, the team questions the screen settles in process (R13, R18,
  DESIGN 2.3): the filtering of saved seats by the given teams; a named team with no saved seat
  asked after the yes through `resolve`; `Answer every question again` running the chooser's
  list with a `No team` row (bare `--teams` or saved teams) or the named team's seat question.
- [ ] ⬜ **Task 2.5**: Test first, the answer (DESIGN 2.1): NUL-separated `run` and the new
  argument list (given `--teams` words removed at the passed positions, bare `--teams` before a
  flag included, a one-word `--teams=SEAT@TEAM` removed as one word; `--settled=launch:STAMP`,
  `token:STAMP` or `ask` (the `--saved-stamp` given, C17) and, on a team, `--teams` added at the
  front; no `--teams` with `--no-bus`; never a token, key or network flag), or
  `attach NAME`, or `quit`; diagnostics on stderr; a closed stdout; no terminal (exit 64); uid 0
  refused (R12); the `Attach` row only with `TMUX` empty (R4); with `--session NAME`, the seats
  recorded in that session's record; with `--no-bus`, no seat probe, no team question, no
  `--teams`.
- [ ] ⬜ **Task 2.6**: `ccy-teams quick-launch` wired in `internal/teams/app.go` with the
  arguments of R16, and `ccy-teams --help`.

### Phase 3: The launcher

- [ ] ⬜ **Task 3.0**: Test first, a terminal mode for the difftest harness (decision C20,
  coordinator default): today it runs the launcher with stdin a reader or nothing and stdout
  and stderr pipes (`internal/ccylaunch/difftest/sandbox_test.go:505-511`), so 875-876's
  attended test never passes and every attended scenario would pin the unattended branch. A
  scenario field names which of stdin, stdout and stderr are a pseudo-terminal (`x/sys/unix`
  is already imported in `fake_test.go`); `TestOracleCanFail` extended to a terminal scenario
  that must fail when the attended path is broken. Fallback, if a pty cannot be made reliable in
  the harness: the attended scenarios go to a `script -q`-driven `scripts/test-ccy-*.bash`
  suite, as `scripts/test-ccy-token-mode.bash` already does, recorded in the journal.
- [ ] ⬜ **Task 3.1**: Difftest scenarios first (`internal/ccylaunch/difftest`, the attended ones
  in Task 3.0's terminal mode), with the
  `ccy-teams` fake (already in `fake_test.go`) given `quick-launch` answers in `scenarios.json`:
  launch accepted, and the re-exec's arguments (token hand-off included); seat held then next
  free; held by a detached session then attach; token used up (cached) then chooser; teammates
  running (no Q3; P0 offered nothing for a team launch); bare `--teams` and `--teams TEAM` with
  saved seats, with a named team that has no saved seat, and with no saved file (the moved
  block before tmux, P0 given canonical seats); the saved file rewritten while the screen is
  open (the stamp differs: the inner run stops, C17); `Answer every question again` (no
  `using the previous configuration` line, C18); `ccy --engine docker` (the screen given it); saved teams with plain `ccy`; `Answer every question again` with teams and its `No team`; `--create-token` and `--update-token` with a saved file (no
  screen, the `Token mode` reason, C16); `--teams=SEAT@TEAM` and
  `--teams SEAT@TEAM` given (no stray word in the re-exec's arguments); a 75 from `seat take`
  refused as today (C9); `Answer every question again` with `--no-bus` (saved team kept); a
  restore and a restart of a session launched from the screen (its record has no `--settled`:
  today's unattended branch); a saved network gone, attended (compose and Q4 as today) and on
  restore; a restore whose saved keys are the agent sentinel; a restore after a `--settled=token`
  launch (the token `select_token` chose, from the file); `--no-bus` and `--opencode` with saved
  teams (no `--teams` in the answer, the saved team kept in the file); stdin a terminal and
  stderr not, insulated (the screen in the pane and its `record-seats`) and not (C14); each unattended path unchanged (R14). Goldens recorded from the bash after Task 3.2, each difference
  from today's golden explained in the journal.
- [ ] ⬜ **Task 3.2**: In `claude-yolo`, per DESIGN 2.4 ("Leaves", "Stays", "New"): the attended
  branch of the Quick Launch block removed; the screen's call before 1195 under the conditions
  of DESIGN 2.1, with `load_launch_config` called there and line 1201 moved above it, and the
  `case` on `run` (hand-off and exec), `attach` and `quit`; the 892-899 block moved to just
  after the screen's place, with the modes that exit between 892 and 1195 listed from the code
  and journalled first (DESIGN 2.3); parsing and checking `--settled` and its stamp (C17), its
  `quick_unasked` line (C18) and the `ask` skip, the token modes' `elif`, the in-place changes
  at 875-876, 1271, 1281 and 1293; the Q3 skip. No new function. In `lib/session-registry.bash`, `--settled=` in the `drop` case pattern and
  `--settled` in `CCY_REGISTRY_DROP_FLAGS` (a restart drops it through the same classes), with
  `scripts/test-ccy-session-registry.bash` passing and a case for both forms. If the counts
  would grow, DESIGN 2.4's C15 applies. The
  allowlist row set to what `qa-bash-limits` measures, which must not exceed today's (lines,
  statements, functions). `CCY_VERSION` bump and `docs/ccy-changelog.md`.
- [ ] ⬜ **Task 3.3**: `scripts/test-*` suites that pin Quick Launch's text or the order of the
  questions updated in the same commit, with the reason.

### Phase 4: Docs, deploy, review

- [ ] ⬜ **Task 4.1**: docs/ccy.md (Quick Launch, sessions with teams, sessions survive a
  reboot: what a record now holds), docs/agent-bus.md (the Getting started line about launching
  again), the launcher's `--help`.
- [ ] ⬜ **Task 4.2**: The meta-deploy entry for `play-claude-yolo.yml` (which already builds
  `ccy-teams` and installs the launcher) in `CLAUDE/Plan/meta-deploy.list`, in the same commit
  as the code.
- [ ] ⬜ **Task 4.3**: Adversarial review of the diff until MERGE, then `qa-reviewer`.
- [ ] ⬜ **Task 4.4**: On the host: launch a team worker again in a repo where a teammate runs
  attached and another detached (one Enter, no Q3, no P0); launch into a saved seat a detached
  session holds (attach offered) and one an attached session holds (next free seat offered),
  both before the image work; start two launches into the same saved seat at once (the loser is
  refused, and its next launch's screen offers the next free seat, C9); launch on a used-up token (token chooser, seats
  kept); `ccy --teams` with saved seats (one Enter); reboot and restore such a session.

## Decisions

All are coordinator defaults; **the owner may change any of them**.

| ID  | Decision                                              | Default                                                                                                                                                                                                                                                                                                                                                           |
| --- | ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| C1  | Name and home of the screen                           | `ccy-teams quick-launch`: that binary has `PickSeat`, the chooser, the describer, `discover`, `tmuxccy` and agent-bus already, and is built by `play-claude-yolo.yml`; no new binary. Not "relaunch" (DESIGN, Words)                                                                                                                                              |
| C2  | The screen's form                                     | rows with the default first and accelerators drawn (R2), not a bare `[Y/n]` read; `y`/Enter and `n` keep their meaning                                                                                                                                                                                                                                            |
| C3  | Held seat                                             | offer agent-bus's DEFAULT as the first row, plus the chooser, plus `Attach` when a detached session of this checkout holds it (R4, R5); never pick it silently                                                                                                                                                                                                    |
| C4  | Role on a moved seat                                  | the new seat's own role; a coordinator's role stays with its held seat; no promotion from the screen (R6)                                                                                                                                                                                                                                                         |
| C5  | Other sessions of this project (Q3)                   | absorbed: `launch` and `token` mean "start beside them"; Q3 kept on "Answer every question again" (R11)                                                                                                                                                                                                                                                           |
| C6  | Where the screen runs and what it hands over          | coordinator decision, which the owner can change: in the outer run before tmux, handing over only the answer (`--settled`) and the seats (`--teams`); token, keys and network stay on today's saved-settings path with no explicit flags, so a gone network, the agent key and restores behave as today (DESIGN 2.1); the team step deferred to it from 892 (2.3) |
| C7  | Token usage on the screen                             | cache only (`fetch=false`); the bash gate at the token's resolution fetches and stays the one authority (R8, DESIGN 3)                                                                                                                                                                                                                                            |
| C8  | `Answer every question again` and teams               | the screen runs the team chooser in process, with a `No team` row in the list, when there were saved teams or `--teams` (R18)                                                                                                                                                                                                                                     |
| C9  | Two launches racing for one seat                      | known gap in v1 (no host-side hold exists before the container's keeper); the loser is refused as today (exit 75) and its next launch's screen offers the next free seat; no in-launcher retry, which would cost about ten lines in a file that may not grow (DESIGN R19). A true reservation waits for v2                                                        |
| C10 | Esc or Ctrl-C at the screen                           | Quit, nothing started; a departure from today's "end of input takes the saved settings" for a terminal only (R20)                                                                                                                                                                                                                                                 |
| C11 | Where the cache-only usage read lives                 | moved into `internal/tokens` after the gate merges, shared by the launcher front and `ccy-teams` (DESIGN 2.4)                                                                                                                                                                                                                                                     |
| C12 | Zombie menu (Q2) and container preflight              | unmoved, after the screen in the inner run; a zombie holding a saved seat is shown on the screen and the seat moved by default (R12, with the reason)                                                                                                                                                                                                             |
| C13 | Detached-session picker (P0) on a launch with no team | kept as today (attaching is a different action from confirming settings); for team launches it has nothing new after the screen (R11). The owner may ask for it folded into the screen                                                                                                                                                                            |
| C14 | Stdin a terminal, stderr not (`ccy 2>file`)           | insulated: the screen shows in the pane, which has all three on terminals; not insulated: the saved settings without the team, as end of input today (DESIGN R14). Today Q1 is read there, with its prompt in the file                                                                                                                                            |
| C15 | The new bash would grow the file                      | shrink it first (DESIGN 2.4); if it still grows, the build stops and the owner decides; this plan never raises the ratchet                                                                                                                                                                                                                                        |
| C16 | `--create-token`, `--update-token` with a saved file  | no question: an unasked reason `Token mode` takes the saved keys and network for the token's GitHub setup, as today's default yes, and says so (DESIGN R14). Today Q1 is asked there; the alternative is the key menus                                                                                                                                            |
| C17 | The saved file changes while the screen is open       | coordinator default the owner can change: the answer carries the file's stamp (`--settled=launch:STAMP`, `token:STAMP`) and the inner run stops if its file's stamp differs (DESIGN 2.1)                                                                                                                                                                          |
| C18 | What `--settled` sets in the block                    | coordinator default the owner can change: only `launch` and `token` set `quick_unasked`; `ask` prints today's `n` words and no `using the previous configuration` line (DESIGN 2.1)                                                                                                                                                                               |
| C19 | The screen's container engine                         | coordinator default the owner can change: the bash passes `--engine "$CONTAINER_ENGINE"` (DESIGN R16)                                                                                                                                                                                                                                                             |
| C20 | Testing the attended paths                            | coordinator default the owner can change: a test-first terminal mode in the difftest harness (Task 3.0), with a `script -q` bash suite as the fallback                                                                                                                                                                                                            |

## Success Criteria

- [ ] Launching again with nothing changed, plain `ccy` on a team or `ccy --teams`, is one Enter,
  with teammates running in the same repo, attached or detached.
- [ ] A saved seat held by another session is shown as held before any image or container work,
  and the next free seat, the chooser or an attach is offered; a seat lost to a racing launch is
  refused as today and offered moved on the next launch.
- [ ] A used-up or expired saved token defaults to the token chooser, and the other saved
  settings are kept.
- [ ] Restore, restart, headless, no-terminal and token-mode launches are unchanged (difftest
  goldens).
- [ ] `claude-yolo`'s counted lines, statements and functions do not grow; its allowlist row is
  what the gate measures.
- [ ] `qa-changed.bash` passes on each commit; review at MERGE; `qa-reviewer` passes.

## Delivery & Milestones

- Plan and design: branch `agent-ae04210a0169536be-fb911524`.
