"""Continuous dictation's segmenter and ordered commit, as pure units (Plan 00148 Task 2.1).

The warm server cuts the microphone stream into segments at natural pauses so that every
segment fits one Whisper window, and commits each segment's text in order. These tests
drive wsi-stream-server's Segmenter with synthetic 512-sample frames and a speech
probability per frame (the VAD's answer), and its Transcript with results arriving out of
order. No model, no audio device.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import array
import importlib.util
import pathlib
import unittest

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

server = stt_stubs.load_script("wsi-stream-server", "wsi_stream_server_segmenter")

FPS = server.SAMPLE_RATE / server.FRAME_SAMPLES  # frames per second


def frame(level):
    return array.array("h", [level] * server.FRAME_SAMPLES)


def seconds(n):
    return int(round(n * FPS))


class Feed:
    """Feeds frames to a Segmenter and keeps every segment it returns."""

    def __init__(self):
        self.seg = server.Segmenter()
        self.segments = []
        self.frames_in = 0

    def speech(self, secs, level=3000):
        self._push(secs, level, 0.9)
        return self

    def silence(self, secs, level=0):
        self._push(secs, level, 0.05)
        return self

    def _push(self, secs, level, prob):
        for _ in range(seconds(secs)):
            self.segments += self.seg.push(frame(level), prob)
            self.frames_in += 1

    def finish(self):
        self.segments += self.seg.finish()
        return self


class SegmenterTest(unittest.TestCase):
    def test_a_pause_after_a_phrase_closes_the_segment(self):
        f = Feed().speech(2).silence(1)
        self.assertEqual(len(f.segments), 1)
        s = f.segments[0]
        self.assertEqual(s.cut, "pause")
        self.assertAlmostEqual(s.duration, 2 + server.Segmenter.END_PAUSE_SECONDS, delta=0.1)

    def test_a_short_pause_inside_a_phrase_does_not_cut(self):
        f = Feed().speech(3).silence(0.4).speech(3).silence(1)
        self.assertEqual(len(f.segments), 1)
        self.assertAlmostEqual(f.segments[0].duration, 6.4 + server.Segmenter.END_PAUSE_SECONDS,
                               delta=0.1)

    def test_pure_silence_is_never_queued(self):
        f = Feed().silence(90).finish()
        self.assertEqual(f.segments, [])

    def test_quiet_noise_without_speech_is_never_queued(self):
        f = Feed().silence(40, level=200).finish()
        self.assertEqual(f.segments, [])

    def test_leading_silence_is_trimmed_to_the_pre_roll(self):
        f = Feed().silence(10).speech(2).silence(1)
        self.assertEqual(len(f.segments), 1)
        s = f.segments[0]
        self.assertAlmostEqual(s.duration, server.Segmenter.PRE_ROLL_SECONDS + 2
                               + server.Segmenter.END_PAUSE_SECONDS, delta=0.1)
        self.assertAlmostEqual(s.start_seconds, 10 - server.Segmenter.PRE_ROLL_SECONDS, delta=0.1)

    def test_a_short_utterance_closes_after_the_longer_pause(self):
        f = Feed().speech(0.5).silence(1)
        self.assertEqual(f.segments, [], "a short phrase waits for more speech")
        f.silence(1.5)
        self.assertEqual(len(f.segments), 1)
        self.assertEqual(f.segments[0].cut, "pause")

    def test_past_the_soft_max_a_brief_pause_cuts(self):
        f = Feed().speech(21).silence(0.3).speech(5).silence(1)
        self.assertEqual([s.cut for s in f.segments], ["soft", "pause"])
        self.assertAlmostEqual(f.segments[0].duration, 21 + server.Segmenter.SOFT_PAUSE_SECONDS,
                               delta=0.1)

    def test_a_brief_pause_before_the_soft_max_does_not_cut(self):
        f = Feed().speech(10).silence(0.3).speech(5).silence(1)
        self.assertEqual([s.cut for s in f.segments], ["pause"])

    def test_without_a_pause_the_hard_max_cuts_at_the_quietest_frame(self):
        f = Feed().speech(26.6).speech(0.05, level=50).speech(5).silence(1)
        self.assertEqual([s.cut for s in f.segments], ["hard", "pause"])
        first = f.segments[0]
        self.assertLessEqual(first.duration, server.Segmenter.HARD_MAX_SECONDS)
        self.assertAlmostEqual(first.duration, 26.6, delta=0.1)
        quietest = f.segments[1].samples[0]
        self.assertEqual(quietest, 50, "the next segment starts at the quietest frame")
        self.assertEqual(f.seg.cut_counts["hard"], 1)

    def test_no_segment_exceeds_the_hard_max(self):
        f = Feed().speech(200).finish()
        self.assertGreater(len(f.segments), 6)
        for s in f.segments:
            self.assertLessEqual(s.duration, server.Segmenter.HARD_MAX_SECONDS + 1e-9)

    def test_cuts_inside_speech_lose_no_audio(self):
        f = Feed().speech(75).finish()
        total = sum(len(s.samples) for s in f.segments)
        self.assertEqual(total, f.frames_in * server.FRAME_SAMPLES)
        starts = [s.start_frame for s in f.segments]
        self.assertEqual(starts, sorted(starts))
        for a, b in zip(f.segments, f.segments[1:]):
            self.assertEqual(a.start_frame + len(a.samples) // server.FRAME_SAMPLES, b.start_frame,
                             "segments are contiguous")

    def test_sequence_numbers_count_up_from_zero(self):
        f = Feed().speech(2).silence(1).speech(2).silence(1).speech(2).finish()
        self.assertEqual([s.seq for s in f.segments], [0, 1, 2])

    def test_finish_returns_the_open_segment_however_short(self):
        f = Feed().speech(0.2).finish()
        self.assertEqual(len(f.segments), 1)
        self.assertEqual(f.segments[0].cut, "stop")

    def test_speech_seen_is_reported_for_the_silence_auto_stop(self):
        seg = server.Segmenter()
        seg.push(frame(0), 0.1)
        self.assertFalse(seg.last_frame_was_speech)
        seg.push(frame(3000), 0.9)
        self.assertTrue(seg.last_frame_was_speech)


class TranscriptTest(unittest.TestCase):
    def test_results_commit_in_sequence_order(self):
        t = server.Transcript()
        self.assertEqual(t.commit(1, "world."), [])
        self.assertEqual(t.text, "")
        self.assertEqual(t.commit(0, "Hello"), [(0, "Hello"), (1, "world.")])
        self.assertEqual(t.text, "Hello world.")
        self.assertEqual(t.committed_count, 2)

    def test_a_gap_holds_back_everything_after_it(self):
        t = server.Transcript()
        t.commit(0, "one")
        t.commit(2, "three")
        t.commit(3, "four")
        self.assertEqual(t.text, "one")
        self.assertEqual(t.held, [2, 3])
        t.commit(1, "two")
        self.assertEqual(t.text, "one two three four")
        self.assertEqual(t.held, [])

    def test_an_empty_segment_adds_no_text_but_keeps_the_order(self):
        t = server.Transcript()
        t.commit(0, "first")
        t.commit(1, "")
        t.commit(2, "third")
        self.assertEqual(t.text, "first third")

    def test_a_sequence_number_cannot_commit_twice(self):
        t = server.Transcript()
        t.commit(0, "a")
        with self.assertRaises(ValueError):
            t.commit(0, "b")
        t.commit(2, "c")
        with self.assertRaises(ValueError):
            t.commit(2, "d")

    def test_the_prompt_is_the_tail_of_the_committed_text(self):
        t = server.Transcript()
        t.commit(0, "x" * 150)
        t.commit(1, "y" * 150)
        prompt = t.prompt(200)
        self.assertEqual(len(prompt), 200)
        self.assertTrue(prompt.endswith("y" * 150))


if __name__ == "__main__":
    unittest.main()
