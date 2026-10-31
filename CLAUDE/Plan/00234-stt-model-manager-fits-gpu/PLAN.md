# Plan 00234: the speech-to-text model manager offers only the models that fit this GPU

**Status**: In Progress (the owner's look passed, Task 2.2; its two follow-ups are built, Tasks 2.4 and 2.5, and wait on the host run and the owner's look, Task 2.6)
**Created**: 2026-10-27
**Owner**: joseph
**Priority**: Medium

## Overview

The owner's request: the foot window that downloads speech-to-text models
(`wsi-model-manager`, opened by the panel's Manage Whisper Models...) "does not actually tell
us which models would fit in the current card"; it should preselect the biggest English-only
model and prevent selecting models too big for the system GPU. The test machine is a hybrid
laptop (Intel iGPU and an NVIDIA RTX A2000 Laptop GPU with little memory), where a large-v3
download loads into a GPU that cannot hold it.

`wsi-resolve-model` already owns the GPU question (CTranslate2's CUDA count, checked against
the NVIDIA device nodes). It now also owns each offered model's size and the rule for how much
GPU memory a model needs, and answers `--fit` with one JSON object. The manager shows that
answer per row, starts its cursor on the most capable English-only model that fits, and
refuses a download that does not fit with what the model needs, what the GPU has and what to
choose instead, leaving the list open for another choice.

The same branch rewrites play-nvidia.yml's Secure Boot MOK enrolment message so a
non-technical person can follow the blue MOK Manager screens after the reboot.

## Goals

- Each row of the manager shows the model's download size and its GPU memory need, marked
  `✓ fits` or `✗ too big` against this machine's NVIDIA GPU; with no usable NVIDIA GPU the
  status line says the models run on the CPU, and no row is refused.
- The cursor starts on the most capable English-only model that fits.
- Downloading a model that does not fit is refused with a clear message, and the manager
  stays open (InteractiveScripts.md: strict validation, friendly recovery).
- One home for the sizes and the fit rule: `wsi-resolve-model` (`SIZE_MB`, `NEED_FACTOR`,
  `NEED_OVERHEAD_MB`); the manager's and the panel's own size strings are gone, and the
  docs' model tables show `SIZE_MB`'s figures (`SizesHaveOneHomeTest` holds both).
- The MOK message of play-nvidia.yml and play-displaylink.yml walks through each blue screen
  by its title (one copy, `tasks/secure-boot-mok-enrolment-steps.yml`).

## Non-Goals

- Changing what `auto` picks. Its GPU picks (distil-large-v3.5, 2.6 GB; large-v3-turbo,
  2.7 GB) fit any 4 GB card by this rule; a card smaller than that is not handled here.
- Judging a CPU machine by its RAM. On the CPU every model loads, only slower; the status
  line says so.
- Porting `wsi-model-manager` to Go (Decision 1).
- `--download-auto` (the unattended download of `auto`'s picks) is unchanged, but for
  also fetching the live preview models (Phase 4).

## Context & Background

- `files/home/.local/bin/wsi-resolve-model`: `--fit`, `fit_report`, `smallest_nvidia_gpu`.
- `files/home/.local/bin/wsi-model-manager`: `machine_fit`, `best_english_fit`,
  `download_refusal`, `fit_cell`, `machine_line`, and the one download path `_start_download`
  (the Download button and Enter on a row both go through it).
- Tests: `tests/speech_to_text/test_resolve_model.py` (`FitTest`, a fake nvidia-smi on PATH
  and the stub ctranslate2) and `tests/speech_to_text/test_model_manager_installed.py`
  (`FitsThisMachineTest`, built on the resolver's own `fit_report`), both run by
  `scripts/test-wsi-stop-grace.bash`.

## Tasks

### Phase 1: The fit, in the resolver and the manager

- [x] ✅ **Task 1.1**: Tests first: `FitTest` and `FitsThisMachineTest`, seen to fail (20 new
  tests red, the 45 existing green)
- [x] ✅ **Task 1.2**: `wsi-resolve-model --fit`: sizes and the need rule, nvidia-smi's
  smallest GPU, fail fast (exit 1) when nvidia-smi is missing, fails or answers unreadably,
  or the GPU check fails; the CPU when CTranslate2 counts no GPU and there are no NVIDIA nodes
- [x] ✅ **Task 1.3**: The manager: GPU memory column, machine line, preselection, refusal;
  every download refused, with the reason, when the fit cannot be told
- [x] ✅ **Task 1.4**: play-nvidia.yml MOK message rewritten for a non-technical reader

### Phase 2: On the host

- [x] ✅ **Task 2.1**: meta-deploy: `changed-plays` picks up play-speech-to-text.yml and
  play-nvidia.yml; then this plan's `acceptance.bash` (installed copies are the checkout's,
  `--fit` judges the real GPU, and the installed manager's helper functions pick and refuse).
  Passed at 525560cfb on a 4.0 GB laptop GPU: Distil Large v3.5 picked (fits, 2.6 GB), Large
  v2 and v3 refused (about 4.4 GB needed). play-nvidia.yml was not picked: it has not run
  there. That run called the helpers only: the screen itself (the cursor move in `on_mount`,
  `_start_download`'s refusal) did not run under test; Task 2.3 covers it.
- [x] ✅ **Task 2.3**: the review fixes on the host (meta-deploy): `acceptance.bash` again,
  with its new `screen` leg driving the installed manager's real Textual screen headless
  (`App.run_test`; textual is installed by the play but not in the QA container, so this
  cannot run in `qa-all`): the cursor on the preselected model, the too-big row marked, Enter
  and the Download key on a too-big model refused with the list left open, no download
  started. Then `gpu-load-acceptance.bash`: distil-large-v3.5 loaded on the real GPU in
  float16, once as wsi-stream-server loads it (beam 5 over 30 s of noise) and once with
  wsi-stream's own streaming settings (real-time transcription on, the real-time model
  beside it), each peak GPU memory reported against the fit rule's figure and failing above
  it. It downloads distil-large-v3.5 if it is missing, nothing else. Passed: `acceptance.bash`
  with the screen leg at `071969780`, and `gpu-load-acceptance.bash` at `36889021f` after
  Phase 4.
- [x] ✅ **Task 2.2**: The owner's look: open Manage Whisper Models... and check the status
  line names the NVIDIA GPU and its memory, the cursor is on Distil Large v3.5, Large v2 and
  Large v3 show `✗ too big`, and choosing one shows the refusal and leaves the list open.
  Passed (the owner, 2026-10-28): "listing models at 4.4GB as too big, other things all look
  fine, it's showing the GPU detail". Two follow-ups, verbatim: "it opens as a black screen
  for quite a long time - can we have a loading message?" (Task 2.4) and "the GPU detail
  ... could be clearer, it's quite low contrast" (Task 2.5)
- [x] ✅ **Task 2.4**: Tests first (`OpensAtOnceTest`), then: the window draws at once with
  `Checking the GPU and the models...` as its status line. The black screen was `on_mount`
  asking the resolver three times (`--suggest` per recorder mode, then `--fit`, each loading
  CTranslate2) on the screen's own thread, so nothing was drawn until all three answered.
  `probe_machine` now asks them in a worker (`_probe_machine`) and `_machine_probed` fills the
  table, the status line and the cursor; a download chosen before then is held with
  `Still checking...`. The `screen` leg of `check-fit.py` checks the first status line and
  waits for the worker before driving the list
- [x] ✅ **Task 2.5**: Tests first: the status line is in the normal text colour (`$text`, was
  `$text-muted`), and its GPU part is bold
- [ ] 🔄 **Task 2.6**: on the host (meta-deploy): `changed-plays` picks up
  play-speech-to-text.yml, then `acceptance.bash` (its `screen` leg now sees the checking line
  first); then the owner opens Manage Whisper Models... and sees the checking message at once
  and a readable GPU line. Host half passed: play-speech-to-text and this plan's acceptance
  at c378092ce (2026-10-30, `$12STku_FsmBt0mrik6T1BG8UzYQ5hhuFtIQixmMF-zE`), the play again
  at be1611132. Left: the owner's look, after their next log-in.

### Phase 3: Review

- [x] ✅ **Task 3.1**: qa-reviewer over the branch diff: FIX-BEFORE-MERGE, six findings
  (`subagent-reports/261027-final-qa-review.md`)
- [x] ✅ **Task 3.2**: the review's fixes: the panel's and docs' sizes (finding 1), the
  screen check and Task 2.1's wording (2), this plan's status (3), the real-GPU load check
  (4), the shared MOK message and the vault wording (5); Decision 1 recorded as awaiting
  the owner (6)

### Phase 4: The live preview model nothing downloaded

The host run of `gpu-load-acceptance.bash` after Task 2.3: the beam-5 leg passed
(distil-large-v3.5 peaked at 2080 MiB against the rule's 2659). The streaming leg failed:
neither `wsi-stream` nor the check named a real-time model, so RealtimeSTT loaded its
default `tiny`, which nothing downloads; offline that raised LocalEntryNotFoundError, and
RealtimeSTT 1.0.0 then hung (its transcription worker is a non-daemon thread, its
`deamon` misspelt) until the 600 s timeout. From the panel, live streaming would have
downloaded tiny on first use. RealtimeSTT also wrote `realtimesst.log` into its working
directory, the home directory when the panel starts `wsi-stream`.

- [x] ✅ **Task 4.1**: Tests first, then: `wsi-resolve-model --realtime` names the live
  preview model (tiny.en for English, tiny otherwise, `REALTIME_MODELS`), checked on disk
  like the main one, and `--realtime-models` lists both; `wsi-stream` passes it as
  `realtime_model_type` with `no_log_file` in both RealtimeSTT modes (`realtime_config`);
  `wsi-model-manager --download-realtime` fetches the missing ones (and `--download-auto`
  does too), run by the play's new Download The Live Preview Models task, failing if
  either cannot be fetched; `check-gpu-load.py`'s streaming leg uses the resolver's model,
  fails at once naming the play when it is not on disk, and its child exits at once on a
  recorder that fails to load. RealtimeSTT can also fail without raising: a main-model
  load error ends only its worker thread, and the constructor waits for the model for
  ever. So `wsi-stream`'s `load_recorder` (both RealtimeSTT modes) ends the process at
  once with `os._exit(1)`, ERROR and a notification that stays, on a constructor that
  raises or on any thread that dies while the recorder is built (`threading.excepthook`,
  set for that window only); the pre-buffer's pw-record is killed first
- [x] ✅ **Task 4.2**: on the host (meta-deploy): `changed-plays` picks up
  play-speech-to-text.yml (downloads tiny.en and tiny), then `gpu-load-acceptance.bash`
  again, both legs within the rule's figure. Passed at `36889021f`
  (`$_Pb6VdryWr-hG2RoCKditTB9DpomZO1RzxLTsRQzdR8`): both live preview models downloaded;
  decode peaked at 2080 MiB and streaming (tiny.en beside it) at 2184 MiB, against 2659; no
  hang, and no new `realtimesst.log` anywhere. An 87 MB one left in the home directory by
  earlier versions was found; Task 4.3 removes it.
- [x] ✅ **Task 4.3**: the play removes the `realtimesst.log` earlier versions left in the
  home directory; on the host, `changed-plays` ran play-speech-to-text.yml at `d153a3a0a` and
  the file is gone (`$jlZOEoyPuagxUS1VfJjSLGfu9erhMcBQr_mz9peM7FQ`). That run also showed
  three tasks changed on every run: ydotool restarted (cutting into a session typing through
  it) and the panel extension disabled and enabled (the panel flickers)
- [x] ✅ **Task 4.4**: the play restarts ydotool only when its override changed, and reloads
  the extension only when its files changed (or enables it when it is not enabled); on the
  host at `a08948346`, play-speech-to-text.yml reported `ok=70 changed=0`, ydotool still
  active and the extension not toggled (`$ihFoh81erl9tdE1iyZrPSQoUMuyL3KlaXFIZ2rzwURA`)

## Technical Decisions

### Decision 1: no Go port in this change

**Context**: CLAUDE.md: "a standalone Python CLI is ported to Go when it is next reworked."
**Options considered**: port the manager to Go (Bubble Tea) now; or change the Python.
**Decision**: change the Python. Both tools are thin over Python libraries with no Go
equivalent: the resolver's GPU answer is CTranslate2's own `get_cuda_device_count` (the engine
faster-whisper loads models with, so the answer cannot come from anywhere else), and the
manager's download is huggingface_hub's `snapshot_download`, which writes the cache layout
(blobs, snapshots, refs) faster-whisper reads. A Go manager would reimplement the Hugging Face
download protocol and cache format and still run Python for the GPU answer. The change is an
owner-visible feature of about 120 lines in two existing scripts. As with Plan 00208's
`wsi-stream`, the next substantive rework of the manager's screen should be the port.
**Approved by the owner, 2026-10-28** (pingbus Q6; "all recommended" to the unblocking list,
item A4): the manager and `wsi-resolve-model` stay Python as an exception to the Go-port rule,
because they are thin wrappers over CTranslate2 and huggingface_hub, which have no Go
equivalent. The same reading keeps Plan 00235's two new host-health probes in Python (its Q5).

### Decision 2: the fit rule

**Context**: a model's GPU memory need is not published per model.
**Decision**: need = 1.25 x download size (MiB) + 850 MiB, rounded up. Calibrated on
faster-whisper's README benchmark for large-v2, float16, beam 5 (4525 MiB on an RTX 3070 Ti);
the rule gives 4534. The recorders load float16 on CUDA (`wsi-stream`, `wsi-stream-server`).
The sizes are the sum of each repo's files from the Hugging Face API, in MiB. The GPU judged
is the one with the least memory nvidia-smi lists (with several, CTranslate2's device 0 may be
any of them), against its total memory: free memory would count the warm server's own model,
which the new one replaces. On a 4096 MiB card: Large v2 and v3 (4.4 GB) do not fit; Large v3
Turbo (2.7 GB), Medium and Distil Large v3.5 (2.6 GB) do.

### Decision 3: "biggest" English-only means most capable

The English-only rows are in capability order, ending with Distil Large v3.5 (a large-v3
distillation, and `auto`'s English pick on a GPU). Medium EN is 13 MiB larger on disk, so a
size order would preselect it; the catalogue order is used instead.

## Success Criteria

- [x] The tests above pass (`scripts/test-wsi-stop-grace.bash`)
- [x] `acceptance.bash` ACCEPTED on the test machine (Task 2.1, at 525560cfb)
- [x] `acceptance.bash` with its `screen` leg, and `gpu-load-acceptance.bash`, ACCEPTED on
  the test machine (Task 2.3; the streaming leg after Task 4.2)
- [x] The owner approves Decision 1's exception to the Go-port rule (2026-10-28, Q6)
- [x] The owner's look (Task 2.2) agrees (2026-10-28)
- [ ] The manager opens with a checking message and a readable GPU line (Task 2.6)

## Risks & Mitigations

| Risk                                                               | Impact | Probability | Mitigation                                                                                |
| ------------------------------------------------------------------ | ------ | ----------- | ----------------------------------------------------------------------------------------- |
| The rule under-states a need and a model that "fits" fails to load | M      | L           | Calibrated on the published figure; the load failure is still loud in the panel           |
| nvidia-smi absent on a GPU machine                                 | M      | L           | `--fit` fails naming play-nvidia.yml; the manager refuses every download with that reason |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00234-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1: the fit in the resolver and manager, and the MOK message (3f4aec19e, merged in 82fcb5a9d)
- Task 3.2: the final review's fixes (branch `fix/00234-review-findings`)
- Task 4.1: the live preview model (branch `fix/00234-gpu-load-log-cwd`)
