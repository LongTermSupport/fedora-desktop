# Research: the stop path, SIGTERM to pasted text (Task 0.1)

Plan 00148 Phase 0. The question: when Insert stops a dictation, where are the last
words lost, in each recorder mode? Line numbers below refer to the code **before** the
Phase 0 change (the parent of the Phase 0 commit) unless marked "now".

RealtimeSTT references are to **1.1.2**, the current release, read from its wheel. The
version installed on the host is not known from the container; Task 1.2 probes it.

## Common entry: the extension

- `extension.js:817-823`: Insert while the state is `RECORDING`, `PREPARING` or
  `TRANSCRIBING` calls `_stopRecording()` instead of starting a recording.
- `extension.js:1096-1119` `_stopRecording()`: reads
  `/dev/shm/stt-recording-<user>.pid` and runs `kill <pid>` (SIGTERM). The stop is
  immediate in every mode; nothing waits for the speaker to finish the word.
- `extension.js:1121-1157` `_abortRecording()` (Escape): only acts while the state is
  exactly `RECORDING` (`:1123`). Server mode gets SIGUSR1 (`:1135-1139`); every other
  mode is SIGKILLed by `pkill` (`:1143`).
- `extension.js:647, 694-697`: the countdown starts at 117 s (streaming) or 27 s (batch)
  and calls `_stopRecording()` at 0, i.e. it also sends SIGTERM.
- States the extension renders (`_updateIconState`, `extension.js:557-628`): `PREPARING`,
  `RECORDING`, `TRANSCRIBING`, `SUCCESS`, `ERROR`, `IDLE`. None means "stop pending".
  Re-emitting `RECORDING` restarts the countdown (`:576`); `PREPARING` would disable
  Escape (abort requires `RECORDING`); `TRANSCRIBING` removes the Escape binding (`:582`).
  So the pending stop is announced with a desktop notification and the state stays
  `RECORDING`: the countdown keeps running, a second Insert still stops, Escape still
  aborts. No extension change, so no logout.

## Mode 1: `wsi` (batch)

Path: SIGTERM → `trap 'stop_recording' TERM INT` (`wsi:289`) → `stop_recording`
(`wsi:254-270`) sends SIGINT to `pw-record` and polls up to 2 s for it to exit →
`wait $REC_PID` (`wsi:707`) returns → resample (`:737-741`) → pad to 1 s only if shorter
(`:759-763`) → `faster-whisper-transcribe` (`:798`) → clipboard / paste.

Where words go missing:

- **No buffered audio is discarded.** `pw-record` gets SIGINT, not SIGKILL, and is
  waited for; with `--latency 50ms` (`:672-674`) at most ~50 ms is unflushed.
- **The recording ends at the key press.** A speaker presses Insert while the last word
  is still coming out, so the clip is cut inside or right at the end of that word,
  with no trailing silence. Whisper commonly drops or garbles a clipped final word.
  This is the likely cause in batch mode; it is a model behaviour and was not measured
  here (Task 0.5 on the host is the check).

Fix: the grace (below). No discard fix needed.

## Mode 2: `wsi-stream` local, standard

Path: SIGTERM → `handle_signal` (`wsi-stream:676-692`) calls `recorder.stop()` **inside
the signal handler** → the recording thread's `recorder.text()` returns the main model's
transcription → main thread waits at most 5 s for it (`:742-744`) → paste.

Where words go missing:

- **`recorder.stop()` freezes the audio at that instant.** RealtimeSTT 1.1.2
  `core/lifecycle.py:143-148` deep-copies `recorder.frames` and sets
  `is_recording = False`. Microphone audio still in the input queue, not yet moved into
  `frames` by the recording worker, is not part of the recording. Usually tens of ms,
  more when the worker is busy.
- **The clipped final word**, as in batch mode.
- **Fallback to preview text** (not a stop-path loss, recorded for Task 4.2): if the
  final transcription takes over 5 s, the timeout branch (`:746-772`) aborts the recorder
  and the paste falls back to the realtime preview (`:811-816`).

Fix: the grace, and `recorder.stop()` moved out of the handler into the main loop.

## Mode 3: `wsi-stream` local, pre-buffer

Path: **none.** `run_prebuffered_streaming` (`wsi-stream:886-1152`) installs no signal
handlers; the only `signal.signal` calls are in server mode (`:460-462`, `:489-491`) and
standard mode (`:691-692`). SIGTERM's default action kills the process at once:
no transcription, no paste, no `IDLE` (atexit does not run on a fatal signal), and the
`pw-record` child lives on until its next write hits the closed pipe. **Every Insert stop
in pre-buffer mode lost the whole dictation.** Found by reading; not reproduced on a host.

Also discarding audio on the stop path, once handlers exist:

- `:1007-1008`: the buffered-audio feed loop broke out on `stop_requested`, dropping
  audio spoken while the model loaded.
- `:1037-1042`: `pw-record` was terminated and its pipe never read again. The pipe holds
  up to 64 KiB, about 2 s of 16 kHz mono s16.
- `:1051-1059`: the final text is `current_text`, the last realtime pass. RealtimeSTT
  publishes realtime text **only while recording** (1.1.2
  `core/realtime_callbacks.py:17-23`), so audio fed after the last pass started is never
  transcribed.

Fixed (now): handlers installed; the buffering loop during the model load also ends
when the stop is due, closing and draining the microphone then and transcribing what
it captured once the model is ready (a first version kept recording until the load
finished, caught in review); every buffered chunk is fed; `read_remaining_audio()`
terminates `pw-record` and feeds the pipe to EOF (a timer SIGKILLs a `pw-record` that
ignores SIGTERM); `wait_for_final_realtime_passes()` waits, bounded at 1.5 s, for two
realtime passes after the last chunk before `recorder.stop()`. Two, because the first
reported pass may already have been in flight; the realtime worker runs one pass at a
time, so the second began after the last chunk.

## Mode 4: `wsi-stream` server mode (client) and `wsi-stream-server`

Path: SIGTERM → client `handle_signal` (`wsi-stream:480-491`) sets `stop_requested` →
wait loop exits (`:517-526`) → `TRANSCRIBING` → `STOP` over the socket (`:534`) →
server `handle_stop_command` (`wsi-stream-server:455-492`) →
`stop_recording_pipeline()` (`:228-314`) → the text in the reply is pasted.

Where words go missing, all in the server:

- **The pipe is thrown away.** Step 1 (`wsi-stream-server:241-243`) set the feeder's
  stop event **before** terminating `pw-record` (`:246-261`). The feeder loop
  (`:175`) checks the event before each read, so it exits without draining the pipe:
  up to ~2 s of the most recent audio is dropped.
- **The last realtime pass is the result, and nothing after it is transcribed.** The
  server never calls `text()`; the reply is `transcription_text`, set by the realtime
  callback (`:92-111`). `recorder.stop()` (`:274-279`) ends realtime publication
  (1.1.2 `core/realtime_callbacks.py:21-23` publishes only while `is_recording`), so the
  2 s "stabilization" poll after it (`:284-301`) can never observe a new update: it only
  ever waits for 0.3 s of no change. The audio between the start of the last pass and
  the stop (the 0.2 s `realtime_processing_pause` plus the pass's own duration) never
  reaches the text.

Fixed (now): `pw-record` is terminated first and the feeder reads to EOF (the stop event
is only a fallback if it has not reached EOF in 3 s); then the server waits, bounded at
1.5 s, for two realtime passes counted from EOF, and only then calls `recorder.stop()`.
The dead post-stop poll is gone. The grace is in the client: `STOP` is sent when it is
due.

Not changed: the text is still the realtime (`tiny`) model's (Task 4.1).

## The delayed stop (Tasks 0.2, 0.3)

- **One source:** GSettings key `stop-grace-seconds` (int, default 3, range 0-30) in the
  extension's own schema. The scripts did not read GSettings before (the extension passes
  everything as flags), and passing a flag would need an `extension.js` change and a
  logout. The schema is compiled into the extension directory by
  `play-speech-to-text.yml` ("Compile GSettings Schema", runs when the XML changes), so
  it is read with `--schemadir`. One reader, `wsi-stop-grace`, prints the value; `wsi`
  and `wsi-stream` call it beside themselves and refuse to record if it fails.
  (`schemas/gschemas.compiled` in git is stale and never deployed, Plan 00049 EXT-09;
  it was not regenerated, as no `glib-compile-schemas` exists in the container.)
- **`wsi`:** the TERM trap (`on_term`) returns at once: the first TERM starts a
  background timer that sends TERM again after the grace; a second TERM (user or timer)
  runs the old `stop_recording`. The main `wait` is now a loop, because a trapped
  signal makes `wait` return while `pw-record` is still capturing. INT stays immediate.
  A trap's bare `return` reports the status of the interrupted `wait` (143), which
  `set -e` turned into an exit before transcription; found by the test, hence
  `return 0`.
- **`wsi-stream` (all three modes):** `GracefulStop` records times only; handlers never
  sleep or block. Each mode's loop polls `due()`. SIGINT and SIGUSR1 (server mode's
  Escape) are `stop_now()`; SIGUSR1 also discards.
- **Visibility:** a desktop notification "Stopping in Ns - finish your sentence.
  Insert: stop now. Escape: discard.", shown for the grace, no state change. With
  notifications turned off only the still-running countdown shows it.
- **Countdown interplay:** the extension's auto-stop at 27 s / 117 s now ends 3 s later,
  at the 30 s / 120 s caps the recorders already enforce, instead of 3 s short of them.

## Not established here

- Whether the clipped-final-word effect (batch, standard) is the dominant loss; Task 0.5
  on the host answers it.
- The installed RealtimeSTT version and whether it publishes realtime text only while
  recording, as 1.1.2 does (Task 1.2). If an older version also published after
  `stop()`, the removed poll would have caught one late update; the pre-stop wait covers
  that case anyway.
- Worst-case server STOP latency: terminate (up to 3 s with the kill), feeder join (up to
  4 s) and the pass wait (1.5 s) can exceed the client's 5 s socket timeout, but only when
  `pw-record` ignores SIGTERM. The normal case is well under 2 s.
