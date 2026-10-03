"""Real-time factor of the streaming model per 20 s segment: <report.md> <speech.raw>.

Plan 00148 triage (research section 3.6). Loads the model wsi-stream would load (the
Whisper Model setting through the deployed wsi-resolve-model) on the device CTranslate2
offers, and transcribes the recording in 20 s slices with the options the continuous
worker uses: beam 5, the language setting, no VAD filter, no conditioning on previous
text, and the last 200 characters of the text so far as the prompt. RTF = transcription
time / audio time; above 1 the backlog grows while the owner speaks. The first slice is
transcribed once first to warm up, and that run is not counted.
"""

from __future__ import annotations

import sys
import time

from probe_common import RATE, append, device, language, load_audio, say, streaming_model

SLICE_SECONDS = 20
PROMPT_CHARS = 200


def main(report: str, audio_path: str) -> int:
    from faster_whisper import WhisperModel

    audio = load_audio(audio_path)
    lang = language()
    model_name = streaming_model(lang)
    dev, compute = device()
    started = time.monotonic()
    model = WhisperModel(model_name, device=dev, compute_type=compute)
    load_seconds = time.monotonic() - started

    def transcribe(samples, prompt):
        segments, _info = model.transcribe(
            samples, language=lang or None, beam_size=5, vad_filter=False,
            condition_on_previous_text=False, initial_prompt=prompt or None)
        return " ".join(s.text.strip() for s in segments).strip()

    step = SLICE_SECONDS * RATE
    transcribe(audio[:step], "")
    rows = []
    text = ""
    for start in range(0, len(audio) - step + 1, step):
        piece = audio[start:start + step]
        began = time.monotonic()
        out = transcribe(piece, text[-PROMPT_CHARS:])
        took = time.monotonic() - began
        rows.append((start / RATE, took, took / SLICE_SECONDS, len(out.split())))
        text = f"{text} {out}".strip()
        say(f"slice at {start / RATE:.0f} s: {took:.2f} s, RTF {took / SLICE_SECONDS:.3f}")

    factors = [r[2] for r in rows]
    lines = [
        "## Real-time factor per 20 s segment", "",
        f"- model: {model_name}; device {dev} ({compute}); language {lang or 'detected'}",
        f"- model load: {load_seconds:.1f} s",
        f"- slices: {len(rows)}; RTF mean {sum(factors) / len(factors):.3f}, max {max(factors):.3f}",
        "", "| starts at (s) | took (s) | RTF | words |", "| --- | --- | --- | --- |",
    ]
    lines += [f"| {r[0]:.0f} | {r[1]:.2f} | {r[2]:.3f} | {r[3]} |" for r in rows]
    lines += ["", "Text (dictated; this report is under untracked/ and never committed):", "",
              f"> {text}"]
    append(report, "\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:]))
