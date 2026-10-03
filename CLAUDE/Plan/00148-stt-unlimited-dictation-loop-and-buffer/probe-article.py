"""Article mode's word loss at phrase boundaries: <report.md> <speech.raw>.

Plan 00148 triage (research section 3.0 and 3.6). wsi-article runs one RealtimeSTT
recorder with post_speech_silence_duration=1.5 and calls recorder.text() in a loop; while
one call is transcribing a phrase the recorder is not re-armed, so speech in that gap
survives only through the 1 s pre-roll. This feeds the recording to a recorder with
wsi-article's settings IN REAL TIME (as a microphone would deliver it), collects phrases
the way wsi-article does, and compares their words with one whole-file transcription of
the same recording by the same model. Words in the reference but not in the phrases are
listed with their context; decoding differences between the two runs show up too, so the
list is evidence to read, not a count to trust blindly.
"""

from __future__ import annotations

import array
import difflib
import sys
import threading
import time

from probe_common import RATE, append, device, language, load_audio, say, setting, words

CHUNK_SAMPLES = 1024
TAIL_SILENCE_SECONDS = 4
DRAIN_LIMIT_SECONDS = 60


def article_model() -> str:
    """What wsi-article loads: WHISPER_MODEL, which the extension sets for any choice but auto."""
    chosen = setting("whisper-model")
    return "base" if chosen in ("", "auto") else chosen


def main(report: str, audio_path: str) -> int:
    from faster_whisper import WhisperModel
    from RealtimeSTT import AudioToTextRecorder

    audio = load_audio(audio_path)
    pcm = (audio * 32767).astype("int16").tobytes()
    lang = language()
    model_name = article_model()
    dev, compute = device()

    recorder = AudioToTextRecorder(
        model=model_name, language=lang, use_microphone=False, silero_sensitivity=0.4,
        post_speech_silence_duration=1.5, min_length_of_recording=0.3,
        min_gap_between_recordings=0.1, enable_realtime_transcription=True,
        realtime_processing_pause=0.2, on_realtime_transcription_update=lambda _t: None,
        spinner=False, device=dev, compute_type=compute)

    phrases: list[str] = []
    errors: list[str] = []
    feeding_done = threading.Event()
    stop = threading.Event()

    def phrase_loop():
        while not stop.is_set():
            try:
                phrase = recorder.text()
            except Exception as e:
                errors.append(f"recorder.text() raised: {e}")
                return
            if phrase and phrase.strip():
                phrases.append(phrase.strip())
                say(f"phrase {len(phrases)}: {len(phrase.split())} words")

    def feed():
        chunk_bytes = CHUNK_SAMPLES * 2
        silence = b"\x00\x00" * (TAIL_SILENCE_SECONDS * RATE)
        stream = pcm + silence
        started = time.monotonic()
        for i, offset in enumerate(range(0, len(stream), chunk_bytes)):
            recorder.feed_audio(array.array("h", stream[offset:offset + chunk_bytes]))
            due = started + (i + 1) * CHUNK_SAMPLES / RATE
            time.sleep(max(0.0, due - time.monotonic()))
        feeding_done.set()

    looper = threading.Thread(target=phrase_loop, daemon=True)
    feeder = threading.Thread(target=feed, daemon=True)
    looper.start()
    feeder.start()
    feeder.join()
    deadline = time.monotonic() + DRAIN_LIMIT_SECONDS
    count = -1
    while time.monotonic() < deadline and count != len(phrases):
        count = len(phrases)
        time.sleep(5)
    stop.set()
    recorder.shutdown()
    if errors:
        raise RuntimeError("; ".join(errors))

    reference_model = WhisperModel(model_name, device=dev, compute_type=compute)
    segments, _info = reference_model.transcribe(audio, language=lang or None, beam_size=5,
                                                 vad_filter=True)
    reference = " ".join(s.text.strip() for s in segments)

    ref_words = words(reference)
    got_words = words(" ".join(phrases))
    matcher = difflib.SequenceMatcher(a=ref_words, b=got_words, autojunk=False)
    lost = []
    for tag, i1, i2, _j1, _j2 in matcher.get_opcodes():
        if tag in ("delete", "replace"):
            before = " ".join(ref_words[max(0, i1 - 4):i1])
            after = " ".join(ref_words[i2:i2 + 4])
            lost.append(f"- {tag}: ... {before} [{' '.join(ref_words[i1:i2])}] {after} ...")
    missing = sum(i2 - i1 for tag, i1, i2, _a, _b in matcher.get_opcodes() if tag == "delete")
    lines = [
        "## Article mode: word loss at phrase boundaries", "",
        f"- model: {model_name}; device {dev} ({compute}); language {lang or 'detected'}",
        f"- phrases returned: {len(phrases)}; words: {len(got_words)} vs the reference's {len(ref_words)}",
        f"- reference words with no counterpart (deleted): {missing}; similarity {matcher.ratio():.3f}",
        "", "Differences (reference words in brackets; dictated text, untracked report):", "",
    ]
    lines += lost or ["- none"]
    append(report, "\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:]))
