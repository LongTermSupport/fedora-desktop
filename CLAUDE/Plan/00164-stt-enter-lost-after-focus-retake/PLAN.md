# Plan 00164: stt enter lost after focus retake

**Status**: In Progress
**Created**: 2026-10-09
**Owner**: joseph
**Priority**: Medium

## Overview

The owner reported: when the window a dictation started in has lost focus by the time the
text is pasted, the panel takes focus back and the text is pasted into the right window,
but the Enter that should send it does not take effect. The message sits unsent.

The paste goes to the window pinned at Insert (Plan 00148 Task 9.10). Before each paste
the recorder (`wsi` or `wsi-stream`) asks the panel's `PasteKey`; if focus has moved,
`PasteTargetPin.answer()` calls `Main.activateWindow()` and answers "not focused", and
the recorder asks again every 0.1 s. Until this plan, the first "focused" answer released
the paste, and the Enter followed on the same fixed timing as any other paste.

This plan makes a window that was given focus back keep it for half a second before the
paste, re-asked at every poll, waits longer between that paste and its Enter, and asks
again just before the Enter (Task 2.4). Both recorders behave the same. A read-only `triage.bash` pulls the focus / paste / Enter
timelines out of the host's debug log, to confirm the cause from real dictations.

## What the code shows (facts)

- The Enter is sent unconditionally on the same path whether or not focus was retaken:
  `wsi` sleeps 0.3 s after the paste, `wsi-stream` `min(0.3 + 0.002 × length, 1.0)` s.
- `answer()` judges "focused" from `global.display.focus_window`. Mutter sets that the
  moment it activates the window, before the client has handled `wl_keyboard.enter`
  (and, for a window on another workspace, during the workspace-switch animation). So
  the first "focused" answer comes as early as one poll (0.1 s) after the activation.
- The Claude modes force `--no-auto-enter` by design (`extension.js`, Claude launch), so
  a missing Enter there is expected. The report is about the plain mode.

## Hypotheses (host log read, Task 1.2; none confirmed until Phase 3)

- **H1** (weakened): the app is still taking focus when the keys arrive. A terminal sends
  its app a focus-in sequence and the app redraws; the paste is read from the clipboard
  asynchronously. The Enter then reaches the app too close to (or before) the pasted text,
  and a TUI that batches input treats it as part of the paste: pasted, not sent. The host
  log showed the Enter sent 1–2 s after the paste in failing and working dictations alike.
- **H2** (leading): focus moves again just after the panel takes it back, so the Enter
  goes to another window. The settle catches a flip before the paste; Task 2.4's ask
  just before the Enter catches, gives back and logs a flip after it.
- **H3** (ruled out by the log: the Enter was sent): the recorder was run with
  `--no-auto-enter` (Claude mode or auto-enter off).

## Goals

- After a focus retake the text is pasted into the pinned window and sent by the Enter.
- `wsi` and `wsi-stream` behave the same.
- The debug log says when focus was given back, when it settled, and how long the Enter
  waited, so a recurrence can be read from the log.

## Non-Goals

- Changing the extension: the fix is in the recorders, which reload without a logout.
- Changing the timing of a paste whose window kept focus.

## Tasks

### Phase 1: Triage

- [x] ✅ **Task 1.1**: `triage.bash` (legs in `probe.bash`): the extension settings that
  decide the paste and its Enter, whether the deployed recorders are this checkout's, and
  the focus / paste / Enter lines of the last dictations with a focus loss, and of the
  last two without. Dictated text is never copied into the report.
- [x] ✅ **Task 1.2**: Run on the host (through `meta-deploy.bash`, which runs triage
  before and after the deploy) and record in the journal which hypothesis the log bears
  out. With Debug Logging off the recorders write nothing; turn it on and reproduce.
  Journal 15:23: H1 weakened, H2 leading (Task 2.4).

### Phase 2: Fix (tests first)

- [x] ✅ **Task 2.1**: Tests: `tests/speech_to_text/test_paste_target.py` (the settle,
  its restart when focus is lost again, the longer wait before the Enter) and
  `scripts/test-wsi-stop-grace.bash` (the same against the real `wsi` with a stub panel).
- [x] ✅ **Task 2.2**: `wsi-stream`: `paste_target_now` returns `refocused`; after a
  retake the window must answer "focused" for `PASTE_FOCUS_SETTLE_SECONDS` (0.5 s) of
  polls in a row, a renewed loss restarting the settle; `auto_paste` then waits
  `PASTE_ENTER_DELAY_AFTER_REFOCUS_SECONDS` (1.0 s) before the Enter. The wait for focus
  itself stays bounded at 2 s of "not focused" answers.
- [x] ✅ **Task 2.3**: `wsi`: the same, `PASTE_FOCUS_SETTLE_POLLS` (5) and
  `PASTE_ENTER_DELAY_AFTER_REFOCUS` (1.0 s), `PASTE_REFOCUSED` set by `paste_target_now`.
- [x] ✅ **Task 2.4** (H2): after a paste that followed a retake, both recorders ask
  `PasteKey` again just before the Enter (`paste_target_now(..., for_enter=True)` /
  `paste_target_now enter`, after the 1 s wait) and log the answer; focus lost again is
  given back and settles as before the paste. A window closed or never given focus back:
  no Enter and no save, a notification that stays says "pasted but not sent", exit 1. A
  window that kept focus throughout is not asked again. Tests in the Task 2.1 files;
  host-verified only by Phase 3.

### Phase 3: Verify on the host

- [ ] ⬜ **Task 3.1**: Deploy (`deploy.bash`, through `meta-deploy.bash`). With Debug
  Logging on, dictate into a terminal app and into a GUI app, click another window (and,
  once, switch workspace) before stopping; the text is pasted into the first window and
  sent. Both batch (`wsi`) and streaming (`wsi-stream`) mode.
- [ ] ⬜ **Task 3.2**: `triage.bash` after it shows, for those dictations, "has focus
  back", "before the Enter" and the Enter lines in that order.
- [x] ✅ **Task 3.3**: `qa-reviewer` over the plan's commits: FIX-BEFORE-MERGE (the feature
  doc did not describe the behaviour after a focus retake), stale plan text, two NITs; all
  fixed. [report](subagent-reports/261009-qa-reviewer-opus.md).

## Success Criteria

- [ ] A dictation whose window lost focus is pasted into it and sent, in both recorders.
- [ ] A dictation whose window kept focus is pasted and sent with no added wait.
- [ ] The unit and end-to-end tests above pass.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00164-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Fix and triage: the commit that adds this plan's scripts.
