# Research: newer speech-to-text models and engines (Task 7.1)

Read-only research. No code was edited and nothing was installed or run against audio.
Sources were read between 2026-10-02 and 2026-10-03. Every claim is tagged **VERIFIED**
(with a source) or **INFERRED** (reasoned, not measured). Leaderboard figures come from
the raw result CSVs behind the Hugging Face Open ASR Leaderboard, not from summaries.

Answer in one line: the field has moved a long way. The quick win is to make
`large-v3-turbo` (any language) or `distil-large-v3.5` (English) the GPU default in the
existing engine. The engine worth adding as an option is **NVIDIA Parakeet TDT 0.6B v2/v3
through `onnx-asr`**. It is more accurate than any Whisper model we offer, adds
punctuation and casing, and runs faster than real time **on the CPU**, so it does not need
the NVIDIA stack.

---

## 1. What runs today (from the repo)

| Aspect                         | Today                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Evidence                                                                             |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Engine, batch (`wsi`)          | faster-whisper (CTranslate2), called through the generated `faster-whisper-transcribe.py`                                                                                                                                                                                                                                                                                                                                                                                                               | `play-speech-to-text.yml:106-116`, `:148-208`; `wsi:31`, `:787-799`                  |
| Engine, streaming/server       | RealtimeSTT `AudioToTextRecorder`, which uses faster-whisper underneath                                                                                                                                                                                                                                                                                                                                                                                                                                 | `play-speech-to-text.yml:233-240`; `wsi-stream-server:114-146`; `wsi-stream:696-718` |
| Optional remote engine         | A whisper.cpp HTTP server (`wsi -n` / `wsi IP:PORT`), posting the WAV to it                                                                                                                                                                                                                                                                                                                                                                                                                             | `wsi:414`, `:422`, `:776-783`                                                        |
| Models offered                 | tiny, base, small, medium, large-v2, large-v3, large-v3-turbo, tiny.en, base.en, small.en, medium.en (plus `auto`)                                                                                                                                                                                                                                                                                                                                                                                      | `extension.js:75-90`; `wsi-model-manager:52-66`                                      |
| Default model                  | `auto` (`extension.js:45`, schema default `'auto'`) = **`small` for batch** (`stt_model` default, `play-speech-to-text.yml:21`, baked into the script at `:174`) and **`base` for streaming and server mode** (`wsi-stream:447`, `:653`, `:908`; `wsi-stream-server:681`)                                                                                                                                                                                                                               | as cited                                                                             |
| What server mode really pastes | RealtimeSTT's realtime model, which defaults to **`tiny`**, whatever model is chosen (already Task 4.1)                                                                                                                                                                                                                                                                                                                                                                                                 | `RESEARCH-120s-limit.md` section 1                                                   |
| Device / compute type          | GPU first: `cuda` + `float16`. Batch falls back to `cpu` + `int8` on any exception (`play-speech-to-text.yml:177-182`). Streaming requests `cuda`/`float16` and leaves fallback to RealtimeSTT (`wsi-stream:713-716`). The server maps `auto`/`gpu` to cuda/float16, `cpu` to int8 (`wsi-stream-server:134-142`)                                                                                                                                                                                        | as cited                                                                             |
| Decoding                       | Batch: `beam_size=5`, fixed language, **no `vad_filter`** (`play-speech-to-text.yml:185`). Streaming: RealtimeSTT defaults                                                                                                                                                                                                                                                                                                                                                                              | as cited                                                                             |
| VAD                            | RealtimeSTT's Silero (`silero_sensitivity: 0.4`, `wsi-stream-server:127`, `wsi-stream:701`). Plan 00148 will use faster-whisper's bundled Silero ONNX                                                                                                                                                                                                                                                                                                                                                   | as cited; `RESEARCH-120s-limit.md` 3.1                                               |
| Language                       | `en` by default (`stt_language`, `play-speech-to-text.yml:24`; `wsi-stream:448`; server `:683`); the panel can override                                                                                                                                                                                                                                                                                                                                                                                 | as cited                                                                             |
| Model download                 | Hugging Face cache `~/.cache/huggingface/hub/`. faster-whisper downloads on first use by name; `wsi-model-manager` pre-fetches with `snapshot_download(token=False)` (`wsi-model-manager:68`, `:325`). The server sets `HF_DATASETS_OFFLINE=1` (`wsi-stream-server:29`)                                                                                                                                                                                                                                 | as cited                                                                             |
| Versions                       | `faster-whisper`, `RealtimeSTT`, `nvidia-cublas-cu12`, `nvidia-cudnn-cu12==9.*` are installed with `pip --user`, **unpinned**. CUDA libraries reach the batch path through `LD_LIBRARY_PATH` in a wrapper                                                                                                                                                                                                                                                                                               | `play-speech-to-text.yml:106-146`                                                    |
| Hardware the repo assumes      | An NVIDIA GPU. The play header says "Prerequisites: NVIDIA drivers installed via play-nvidia.yml" (`:5`) and installs the CUDA wheels unconditionally (`:111-113`); there is no GPU detection or CPU-only branch. The docs call the GPU "Recommended" and list GTX 1050 Ti (2 GB) to RTX 3060+ (12 GB) tiers (`docs/features/speech-to-text.md:69-80`). CPU is a fallback only, described as "considerably slower" (`:72-74`). RealtimeSTT pulls in PyTorch (~2 GB) (`play-speech-to-text.yml:216-217`) | as cited                                                                             |

Small defects found on the way (all VERIFIED):

- `wsi-model-manager:58` lists `large-v3-turbo` as "~800MB", and its progress maths uses
  800 MB (`:80`). The CTranslate2 `model.bin` is **1,617,884,929 bytes**
  ([HF tree API, read 2026-10-03](https://huggingface.co/api/models/mobiuslabsgmbh/faster-whisper-large-v3-turbo/tree/main)).
  `extension.js:83` and the docs say ~1.6 GB, which is right.
- Both lists call turbo "Distilled". It is not: it is "a finetuned version of a pruned
  Whisper large-v3 … the number of decoding layers have reduced from 32 to 4"
  ([openai/whisper-large-v3-turbo card](https://huggingface.co/openai/whisper-large-v3-turbo)).
- The turbo repo `mobiuslabsgmbh/faster-whisper-large-v3-turbo` now **redirects** to
  `dropbox-dash/faster-whisper-large-v3-turbo` (HTTP 307 from the HF API, 2026-10-03).
  faster-whisper 1.2.1 still uses the old name (`faster_whisper/utils.py:29-30`
  at tag v1.2.1). It works while the redirect lasts.
- **RealtimeSTT 1.1.0 to 1.1.2 declare `Requires-Python <3.13,>=3.11`**; 1.0.4
  (2026-08-20) is the newest without an upper bound
  ([PyPI JSON](https://pypi.org/pypi/RealtimeSTT/json)). The play targets Python 3.14
  (`play-speech-to-text.yml:56`, `:227-231`), so pip on the host would resolve **1.0.4 or
  older**, not the 1.1.2 that `RESEARCH-120s-limit.md` read (INFERRED; Task 1.2's
  `pip show` settles it). Task 4.6 must pin a version that installs on 3.14.

---

## 2. The landscape (October 2026)

### 2.1 How to read the numbers

- **WER** is the leaderboard's average word error rate (lower is better). It is computed
  after text normalisation that lowercases and strips punctuation (VERIFIED, stated in the
  [distil-large-v3.5 card](https://huggingface.co/distil-whisper/distil-large-v3.5)), so
  **it does not measure punctuation or casing**, which matter for pasted dictation.
- **RTFx** is seconds of audio per second of compute, measured on a data-centre GPU with
  batching. It ranks models by throughput but is not dictation latency on a desktop
  (INFERRED).
- Two leaderboard snapshots are quoted, because the board changed its dataset mix in
  mid 2026 and some older models (Whisper small/base, Moonshine) are not on the new mix.
  **Do not compare numbers across the two columns.**
  - **May-26**: `english_shortform_results.csv` at commit `7c92e785` (2026-05-12), the
    classic 8-set average (AMI, Earnings22, GigaSpeech, LibriSpeech clean/other,
    SPGISpeech, TED-LIUM, VoxPopuli).
  - **Oct-26**: `english_short_latest.csv` on `main` (dataset last modified 2026-10-01;
    leaderboard version "02-10-2026"). It uses cleaned sets plus Voice Arena and private
    sets.
  - Source for both:
    [hf-audio/open-asr-leaderboard-results](https://huggingface.co/datasets/hf-audio/open-asr-leaderboard-results)
    (VERIFIED, read 2026-10-03).

### 2.2 Leaderboard extract (open-weight models relevant here)

| Model                                             | Params | Licence     | WER May-26 | WER Oct-26 | RTFx Oct-26 | Engine on Linux                    |
| ------------------------------------------------- | ------ | ----------- | ---------- | ---------- | ----------- | ---------------------------------- |
| openai/whisper-tiny.en                            | 0.04B  | Apache-2.0  | 12.81      | not listed |             | faster-whisper (**offered**)       |
| openai/whisper-base.en                            | 0.07B  | Apache-2.0  | 10.32      | not listed |             | faster-whisper (**offered**)       |
| openai/whisper-small.en                           | 0.2B   | Apache-2.0  | 8.59       | not listed |             | faster-whisper (**offered**)       |
| openai/whisper-medium.en                          | 0.8B   | Apache-2.0  | 8.09       | not listed |             | faster-whisper (**offered**)       |
| openai/whisper-large-v3-turbo                     | 0.8B   | MIT         | 7.83       | 6.36       | 797         | faster-whisper (**offered**)       |
| openai/whisper-large-v3                           | 1.55B  | Apache-2.0  | 7.44       | 5.78       | 470         | faster-whisper (**offered**)       |
| distil-whisper/distil-large-v3.5 (En)             | 0.76B  | MIT         | 7.21       | 5.40       | 879         | faster-whisper (not offered)       |
| usefulsensors/moonshine-streaming-small           | 0.12B  | MIT         | 7.84       | not listed |             | moonshine-voice                    |
| usefulsensors/moonshine-streaming-med.            | 0.24B  | MIT         | 6.66       | not listed |             | moonshine-voice                    |
| kyutai/stt-2.6b-en                                | 2.6B   | CC-BY-4.0   | 6.40       | 5.57       | 133         | moshi (PyTorch / Rust server)      |
| mistralai/Voxtral-Mini-3B-2507                    | ~5B    | Apache-2.0  | 7.05       | 5.54       | 181         | vLLM / transformers                |
| mistralai/Voxtral-Mini-4B-Realtime-2602           | 4B     | Apache-2.0  | 7.68       | 6.46       | 103         | vLLM                               |
| mistralai/Voxtral-Small-24B-2507                  | 24B    | Apache-2.0  | 6.62       | 4.99       | 101         | vLLM                               |
| microsoft/Phi-4-multimodal-instruct               | 6B     | MIT         | 6.02       | 5.02       | 163         | transformers + flash-attn          |
| nvidia/parakeet-tdt-0.6b-v2 (En)                  | 0.6B   | CC-BY-4.0   | 6.05       | 4.70       | 6025        | NeMo; **onnx-asr**; sherpa-onnx    |
| nvidia/parakeet-tdt-0.6b-v3 (25 langs)            | 0.6B   | CC-BY-4.0   | 6.32       | 4.86       | 6076        | NeMo; **onnx-asr**; sherpa-onnx    |
| nvidia/canary-1b-v2                               | 1B     | CC-BY-4.0   | 7.15       | 5.71       | 1825        | NeMo; onnx-asr                     |
| nvidia/canary-qwen-2.5b                           | 2.5B   | CC-BY-4.0   | 5.63       | 4.43       | 867         | NeMo                               |
| nvidia/nemotron-speech-streaming-en-0.6b          | 0.6B   | NVIDIA open | n/a        | 5.25       | 1167        | NeMo                               |
| ibm-granite/granite-speech-4.1-2b                 | 2B     | Apache-2.0  | 5.33       | 4.62       | 546         | transformers                       |
| ibm-granite/granite-speech-5.0-470m-turboctc (En) | 0.47B  | Apache-2.0  | 5.04       | 5.03       | 20946       | transformers ≥5.16; transcribe.cpp |
| Qwen/Qwen3-ASR-1.7B                               | 1.7B   | Apache-2.0  | 5.76       | 4.31       | 820         | qwen-asr (PyTorch), vLLM           |
| Qwen/Qwen3-ASR-0.6B                               | 0.6B   | Apache-2.0  | 6.42       | 5.04       | 744         | qwen-asr (PyTorch), vLLM           |
| CohereLabs/cohere-transcribe-03-2026              | 2B     | Apache-2.0  | n/a        | 4.67       | 907         | transformers                       |

All rows VERIFIED from the two CSVs (sizes, licences and encoder types are the CSVs' own
columns). For scale, the best proprietary APIs are at 3.6 to 4.3 on Oct-26.

What this says (INFERRED from the table):

- Everything we ship by default (base for streaming, small for batch, and tiny in server
  mode) sits at the **bottom of the board**. Moving from `small.en` (8.59) to
  `distil-large-v3.5` (7.21) or `large-v3-turbo` (7.83) is a relative WER cut of about
  9 to 16 %. Moving to Parakeet TDT v2 (6.05) is about **30 %**.
- The accuracy leaders now pair a speech encoder with an LLM decoder (Canary-Qwen,
  Granite Speech, Qwen3-ASR, Cohere). They are 1.7B to 2.5B parameters and need PyTorch
  and a few GB of VRAM.
- Parakeet TDT 0.6B is the best accuracy per parameter and per watt. It is the only
  top-tier model with a mature **CPU** path.

### 2.3 Per family

**Whisper large-v3 / turbo / distil-whisper (drop-in through faster-whisper).**

- faster-whisper 1.2.1 already maps `turbo`/`large-v3-turbo` and **`distil-large-v3.5`**
  to CTranslate2 repos (VERIFIED, `faster_whisper/utils.py` at tag v1.2.1). The
  distil-large-v3.5 alias is not in 1.1.0.
- distil-large-v3.5 is **English-only**, 756M parameters, "1.46x" the speed of turbo,
  with short-form OOD WER 7.08 against 7.30 for turbo
  ([card](https://huggingface.co/distil-whisper/distil-large-v3.5), updated 2026-04-13,
  VERIFIED). Its CT2 `model.bin` is 1,512,927,867 bytes
  ([distil-large-v3.5-ct2](https://huggingface.co/distil-whisper/distil-large-v3.5-ct2),
  VERIFIED).
- Punctuation and casing: native in Whisper (VERIFIED by use; this is why the repo needs
  no punctuation step). Whisper still hallucinates on silence and drifts over long
  context (`RESEARCH-120s-limit.md` section 2). Plan 00148's VAD segmenting reduces
  both (INFERRED).
- faster-whisper's own benchmark: `small` on CPU int8 transcribes 13 min in 1m42s (about
  7.6x real time, i7-12700K, 8 threads). `large-v2` int8 on GPU uses 2926 MB VRAM against
  4525 MB for fp16, at the same speed
  ([README](https://github.com/SYSTRAN/faster-whisper), benchmarked with v1.1.0;
  VERIFIED).

**NVIDIA Parakeet TDT 0.6B v2 / v3.**

- v2 is English. v3 adds 24 more European languages (VERIFIED,
  [v3 card](https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3), updated 2026-08-05).
- Automatic punctuation and capitalisation (VERIFIED, same card). CC-BY-4.0
  (attribution required).
- Non-autoregressive TDT decoding (VERIFIED, CSV). It cannot loop or repeat the way
  Whisper does (INFERRED from the architecture).
- No prompt or `initial_prompt` equivalent (INFERRED). Plan 00148's "short initial
  prompt from committed text" trick does not carry over.
- CPU path through **onnx-asr** (v0.12.0, 2026-07-15, Python 3.10 to 3.14, NumPy in,
  no PyTorch, built-in Silero VAD, CUDA/TensorRT providers optional;
  [README](https://github.com/istupakov/onnx-asr), VERIFIED).
  - Its benchmark gives Parakeet v2/v3 an RTFx of **36 on a Ryzen 7 9800X3D CPU**, 57 on
    a T4, 320 on an RTX 5070 Ti with TensorRT. Whisper large-v3-turbo on the same CPU
    is 3.9 (5.4 int8) (VERIFIED, README and
    [benchmarks page](https://istupakov.github.io/onnx-asr/benchmarks/), page undated).
  - onnx-asr notes "maximum audio length for most models is 20–30 seconds; use VAD for
    longer" (VERIFIED). That matches Plan 00148's segments of at most 28 s.
- Also in sherpa-onnx (v1.13.8, 2026-09-10), with int8 builds (VERIFIED, README).
- Adoption signal: Handy (open-source desktop dictation app, about 32.7k stars, v0.9.8 on
  2026-10-03) offers "Parakeet V3 – CPU-optimized model with excellent performance" next
  to Whisper on Linux (VERIFIED, [README](https://github.com/cjpais/Handy)).

**Canary (1B v2, 1B flash, 180M flash, Qwen-2.5B).**

- Canary-Qwen-2.5B is the most accurate open NVIDIA model (4.43). It needs NeMo and
  PyTorch (VERIFIED, card/CSV). NeMo 3.0.0 came out 2026-08-07 (PyPI).
- Canary-1b-v2 runs in onnx-asr at a CPU RTFx of only 8 (VERIFIED, README), and is less
  accurate than Parakeet on English.

**Moonshine (v2 / "Streaming").**

- MIT licence. Tiny 34M, Small 123M, Medium 245M (VERIFIED,
  [Moonshine v2 paper](https://download.moonshine.ai/docs/moonshine_streaming_paper.pdf),
  early 2026, references dated 2026-01-29).
- Medium: WER 6.7 at 245M, against Whisper large-v3 at 7.2 and Medium at 8.1 (paper's
  Open ASR table). Time to first token is 130 ms against 2186 ms for turbo, on a MacBook
  M3 (VERIFIED, paper Table 3).
- English for the MIT streaming models; other languages are legacy and non-commercial
  (VERIFIED, [repo README](https://github.com/moonshine-ai/moonshine)).
- `moonshine-voice` 0.1.5 (2026-08-24) is a young, whole-pipeline library (microphone,
  VAD, streaming) (VERIFIED, PyPI/GitHub).
- Punctuation and casing quality: **not established**.

**Kyutai STT.**

- `stt-2.6b-en` (English, 2.5 s delay) and `stt-1b-en_fr` (0.5 s delay, semantic VAD).
  CC-BY-4.0, punctuation and casing, true streaming (VERIFIED,
  [card](https://huggingface.co/kyutai/stt-2.6b-en)). Runs through `moshi` (PyTorch,
  Python \<3.15) or a Rust server.
- GitHub repo last pushed 2026-01-26 (VERIFIED, `gh api`), so development looks quiet.
- WER 5.57 (Oct-26) at RTFx 133.

**Mistral Voxtral.**

- Mini 3B (2507) and Small 24B are audio LLMs, recommended with vLLM (VERIFIED, cards).
- Voxtral Mini 4B Realtime (2602) streams natively with a configurable delay of 80 ms to
  2.4 s, and claims offline-level accuracy at 480 ms. It supports 13 languages and has
  "library_name: vllm" (VERIFIED,
  [card](https://huggingface.co/mistralai/Voxtral-Mini-4B-Realtime-2602)). Apache-2.0.
  On the board it is behind turbo-class models (6.46).
- These need a GPU with several GB of free VRAM and a vLLM server (INFERRED from the
  4B/24B sizes).

**IBM Granite Speech.**

- 4.1-2B: Apache-2.0, multilingual, "Punctuation and truecasing … with a simple prompt
  change", `transformers>=4.52.1` (VERIFIED,
  [card](https://huggingface.co/ibm-granite/granite-speech-4.1-2b)). Best Apache-licensed
  accuracy that is plainly documented with punctuation (4.62).
- 5.0-470M TurboCTC (2026-08): English, CTC greedy, Apache-2.0 (a `-nc` variant is
  CC-BY-NC-SA). RTFx about 21,000. Runs in transformers ≥5.16 or as GGUF through
  `handy-computer/transcribe.cpp` on CPU/Vulkan/CUDA (VERIFIED,
  [card](https://huggingface.co/ibm-granite/granite-speech-5.0-470m-turboctc), modified
  2026-10-02). Whether it outputs punctuation and casing: **not established**.

**Microsoft Phi-4-multimodal.**

- A 6B general multimodal LLM, MIT. The card pins `flash_attn==2.7.4.post1` (VERIFIED,
  [card](https://huggingface.co/microsoft/Phi-4-multimodal-instruct)). WER 5.02 is no
  better than Parakeet at ten times the size.

**Others found.**

- Qwen3-ASR 1.7B / 0.6B (2026-01, Apache-2.0, 52 languages/dialects, `qwen-asr`
  package, vLLM for streaming). The 1.7B is the best open model on Oct-26 (4.31)
  (VERIFIED, card, CSV).
- Cohere Transcribe 03-2026 (2B, Apache-2.0, 4.67) (VERIFIED, CSV).
- Nemotron 3.5 ASR Streaming 0.6B (40 locales, OpenMDW licence, 7.88) (VERIFIED, card,
  CSV).
- TheStageAI thewhisper-large-v3-turbo (4.54). It needs a TheStage access token, Python
  3.10 to 3.12, and named data-centre or 4090/5090 GPUs (VERIFIED,
  [card](https://huggingface.co/TheStageAI/thewhisper-large-v3-turbo)).

### 2.4 Engine releases

| Engine         | Latest                                                                                                                              | Note                                                                                                                                                                                    |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| faster-whisper | **1.2.1, 2025-10-31**; no release since. `main` has Silero VAD v6.2 and new VAD parameters (2025-11) and a PyAV 19 fix (2026-09-30) | Maintained, but slow to release (VERIFIED, GitHub releases/commits). Needs CUDA 12 + cuDNN 9 (VERIFIED, README)                                                                         |
| CTranslate2    | 4.8.2, 2026-08-31; cp314 manylinux wheels published                                                                                 | Lazy converter import (no torch for inference), GCC 15 fixes (VERIFIED, release notes, PyPI)                                                                                            |
| whisper.cpp    | v1.9.4, 2026-09-11                                                                                                                  | Same Whisper models; Vulkan/CUDA/CPU backends. On CPU, `small` is a little faster than faster-whisper fp32 but slower than int8 (VERIFIED, faster-whisper README table, older versions) |
| sherpa-onnx    | v1.13.8, 2026-09-10                                                                                                                 | Parakeet, Moonshine, Whisper, streaming Zipformer; VAD; Python and C APIs (VERIFIED)                                                                                                    |
| onnx-asr       | v0.12.0, 2026-07-15                                                                                                                 | Parakeet v2/v3, Canary, Whisper; Python 3.10 to 3.14 (VERIFIED)                                                                                                                         |
| RealtimeSTT    | 1.1.2, 2026-08-30 (Python \<3.13)                                                                                                   | See section 1 side finding                                                                                                                                                              |

---

## 3. Fit to this system

What the use case needs (INFERRED from the plan and scripts):

- Push-to-talk, paste once at stop, warm server, and VAD segments of at most 28 s (Plan
  00148).
- Low **stop-to-paste** latency: only the last segment is still being transcribed when the
  user stops.
- Good punctuation and casing, because the text is pasted as is (unless the Claude
  post-process is used).
- Mostly English (`en` default).

True streaming models (Kyutai, Voxtral Realtime, Nemotron streaming, Moonshine streaming)
mostly help a live preview, which Plan 00148 rules out. With segments of at most 28 s, a
fast offline model on the last segment gives the same stop latency (INFERRED).

| Option                                                         | Drop-in via faster-whisper?                    | Expected accuracy change vs today's defaults                                      | Latency / resources (INFERRED unless noted)                                                                                                                                                                                                                                                                        |
| -------------------------------------------------------------- | ---------------------------------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `large-v3-turbo`                                               | **Yes** (already offered)                      | Lower WER than small/base/tiny (May-26: 7.83 vs 8.59 / 10.32 / 12.81)             | About 1.6 GB on disk; fp16 VRAM likely 2 to 3 GB, less with `int8_float16`. A 20 s segment should take well under 1 s on a mid-range GPU. Too slow on the CPU (RTFx 4 to 5, onnx-asr figure)                                                                                                                       |
| `distil-large-v3.5` (English)                                  | **Yes** (name built into faster-whisper 1.2.x) | Better than turbo on English (7.21 vs 7.83; Oct-26 5.40 vs 6.36)                  | About 1.5 GB; about 1.5x turbo's speed (card). Trained on 30 s windows, which matches segments of at most 28 s                                                                                                                                                                                                     |
| `large-v3`                                                     | Yes (offered)                                  | Slightly better than turbo                                                        | About twice turbo's VRAM; slower (32 decoder layers vs 4)                                                                                                                                                                                                                                                          |
| Parakeet TDT v2 (En) / v3 (multi)                              | **No**: new engine (`onnx-asr`), different API | About 30 % fewer errors than small.en; better than large-v3 (6.05 vs 7.44 May-26) | CPU RTFx about 36 (desktop Ryzen), so a 20 s segment takes about 0.6 s **with no GPU**. About 0.6B parameters (ONNX roughly 2.4 GB fp32, about 0.7 GB int8, sizes not checked). No PyTorch. `onnxruntime` is already present as a faster-whisper dependency (`requirements.txt`: `onnxruntime>=1.14,<2`, VERIFIED) |
| Moonshine v2 Medium                                            | No: new engine                                 | Slightly better than large-v3 (6.66 vs 7.44 May-26)                               | 245M; very light CPU use. The library is young (0.1.x) and owns the microphone and VAD pipeline, which overlaps Plan 00148's server                                                                                                                                                                                |
| Granite 4.1 2B / Qwen3-ASR 1.7B / Canary-Qwen 2.5B / Cohere 2B | No: PyTorch/transformers/NeMo engine           | Best open accuracy (4.3 to 4.7 Oct-26 vs turbo 6.36)                              | 2B-class: likely 4 to 6 GB VRAM in bf16 and seconds per segment on the CPU. PyTorch is already present through RealtimeSTT, but its CUDA build is not checked                                                                                                                                                      |

Host hardware: the repo assumes an NVIDIA GPU (section 1) and only treats the CPU as a
slow fallback. A CPU-capable model as good as Parakeet would change that assumption. It
would make the feature first-class on machines without NVIDIA, and it would free the GPU
for other work (INFERRED).

---

## 4. Recommendations (ranked)

### Quick wins (same engine)

1. **Better defaults for `auto`.** On the GPU: `distil-large-v3.5` when the language is
   `en`, and `large-v3-turbo` otherwise, for both batch and the Plan 00148 continuous
   worker. On the CPU fallback, keep `small`/`small.en`.
   - Change:
     - `play-speech-to-text.yml:8-9`, `:19-21` (default `stt_model`, header comment)
     - `wsi-stream:447`, `:653`, `:908` and `wsi-stream-server:681` (the `base` defaults)
     - `extension.js:76` (the `auto` description)
     - docs table at `docs/features/speech-to-text.md:161-178`
   - Risk: low. First use downloads about 1.5 GB. VRAM grows by about 1 to 2 GB over
     `small`; check the 2 GB-VRAM tier in the docs. Measure RTF per 20 s segment in Task
     1.2 before switching. The extension change needs a logout.
2. **Offer `distil-large-v3.5`.**
   - Change:
     - add `['distil-large-v3.5', …, '~1.5GB', 'English only', true]` to
       `extension.js:75-90`
     - add `('distil-large-v3.5', …, 'distil-whisper/distil-large-v3.5-ct2', '~1.5GB', True, …)`
       plus `EXPECTED_BYTES` to `wsi-model-manager:52-84`
     - add a docs row
   - Requires faster-whisper ≥1.2.0 for the alias (pin in Task 4.6).
   - Risk: low. English only, so warn or refuse it when the language is not `en`.
3. **Fix the catalogue defects.** Turbo size is ~1.6 GB, not ~800 MB, in
   `wsi-model-manager:58`, `:80`. Turbo is "pruned decoder", not "Distilled", in
   `extension.js:83`, `wsi-model-manager:58` and the docs.
   - Optionally point turbo at `dropbox-dash/faster-whisper-large-v3-turbo` directly
     instead of relying on the HF redirect.
   - Risk: none.
4. **Allow `int8_float16` on the GPU** for large models. It roughly halves VRAM at about
   the same speed (faster-whisper benchmark: 2926 vs 4525 MB for large-v2).
   - Change: the `compute_type` lines in `play-speech-to-text.yml:179`,
     `wsi-stream-server:137`, `wsi-stream:715`, `:961`, ideally as one setting.
   - Risk: small accuracy change (not measured here); test in Task 1.2.
5. **Fold into existing tasks.** Task 4.1 (server mode pastes `tiny`) is the biggest
   accuracy gain of all, because server-mode users are on tiny today. In Task 4.6, pin
   RealtimeSTT to a version that installs on Python 3.14 (≤1.0.4 unless upstream lifts
   `<3.13`), and pin faster-whisper ≥1.2.1.

### Optional engine worth adding

6. **Parakeet TDT 0.6B through `onnx-asr`, as a selectable backend of the Plan 00148
   continuous worker** (and optionally batch `wsi`).
   - Default `parakeet-tdt-0.6b-v2` for `en`, `v3` for the other European languages.
   - Why:
     - best accuracy per resource among open models
     - native punctuation and casing
     - non-autoregressive, so no Whisper-style hallucination loops
     - real-time-plus on the CPU, without PyTorch
     - segments of at most 28 s fit its 20 to 30 s window exactly
   - Change:
     - add `onnx-asr[cpu,hub]==<ver>` to the existing pip task (`play-speech-to-text.yml:110-116`)
     - a backend interface in `wsi-stream-server`'s worker (`whisper` or `parakeet`) with
       one `transcribe(segment) -> text` contract
     - GSettings key `stt-engine` + `prefs.js`/menu
     - `extension.js` model list split by engine
     - `wsi-model-manager` entries for the ONNX repos
     - docs, including the CC-BY-4.0 attribution
     - unit tests for backend selection
     - no new playbook (edit the existing play)
   - Risks:
     - a second engine to maintain
     - no `initial_prompt`, so cross-segment casing and vocabulary hints are lost
     - the RealtimeSTT paths (standard, pre-buffer, article) stay Whisper-only until they
       move to the server
     - using CUDA through `onnxruntime-gpu` would clash with the CPU `onnxruntime` that
       faster-whisper installs; start CPU-only
     - onnx-asr is a single-maintainer project (380 stars); sherpa-onnx is the heavier
       but better-backed fallback for the same models
   - Expected gain: about 30 % fewer word errors than `small.en` on the CPU or GPU, and a
     working setup without NVIDIA.
7. **Later, if accuracy still disappoints:** an "accuracy" backend using Granite Speech
   4.1 2B (Apache-2.0, documented punctuation) or Qwen3-ASR 1.7B. These need a GPU with
   several GB free and a PyTorch path. Decide only after option 6 has been measured.

### Ignore for now, and why

- **Kyutai STT.** Its strength is live streaming, which this design does not use. It is
  2.6B for English, needs the moshi stack, and upstream has been quiet since January 2026.
- **Voxtral (Mini, Realtime, Small).** Needs a vLLM server and a large VRAM budget.
  Realtime is less accurate than distil-v3.5 on the board.
- **Phi-4-multimodal.** A 6B general model with a pinned flash-attn; no better than
  Parakeet.
- **Nemotron streaming.** NeMo plus a restrictive or new licence; streaming does not
  help here; 3.5 is weaker.
- **TheStageAI Whisper.** Needs a vendor token, Python ≤3.12 and specific GPUs.
- **Granite 5.0 TurboCTC, Moonshine v2.** Promising and light; watch them. Punctuation is
  not established for TurboCTC, and the Moonshine library is 0.1.x and owns the whole
  audio pipeline.
- **Moving off faster-whisper to whisper.cpp.** Same models and no accuracy gain. Keep
  the existing remote-server option (`wsi -n`).
- **Leaderboard RTFx as a latency promise.** A100 batch numbers; measure on the host
  (Task 1.2).

---

## 5. Not established

- Real latency, VRAM and RTF of any model **on the owner's GPU and CPU**. All latency
  claims for this host are INFERRED. Task 1.2's probe should time `small`,
  `large-v3-turbo`, `distil-large-v3.5` and Parakeet v2 (onnx-asr, CPU) on the same 20 s
  segments.
- Punctuation and casing quality, compared head to head. The leaderboard normalises
  them away. A small local A/B on the owner's own dictation is the only real test.
- Installed versions of RealtimeSTT and faster-whisper on the host (Task 1.2).
- Exact ONNX file sizes for Parakeet int8/fp32, and whether onnx-asr's int8 Parakeet
  loses accuracy.
- Whether Granite 5.0 TurboCTC and Moonshine v2 output punctuation and casing.
- Whether the PyTorch that RealtimeSTT pulls in is a CUDA build (this matters for any
  PyTorch-based option).
