# Plan 00229: speech-to-text capture must be raw PCM, and fail fast when it is not

**Status**: In Progress
**Created**: 2026-10-27
**Owner**: joseph
**Priority**: High

## Overview

Every dictation turned into "so so so" after a package upgrade. pipewire-utils 1.6.10 changed
what `pw-record` writes when its output is `-`: a Sun AU container (`.snd`, big-endian 16-bit
samples) where 1.6.9 wrote raw PCM. `wsi-stream-server` reads that stream as raw little-endian
int16. Every sample was byte-swapped into loud noise that still followed the rhythm of the speech.
The voice detector cut it at the pauses, and Whisper decoded each segment as "so" or "you".
Nothing failed, so the wrong text was pasted. The microphone, its level, the routing, the model
and the GPU were all proven fine (journal).

This plan fixes it under Defence Before Fix:

- the capture asks for raw PCM explicitly;
- a stream that carries a container header fails the dictation with a clear error;
- a rule stops any capture to stdout without `--raw` from coming back.

## Goals

- `pw-record` piped to a reader is always `--raw`, in every caller.
- The server fails a dictation whose capture stream starts with a container header, naming the
  container, instead of transcribing noise.
- A semgrep rule (`pw-record-stdout-not-raw`) fails QA on a `pw-record` or `pw-cat` stdout capture
  without `--raw`.
- The capture child is reaped on every exit path (no zombie `pw-record`).

## Non-Goals

- Changing the Go `wsi` recorder: it writes a `.wav` file, where a container is correct.
- Pinning PipeWire.

## Tasks

### Phase 1: Defence Before Fix

- [x] ✅ **Task 1.1**: Red tests: a capture stream that starts with a container header (`.snd`,
  `RIFF`, ...) fails the session with an error naming the container; the capture child is reaped
  after a dictation ends, fails or is aborted.
- [x] ✅ **Task 1.2**: The rule `pw-record-stdout-not-raw` in `.semgrep/speech-to-text.yml`, red on
  its Python and JavaScript fixtures and on the real tree (the server's `CAPTURE_CMD`, the
  `wsi-stream` pre-buffer capture).
- [x] ✅ **Task 1.3**: Green: `--raw` in every stdout capture, the header check, the reaping. The
  sweep: every `pw-record`/`pw-cat` caller in the repo is listed with its verdict. `CLAUDE/QA.md`
  gets the rule's section. Sweep verdicts: the server's `CAPTURE_CMD` and `wsi-stream`'s
  pre-buffer capture were the only stdout captures (fixed); `wsi` (Go) records to a `.wav` file
  (correct); `wsi-server-manager` only finds and signals pw-record; the extension and docs only
  mention it; the test stubs stand in for it. `wsi-stream`'s standard mode has no `pw-record`
  (RealtimeSTT opens the microphone), so its check is in pre-buffer mode, before `feed_audio()`.
- [ ] ⬜ **Task 1.4**: Deploy `play-speech-to-text.yml` through `meta-deploy.bash`; the owner
  dictates and gets the words spoken.

### Phase 2: The upgrade that changed a tool under running services

- [ ] ⬜ **Task 2.1**: `play-AB-dnf-upgrade.yml` replaced pipewire-utils under running user services
  and changed a tool's output format, with nothing noticing. Phase 1's stream check covers this
  case. Decide whether the play should also report user services left running on replaced
  libraries (as it reports a kernel change), and build it if so. Open: not started.

## Success Criteria

- [ ] A dictation on pipewire 1.6.10 transcribes the words spoken.
- [ ] A capture stream with a container header fails the dictation with an error naming it.
- [ ] `qa-speech-to-text-rules` is red on a stdout `pw-record` without `--raw` and green on the
  tree.
- [ ] No `pw-record` is left defunct after a dictation.

## Delivery & Milestones

- Root cause and evidence: [JOURNAL/](JOURNAL/).
