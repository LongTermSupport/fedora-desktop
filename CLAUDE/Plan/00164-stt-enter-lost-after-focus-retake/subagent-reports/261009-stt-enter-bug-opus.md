# Plan 00164: findings on the Enter lost after a focus retake

Sub-agent report. The fix and the plan scripts are in commit e8b93da5.

## The report

When the dictation's window has lost focus by paste time, the panel takes focus back and
the text is pasted into the right window, but the Enter does not send it.

## What the code shows

- `pasteTarget.js` `PasteTargetPin.answer()` judges "focused" from
  `global.display.focus_window`. When focus has moved it calls `Main.activateWindow()`
  and answers "not focused"; the recorder asks again every 0.1 s.
- Mutter sets the focus window the moment it activates it, before the client has handled
  keyboard enter. A window on another workspace is mid-animation at that point. So the
  first "focused" answer can come one poll (0.1 s) after the activation.
- Until this plan the recorders pasted on that first "focused" answer, and sent the Enter
  on the usual fixed timing: `wsi` 0.3 s after the paste, `wsi-stream`
  `min(0.3 + 0.002 × length, 1.0)` s. The Enter path is the same whether or not focus
  was retaken.
- The Claude modes force `--no-auto-enter` by design (`extension.js`, Claude launch), so
  a missing Enter there is expected, not this bug.

## Hypotheses (to be confirmed from the host's debug log, Task 1.2)

- **H1 (leading)**: the app is still handling its focus-in when the keys arrive. A
  terminal sends its app a focus-in sequence and the app redraws; the paste is read from
  the clipboard asynchronously. The Enter lands too close to, or before, the pasted text,
  and a TUI that batches input reads it as part of the paste.
- **H2**: focus moves away again after the retake, so the Enter goes to another window.
  The settle below catches a flip within 0.5 s; a later flip would not be caught.
- **H3**: the recorder ran with `--no-auto-enter`. Ruled out for the plain mode by the
  code; the triage prints the launch flags and the `auto-enter` setting to confirm.

## The fix

Only the recorders change; the extension is untouched, so no logout is needed. After a
focus retake only, both `wsi` and `wsi-stream`:

- paste once the panel has answered "focused" for 0.5 s of 0.1 s polls in a row. A
  renewed loss restarts the settle; the wait for focus itself stays capped at 2 s of
  "not focused" answers;
- wait 1.0 s between the paste and the Enter;
- log both steps to the debug log.

A window that kept focus is pasted with the old timing.

Names: `wsi-stream` has `PASTE_FOCUS_SETTLE_SECONDS` and
`PASTE_ENTER_DELAY_AFTER_REFOCUS_SECONDS`, and `paste_target_now` returns
`(with_shift, save_after, refocused)`. `wsi` has `PASTE_FOCUS_SETTLE_POLLS=5`,
`PASTE_ENTER_DELAY_AFTER_REFOCUS=1.0` and `PASTE_REFOCUSED`.

Limits: the settle rests on a real signal, focus re-checked at every poll. The longer
wait before the Enter is a bounded delay; there is no signal for "the paste has landed in
the app".

## Tests (written first, seen failing before the fix)

- `tests/speech_to_text/test_paste_target.py`: the settle, its restart, the 1 s Enter
  wait, and no added wait when focus was kept. All pass.
- `scripts/test-wsi-stop-grace.bash`, against the real `wsi` with a stub panel: 3 + 5
  asks for a retake, 4 + 1 + 5 for a restart, the "focus was given back" log line, and no
  settle when focus was kept. All checks pass, including the `tests/speech_to_text` suite
  it runs.
- shellcheck: no warnings or errors on the changed and new scripts. ruff check: clean.
  `qa-all.bash` was not run (the coordinator runs it).

## Triage

`triage.bash` (legs in `probe.bash`) refuses to run in the container. On the host it
reads the speech-to-text debug log and its rotated copy and prints:

- the settings that decide the paste and its Enter, read through `wsi-setting`;
- whether the deployed `wsi` and `wsi-stream` match the checkout;
- the launch flags and the focus, PasteKey, paste, Enter and save lines of the last 5
  dictations with a focus loss, and of the last 2 without, for comparison.

It never copies dictated text. The panel's lines have millisecond timestamps; the
recorders' have seconds, and they are written only with Debug Logging on. The log
parsing was checked against a synthetic log in the container.

## Host run

`meta-deploy.bash` entry: `00164-stt-enter-lost-after-focus-retake`. It runs triage,
then `deploy.bash` (`play-speech-to-text.yml`, which ships both recorders and restarts
the warm speech server), then triage again. Then: Debug Logging on, dictate, click
another window before stopping, in batch and streaming mode, and run triage once more.
