# Plan 00171: Speech-to-text: Insert is never ignored, the icon is always in sync

**Status**: In Progress (owner decisions D1 to D3 made; build from Phase 2)
**Created**: 2026-10-12
**Owner**: joseph
**Priority**: High

## Overview

On the host, Insert stopped working: every press flashed nothing, and a dictation kept
recording with the panel showing the idle mic. The research
([RESEARCH-race.md](RESEARCH-race.md)) found the cause in the host's own debug log.
Insert was pressed 376 ms after a dictation's green SUCCESS, so the extension started a
new client. Then the old client's finishing IDLE signals, which carry no identity,
overwrote the new client's RECORDING, and the old client deleted the shared PID file
the new client had just written. From then on the extension believed it was idle.
Each Insert spawned another client, the busy server rejected it, and the rejection
was wiped from the icon within about 15 ms.

The owner's requirements:

- **Insert is never ignored.** Every press has a visible effect.
- **Blue (idle) icon + Insert → a recording starts.** Always.
- **Insert while not yet ready to record → an hourglass ("please wait"), then the
  recording starts once ready.** An Insert is an intention to record. A stop sign would
  say the press was ignored, so there is none (owner's decision, D1 to D3).
- **If the icon says ready, the system is ready.** A press on a ready icon starts a
  recording.
- **The icon always matches what is actually happening.** An icon showing "not
  recording" while a mic is recording is a defect that the design must make
  impossible, not merely unlikely.

## Goals

- Signals from a client that is not the current one can never change the icon.
- A client removes the PID file only if the file holds its own PID.
- A START the server rejects is visible (icon and message) and leaves the icon showing
  the server's real state, never the idle mic over a live dictation.
- The extension reconciles with reality: a live recorder while the icon is idle, or no
  recorder while it shows recording, is detected and corrected.
- Insert's effect is defined, tested and documented for every state.

## Non-Goals

- Bluetooth headset microphone input: Plan 00172.
- Changing what the grace stop, continuous dictation or paste do (Plan 00148). This
  plan changes only which client owns the panel and what Insert does in each state.

## Tasks

### Phase 1: Owner decisions (decision gate)

- [x] ✅ **Task 1.1 — D1: Insert in the grace period.** STOPPING (mic still open) keeps
  Insert = stop now (Plan 00148 Task 0.6). The hourglass applies only where the mic is
  closed and the system is not yet ready to record.
- [x] ✅ **Task 1.2 — D2: A press while not ready is queued.** The hourglass shows, and the
  recording starts by itself as soon as the system is ready.
- [x] ✅ **Task 1.3 — D3: The icon is an hourglass**, not a stop sign. No installed theme
  ships one, so the extension bundles its own symbolic SVG.
- [x] ✅ **Task 1.4**: Answers recorded under Technical Decisions.

### Phase 2: One owner for the panel (tests first)

- [ ] ⬜ **Task 2.1**: A session token per spawn. The extension generates it and passes it to
  the client (`--session-token`, both `wsi-stream` and `wsi`). Every `StateChanged`,
  `Error` and `Progress` signal carries it. The extension drops any signal whose token
  is not the current client's, and logs the drop. This changes the signal signature
  (`(s)` → `(ss)`), so extension and clients change together in the same deploy.
- [ ] ⬜ **Task 2.2**: The PID file is removed only if it holds the remover's own PID, in
  every unlink (`wsi-stream` and `wsi`). The extension keeps the current client's PID,
  so stop and Escape no longer depend on the file at all. Caveat: with `WHISPER_MODEL`
  set, the spawn is a `/bin/bash -c` wrapper (`extension.js:960`), so the spawned PID
  is the wrapper's. Either the wrapper `exec`s the client, or the client reports its
  own PID in its first token-tagged signal. Choose one and test it.
- [ ] ⬜ **Task 2.3**: No signal after a terminal state. Remove the extra IDLE after SUCCESS
  (`wsi-stream:1493-1494`, `wsi`'s delayed IDLE): the extension returns to idle by
  itself when the SUCCESS/ERROR display ends. The atexit IDLE stays only for a client
  that never reached a terminal state, and is token-tagged.
- [ ] ⬜ **Task 2.4**: Pull the panel's state and Insert decisions out of `extension.js` into
  a plain module (as `recorderLaunch.js` is). Write node tests in `tests/extensions/`
  for every state × Insert/Escape combination and for the race in RESEARCH-race.md: a
  stale client's IDLE after a new client's RECORDING must not change the state.
- [ ] ⬜ **Task 2.5**: Python tests in `tests/speech_to_text/test_server_client.py`: a PID
  file owned by another PID is not removed; no IDLE follows SUCCESS; every signal
  carries the token.

### Phase 3: Insert in every state, and reconciliation

- [ ] ⬜ **Task 3.1**: Idle (blue) + Insert always starts a recording. If the server is
  still finishing the previous dictation, the D2 behaviour applies. A START the server
  rejects shows the error (icon and an `Error` signal, so the menu shows it too) for
  its full display time. The ready (blue) icon is shown only when no owned client is
  alive, so a press on it can always start. The client sends RECORDING only after START is accepted.
- [ ] ⬜ **Task 3.2**: Insert while not ready (TRANSCRIBING, the paste, SUCCESS/ERROR before
  the client has exited, a cold server start): show the bundled hourglass and queue the
  start (Decision 1). The queued start runs when the owned client has exited and the
  server answers ready. A second Insert while queued cancels it. Tests for each.
- [ ] ⬜ **Task 3.3**: Reconciliation. The extension watches the current client's exit for
  its whole life, not only while the launch is pending. A client that exits without a
  terminal state resets the panel and logs why. A live recorder process at Insert that
  the extension does not own is stopped and reported, never silently left recording.
- [ ] ⬜ **Task 3.4**: The server logs every rejected command and its reason
  (`wsi-stream-server` logs only "Received command: START" today).

### Phase 4: Deploy and verify

- [ ] ⬜ **Task 4.1**: `deploy.bash` (`play-speech-to-text.yml`) and `acceptance.bash`. The
  acceptance replays the incident: Insert within 0.5 s of SUCCESS, then Insert again;
  the second press must stop the new dictation. Debug-log lines are the evidence. What
  needs eyes on the panel is NOT ESTABLISHABLE and is listed for the owner. Add the
  plan to `meta-deploy.bash`; an extension change needs a logout, so it runs in two
  rounds.
- [ ] ⬜ **Task 4.2**: Docs: the Insert table in `docs/features/speech-to-text.md`.
- [ ] ⬜ **Task 4.3**: `./scripts/qa-all.bash`, ESLint for the extension, then the
  `qa-reviewer` agent over the plan's diff.

## Technical Decisions

### Decision 1: An hourglass and a queued start, never a stop sign

**Context**: What an Insert press should do when the system cannot record yet (the
previous dictation is transcribing, pasting or finishing, or the server is starting).
**Options considered**: a stop sign, with the owner pressing again later; or an
hourglass, with the recording starting when ready.
**Decision**: the hourglass and the queued start. The owner's reasoning: "If we show the
stop sign, it means that the press has been ignored … an insert press clearly indicates
an intention to start recording. So if we're not ready to record, display the
hourglass." The grace period (mic still open) keeps Insert = stop now. A second Insert
while a start is queued cancels the queued start, so the press still does something.
**Date**: 2026-10-12

### Decision 2: An icon that lies is a defect to make impossible

**Context**: The owner's rule: "if the extension says that we're ready to record, then
we have to be ready to record. And if the icon is showing that we're not recording but
we are recording, then something is definitely wrong … make that impossible."
**Decision**: The icon is derived from the state of one owned client (session token,
Phase 2), corrected by liveness reconciliation (Task 3.3). It is never set from an
unidentified signal. Each of these invariants has a test.
**Date**: 2026-10-12

## Success Criteria

- [ ] The incident's sequence (Insert just after SUCCESS, then Insert again) starts and
  then stops a dictation, proven by a test and by acceptance on the host.
- [ ] No test or host run shows the idle icon while a recorder is alive.
- [ ] Every Insert press has a visible effect in every state, per the documented table.
- [ ] `./scripts/qa-all.bash` and ESLint pass; `qa-reviewer` reports nothing unresolved.

## Risks & Mitigations

| Risk                                                                        | Impact | Probability | Mitigation                                             |
| --------------------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------ |
| Signal signature change: a new extension with an old client, or the reverse | H      | M           | One deploy changes both; logout before acceptance      |
| Reconciliation stops a recorder the owner wanted                            | M      | L           | It only stops a recorder no panel owns, and reports it |
| Article mode and Claude modes (Ctrl/Alt+Insert) share the paths             | M      | M           | The state-module tests cover every launch mode         |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00171-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed with the root cause (RESEARCH-race.md)
