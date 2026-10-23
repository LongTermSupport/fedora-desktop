# Plan 00223: ccy one confirm relaunch

**Status**: Not Started (plan only; decisions C1-C7 open, coordinator defaults stated)
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

Today a relaunch that changes nothing still asks Quick Launch's `[Y/n]`, then the existing
containers menu whenever a teammate runs in the same repo; through `ccy --teams` it first asks
the team, the seat and the role and then Quick Launch as well. A saved seat that another
session now holds is found only at the very end, when `agent-bus seat take` refuses it. The
full inventory, with file and line for each question, is [DESIGN.md section 1](DESIGN.md#1-what-a-relaunch-asks-today).

This plan replaces those with **one relaunch screen**: token, SSH keys, network, each team's
seat, role and (once Plan 00221 lands) label, and the other sessions here, with Enter to
relaunch. Only the seat is rechecked; a held seat is offered the next free one or the full
seat chooser; a token with no usage left goes to the token chooser. The screen is a Go command
the bash launcher calls, so the launcher shrinks. Design and expectations R1-R16:
[DESIGN.md](DESIGN.md).

## Goals

- A relaunch with nothing changed is one answer (Enter), for plain `ccy` and for
  `ccy --teams` with saved seats (R1, R13).
- The saved seat is checked before anything else runs; a held one is never launched into, and
  the screen offers the next free seat or the chooser (R3-R7).
- The token line shows expiry and usage; a used-up or expired token defaults to the token
  chooser and keeps every other setting (R8-R10), through the sibling's token-usage gate.
- Q3 (`[c/s/m/q]`, other containers of this project) is not asked after an accepted screen
  (R11).
- Unattended launches behave exactly as today (R14); the saved file's format is unchanged
  (R15).

## Non-Goals

- Restarts, restores and headless runs: they already ask nothing.
- Labels themselves (Plan 00221), the tree board (Plan 00221), worker tool restrictions
  (Plan 00222).
- `cc --teams` on the host: it has no Quick Launch; it can call the same screen later.
- Defining "used up": the token-usage gate's (DESIGN section 3).
- Porting the rest of the launcher to Go (Plan 00191).
- Changing the zombie menu (Q2), the compose questions (Q5) or the passphrase (Q7).

## Dependencies

- **The token-usage gate** (a sibling worker, `ccy-token-usable`, `internal/ccylaunch` F15):
  must be merged before Phase 2. The screen reads its `Usability`; the bash gate stays the
  authority after the screen.
- **Plan 00221** (labels): independent order. Before it lands the label column is absent; after,
  it appears through the describer (DESIGN section 4). One requirement for 00221 is listed there.
- **Plan 00146 Task 3.3**: rewrites `.last-launch.conf` after a token switch; unaffected (R15).
- **Plan 00209** (v2): supplies its own seat probe when a team migrates (DESIGN section 5).

## Tasks

Every build task is test first: the failing test is committed or shown before the code
(src-go/CLAUDE.md; red and green noted in the journal).

### Phase 0: Decisions and base

- [ ] ⬜ **Task 0.1**: The owner answers C1-C7, or the defaults stand; record each in the
  journal and adjust the tasks.
- [ ] ⬜ **Task 0.2**: Confirm the token-usage gate is merged into the base, and re-read its
  final `Usability` and exit codes; correct DESIGN section 3 if they moved.

### Phase 1: Reading what was saved

- [ ] ⬜ **Task 1.1**: Test first, a strict reader of `.last-launch.conf` in
  `internal/ccylaunch` (the keys `load_launch_config` requires, `KEY=VALUE` and `KEY="VALUE"`
  only; any other line, a missing key or another `SAVED_CONFIG_VERSION` refused with the
  reason). Oracle test: lines written by the bash writer (`claude-yolo:3449-3463`) read back
  to the same values.
- [ ] ⬜ **Task 1.2**: Test first, the seat probe (`seatProbe`, v1 over
  `agentbus.SeatChooseAs`): for each saved `SEAT@TEAM`, its state and the team's DEFAULT;
  held, free, new, a held DEFAULT, and an agent-bus failure (stops, R16).

### Phase 2: The screen

- [ ] ⬜ **Task 2.1**: Test first, the screen model: the lines of DESIGN 2.2 from the saved
  settings, the seat probe, the token's expiry and `Usability`, and the running containers;
  every R4, R6, R8-R11 case as a table test, with and without colour (`NO_COLOR`).
- [ ] ⬜ **Task 2.2**: Test first, the rows and default (R2, R4, R9): which row is first in
  each case; accelerators drawn beside their rows; heading crumbs asserted with
  `Script.HeadingAt`.
- [ ] ⬜ **Task 2.3**: Test first, `Choose a seat...` (R7): `choose.PickSeat` for the picked
  teams, then back to the screen with the new seats; leaving the chooser returns to the
  screen unchanged.
- [ ] ⬜ **Task 2.4**: The command `ccy-relaunch` (name per C1), thin `cmd/` main, the payload
  on stdout (answer, then the seat list), diagnostics on stderr; tests for each answer, a closed
  stdout, and no terminal (refuses: the bash never calls it unattended).

### Phase 3: The launcher

- [ ] ⬜ **Task 3.1**: Difftest scenarios first (`internal/ccylaunch/difftest`): relaunch
  accepted, seat held then next free, token used up then chooser, teammates running (no Q3),
  `--teams` bare with saved seats (R13), and each unattended path unchanged (R14).
- [ ] ⬜ **Task 3.2**: Replace `claude-yolo:1237-1343` with the call and a `case` on the
  answer; skip Q3 after `relaunch`; R13 ordering for bare `--teams` and `--teams TEAM`
  (`claude-yolo:877-893`). Lower the `scripts/bash-limits.allowlist` row to the new counts.
  `CCY_VERSION` bump (and `docs/ccy-changelog.md`).
- [ ] ⬜ **Task 3.3**: `scripts/test-*` suites that pin Quick Launch's text updated in the same
  commit, with the reason.

### Phase 4: Docs, deploy, review

- [ ] ⬜ **Task 4.1**: docs/ccy.md (Quick Launch, sessions with teams), docs/agent-bus.md
  (the Getting started relaunch line), the launcher's `--help`.
- [ ] ⬜ **Task 4.2**: `play-claude-yolo.yml` builds and installs `ccy-relaunch` through
  `tasks/build-go-command.yml`; a row in `scripts/go-binary-suites.tsv`; the meta-deploy entry
  in `CLAUDE/Plan/meta-deploy.list` in the same commit.
- [ ] ⬜ **Task 4.3**: Adversarial review of the diff until MERGE, then `qa-reviewer`.
- [ ] ⬜ **Task 4.4**: On the host: relaunch a team worker in a repo where a teammate runs
  (one Enter, no Q3); relaunch into a seat another session holds (next free seat offered, no
  late refusal); relaunch on a used-up token (token chooser, seats kept).

## Decisions

All are coordinator defaults; **the owner may change any of them**.

| ID  | Decision                            | Default                                                                                                                          |
| --- | ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| C1  | Name and home of the command        | `ccy-relaunch`, in `internal/ccylaunch` beside `ccy-token-usable`, installed by `play-claude-yolo.yml`                           |
| C2  | The screen's form                   | gum-style rows with the default first and accelerators drawn (R2), not a bare `[Y/n]` read; `y`/Enter and `n` keep their meaning |
| C3  | Held seat                           | offer agent-bus's DEFAULT as the first row, plus the chooser (R4, R5); never pick it silently                                    |
| C4  | Role on a moved seat                | the new seat's own role; a coordinator's role stays with its held seat; no promotion from the screen (R6)                        |
| C5  | Other sessions of this project (Q3) | absorbed: an accepted screen means "start beside them"; Q3 kept on "Answer every question again" (R11)                           |
| C6  | `ccy --teams` with saved seats      | the screen first, the team and seat chooser behind `Choose a seat...` (R13)                                                      |
| C7  | Token preview versus the bash gate  | the screen previews and routes; the bash gate at the token's resolution stays the one authority (DESIGN section 3)               |

## Success Criteria

- [ ] Relaunching with nothing changed, plain `ccy` or `ccy --teams`, is one Enter, with a
  teammate running in the same repo too.
- [ ] A saved seat held by another session is shown as held before any image or container work,
  and the next free seat or the chooser is offered; F1 is not reached from the screen.
- [ ] A used-up or expired saved token defaults to the token chooser, and the other saved
  settings are kept.
- [ ] Restore, headless and no-terminal launches are unchanged (difftest goldens).
- [ ] `claude-yolo` is shorter than before and its allowlist row lowered.
- [ ] `qa-changed.bash` passes on each commit; review at MERGE; `qa-reviewer` passes.

## Delivery & Milestones

- Plan and design: branch `agent-ae04210a0169536be-fb911524`.
