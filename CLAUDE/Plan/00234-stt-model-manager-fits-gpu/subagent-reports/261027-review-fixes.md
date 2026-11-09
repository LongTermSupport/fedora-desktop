# Plan 00234 worker report: the final review's fixes

Branch `fix/00234-review-findings`, from origin/F44, three commits (hashes in the reply to
the coordinator); not pushed. `./scripts/qa-changed.bash` passed before each commit; qa-all
was not run (the coordinator's).

1. **Sizes, one home.** `extension.js`'s `_whisperModels` lost its size column. Only the
   model names were ever read from it, and its sizes disagreed with `SIZE_MB`. The docs'
   model tables (`docs/features/speech-to-text.md`) now show `SIZE_MB` as the manager shows
   it (`human_mb`). `SizesHaveOneHomeTest` in `tests/speech_to_text/test_resolve_model.py`
   failed on both before the fix: a `~NNMB` size anywhere in the extension's JS or the docs
   section fails it, and so does a docs size that differs from `--fit`'s `size_mb`.
2. **The real screen.** textual cannot be imported in the QA container (`ModuleNotFoundError`).
   The play installs it only on the host (`pip --user`, play-speech-to-text.yml), so a pilot
   test in `tests/` would fail `qa-all`. `check-fit.py screen` is a new acceptance leg that
   runs the installed `ModelManagerApp` through `App.run_test()`. A subclass replaces
   `_do_download`/`_poll_progress` with recorders and records `notify`. The leg checks that
   the cursor starts on `best_english_fit`, that the too-big row's GPU cell starts with `✗`,
   and that Enter and `d` on a too-big model each show `download_refusal`'s text, with no
   download started and the list still open on that row. **Unproven until the host runs
   it**: the leg was written from the manager's source and the Textual API, and has never
   been executed. Task 2.1 now says its pass called the helpers only.
3. **Status.** The PLAN.md header, the acceptance criterion (ticked, at 525560cfb), the
   new Task 2.3 and Task 3.x, and the README row.
4. **Real load.** `gpu-load-acceptance.bash` (deploy mode, because it may download) and
   `check-gpu-load.py`. Legs: download distil-large-v3.5 if it is missing; a beam-5
   transcription of 30 s of noise via `WhisperModel` (cuda, float16); wsi-stream's
   prebuffered `AudioToTextRecorder` config, so the real-time model loads too. Each load
   runs in a child with `HF_HUB_OFFLINE=1`. The parent samples nvidia-smi `memory.used`
   every 0.2 s, and the check fails if the peak above the baseline exceeds `needs_mb` for
   distil-large-v3.5 (2659 MiB). Also unexecuted.
5. **MOK.** `tasks/secure-boot-mok-enrolment-steps.yml` is included by play-nvidia.yml and
   play-displaylink.yml with `mok_module`. The password is now described as the one kept in
   the vault since a play first asked for it. Syntax checks pass, and the Jinja for the
   capitalised module name was rendered with ansible's jinja2.
6. **Decision 1** is marked as awaiting the owner's approval, and is a success criterion.

Queued in `meta-deploy.list` after `changed-plays`: the plan folder (`acceptance.bash`),
then `00234-stt-model-manager-fits-gpu/gpu-load-acceptance.bash`.
