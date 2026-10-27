# Plan 00234: the speech-to-text model manager offers only the models that fit this GPU

**Status**: In Progress (review fixes made; the screen and real-GPU checks queued for the host; the owner's look and Decision 1's approval pending)
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
- `--download-auto` (the unattended download of `auto`'s picks) is unchanged.

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
- [ ] ⬜ **Task 2.3**: the review fixes on the host (meta-deploy): `acceptance.bash` again,
  with its new `screen` leg driving the installed manager's real Textual screen headless
  (`App.run_test`; textual is installed by the play but not in the QA container, so this
  cannot run in `qa-all`): the cursor on the preselected model, the too-big row marked, Enter
  and the Download key on a too-big model refused with the list left open, no download
  started. Then `gpu-load-acceptance.bash`: distil-large-v3.5 loaded on the real GPU in
  float16, once as wsi-stream-server loads it (beam 5 over 30 s of noise) and once with
  wsi-stream's own streaming settings (real-time transcription on, the real-time model
  beside it), each peak GPU memory reported against the fit rule's figure and failing above
  it. It downloads distil-large-v3.5 if it is missing, nothing else.
- [ ] ⬜ **Task 2.2**: The owner's look: open Manage Whisper Models... and check the status
  line names the NVIDIA GPU and its memory, the cursor is on Distil Large v3.5, Large v2 and
  Large v3 show `✗ too big`, and choosing one shows the refusal and leaves the list open

### Phase 3: Review

- [x] ✅ **Task 3.1**: qa-reviewer over the branch diff: FIX-BEFORE-MERGE, six findings
  (`subagent-reports/261027-final-qa-review.md`)
- [x] ✅ **Task 3.2**: the review's fixes: the panel's and docs' sizes (finding 1), the
  screen check and Task 2.1's wording (2), this plan's status (3), the real-GPU load check
  (4), the shared MOK message and the vault wording (5); Decision 1 recorded as awaiting
  the owner (6)

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
**Awaiting the owner's approval**: CLAUDE.md makes the Go port a hard rule, so this exception
stands only once the owner approves it (the final review's finding 6).

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
- [ ] `acceptance.bash` with its `screen` leg, and `gpu-load-acceptance.bash`, ACCEPTED on
  the test machine (Task 2.3)
- [ ] The owner approves Decision 1's exception to the Go-port rule
- [ ] The owner's look (Task 2.2) agrees

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
