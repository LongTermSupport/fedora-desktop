# Plan 00171: Speech-to-text: Insert is never ignored, the icon is always in sync

**Status**: Not Started (decision gate: Phase 1, owner decisions D1 to D3)
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
- **Orange dots (finishing) + Insert → the icon changes to a "blocked" sign.** The press
  is acknowledged, not swallowed.
- **The icon always matches what is actually happening.** It must never be the
  idle mic while a microphone is recording, or red while nothing is recording.

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

- [ ] ⬜ **Task 1.1 — D1: Insert in the orange STOPPING state.** Today the orange dots mean
  two different things. STOPPING is the grace period: the mic is still open, and Insert
  means "stop now" (Plan 00148 Task 0.6, the owner's earlier request). TRANSCRIBING
  means the mic is closed and the text is being finished; there, Insert does nothing
  visible. Options: (a) the blocked sign in TRANSCRIBING only, with STOPPING keeping
  stop-now; (b) the blocked sign in both, dropping stop-now. Recommended: (a). The
  blocked sign then means "the mic is closed, wait".
- [ ] ⬜ **Task 1.2 — D2: What a blocked press leads to.** (a) The blocked sign only: the
  press is shown, and the owner presses again once the icon is blue. (b) The blocked
  sign, and a recording starts by itself the moment the previous paste is done
  (queued). Option (b) is closer to "never ignored", but the new recording would start
  with its window pinned at the press, not at the start.
- [ ] ⬜ **Task 1.3 — D3: The blocked icon.** Candidates that ship in Adwaita:
  `action-unavailable-symbolic` (a no-entry circle, the closest to a traffic "no entry"
  sign) or `process-stop-symbolic`. Shown how long: about 1.5 s, then back to the
  state's own icon.
- [ ] ⬜ **Task 1.4**: Record the answers under Technical Decisions.

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
  its full display time. The client sends RECORDING only after START is accepted.
- [ ] ⬜ **Task 3.2**: Orange + Insert as D1 and D3 decided (the blocked sign).
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

Pending Phase 1.

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
