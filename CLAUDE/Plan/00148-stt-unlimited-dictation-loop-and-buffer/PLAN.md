# Plan 00148: speech-to-text: unlimited dictation by loop and buffer

**Status**: In Progress (Phase 0 built, host check Task 0.5 pending; Phases 2-5 wait on the Task 1.1 decision)
**Created**: 2026-10-02
**Owner**: joseph
**Priority**: Medium

## Overview

Streaming dictation stops at 120 seconds. Nothing was measured to arrive at that number: it
was set to four times the earlier 30 s cap "since transcription is real-time", and a 125 s
server watchdog and article mode's flush interval were later built on it. The pasted text
is not in fact produced in real time, and simply raising the cap makes the stop latency,
silent fallbacks and server-mode truncation worse as recordings get longer.

This plan builds continuous dictation once, in the warm server (`wsi-stream-server`), as a
loop and a buffer. faster-whisper's bundled Silero VAD (already installed, no new
dependency) cuts the audio into segments of at most 28 s at natural pauses, so every
segment is a single Whisper window. One worker transcribes them in order with the
user-selected model. The text builds up in memory and in a journal file in
`$XDG_RUNTIME_DIR`, and is pasted once at stop. If any segment fails, the session stops
loudly and copies the text so far to the clipboard; no segment is ever silently dropped. The
fixed 125 s watchdog is replaced by a client heartbeat, a no-speech auto-stop and a large
configurable cap.

The same research found several unrelated defects, recorded here as their own tasks.
Lineage of the 120, failure analysis, the full pipeline and the IaC file list:
[RESEARCH-120s-limit.md](RESEARCH-120s-limit.md).

## Goals

- A dictation runs until the user stops it (or a safety stop fires), with no 120 s cap.
- Every segment's text reaches the result, in order, or the session fails loudly with the
  text so far on the clipboard and the failed audio kept.
- A stuck or forgotten microphone is still stopped, by heartbeat loss, no-speech timeout or
  the absolute cap.
- The recording limits live in one place.

## Non-Goals

- Typing text into the focused window during recording (paste once at stop).
- Changing batch `wsi` (30 s) or standard/pre-buffer streaming beyond pointing their help
  and docs at continuous mode.
- Moving article mode onto the server session in this plan's first delivery (Phase 5 is a
  follow-on inside the plan, not a prerequisite).

## Tasks

### Phase 0: Delayed stop keeps the last words (independent of Task 1.1)

Pressing Insert to stop often loses the last word or two: `extension.js` `_stopRecording()`
sends SIGTERM to the recorder's PID and each recorder stops capturing at once (`wsi` traps
TERM into `stop_recording`; `wsi-stream` has SIGTERM handlers for server and local mode).
Owner's fix: recording continues for a grace period after the press (default 3 s), then
stops. It lives in the recorders' TERM handlers, so it needs no logout.

- [x] ✅ **Task 0.1**: Per mode (`wsi`, `wsi-stream` local, `wsi-stream` server), trace
  SIGTERM to final text and find where audio or words are dropped
  (findings: [RESEARCH-stop-path.md](RESEARCH-stop-path.md)). If a mode also discards
  buffered audio at stop, fix that too. Pre-buffer mode had no TERM handler at all, and
  pre-buffer and server mode dropped the `pw-record` pipe and the last realtime pass;
  all fixed.
- [x] ✅ **Task 0.2**: One grace setting (default 3 s; 0 = immediate), read by every
  recorder from a single source: GSettings `stop-grace-seconds`, read via `wsi-stop-grace`.
- [x] ✅ **Task 0.3**: First TERM: keep recording for the grace, then stop as today. A
  second TERM during the grace stops at once. SIGUSR1 (Escape, abort) stays immediate.
  The pending stop is a desktop notification; no extension change. In pre-buffer mode
  this holds during the model load too: the microphone closes when the stop is due, and
  the captured audio is transcribed once the model is ready.
- [x] ✅ **Task 0.4**: Tests where the logic is testable outside GNOME
  (`scripts/test-wsi-stop-grace.bash`, `tests/speech_to_text/`, gated in `qa-all.bash`:
  batch `wsi`, pre-buffer mode and the server's stop order end to end with stubs;
  standard streaming and the server-mode client loop only through their shared units);
  docs. Targeted QA done; the full `qa-all.bash` and `qa-reviewer` run by the coordinator.
- [ ] ⬜ **Task 0.5**: **HOST**: deploy `play-speech-to-text.yml`; press Insert right on
  the last word in each mode; the word is in the pasted text.

### Phase 1: Decision and measurements

- [ ] 🚫 **Task 1.1**: Owner decision: loop-and-buffer or a raised cap. Options: (a)
  loop-and-buffer in the warm server as above; (b) raise the cap (e.g. to 300 s) in all six
  places, grow the stop-wait budget and move the watchdog in the same commit. Recommendation:
  (a); (b) worsens stop latency, the silent fallback to `tiny` text and server-mode
  truncation, all of which grow with length (research section 2). Blocked on the owner.
- [ ] ⬜ **Task 1.2**: `triage.bash` (HOST, read-only) for the research's section 3.6
  probes: installed RealtimeSTT and faster-whisper versions, real-time factor of the chosen
  model per 20 s segment, hard-cut frequency at 20 s soft / 28 s hard max, and word loss at
  phrase boundaries in article mode.

### Phase 2: Continuous dictation in the server

- [ ] ⬜ **Task 2.1**: Tests first: unit tests under `tests/` for the segmenter as a pure
  function on synthetic arrays (silence cut, soft max, hard cut at the energy minimum, pure
  silence never queued) and for ordered commit.
- [ ] ⬜ **Task 2.2**: `wsi-stream-server`: VAD segmenter, one ordered transcription worker
  (main model, `condition_on_previous_text=False`, a short `initial_prompt` from committed
  text), in-memory buffer plus append-only JSONL journal in `$XDG_RUNTIME_DIR`, commands
  `START {continuous:true}`, `PROGRESS`, `KEEPALIVE`, `ABORT`, and a STOP that drains under a
  backlog-scaled deadline. Any failure (segment error, `pw-record` exit, backlog ceiling,
  drain deadline, journal write) marks the session FAILED, keeps the audio, copies the text
  so far.
- [ ] ⬜ **Task 2.3**: Replace `WATCHDOG_TIMEOUT = 125` with the heartbeat (stop after 15 s
  without `KEEPALIVE`), the no-speech auto-stop, and the large configurable absolute cap.
- [ ] ⬜ **Task 2.4**: `wsi-stream` server-mode client: no fixed `--timeout`, sends
  keepalives, relays progress, exits non-zero on FAILED with the partial text on the
  clipboard. Diagnostics to stderr (`CLAUDE/StderrHygiene.md`).

### Phase 3: Panel extension and settings

- [ ] ⬜ **Task 3.1**: GSettings keys `continuous-dictation` (default off until verified),
  `max-recording-minutes`, `silence-autostop-seconds`, with `prefs.js` controls.
- [ ] ⬜ **Task 3.2**: `extension.js`: elapsed time and backlog instead of the 117/27
  countdown (a countdown only in the absolute cap's last minute), limits read from
  GSettings, the `117`/`120` literals removed. ESLint green.

### Phase 4: Side findings

- [ ] ⬜ **Task 4.1**: Server mode pastes the `tiny` realtime-preview model's text whatever
  model is selected (`wsi-stream-server` never calls `text()`). Continuous mode uses the
  main model; fix or retire the old server-mode path so it cannot paste preview text.
- [ ] ⬜ **Task 4.2**: Remove the silent fallback to buffered `tiny` text in standard
  streaming (`wsi-stream`, `run_standard_streaming`), or make it a loud warning.
- [ ] ⬜ **Task 4.3**: Plan/code drift: completed Plan 015 says a `Shift+Insert` article-mode
  binding shipped; no such binding exists (article mode is menu-only). Correct the record
  without rewriting the completed plan's history.
- [ ] ⬜ **Task 4.4**: Docs drift: `docs/features/speech-to-text.md` says 30 s only; the
  `streaming-mode` schema description claims it auto-stops on silence, which it does not.
  Document the real per-mode limits and continuous mode.
- [ ] ⬜ **Task 4.5**: The 120 is held in six places (`extension.js` twice, `wsi-stream`,
  `wsi-stream-server`, `wsi`, `wsi-article`) with nothing keeping them in step. After Phase 3
  each limit has one source, and a QA check fails if a literal copy reappears.
- [ ] ⬜ **Task 4.6**: Pin `RealtimeSTT` and `faster-whisper` in
  `play-speech-to-text.yml`'s existing pip task, to the versions Task 1.2 finds; fix the
  play's stale header comment on the default model.

### Phase 5: Article mode on the server session

- [ ] ⬜ **Task 5.1**: Point `wsi-article` at the server session and journal; remove its own
  loop, its swallowed exceptions and the window's 3 s SIGKILL on Stop.

### Phase 6: Verification

- [ ] ⬜ **Task 6.1**: `deploy.bash` and `acceptance.bash` in this folder;
  `./scripts/qa-all.bash` green; `qa-reviewer` agent over the full diff.
- [ ] 🚫 **Task 6.2**: **HOST**: run `deploy.bash` and `acceptance.bash`, log out and in for
  the extension, and dictate past five minutes with a forced segment failure. Blocked on the
  owner: Ansible never runs in the ccy container, and dictation needs a person.

## Success Criteria

- [ ] Pressing Insert on the last word keeps that word, in every mode; a second Insert
  during the grace stops at once; Escape still aborts at once.
- [ ] A dictation of several minutes pastes complete, ordered text once at stop.
- [ ] A forced segment failure stops the session, copies the text so far, keeps the audio,
  and says so.
- [ ] Heartbeat loss and long silence each stop the microphone.
- [ ] No recording limit exists as more than one literal.
- [ ] RealtimeSTT and faster-whisper are pinned.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00148-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- (none yet)
