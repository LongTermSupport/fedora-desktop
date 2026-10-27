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
- [x] ✅ **Task 1.4**: Deploy `play-speech-to-text.yml` through `meta-deploy.bash`; the owner
  dictates and gets the words spoken. PASS 27-10-27 at F44 59ada11bb (host agent): META_RC=0,
  the deployed server's `CAPTURE_CMD` and `wsi-stream`'s pre-buffer capture carry `--raw`; the
  owner's dictation pasted exactly what was said (1 segment, status ok, no container-header
  error), and no `pw-record` was left defunct.
- [ ] ⬜ **Task 1.5**: `changed-plays` ran `play-agent-bus.yml` and `play-network-tools.yml` as
  well, as plays whose inputs include `files/home/.local/bin/wsi-stream` and
  `wsi-stream-server`. Neither play deploys them. Find why the play ledger counts them as
  those plays' inputs (a glob or directory input too wide, a shared task file) and narrow it,
  test first, so a speech-to-text change re-offers only the speech-to-text play.

### Phase 2: The upgrade that changed a tool under running services

- [ ] ⬜ **Task 2.1**: `play-AB-dnf-upgrade.yml` replaced pipewire-utils under running user services
  and changed a tool's output format, with nothing noticing. Phase 1's stream check covers this
  case. Decide whether the play should also report user services left running on replaced
  libraries (as it reports a kernel change), and build it if so. Open: not started.
- [x] ✅ **Task 2.2**: The same play replaced python3 under the running main `ansible-playbook`
  (VM lab, desktop-fresh-install on the 10-06 base: python3 3.14.7 to 3.14.8, then
  `lookup('url')` in `play-toolbox-install.yml` failed with "name 'ssl' is not defined"), so
  every fresh install from a base behind the repos died there. Shape chosen: run.bash's
  dependency step (developer and headless) and `consumer-install` run `dnf -y upgrade --refresh` themselves before any Ansible process starts; a failed upgrade stops the run. Why
  not a separate `ansible-playbook` of the upgrade play first: that process would itself
  outlive the upgrade for the play's remaining tasks, and run.bash's ratchet would have to pay
  for a second three-branch become block; a plain dnf leaves no Ansible process alive across
  the upgrade at all, and covers python3-libs, openssl-libs and every other library ansible
  loads without listing them. `play-AB-dnf-upgrade.yml` stays in `playbook-main.yml` at its
  position (a cheap no-op upgrade there; `--changed`, `--rerun` and the play ledger keep it as
  a play; a single play of it still upgrades), keeps its half-installed kernel cleanup, and its
  reboot notice now also fires when the running kernel is not the newest installed (the
  kernel now changes before its snapshot). Tests first: `scripts/test-run-bash-upgrade-first.bash`
  (gate `run-bash-upgrade-first`) and the consumer tests pin the order and the stop. run.bash
  1.35.0. Host check (pending): the VM lab's desktop-fresh-install and
  desktop-consumer-install on the 10-06 base.

## Success Criteria

- [x] A dictation on pipewire 1.6.10 transcribes the words spoken (Task 1.4).
- [x] A capture stream with a container header fails the dictation with an error naming it
  (Task 1.1's tests, green at Task 1.3).
- [x] `qa-speech-to-text-rules` is red on a stdout `pw-record` without `--raw` and green on the
  tree (Task 1.2, Task 1.3).
- [x] No `pw-record` is left defunct after a dictation (Task 1.3's tests; on the host, Task 1.4).

## Delivery & Milestones

- Root cause and evidence: [JOURNAL/](JOURNAL/).
