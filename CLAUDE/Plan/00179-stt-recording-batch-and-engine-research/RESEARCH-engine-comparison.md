# Research: speech-to-text engine comparison on long recordings (Phase 1)

Measured results from the speech-to-text experiment session, for Task 1.2 (memory and
speed on long Opus audio) and the engine question of #9, feeding the owner's decision in
Task 2.1. Every figure below was measured; nothing is taken from a leaderboard.

## Test conditions

- **Audio**: two long single-speaker English dictations recorded on a phone, Opus, 20 and
  57 minutes. Between them they hold four known clauses that the current batch path skips.
- **Hardware class**: a laptop with a 4 GB NVIDIA GPU, a 16-core laptop CPU (AVX2 and
  AVX-VNNI, no AVX-512 or AMX) and ample RAM (over 90 GB, so RAM never limited a run).
- **Ranking rule (the owner's ruling)**: accuracy first, then memory fit, time only as a
  tie-breaker.
- Times were taken on a shared laptop and are rough. Where two figures are given
  (`16-21`), the engine was run more than once.

## Results

Columns: where it ran; wall time for the 20 and the 57 minute recording; peak GPU memory
and RAM; how many of the four skipped clauses came back; stretches of four or more words
lost compared with the current output (20 / 57 minutes); whether a second run gave the same
output.

| Engine                                                | Where                        | 20 / 57 min (s)   | Peak GPU MiB / RAM GiB | Known clauses back (of 4) | 4+-word stretches lost, 20 / 57 min | Same output twice                                     |
| ----------------------------------------------------- | ---------------------------- | ----------------- | ---------------------- | ------------------------- | ----------------------------------- | ----------------------------------------------------- |
| Parakeet TDT 0.6B v3 (onnx-asr)                       | GPU                          | 16-21 / 38-50     | 3603 / 1.5             | 4                         | 2 / 0                               | yes, and CPU = GPU word for word                      |
| Parakeet TDT 0.6B v3 (onnx-asr)                       | CPU                          | 154 / 333         | - / 3.1                | 4                         | 2 / 0                               | identical to GPU                                      |
| Parakeet TDT 0.6B v2                                  | GPU                          | 11-15 / 37-39     | 3481 / 1.5             | 4                         | 4 / 0                               | yes                                                   |
| large-v3-turbo (faster-whisper)                       | GPU fp16                     | 38-44 / 95-135    | 2507 / 5.1             | 4                         | 3 / 0                               | yes; CPU run differs                                  |
| Canary 1B Flash (NeMo)                                | GPU fp16                     | 68 / 129          | 2143 / 7.9             | 4                         | 3 / 1                               | not repeated                                          |
| Qwen3-ASR 0.6B                                        | GPU bf16                     | 154-254 / 294-674 | 1979 / 3.1             | 4                         | 5 / 1                               | yes                                                   |
| large-v3 (faster-whisper)                             | GPU int8 (fp16 does not fit) | 74-154 / 193-241  | 2251 / 6.5             | 4                         | 2 / 1                               | yes                                                   |
| Qwen3-ASR 1.7B                                        | CPU only (GPU OOM at load)   | 1497 / 4885       | - / 5.4                | 4                         | 2 / 0                               | not repeated                                          |
| Canary-Qwen 2.5B                                      | CPU                          | 1142 / 3971       | - / 15.8               | 4                         | 4 / 1                               | not repeated                                          |
| Granite Speech 4.0 1B                                 | CPU fp32 (GPU OOM even bf16) | 1289 / 3451       | - / 12.1               | 4                         | 3 / 1                               | fp32 and bf16 agree to 0.9975; no punctuation or case |
| Granite Speech 4.1 2B                                 | CPU fp32                     | 1992 / 4782       | - / 12.6               | 4                         | 6 / 1                               | not repeated                                          |
| Kyutai STT 2.6B, whole file                           | CPU fp32                     | 8343 / not run    | - / 16.2               | 1 of 1 judged             | 5 / -                               | not repeated                                          |
| Moonshine base                                        | CPU                          | 104 / 305         | - / 0.9                | 3, 1 not judged           | 13 / 4                              | not repeated                                          |
| **current**: distil-large-v3.5, joined stream, VAD on | GPU fp16                     | 29 / 86           | 2251 / -               | 0                         | baseline                            | yes, deterministic                                    |

**Not run**: Cohere Transcribe. The model is gated on Hugging Face and the test host had
no login.

## Other findings

- **The skips are the decoder's**, on the long joined stream; they are not in the audio and
  not caused by the VAD.
- **No faster-whisper tuning fixes them.** Every faster-whisper setting change, and every
  audio preprocessing tried (loudness, EQ, compression, denoise), recovers some skipped
  clauses and drops other stretches instead.
- `condition_on_previous_text=True` loses 38-59% of the words; it stays off.
- **bf16 on this CPU class is emulated** (no AVX-512 or AMX) and runs over ten times slower
  than fp32.

## Recommendation (for the owner's decision, Task 2.1)

**Parakeet TDT 0.6B v3 through `onnx-asr`, on VAD-cut segments of at most 20 s.**

- Most accurate in this test: all four skipped clauses back, the fewest lost stretches.
- Deterministic, and device-independent: the CPU and GPU outputs are identical word for
  word, so the same engine serves the non-NVIDIA path of #11 at about 3 GB RAM.
- Fits the 4 GB card (peak 3.6 GB) and is in the fastest tier.

**Weak points**: it keeps fillers and repeats ("uh"), and it mishears jargon (which the
inline jargon markup of Task 3.3 and the vocabulary of Plan 00159 are meant to address).
Its GPU peak is the highest of the engines that fit, which leaves the least room beside the
warm dictation model.

**Runner-up**, if staying inside faster-whisper matters: `large-v3-turbo` (all four
clauses back, three lost stretches on the shorter recording, 2.5 GB GPU).

**Implementation notes** from the experiment session: the segmenting can use
faster-whisper's Silero VAD with `max_speech_duration_s=20`. With onnxruntime 1.31 the model
has to be downloaded to a plain folder rather than loaded through the Hugging Face hub
cache's symlinks. The experiment session offered to pair on the implementation.

## Caveat

Two recordings, both from one speaker, are thin evidence. A third recording and a
hand-checked reference transcript would test the ranking; until then the "stretches lost"
column is relative to the current output, not to a ground truth.
