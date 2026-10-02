# Research: the 120 s speech-to-text recording limit

Read-only research. No code was edited, no Ansible run, no audio recorded.
RealtimeSTT 1.1.2 and faster-whisper 1.2.1 wheels were downloaded (no deps) only to
read library source (downloaded upstream source, not tracked). The playbook does not pin
either package, so the version on the host is not known. Library-behaviour claims
below were checked against 1.1.2. Older RealtimeSTT releases behaved the same way on the
points that matter here (realtime model defaults to `tiny`, realtime pass re-reads
the whole buffer), but that comes from memory and has not been checked against the
installed version.

---

## 1. Why 120 seconds: where it came from

### Short answer

Nobody measured anything to arrive at 120 s. It is a round number (4 x the earlier 30 s cap), chosen in one
commit whose only stated reason is "since transcription is real-time". No plan, journal
entry, benchmark or library limit sits behind it. Later work then built on it: a
server watchdog at 120 + 5 s, and article mode copying 120 s as its flush interval.

### Lineage

| Step                      | Evidence                                                                                                                                                                                                                                                                                                                                                      |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Earliest cap: 60 s        | `CLAUDE/Plan/Archive/speech-to-text.md:184`: "**Max duration**: 60 seconds (safety limit)" (sox `rec` era, whisperfile backend)                                                                                                                                                                                                                               |
| 30 s batch cap            | commit `266e590b` (2026-01-12) "feat: enhance speech-to-text extension with UI improvements…". Added the `sleep 30` auto-stop timer in `wsi` and the 27 s countdown "3s safety buffer before 30s hard limit". The README it added gave the reason: "**30-second maximum** - Hard limit due to Whisper's context window" (README since removed from the tree)  |
| 120 s streaming cap       | commit `2096a27e` (2026-01-14) "feat(speech-to-text): add notifications toggle, fix streaming buffer, extend timeout". Message: "Extend streaming mode timeout from 30s to 120s **since transcription is real-time**" / "Countdown timer now shows mode-appropriate limit (117s streaming, 27s batch)". Changed `--timeout` default 30 -> 120 in `wsi-stream` |
| Watchdog at 125 s         | Plan 007 (`CLAUDE/Plan/007-speech-to-text-resource-leak-fixes/PLAN.md:164`, `:339`): "Starts 125s timer when recording begins (5s buffer beyond client's 120s timeout)". This is the stuck-mic / mic-leak backstop ("Layer 3")                                                                                                                                |
| Article mode reuses 120 s | commit `a04aae3a` + `CLAUDE/Plan/Completed/015-article-mode/PLAN.md:13`: "single-session recording (up to 120 seconds)… every 120 seconds the accumulated transcription is flushed". `wsi-article:46` `CHUNK_DURATION = 120`. Here it is only the interval between Claude re-polish passes, not a limit                                                       |
| 117 s collision           | commit `1062ad4d` "stop old extension killing article recording at ~117s". The streaming countdown auto-stopped article mode at 117 s, so wsi-article stopped emitting RECORDING                                                                                                                                                                              |

### Where the number lives today (all four must agree)

| Location                                                    | Value                                                                                                                                                                                              |
| ----------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `extensions/speech-to-text@fedora-desktop/extension.js:647` | `this._remainingSeconds = this._streamingMode ? 117 : 27;` (auto-stop at 0, line 693-698)                                                                                                          |
| `extensions/speech-to-text@fedora-desktop/extension.js:666` | `const limit = this._streamingMode ? 120 : 30;` (log text only)                                                                                                                                    |
| `files/home/.local/bin/wsi-stream:1171`                     | `--timeout` default `120`; fallbacks `args.timeout or 120` (:514, server mode) and `args.timeout or 30` (:737, standard mode, dead because default is 120); pre-buffer uses `args.timeout` (:1020) |
| `files/home/.local/bin/wsi-stream-server:58`                | `WATCHDOG_TIMEOUT = 125`                                                                                                                                                                           |
| `files/home/.local/bin/wsi:691-700`                         | batch: `sleep 30` then SIGINT to pw-record                                                                                                                                                         |
| `files/home/.local/bin/wsi-article:46`                      | `CHUNK_DURATION = 120` (flush interval, not a cap)                                                                                                                                                 |

The extension never passes `--timeout`, so it does not hand its own number to the
scripts. The extension and the scripts each hold a copy, and nothing checks that they agree.

### Checking the two stated reasons

- **"Whisper's context window" (30 s).** Whisper's encoder does take 30 s windows,
  but faster-whisper (`model.transcribe`) and RealtimeSTT slide through longer audio
  one window at a time. It is a quality and latency matter, not a hard limit. The batch
  transcriber (`play-speech-to-text.yml:148-208`) already accepts any length.
- **"Transcription is real-time".** Only half true:
  - *Standard streaming mode:* the pasted text is `recorder.text()`. That is the **main
    model's final transcription of the whole utterance, and it runs only after stop**
    (RealtimeSTT `core/transcription_api.py:22-63`, `core/recording_buffers.py:110-119`).
    The live preview runs in real time; the pasted text does not.
  - *Server mode:* the server never calls `text()` (`wsi-stream-server:271-273`). It
    returns the last **realtime-callback** text. That text comes from RealtimeSTT's
    `realtime_model_type`, which defaults to **`tiny`** (`audio_recorder.py:68`,
    `core/realtime.py:1567-1570`), because the server does not set
    `use_main_model_for_realtime`. So server-mode output uses the tiny model whatever model
    the user picked. This is a quality defect unrelated to the limit, and worth its own fix.

---

## 2. Can it be extended, and what breaks?

Mechanically, yes: change the six places above together. The risks grow with length,
and some of them lose text without saying so:

| Area                              | Effect of a longer cap                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Lockstep drift                    | Raise the extension but not the script: the script stops at 120 s while the panel still counts. Raise the client but not `WATCHDOG_TIMEOUT`: the server force-stops at 125 s, the client's later STOP gets "No active recording", and `wsi-stream:547-550` exits "Failed to stop recording". **The whole transcript is lost.**                                                                                                                                       |
| Stop latency (standard mode)      | The final main-model pass over N seconds of audio starts at stop, so latency grows roughly linearly. The client waits only 5 s (`:744`), then stop + 3 s, then abort + 2 s (`:746-772`). Past that it **silently falls back** to the realtime/tiny buffer text (`:812-822`, logged only as "Using buffered text"). Longer audio makes that fallback more likely.                                                                                                     |
| Server mode cost / truncation     | Each realtime pass transcribes the **whole buffer so far** (`core/realtime.py:1505-1570`, `_snapshot_frames`), about every 0.2 s, so total compute is O(n^2) and each pass gets slower. At stop the server treats the text as final once it is unchanged for 0.3 s, waiting at most 2 s (`wsi-stream-server:281-301`). When one pass takes longer than 0.3 s (likely on long buffers), the last words are dropped. Plan 007 already fought this truncation at 120 s. |
| Audio drop under load             | RealtimeSTT discards queued audio when the queue exceeds `allowed_latency_limit` (default 100 chunks). It only logs a warning to its own logger (`core/recording.py:183-198`), which the user never sees. Heavier realtime passes make that backlog more likely.                                                                                                                                                                                                     |
| Memory                            | Not a constraint. 16 kHz mono s16 is 32 KB/s, about 1.9 MB/min (60 min is about 115 MB). Batch WAV in `/dev/shm` at 44.1 kHz stereo is about 10.6 MB/min, and `wsi:649-655` has a hard-coded "need 10MB" check sized for 30 s. VRAM depends on the model, not the audio length.                                                                                                                                                                                      |
| Transcription quality, long audio | Long single passes drift (`condition_on_previous_text`) and Whisper hallucinates or repeats over long silences. The batch transcriber does not set `vad_filter`.                                                                                                                                                                                                                                                                                                     |
| UI                                | The countdown (`REC 117`) was truncated once already (`71261e9d`). Three digits fit now. A countdown from many minutes is not a useful display.                                                                                                                                                                                                                                                                                                                      |
| Safety                            | The 120/125 pair is the stuck-mic backstop. Raising it makes a forgotten or stuck mic stay live longer unless the backstop is replaced by something else (see 3.7).                                                                                                                                                                                                                                                                                                  |

**Verdict:** raising it to something like 300 s is safe to do as a one-off only in
standard or pre-buffer mode, and only if the final-wait budget grows with it and the
watchdog changes in the same commit. It makes the latency and silent-fallback problems
worse, so it is not the right long-term fix. Chunking is.

---

## 3. Loop and buffer design: no effective limit

### 3.0 What already exists

**Article mode already loops without a time limit** (`files/home/.local/bin/wsi-article`,
launched from the panel menu "Create Article..."). It runs one RealtimeSTT recorder with
`post_speech_silence_duration=1.5` and calls `recorder.text()` repeatedly. Each call
returns one utterance ended by voice-activity detection (VAD), transcribed by the main
model. Results build up in `article-buffer.txt`, get flushed to `article-raw.txt` every
120 s, and a GTK window polls those files. So the concept is proven. The implementation
breaks the project's fail-fast rule and should not be copied as it stands:

- `_phrase_thread_fn` catches every exception, logs WARN and sets the result to `None`.
  **A failed phrase vanishes** (`wsi-article:283-285`).
- `flush_chunk` logs and continues if the write fails. **The chunk text is lost**
  (`:167-168`).
- Bare `except Exception: pass` throughout (`:60, :67, :81, :127, :138`).
- On Stop, the window SIGKILLs after 3 s (`wsi-article-window:436-444`). If the last
  phrase is still being transcribed, **it is lost silently**.
- Gaps between phrases: while `text()` is busy transcribing a phrase, the recorder is
  not re-armed to start on voice activity, so speech in that window survives only
  through the 1.0 s pre-roll buffer (`INIT_PRE_RECORDING_BUFFER_DURATION = 1.0`;
  `core/recording.py:316-320, 531-539`). If someone keeps talking through a slow
  transcription, words can drop at phrase boundaries. Newer RealtimeSTT has a
  `continuous_listening` re-arm, but the installed version is unknown. **This has not
  been measured and needs a triage probe before design sign-off.**

### 3.1 Recommendation

Build **continuous dictation once, inside the warm server** (`wsi-stream-server`). It
already owns a `pw-record` -> feed-thread pipeline (`:156-199`, `:359-437`) and keeps the
model loaded. Use **our own segmenter plus faster-whisper directly** rather than
RealtimeSTT's realtime machinery, because:

- RealtimeSTT is unpinned and changed heavily by 1.1.2 (faster-whisper is now an *extra*
  dependency; there is a new punctuation-split feature that rewrites `self.frames`).
- Its realtime path uses the tiny model, costs O(n^2), and drops audio without telling
  anyone. Each of those conflicts with fail-fast.
- faster-whisper is already installed by the playbook (`play-speech-to-text.yml:106-111`)
  and **ships Silero VAD v6 as ONNX** (`faster_whisper/vad.py`, `get_vad_model()`,
  `assets/silero_vad_v6.onnx`). It returns a speech probability for each 512-sample
  (32 ms) frame, using onnxruntime, which faster-whisper already requires. That means
  **no new dependency** for VAD.

### 3.2 Pipeline

```
pw-record (16k mono s16) -> capture thread -> segmenter (VAD) -> segment queue -> ONE transcription worker -> ordered commit -> buffer + journal
                                   |                                                                                   |
                                   +-- never blocks on transcription                         client polls PROGRESS / STOP returns full text
```

- **Capture thread.** Reads 512-sample frames and appends them to the current segment.
  It never waits on transcription. The queue is unbounded in memory (audio is cheap,
  see section 2), so nothing is ever dropped. If the backlog passes a ceiling (for
  example 120 s of untranscribed audio), the session **fails loudly** rather than
  discarding audio.
- **Segment boundaries (VAD, not fixed).**
  - Close a segment after **>= 700 ms of non-speech** that follows **>= 1 s of speech**.
  - **Soft max 20 s:** after that, cut at the next pause of >= 250 ms.
  - **Hard max 28 s:** with no pause, cut at the **lowest-energy 32 ms frame within the
    last 2 s**.
  - Keeping every segment at \<= 30 s means each one is a **single Whisper window**:
    no seam inside a segment, best accuracy, and transcription time stays bounded.
  - Pure silence is never queued, which also removes long-silence hallucinations.
- **Overlap.** None at silence cuts; a pause is a natural word boundary. Hard cuts are
  rare and land at an energy minimum. Start **without overlap** (YAGNI), but count and
  log every hard cut. If triage shows split words, add 0.5 s overlap and remove
  duplicates using faster-whisper `word_timestamps=True`, dropping words in segment
  N+1 that start before the cut point.
- **Transcription worker.** One thread. It uses the **user-selected main model** (not
  tiny), `beam_size=5`, a fixed language, `vad_filter=False` (segments are already cut),
  `condition_on_previous_text=False`, and `initial_prompt` = the last ~200 characters
  of committed text. The prompt keeps casing, punctuation and vocabulary consistent
  across segments without carrying drift forward.
- **Ordering.** Every segment gets a sequence number. One FIFO worker keeps results in
  order by construction. Commits are still keyed by sequence number and applied only in
  contiguous order, so that a later move to parallel workers stays correct.
- **Where the buffer lives.**
  - The server's memory holds the authoritative copy.
  - It is mirrored to an **append-only JSONL journal** in `$XDG_RUNTIME_DIR`
    (per-user tmpfs, mode 0700, cleared at logout), one line per segment:
    `{seq, t_start, t_end, status, text}`.
  - Do not use `~/.cache`. Dictated text is sensitive and should not persist across
    logins.
  - If the client crashes or the server restarts mid-session, the journal is what you
    recover from.
- **Partial output.**
  - The panel replaces the countdown with **elapsed time plus backlog**, e.g.
    `REC 3:42 ·2` (2 segments waiting). The label turns amber while the backlog grows
    (real-time factor above 1) and red on failure.
  - Committed text is mirrored to the existing `BUFFER_FILE` so the article window or a
    preview can tail it.
  - **Do not type into the focused window during recording.** The user may change focus.
    Paste once at stop. "Type each segment as it commits" could be a later option and
    is out of scope now.
- **Stop (Insert).** The client sends STOP. The server stops capture and closes the
  current segment, however short (pad to 1 s, as `wsi:759` does). It then drains the
  queue under a **deadline that grows with the backlog**, for example
  `10 s + 3 x backlog_seconds x measured_RTF`, and reports progress to the panel
  ("finishing 2 segments"). STOP returns the full ordered text, and the client pastes
  once, using the existing `auto_paste` / `copy_to_clipboard` / Claude post-process path.
  **Escape** keeps today's meaning: SIGUSR1 tells the server to ABORT and discard the
  text (`extension.js:1135-1139`).

### 3.3 Failure handling (fail fast, never a silent hole)

| Failure                                | Behaviour                                                                                                                                                                                                                                                                                                                   |
| -------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Segment transcription raises           | Mark the session FAILED and stop capture immediately. The panel goes to ERROR. Keep that segment's **audio as WAV** in the runtime dir. Copy the text committed so far to the clipboard (never auto-paste a transcript with a hole). Notify: "segment N failed - text up to N copied, audio kept at <path>". Non-zero exit. |
| Segment with speech returns empty text | Not fatal (it may be noise). Count it, keep its audio, and **list it in the final notification** ("1 segment produced no text").                                                                                                                                                                                            |
| `pw-record` exits during the session   | FAILED, same handling as the first row.                                                                                                                                                                                                                                                                                     |
| Backlog passes the ceiling             | FAILED with "transcription cannot keep up (RTF x.x)". Audio is never discarded.                                                                                                                                                                                                                                             |
| Drain deadline expires at STOP         | FAILED. Report how many segments were still pending. Copy the committed text and keep the pending audio. **No fallback to a lower-quality model's text** (unlike today's tiny-text fallback).                                                                                                                               |
| Client vanishes                        | The existing Layer 2 handling stays (`wsi-stream-server:540-589`). Plus the heartbeat below.                                                                                                                                                                                                                                |
| Journal write fails                    | FAILED. Today article mode logs this and carries on.                                                                                                                                                                                                                                                                        |

### 3.4 Replacing the 120/125 s safety net

A fixed 125 s watchdog cannot coexist with unlimited recording. Replace it with:

1. **Heartbeat:** the client sends `KEEPALIVE` every 5 s. The server stops the session
   after 15 s without one. This keeps the purpose of the Layer 3 watchdog (a mic is
   never left live with nobody listening) without capping length.
2. **Inactivity cap:** auto-stop after e.g. 120 s with no speech at all, which covers a
   forgotten or stuck mic. Stop normally (transcribe and paste), and say why in the
   notification.
3. **Absolute cap:** large and configurable (e.g. 60 min, GSettings key), as a last
   resort. The panel counts down only in the final 60 s.

### 3.5 Scope and order

- Phase 1: continuous dictation in **server mode** (`--server-mode`), behind a GSettings
  toggle that defaults off until verified.
- Phase 2: point `wsi-article` at the same server session API and polling journal (DRY).
  That removes its own loop and its fail-fast violations.
- Batch `wsi` (30 s) and standard/pre-buffer streaming keep their caps. Their help text
  and docs should point long dictation at continuous mode. Raising batch to 120 s is
  low-risk (faster-whisper handles any length) if the `/dev/shm` space check scales with it.

### 3.6 To measure first (triage probes)

- Real-time factor of the chosen model per 20 s segment on the GPU (sets the backlog
  ceiling and drain deadline).
- Word loss at phrase boundaries in the current article mode (decides whether 3.0's
  gap concern is real).
- Hard-cut frequency for a natural speaker at a 28 s hard max / 20 s soft max.
- Installed RealtimeSTT and faster-whisper versions (`pip show`, user site).

---

## 4. IaC changes needed

No new playbook is needed. Everything is edits to the existing STT play and the files it
deploys. This is not a CCY change, so no CCY version bump.

| File                                                                                        | Change                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `files/home/.local/bin/wsi-stream-server`                                                   | New session mode: VAD segmenter (`faster_whisper.vad.get_vad_model`), one `WhisperModel` worker, ordered commit, runtime-dir journal. New commands `START {continuous:true}`, `PROGRESS`, `KEEPALIVE`, `ABORT`. STOP drains with a backlog-scaled deadline. Replace `WATCHDOG_TIMEOUT=125` with heartbeat, inactivity and absolute caps. Remove the bare `except: pass` paths it touches.                                                              |
| `files/home/.local/bin/wsi-stream`                                                          | Server-mode client: drop the fixed `--timeout 120`, send keepalives, relay PROGRESS to DBus, FAILED exits non-zero with the partial text on the clipboard. Remove the silent tiny-text fallback in `run_standard_streaming` (`:812-822`), or make it a loud warning. Logs go to stderr/log file; stdout stays empty or is the payload (StderrHygiene).                                                                                                 |
| `extensions/speech-to-text@fedora-desktop/extension.js`                                     | Continuous mode: elapsed + backlog label instead of the 117/27 countdown. Read limits from GSettings so the constants are not duplicated (the countdown appears only for the absolute cap). Remove the `117`/`120` literals. ESLint is required. Logout/login is needed to load it (Wayland).                                                                                                                                                          |
| `extensions/.../schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml` + `prefs.js` | New keys `continuous-dictation` (b, default false), `max-recording-minutes` (i), `silence-autostop-seconds` (i). Fix the `streaming-mode` description: it says "auto-stops on silence", which is false (`post_speech_silence_duration: 300.0`, `wsi-stream:703`). The existing `Compile GSettings Schema` task handles recompiling.                                                                                                                    |
| `playbooks/imports/optional/common/play-speech-to-text.yml`                                 | Deploy tasks for `wsi-stream`/`wsi-stream-server` already exist and notify `restart wsi-stream-server`, so no new task is needed for the scripts. **Pin** `RealtimeSTT==<ver>` and `faster-whisper==<ver>` (both unpinned today; 1.1.2 changed the dependency shape) after `pip show` on the host. Fix the stale header comment (line 8 says default `small`; `wsi-stream` defaults to `base`). No host_vars option: the knobs are per-user GSettings. |
| `files/home/.local/bin/wsi-article`, `wsi-article-window` (phase 2)                         | Use the server session. Remove the 3 s SIGKILL on Stop (wait for the drain). Remove the swallowed exceptions.                                                                                                                                                                                                                                                                                                                                          |
| `docs/features/speech-to-text.md`                                                           | `:226` says "Maximum duration: 30 seconds" and never mentions 120 s. Document the per-mode limits, continuous mode, and the failure notifications.                                                                                                                                                                                                                                                                                                     |
| `CLAUDE/Plan/` (via `CLAUDE/Plan/mkplan.bash`)                                              | New plan with `triage.bash` (the 3.6 probes; HOST-only, read-only) and `acceptance.bash`. Unit tests for the segmenter as a pure function on synthetic arrays under `tests/` (ruff gate). `qa-all.bash` plus the `qa-reviewer` agent as the final step.                                                                                                                                                                                                |

Convention notes:

- `wsi-*` scripts are launched by the extension and never prompt, so
  **InteractiveScripts.md does not apply**. They must stay non-interactive and fail fast.
- **StderrHygiene:** every log and progress line goes to stderr or the log file. A
  status query, if one is added, prints its JSON on stdout.
- **AnsibleStyle:** every copy task keeps `owner`/`group`/`mode`. No `failed_when: false`
  without the probe-then-check pattern. Any new pip requirement goes into the existing
  `ansible.builtin.pip` task rather than a new task.
- **Missing-dependency rule:** if onnxruntime turns out to be absent, add it to the
  playbook's pip task. Do not make the code tolerate it being missing.

---

## Side findings (separate from the limit)

1. **Server mode pastes tiny-model text** whatever model is selected (section 1).
2. **Plan/code drift:** `CLAUDE/Plan/Completed/015-article-mode/PLAN.md:7-9` says the
   "`Shift+Insert` binding" shipped. The schema has no such key; article mode is
   menu-only (commit `a04aae3a` says "not a keybinding").
3. **Docs drift:** `docs/features/speech-to-text.md:226` (30 s only); schema description
   of `streaming-mode` (says it auto-stops on silence).
4. **Silent text loss** in article mode (section 3.0) and the silent tiny-text fallback in
   standard streaming (section 2).
5. RealtimeSTT and faster-whisper are **unpinned** (`play-speech-to-text.yml:233-239`).
