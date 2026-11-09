# Task 1.6: speech-to-text without an NVIDIA GPU

Branch `fix/00229-stt-no-gpu`, cut from F44. One file of code changed:
`playbooks/imports/optional/common/play-speech-to-text.yml`.

## What changed

- **Header comment.** It no longer says the play needs play-nvidia.yml. It now says a GPU is
  optional, and what each case gets: no node means no CUDA wheels, the CPU, and auto picking
  small for batch and base for streaming. With a node, the driver must already be loaded,
  the cu12 wheels go on the wrapper's library path, auto picks distil-large-v3.5 for English
  and large-v3-turbo for other languages, and a GPU that CTranslate2 cannot see fails the play.
- **One GPU fact.** `Find NVIDIA GPU Device Nodes` (`/dev`, `nvidia[0-9]*`) moved up, before
  any pip install. The same register `nvidia_gpu_nodes` still drives the later
  `Verify CTranslate2 Sees The GPU` check. The play has no lspci convention.
- **Install split.** `faster-whisper==1.2.1` and `textual>=0.85.0` are installed on every
  host. `nvidia-cublas-cu12` and `nvidia-cudnn-cu12==9.*` are installed only when
  `nvidia_gpu_nodes.matched > 0`.
- **`Find CUDA Library Paths`** runs only on a GPU host. It is now `command: argv` with a
  plain import. The old `try/except` that printed an empty path on `ImportError` was a silent
  failure, so it is gone: the wheels were installed one task earlier, and a failed import now
  fails the play.
- **`faster-whisper-transcribe` wrapper.** The `LD_LIBRARY_PATH` export sits inside
  `{% if nvidia_gpu_nodes.matched > 0 %}`. I rendered it with ansible-core's own Templar
  (`trust_as_template`). With a GPU, the output is byte-identical to the old wrapper. Without
  one, it is just `#!/bin/bash` followed by the `exec`.

No helper was needed. The logic is two `when:` conditions and one Jinja `if`, with no shell
branching.

## Consumers of cuda_lib_paths

Only the `faster-whisper-transcribe` wrapper uses it. `grep` across `files/`, `src-go/`,
`extensions/` and `playbooks/` finds no other `LD_LIBRARY_PATH`, `cublas` or `cudnn` use:

- `wsi-stream-server` chooses its device with `ctranslate2.get_cuda_device_count()`.
- The systemd units and `stt-watch.service.j2` set no library path.
- `faster-whisper-transcribe.py` falls back to the CPU on any CUDA exception.

## Behaviour

- **GPU host** (has `/dev/nvidiaN`): the same packages, the same wrapper, the same checks as
  before. Nothing changes on disk, so no host run is queued in meta-deploy.
- **No GPU:** no cu12 wheels from this play and no `LD_LIBRARY_PATH` line. CTranslate2 counts
  0, the verify check passes, and auto picks small (batch) or base (streaming) on the CPU.
- **A host that already ran the old play without a GPU:** its wrapper is rewritten without
  the line. The wheels already installed stay; the play does not uninstall them.

## Findings outside this change (not fixed)

1. **torch still pulls CUDA wheels.** RealtimeSTT 1.0.0 requires unpinned `torch`. PyPI's
   torch 2.14.1 requires `nvidia-cudnn-cu13`, `nvidia-cusparselt-cu13`, `nvidia-nccl-cu13` and
   `nvidia-nvshmem-cu13` on every Linux machine. So a laptop with no GPU still downloads cu13
   CUDA libraries through the streaming install. Avoiding that means taking torch from the
   PyTorch CPU index, which is not PyPI. That is the owner's decision.
2. **CPU float16 in the direct modes.** `wsi-stream`'s standard and pre-buffered modes (the
   RealtimeSTT direct modes, not the server) always ask for `device=cuda`,
   `compute_type=float16`, and leave the fallback to RealtimeSTT. I could not check here
   whether CTranslate2 accepts float16 on the CPU after that fallback (RealtimeSTT is not
   installed in the container). Server mode is clean: it uses int8 on the CPU.
3. **GPU present, driver not yet loaded.** A GPU host where play-nvidia.yml ran but the
   machine has not rebooted has no nodes yet. This play then takes the CPU path. Re-running
   it after the reboot installs the wheels. The header says to reboot first.

## Host check (pending)

Run the play on a machine with no NVIDIA GPU, likely the owner's consumer laptop. That is
another machine, so it is asked for separately and not through meta-deploy. Expected:

- the cu12 tasks show as skipped;
- `~/.local/bin/faster-whisper-transcribe` has no `LD_LIBRARY_PATH`;
- a dictation works on the CPU model.
