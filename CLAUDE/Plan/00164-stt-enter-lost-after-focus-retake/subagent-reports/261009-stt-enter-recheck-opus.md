# Plan 00164: re-check focus before the Enter after a retake (Task 2.4)

## What changed

- `files/home/.local/bin/wsi-stream`: `paste_target_now(..., for_enter=True)` reuses the
  paste's ask-and-settle loop with Enter-specific log lines. `auto_paste`, after a paste
  that followed a retake, waits the 1 s, then asks again; `PasteTargetUnavailable` there
  becomes `EnterNotSent`, which `paste_and_report` turns into `report_unsent` (ERROR
  state, a notification that stays saying "pasted but not sent", exit 1). No Enter and
  no Ctrl+S go to another window. A window that kept focus is not asked again.
- `files/home/.local/bin/wsi`: `paste_target_now enter` sets only `PASTE_TARGET` (the
  paste's key and save choice are kept, and a panel that cannot answer changes nothing),
  with the same log lines. On closed / unfocused: `log_error`, a notification that
  stays, exit 1, no Enter, no save. `paste_target_why` replaces the duplicated
  closed/unfocused wording.
- The WIP patch was applied and kept. Its `wsi` side was only a signature stub, so
  the `wsi` behaviour above is new. One `wsi-stream` log line was reworded to match `wsi`.

## Decision: the 1 s Enter delay stays

H1 is weakened but not ruled out. The log records when the keys were sent, not when the
terminal read the clipboard and drew the text. The wait only applies after a retake.
The new ask comes after the wait, so focus is judged when the Enter goes.

## Tests

- `scripts/test-wsi-stop-grace.bash`: 110 checks passed, 0 failed. This includes the
  new stub-panel cases: focus kept to the Enter (one extra ask, Enter sent, logged),
  focus lost after the paste and given back (Enter sent after the settle, logged), and
  window closed after the paste (Ctrl+V only, a notification that stays, ERROR, exit 1).
  The existing ask counts went up by one for the extra ask. The suite's unit-test step
  ran 257 tests and they passed.
- `tests/speech_to_text/test_paste_target.py`: 34 tests, OK.
- `shellcheck` on `wsi`: the same 7 info-level findings as HEAD and nothing new. On the
  test script: clean.
- `ruff check` on `wsi-stream` and the test: passed. `ruff format --check` reports
  formatting differences across both whole files. Most of them are in lines this change
  did not touch.

## Not done

- Host verification (Phase 3) and the qa-reviewer pass.
