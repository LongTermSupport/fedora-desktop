# Plan 00208: stt paste uses stale clipboard

**Status**: In Progress
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: High

## Overview

A dictation was transcribed in full, but the auto-paste pasted the previous dictation's text.
"Copy Last Transcription" in the panel menu then gave the right text, so recording and
transcription were fine; the paste ran while the clipboard still held the old text.

Host evidence (read-only, anonymised) proved the mechanism on the streaming path
(`wsi-stream`): the dictation's `wl-copy` never got the selection from the compositor and was
still running, stdin on the pipe, long after. `copy_to_clipboard` started it, slept 0.1 s,
failed only if it had already exited non-zero, read the clipboard back with `wl-paste` and
logged only the LENGTH (785 characters: the previous 784-character dictation and a newline,
against 884 expected). `auto_paste` then slept 0.3 s and pressed Ctrl+Shift+V. Fixed sleeps
were the only ordering between the copy and the paste key. The batch recorder (`wsi`, Go) has
the same class: `wl-copy` with no deadline, then a fixed 0.05 s sleep before the keys.

The fix makes the read-back the only gate. The paste key is pressed only after the selection
reads back as this dictation's text (one trailing newline allowed), within a hard deadline;
otherwise nothing is pasted, a notification that stays says so and names "Copy Last
Transcription", and the run exits non-zero.

## Goals

- No paste key is pressed until the selection reads back as this dictation's text
- A copy that never takes is a loud failure that pastes nothing, never a stale paste
- No copy process this tool started and gave up on is left running holding its text (a real
  wl-copy forks once it has the selection; that detached server child is legitimate and stays)
- A copy that cannot start at all (wl-copy missing, exec failure) fails at once, with no retry

## Non-Goals

- Porting `wsi-stream` to Go (follow-up, Task 2.1): this fix stays surgical in Python
- The extension's own "Copy Last Transcription" copy (a manual action the owner sees)

## Tasks

### Phase 1: Gate the paste on a verified read-back

- [x] ✅ **Task 1.1**: Prove the mechanism. Host evidence (above); a one-off run of the old
  `auto_paste` against a fake `wl-copy` that never takes pressed Ctrl+Shift+V with the
  previous text on the clipboard and returned success; the new Go test
  `TestW32ASelectionThatNeverTakesTheTextIsNeverPasted` failed on the old `wsi` the same way
  (exit 0, keys pressed)
- [x] ✅ **Task 1.2**: `wsi-stream`: `copy_confirmed` reads the selection back with
  `wl-paste -n` until it holds the text; a copy that has not taken in 1 s is killed by its
  process handle and copied again; past 3 s, `ClipboardNotTaken`, with every copy this run
  started killed; a copy that cannot start raises at once (review B1: it had busy-looped).
  `auto_paste` presses nothing before it; `paste_and_report` reports it (an urgent
  notification, shown even with `--no-notify`; ERROR; exit 1); a chunk not taken is not
  counted. Tests:
  `tests/speech_to_text/test_clipboard_confirm.py`, through fake `wl-copy`/`wl-paste`
  executables that take at once, late, never, or on the second copy
- [x] ✅ **Task 1.3**: `wsi` (Go): the auto-paste copy and the read-back share a 3 s deadline;
  the read-back (`wl-paste -n [-p]`, `xsel -o[bp]`) replaces the fixed 0.05 s sleep (spec W32
  in `00192-.../SPEC-wsi-watermark.md`; tests in `src-go/internal/wsi/paste_test.go`). No
  recopy in Go, unlike Python: a copy that has not taken by the deadline is a hard failure
- [ ] ⬜ **Task 1.4**: Deploy through meta-deploy (`play-speech-to-text.yml`) and confirm on
  the host: the next dictations log `Clipboard copy confirmed`, and no `wl-copy` started by
  `wsi-stream` stays running after a dictation

### Phase 2: Follow-up

- [ ] ⬜ **Task 2.1**: Port `wsi-stream` to Go when it is next reworked (CLAUDE.md "Tooling Is
  Go"); the read-back gate moves with it, sharing `wsi`'s

## Success Criteria

- [x] A selection that never takes the text pastes nothing and fails loudly (both recorders, tested)
- [x] A selection that takes late is waited for, and pasted once it holds the text (tested)
- [ ] On the host, dictations paste their own text, and a failure is reported, not pasted stale

## Delivery & Milestones

- Phase 1 code: branch `fix/stt-paste-stale-clipboard`
