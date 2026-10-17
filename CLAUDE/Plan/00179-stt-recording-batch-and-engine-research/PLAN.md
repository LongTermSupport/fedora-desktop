# Plan 00179: speech-to-text batch transcription of recordings, and engine research

**Status**: In Progress
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Medium

Addresses #98, #11, #9 and #84.

## Overview

Speech-to-text today is live dictation only. #98 asks for a batch mode for long recordings
made away from the desk: an hour or more of dictation recorded on the phone arrives in a
cloud folder, and the desktop transcribes it locally into a markdown transcript.
`faster-whisper-transcribe <file>` already exists, but it runs `model.transcribe()` with no
voice activity detection, no vocabulary and no post-processing, so a long recording with
pauses is prone to hallucinated and repeated text. The owner settled the shape in the issue's
comments: three layers that each know only their own part. The phone app puts complete
recordings in a known cloud folder. fedora-desktop provides generic tools: the folder mounted
locally through the existing rclone support (`play-rclone.yml`, `scripts/setup-rclone.bash`)
and reachable inside project containers, a transcription command, and a generic way to mark
a recording done. Each project keeps its own small script over local folders. Everything runs
locally, recordings are kept as the source, and the default model must fit a 4 GB GPU.

The other three issues are about which engine and which install path. #11 asks for a split
between NVIDIA hosts (faster-whisper with CUDA, as today) and hosts without one, where the
play currently installs the CUDA wheels anyway. #9 (Moonshine and the Open ASR Leaderboard)
and #84 (voxtype) ask whether other engines or tools are worth adopting. Plan 00148 Phase 7
has already researched this ground
([RESEARCH-stt-models-2026.md](../00148-stt-unlimited-dictation-loop-and-buffer/RESEARCH-stt-models-2026.md),
covering Moonshine and Parakeet, and
[research-linux-dictation-landscape.md](../00148-stt-unlimited-dictation-loop-and-buffer/research-linux-dictation-landscape.md),
covering voxtype). The owner chose only the better default model there. This plan starts from
that research. It answers what it left open: a CPU path for hosts without NVIDIA, and the
model for an hour of audio on 4 GB.

The vocabulary feature belongs to Plan 00159 (Not Started). Batch mode reuses its reader;
until 00159 lands, the batch command runs without a vocabulary rather than building a second
one.

## Goals

- A file command (working name `wsi-file`) that transcribes Opus, M4A/AAC and WAV with
  VAD on, `condition_on_previous_text=False`, paragraphs split at longer pauses, time markers
  every few minutes, and the raw transcript kept beside any post-processed one.
- Likely jargon corrections marked inline (for example `heard [[meant?]]`), never silently
  replaced.
- A done-marker tool and an rclone mount of the recordings folder, so a recording is
  transcribed once and a truncated or corrupt one fails visibly.
- A measured default model for an hour of audio on a 4 GB card (memory and speed recorded).
- A decision, with evidence, on the non-NVIDIA install path (#11) and on whether Moonshine,
  Parakeet or voxtype (#9, #84) earn a place.

## Non-Goals

- The phone app, and delivering transcripts into any project (each project's own script).
- Cloud transcription of any kind.
- A second vocabulary store: Plan 00159 owns it.
- Re-researching what 00148 Phase 7 already settled.

## Owner decisions (file transcription)

- **One warm model.** Files are transcribed by the warm streaming server's model
  (`wsi-stream-server`), not a second process: a 4 GB card cannot hold a second large model
  beside it (a `large-v3` trial beside the server ran out of GPU memory). Dictation and a
  file job share the model; CTranslate2 runs one call at a time, so dictation waits at most
  one 30 s window of the file.
- **Sidecars mark done.** Each recording gets `<recording>.stt.md` beside it; a recording
  with a sidecar is done. This is the done-marker of Task 4.1.
- **Recordings are folders.** The phone app writes one folder per recording: numbered Opus
  parts and a `manifest.json` (state, each part's size and MD5). The parts are joined into
  one stream before transcription, so a word cut at a part boundary is heard whole.
- **Accidental taps are skipped.** A recording under 3 s (`--min-seconds`) is not
  transcribed; `stt --short` lists them for cleanup. Deleting them is left to the owner.
- **Mounting into a project container** is configuration only: a line in that project's
  `.claude/ccy/mounts` (docs/ccy.md, Extra Mounts). Nothing here knows the folder is rclone.

## Tasks

### Phase 0: File transcription with the warm model (tests first)

- [x] ✅ **Task 0.1**: `wsi-stream-server` `FILE_START {paths, language}` and
  `FILE_PROGRESS {since}`: one recording's files joined and transcribed on a thread of
  their own by the loaded model (VAD on, beam 5, no previous-text conditioning); one file
  job at a time; it holds off the idle timeout and never makes a dictation START refused
  (`tests/speech_to_text/test_server_file_job.py`).
- [x] ✅ **Task 0.2**: `wsi-stream --ensure-server`: the server started the one way an
  Insert starts it (`tests/speech_to_text/test_ensure_server.py`).
- [x] ✅ **Task 0.3**: `stt` in Go (`src-go/cmd/stt`, `internal/stt`, S1-S17 in its package
  doc): finds recordings, checks manifests (complete, synced, size, MD5), skips short ones,
  sends each pending one to the server, writes the sidecar atomically; `--list`, `--short`.
- [x] ✅ **Task 0.4**: `play-speech-to-text.yml` builds and installs `stt`; docs in
  `docs/features/speech-to-text.md`.
- [ ] 🔄 **Task 0.5**: Deploy, then transcribe the recordings folder with `stt`. Accepted on a
  local copy first (paragraphs now also break at a sentence end after a minute), the play
  run on the host, and the real folder transcribed: seven sidecars, identical to the
  accepted ones, two taps skipped. Still to check: a dictation during a long file job
  (latency and GPU memory with both running), and the owner reading the sidecars for
  accuracy before any tuning (beam, VAD settings, paragraph rules).
- [ ] ⬜ **Task 0.6**: Let a CCY container ask the desktop to transcribe (owner: "would be
  cool", not required). Needs a narrow host bridge; nothing like it exists today.
- [ ] 🔄 **Task 0.7**: Watch folders (owner request): `stt_watch_folders` in host_vars;
  `stt-watch.timer` runs `stt` over them every 5 minutes (polled: inotify on an rclone mount
  misses remote arrivals); a failed run raises a desktop notification
  (`stt-watch-failed.service`). Readiness is stt's own: manifest `COMPLETE`, parts synced,
  size and MD5; no manifest, sent as it is. Built; still to deploy, set the folder in
  host_vars, and see a new recording transcribed by the timer.

### Phase 1: Triage and research (read-only)

- [ ] ⬜ **Task 1.1**: `triage.bash`: GPU memory, installed faster-whisper and CTranslate2
  versions, whether the rclone remote and mount exist, and how `faster-whisper-transcribe`
  is called today.
- [ ] ⬜ **Task 1.2**: Measure memory and speed on an hour of Opus audio for `large-v3`
  int8, `large-v3-turbo` and `distil-large-v3.5`, VAD on. Record the results in a research
  file in this folder.
- [ ] ⬜ **Task 1.3**: Test whether a desktop client authorised separately can read files
  the phone uploaded with the `drive.file` scope (#98 comment). If it cannot, record the
  narrowest read access the one folder needs.
- [ ] ⬜ **Task 1.4**: CPU path for #11: from 00148's research, compare faster-whisper CPU
  int8, Parakeet through `onnx-asr`/sherpa-onnx, whisper.cpp and Moonshine on latency and
  accuracy on a laptop CPU. Read voxtype (#84) for ideas worth borrowing. Write a ranked
  recommendation.

### Phase 2: Owner decisions (decision gate)

- [ ] ⬜ **Task 2.1**: The default batch model, the non-NVIDIA engine and install path,
  and whether any of #9 and #84 is adopted or closed as researched.

### Phase 3: The batch command (tests first)

- [ ] ⬜ **Task 3.1**: Tests, then `wsi-file`: VAD, long-form settings, paragraphs, time
  markers, raw and processed output, a visible failure for a truncated or corrupt file.
- [ ] ⬜ **Task 3.2**: Use Plan 00159's vocabulary reader once it exists. Rank the
  vocabulary, most important first: faster-whisper truncates `hotwords` at about 223 tokens.
- [ ] ⬜ **Task 3.3**: Inline jargon markup, and a list of candidate vocabulary entries
  (low-confidence words) for a person to accept or reject.

### Phase 4: Folder, done-tracking, install path

- [x] ✅ **Task 4.1**: The recordings folder as an rclone mount, reachable in project
  containers; a generic tool marks a recording done so it is transcribed once. Done by the
  existing rclone mount, a project's `.claude/ccy/mounts` line, and Phase 0's sidecars. The
  line is declared; that a container sees the folder is shown at its next launch.
- [ ] ⬜ **Task 4.2**: `play-speech-to-text.yml` installs the engine Task 2.1 chose for
  hosts without NVIDIA, and the CUDA wheels only where a GPU is present.
- [ ] ⬜ **Task 4.3**: `deploy.bash`, `acceptance.bash`, an entry in `meta-deploy.bash`, and
  docs in `docs/features/speech-to-text.md`.
- [ ] ⬜ **Task 4.4**: `qa-all.bash`, then the `qa-reviewer` agent over the plan's diff.

## Success Criteria

- [ ] An hour-long Opus recording with long pauses transcribes without repetition loops,
  with paragraphs and time markers, within the 4 GB card.
- [ ] A recording seen twice is transcribed once; a truncated or corrupt one is a visible
  failure, not an empty transcript.
- [ ] A host without NVIDIA installs a working engine and no CUDA wheels.
- [ ] #9, #11 and #84 each have a recorded decision.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00179-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from the four issues and 00148's research.
