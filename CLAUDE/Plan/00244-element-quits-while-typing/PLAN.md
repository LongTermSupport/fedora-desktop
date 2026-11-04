# Plan 00244: element quits while typing

**Status**: In Progress
**Created**: 2026-11-03
**Owner**: joseph
**Priority**: Medium

## Overview

The Element desktop client (the Flathub `im.riot.Riot` flatpak, one `--profile` per agent
team) vanished twice within ten minutes while the owner was typing a long message in a
team room. There was no "not responding" dialog: the window disappeared at once, and the
whole application exited. The unsent draft survived the restart.

This plan establishes the cause from evidence, then decides what to do about it: nothing,
a workaround, an upstream report, or a different Matrix client.

## Evidence So Far

Both exits are visible only as the app's systemd user scopes ending. Ruled out:

- **A native crash.** `coredumpctl` recorded no dump, although it does capture crashes on
  this host. No Crashpad dump was written in the profile either.
- **Memory.** No kernel OOM kill, no systemd-oomd action, and most of the RAM was free.
- **Anything we run.** No agent session and nothing in this repository stops Element.
- **A freeze then Force Quit.** The owner saw no "not responding" dialog.
- **Element's own log.** Its output in the journal stops mid-run, with no error line and no
  quit line.

The leading hypothesis is a stray Ctrl+Q while typing. Element's source
([electron-main.ts](https://github.com/element-hq/element-desktop/blob/develop/src/electron-main.ts))
catches Ctrl+Q, Ctrl+Shift+Q and Alt+F4 when `warnBeforeExit` is on (the default, and on
here). It then shows a confirmation whose default button is "Close Element"
(`defaultId: 1`; only `cancelId: 0` cancels). If the next key typed is Enter, the dialog
confirms the quit before it can be read. The owner confirmed that both Ctrl+Q shortcuts do
show the prompt. Ctrl+W is ruled out: with the tray icon on, it only hides the window.

## Goals

- Know why Element exited, from evidence rather than inference.
- Leave the owner with a setup in which a typing slip cannot silently close the client.

## Non-Goals

- Changing how the agent bus or its rooms work.
- Patching or rebuilding Element ourselves.

## Tasks

### Phase 1: Establish the cause

- [x] ✅ **Task 1.1**: Rule out a native crash, OOM, our own tooling and a freeze (see
  Evidence So Far).
- [x] ✅ **Task 1.2**: Read Element's quit-shortcut code: Ctrl+Q prompts, and the prompt's
  default button is quit.
- [ ] 🔄 **Task 1.3**: Capture the next exit. The profile now runs from a terminal with its
  output in a gitignored log under `untracked/scratch/`. A log that just stops points to a
  quit; an error trace at the end points to a crash.
- [ ] ⬜ **Task 1.4**: Reproduce: type in the composer, press Ctrl+Q, then Enter at once. If
  the client closes, the mechanism is confirmed.

### Phase 2: Decide

- [ ] ⬜ **Task 2.1**: The owner chooses: live with it, report upstream (a quit warning
  whose default is "quit" defeats its purpose; public, so the owner's call), or work
  around it.
- [ ] ⬜ **Task 2.2**: If the owner wants it, compare Matrix clients for the team rooms.
  Threads are a must, since agents' questions to humans are threads. Several logins side
  by side are also a must, since there is one per team. This is parked behind finishing
  open work.

### Phase 3: Matrix clients get their own play

- [x] ✅ **Task 3.1**: `playbooks/imports/optional/common/play-matrix-clients.yml` installs
  Nheko from Flathub (`im.nheko.Nheko`, plain install), decoupled from the
  agent bus. Queued in `CLAUDE/Plan/meta-deploy.list`.
- [ ] 🔄 **Task 3.2**: Bring Element across: its Flatpak install and its keyring grant move
  from `play-agent-bus-element.yml` into `tasks/element-desktop.yml`, which
  `play-matrix-clients.yml` imports, and the agent-bus play keeps its per-team profile and
  launcher wiring (`helpers/agent_bus/element.py`). Built as a shared task file and not as a
  plain move, because `ccy-teams` runs the agent-bus play on its own after a team's play
  (`src-go/internal/teams/ops/ops.go`), so a play that no longer installed Element would fail
  on a host that never ran the clients play. The owner may prefer the stricter move: the
  agent-bus play only checks that Element is there, and `ccy-teams` runs the clients play
  first (a Go change to `ops.go`, its tests and its usage text).

## Success Criteria

- [ ] The cause is shown by a captured exit or a reproduction.
- [ ] The owner has made the Phase 2 decision.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
