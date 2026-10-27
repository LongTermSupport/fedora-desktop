# Plan 00234: the qa-reviewer final review of the merged branch

**Verdict: FIX-BEFORE-MERGE.** The branch is already merged, so these need a follow-up.

1. **Size data still lives in two places.** `extensions/speech-to-text@fedora-desktop/extension.js:123-135` keeps its own sizes, and `docs/features/speech-to-text.md:186-204` is generated from them. Several now disagree with `SIZE_MB` (tiny.en 41 vs 75 MB, medium.en 789 vs 1460). The plan's "one home" goal (PLAN.md:35) is false, and the panel and docs contradict the manager.

2. **The real screen was never tested.** The tests replace textual with stand-ins (test_model_manager_installed.py:46), and `check-fit.py:63-81` calls only the helper functions. So the cursor move (wsi-model-manager:357-359), `_start_download`'s refusal (:471) and `_poll_progress` (:498) never ran. Task 2.1 still says "the installed manager preselects and refuses". Fix: add a Textual `run_test` test, or reword Task 2.1.

3. **The plan's status lags.** PLAN.md:124 (acceptance ACCEPTED) is unticked although Task 2.1 (:74-78) records the pass. The header and README.md:37 still say "host acceptance pending".

4. **The fit rule was never checked against a real model load.** The acceptance loads nothing (acceptance.bash:11). The calibration is one batch, beam-5 figure, but streaming runs with real-time transcription on (wsi-stream:1919), and the rule judges total memory, not free. One real load on the 4 GB card would settle it.

5. **The old MOK text survives in `play-displaylink.yml:210-223`.** Also, "you set earlier in this play" (play-nvidia.yml:300) is wrong on a rerun, when the password comes from the vault.

6. **Decision 1 grants itself an exception to the hard Go-port rule.** The owner needs to approve it, and the plan needs to record that.

Checked and clean: stderr hygiene, failing fast (nvidia-smi missing, failing or unreadable; the GPU-count check), refusal leaves the list open, every offered model has a size (tested), no leaked identifiers.

Gates: wsi tests passed 121, failed 0. Play syntax check OK. plan-qa: 0 block. `qa-all` passed its first 15 gates, then hit my 900 s timeout (exit 124), so it is not a complete pass.