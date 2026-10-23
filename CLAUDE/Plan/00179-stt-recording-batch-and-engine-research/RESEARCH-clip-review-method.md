# Research: settling engine disagreements by a person listening (Phase 1)

The method, audio findings and open items reported by the speech-to-text experiment
session after the engine comparison
([RESEARCH-engine-comparison.md](RESEARCH-engine-comparison.md)). It answers that file's
caveat (no ground truth) and feeds Task 2.1 and Tasks 1.5 to 1.8.

## The method: two engines, the differences reviewed, a person settles the doubtful ones

This is a **polishing step for edge cases**, not a replacement: bulk transcription stays
automatic. Only the stretches the automatic pass is unsure of reach a person.

1. **Two engines of different families transcribe the same VAD segments**: Parakeet TDT
   0.6B v3 through `onnx-asr` and `large-v3-turbo` through faster-whisper, each under 4 GB
   of GPU memory. Different families make different mistakes, so where they agree the text
   is very likely right.
2. **Every difference is written as a hunk**: the segment's time range, both wordings, and
   the context around them. Nothing is filtered at this stage. On the test dictations this
   came to about 700 hunks per hour of audio.
3. **An agent triages each hunk** as trivia (punctuation, case, a filler, a spelling of the
   same word) or substantive (a word or clause one engine has and the other lacks or
   changes), with a one-line reason. It flags a hunk when in doubt, and it never chooses an
   engine's wording.
4. **Each segment holding a substantive hunk becomes a clip** for the person. Unmarked
   controls are mixed in: segments whose hunks were all dismissed as trivia, and segments
   with no hunk at all, so the review also measures how often the triage and the agreement
   are wrong.
5. **The person's typed text (the clip's sidecar) settles that stretch.** The person is shown
   no engine text, so the reference is not led. The clip review tool is issue #112.
6. **The sidecars are also the hand-checked reference** to score engines against, which
   the engine comparison lacked.

## Audio findings on the two test dictations

- Both are **quiet**: about -28 and -32 LUFS integrated, with no clipping.
- One is **noisy**: its pauses sit only about 7 dB below the speech.
- Of the four known skipped clauses, two fall in quiet speech and two at ordinary level.
- **Loudness normalisation moved the skips rather than removing them**, consistent with
  the engine comparison's finding that the skips are the decoder's, not the audio's.

## Approaches tried and superseded

- **A three-engine majority vote** (Parakeet, `large-v3-turbo` and Canary 1B Flash)
  recovered every known skip but gained nothing over Parakeet alone.
- **A rule-based filter** for doubtful segments flagged 26 to 83 segments per hour: too many
  for a person to review.

Both are superseded by the method above.

## Open items

| Item                                                                                                                                                                                                                                      | Where it is tracked             |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------- |
| The owner's engine choice                                                                                                                                                                                                                 | Task 2.1                        |
| The clip review tool                                                                                                                                                                                                                      | #112, its own plan (Task 1.5)   |
| A merge step: write a new transcript with each reviewed segment's sidecar text in place of the machine text, marking each segment reviewed, inaudible or awaiting review, and editing nothing. Proposed, not built until the owner agrees | Task 1.6 (owner decision first) |
| A third long recording, to test the ranking                                                                                                                                                                                               | Task 1.7                        |
| Cohere Transcribe (gated on Hugging Face; needs a login)                                                                                                                                                                                  | Task 1.8                        |
| Kyutai STT on a long recording (about seven times real time on the CPU)                                                                                                                                                                   | Task 1.8                        |
