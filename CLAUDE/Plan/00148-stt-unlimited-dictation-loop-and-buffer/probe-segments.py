"""How continuous dictation cuts real speech: <report.md> <speech.raw> <repo root>.

Plan 00148 triage (research section 3.6: hard-cut frequency at a 20 s soft / 28 s hard
max). Runs the checkout's own wsi-stream-server Segmenter and its Silero VAD adapter (the
code Insert runs once deployed) over the recording, frame by frame as the server's
capture thread does, and reports every segment: why it was cut, how long it is. A hard
cut is one made inside speech, at the quietest frame, because no pause came by 28 s.
"""

from __future__ import annotations

import array
import pathlib
import sys
from importlib.machinery import SourceFileLoader
from importlib.util import module_from_spec, spec_from_loader

from probe_common import RATE, append


def load_server(repo_root: str):
    path = pathlib.Path(repo_root) / "files" / "home" / ".local" / "bin" / "wsi-stream-server"
    loader = SourceFileLoader("wsi_stream_server", str(path))
    module = module_from_spec(spec_from_loader("wsi_stream_server", loader))
    loader.exec_module(module)
    return module


def main(report: str, audio_path: str, repo_root: str) -> int:
    from faster_whisper.vad import get_vad_model

    server = load_server(repo_root)
    raw = pathlib.Path(audio_path).read_bytes()
    # The adapter's constructor finds the model's input shape by test and raises if
    # none yields one "no speech" probability per frame of silence
    vad = server.SileroFrameVad(get_vad_model())
    segmenter = server.Segmenter()
    segments = []
    batch = []
    speech_frames = 0
    for offset in range(0, len(raw) - server.FRAME_BYTES + 1, server.FRAME_BYTES):
        batch.append(array.array("h", raw[offset:offset + server.FRAME_BYTES]))
        if len(batch) == server.VAD_BATCH_FRAMES:
            for frame, prob in zip(batch, vad.probabilities(batch)):
                segments += segmenter.push(frame, prob)
                speech_frames += segmenter.last_frame_was_speech
            batch = []
    if batch:
        for frame, prob in zip(batch, vad.probabilities(batch)):
            segments += segmenter.push(frame, prob)
            speech_frames += segmenter.last_frame_was_speech
    segments += segmenter.finish()

    minutes = len(raw) / 2 / RATE / 60
    lengths = [s.duration for s in segments] or [0.0]
    counts = segmenter.cut_counts
    lines = [
        "## Continuous dictation: how the segmenter cuts this speech", "",
        f"- Silero VAD adapter: passed its silence self-test with {vad.input_shape} input",
        f"- audio: {minutes * 60:.1f} s; frames judged speech: "
        f"{speech_frames * server.FRAME_SAMPLES / RATE:.1f} s",
        f"- segments: {len(segments)}; cuts: pause {counts['pause']}, soft {counts['soft']}, "
        f"hard {counts['hard']}, stop {counts['stop']}",
        f"- hard cuts per minute of audio: {counts['hard'] / minutes:.2f}",
        f"- segment length: min {min(lengths):.1f} s, mean {sum(lengths) / len(lengths):.1f} s, "
        f"max {max(lengths):.1f} s",
        "", "| seq | starts (s) | length (s) | cut |", "| --- | --- | --- | --- |",
    ]
    lines += [f"| {s.seq} | {s.start_seconds:.1f} | {s.duration:.1f} | {s.cut} |" for s in segments]
    append(report, "\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:]))
