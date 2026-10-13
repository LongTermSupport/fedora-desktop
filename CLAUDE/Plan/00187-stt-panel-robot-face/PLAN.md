# Plan 00187: speech-to-text panel shows a robot face while Claude post-processes

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #100.

## Overview

With Ctrl+Insert or Alt+Insert, Claude post-processes the transcript, and the panel icon
stays on the amber "loading" look for the whole Claude step (#100). Nothing shows that the
transcript is done and Claude is now working on it. `wsi` emits `TRANSCRIBING` before Whisper
runs and nothing more until `SUCCESS`/`ERROR`, and `wsi-claude-process` runs inside that
state. The streaming client `wsi-stream` has its own Claude step (`ClaudeProcessingFailed`,
`CLAUDE_TIMEOUT_SECONDS`), so it needs the same state.

The owner decided the look in the issue's comment: the emoji, 🤖 for Ctrl+Insert and 💬 for
Alt+Insert, not a custom SVG. This matches the recording countdown, which already shows them
for the Claude modes (`_countdownText` in `extension.js`). This is display only: the Claude
step itself does not change.

Plan 00171 owns the panel's state table (`panelSession.js`, `panelLook.js`) and is in host
acceptance. This plan's state is added after 00171 deploys, so the two do not change the
same tables in the same deploy.

## Goals

- A `CLAUDE_PROCESSING` state, emitted by `wsi` and `wsi-stream` right before the Claude
  step, token-tagged like every other signal.
- The panel shows 🤖 for Ctrl+Insert and 💬 for Alt+Insert during that state.
- `panelSession.js` and `isKnownState` know the state; Insert during it behaves as during
  rendering (Plan 00171 Decision 8).

## Non-Goals

- A custom SVG icon (owner decision).
- Any change to what the Claude step does.

## Tasks

### Phase 1: Build (tests first)

- [ ] ⬜ **Task 1.1**: Extend the extension's state tests (`test-stt-panel-session.mjs`,
  `test-stt-panel-look.mjs`) for `CLAUDE_PROCESSING` in both styles; red first.
- [ ] ⬜ **Task 1.2**: `wsi` and `wsi-stream` emit the state before the Claude step; Python
  and bash tests show it is sent and token-tagged.
- [ ] ⬜ **Task 1.3**: `panelLook.js`, `panelSession.js`, `isKnownState`; the extension's
  version bump; ESLint.

### Phase 2: Ship

- [ ] ⬜ **Task 2.1**: `deploy.bash` (`play-speech-to-text.yml`), `acceptance.bash` reading
  `Display: CLAUDE_PROCESSING` from the debug log, an entry in `meta-deploy.bash` (two
  rounds: an extension change needs a logout). The emoji's look is NOT ESTABLISHABLE by
  script and is listed for the owner.
- [ ] ⬜ **Task 2.2**: Docs in `docs/features/speech-to-text.md`; `qa-all.bash`; the
  `qa-reviewer` agent.

## Success Criteria

- [ ] During a Ctrl+Insert dictation's Claude step the panel shows 🤖, and 💬 for
  Alt+Insert, in both batch and streaming.
- [ ] An Insert during the state behaves as during rendering.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00187-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #100 and the owner's emoji decision.
