# Plan 00171 closeout: probe gap, review, #96 evidence

## 1. Probe gap (the "orange dots" press NOT ESTABLISHED)

**The panel debug log cannot be read from the container.** Only the checkout is mounted
here, so the desktop user's `~/.local/share/speech-to-text/debug.log` is out of reach and
no log line from the replay can be quoted. The run log
(`untracked/plan-runs/00171-…/acceptance/20261013-185344/`) shows only the probe's verdicts:

```
PASS: a press during A's grace did nothing (2 press(es); BLOCKED shown for each: True)
NOT ESTABLISHED: a press while A was transcribing or pasting did nothing (no such press: press Insert then)
```

The three earlier runs (16:08, 16:48, 18:49) were all `NOT PLAYED`.

**Verdict: none of (a), (b) or (c) as stated; the instruction was ambiguous. (b) is the
nearest, but the cause is the prompt, not the classifier.**

- `panelLook.js` gives STOPPING and TRANSCRIBING **the same look**:
  `STOPPING: {icon: LOADING, colour: COLOURS.amber}` and
  `TRANSCRIBING: {icon: LOADING, colour: COLOURS.amber}`. The orange dots therefore appear
  at the first (stop) press and stay through the whole 3 s grace
  (`stop-grace-seconds` default 3), and then through transcribing.
- The probe told the owner "press it again during the grace and again while the icon
  shows the orange dots". The grace *is* orange dots, so the owner's two "orange dots"
  presses most likely fell in STOPPING and were counted, correctly, as grace presses
  (the 2 grace presses above).
- The classifier itself is right, and a test now pins it: a block logged as
  `TRANSCRIBING: <pid> is stopping…` or `SUCCESS and <pid> has exited…` counts as
  "transcribing or pasting". `wsi-stream` emits TRANSCRIBING after the grace (server mode
  line ~1590, local mode ~1924), and `_block` logs every ignored press, so this is not a
  logging gap (c).
- (a) is unlikely: the replay the probe picks is the latest stopped dictation with a
  blocked press after it, and the grace presses sit in that same dictation.

**Fix (probe only, no extension change, no logout needed), test first:**
`test_probe_insert_sequence.py` (6 tests, all pass; ruff check clean):

- every press that did nothing behind A is listed as `HH:MM:SS.mmm STATE`, so the next
  run shows which state each press hit;
- the missing-press hint now says "the grace shows the orange dots too; after the grace
  keep pressing Insert until the text is pasted", and the NOT PLAYED text says the same.

Commit `29cd4478`.

**The newest replay** (a dictation of about 15 s, presses on the orange dots): not readable from
here either. Re-running `acceptance.bash` on the host prints the new per-press state
list, which answers whether those presses hit TRANSCRIBING.

## 3. Issue #96 (Insert within 1 s of a paste)

The current evidence cannot answer it: the 18:53 report does not give the time of B's press
relative to A's paste, and the log is not readable here. The probe now reports it
(commit `5a8f6d68`, tests first):
`The press that started B came <s> s after A's SUCCESS (within 1 s of the paste: yes|no)`.
The next host acceptance run gives the number. If it says yes, with the PASS lines
"nothing showed the ready icon while B recorded" and "B's own signals dropped: 0", the
draft comment for #96 is:

> Replayed on the host with the Plan 00171 build. A dictation was stopped and its text
> pasted; Insert was pressed again <gap> s after the paste (within 1 s). That press
> started a new dictation, which reached RECORDING and was stopped by the next press.
> While it recorded, the panel took no IDLE, the icon never showed the ready state, and
> none of the new recorder's signals were dropped: <N> late signal(s) from the previous
> recorder were dropped by its session token instead of overwriting the new state. The
> stale-IDLE case is also covered by `tests/extensions/test-stt-panel-session.mjs`. This
> looks fixed; leaving it open for the owner to close.

If the gap is 1 s or more, the replay needs one more press made straight after the paste.
