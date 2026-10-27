# Plan 00234 worker report: the model manager fits the GPU; play-nvidia MOK steps

Branch `fix/stt-model-fits-gpu`, one commit on F44 (its hash is in the reply to the
coordinator). `./scripts/qa-changed.bash` passed over the staged tree (12 files, including
`scripts/test-wsi-stop-grace.bash`, 121 passed). qa-all was not run (the coordinator's).

## What changed

- `files/home/.local/bin/wsi-resolve-model --fit`: one JSON object (device, GPU name and
  total MiB from nvidia-smi, the smallest GPU when there are several; per model `size_mb`,
  `needs_mb`, `fits`). It is now the only place the model sizes and the fit rule live
  (`SIZE_MB`, `NEED_FACTOR` 1.25, `NEED_OVERHEAD_MB` 850, calibrated on faster-whisper's
  published large-v2 fp16 figure, 4525 MiB; the rule gives 4534). It fails with exit 1 and a
  reason when CTranslate2's GPU check fails, nvidia-smi is missing (naming play-nvidia.yml),
  nvidia-smi fails, or its answer cannot be read. With no GPU CTranslate2 can use and no
  NVIDIA device nodes it reports the CPU, and every model fits.
- `files/home/.local/bin/wsi-model-manager`: a GPU memory column (`✓ fits (2.6 GB)` /
  `✗ too big (4.4 GB)` / `CPU`), sizes from the resolver (the old size strings were wrong for
  four rows), a status line naming the GPU and its memory, or saying there is no usable NVIDIA
  GPU and the models run on the CPU. The cursor starts on the most capable English-only model
  that fits. The Download button and Enter both go through one `_start_download`. It refuses a
  model that does not fit, saying what the model needs, what the GPU has, and which model to
  pick, and the list stays open. When the fit cannot be told, every download is refused with
  the reason.
- `play-nvidia.yml`: the MOK message walks each blue screen by its title. It warns about the
  10-second "Press any key" screen, says what to do when that screen is missed, and says
  nothing shows while the password is typed.

## Decisions for the coordinator to check

1. No Go port (PLAN.md Decision 1): both tools are thin over CTranslate2 and huggingface_hub,
   which have no Go equivalent. A Go manager would reimplement the HF download protocol and
   cache format, and would still run Python for the GPU answer. The port is recommended for
   the next substantive rework of the manager's screen. Overrule if you disagree.
2. "Biggest English-only" is taken in capability order (Distil Large v3.5). Medium EN is
   13 MiB larger on disk.
3. `auto` is unchanged. Its GPU picks fit any 4 GB card by this rule.

## Not verified here

textual is not installed in the container, so the TUI was not run. The pure functions are
unit tested, and the Textual calls used (`DataTable.move_cursor(row=)`,
`notify(title=, severity=, timeout=)`) were checked by reading only. `acceptance.bash` loads
the installed manager on the host. The screen itself is the owner's look (PLAN.md Task 2.2).

## Host

`meta-deploy.list`: the `changed-plays` comment now names play-speech-to-text.yml and
play-nvidia.yml, and a new entry `00234-stt-model-manager-fits-gpu` runs its read-only
`acceptance.bash` (installed copies are the checkout's; `--fit` names the real GPU; the
installed manager preselects and refuses).
