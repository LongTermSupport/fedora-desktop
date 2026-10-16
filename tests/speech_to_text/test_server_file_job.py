"""wsi-stream-server: audio files transcribed with the warm model (Plan 00179).

FILE_START hands the server one recording's audio files, in order. They are joined into
one stream and transcribed on a thread of their own by the same model dictation uses, so
one model is ever in GPU memory; CTranslate2 serialises the two callers. FILE_PROGRESS
reports the segments so far, then done or failed. A dictation START is never refused
because a file is being transcribed, and the idle timeout waits for the file job.

Stdlib only; the model and the audio decoder are stubs. Run by
scripts/test-wsi-stop-grace.bash.
"""

import importlib.util
import json
import pathlib
import tempfile
import threading
import time
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

server = stt_stubs.load_script("wsi-stream-server", "wsi_stream_server_file_job")


class Segment:
    def __init__(self, start, end, text):
        self.start, self.end, self.text = start, end, text


class Info:
    def __init__(self, duration, language):
        self.duration, self.language = duration, language


class StubModel:
    """Yields its segments; `gate`, when set, holds each one until the test releases it."""

    def __init__(self, segments, fail_after=None):
        self.segments = segments
        self.fail_after = fail_after
        self.calls = []
        self.gate = None

    def transcribe(self, audio, **options):
        self.calls.append({"audio": audio, **options})

        def generate():
            for i, segment in enumerate(self.segments):
                if self.gate is not None:
                    self.gate.acquire()
                if self.fail_after is not None and i == self.fail_after:
                    raise RuntimeError("CUDA failed with error out of memory")
                yield segment
        return generate(), Info(12.0, options.get("language") or "de")


class FakeSocket:
    def __init__(self):
        self.replies = []

    def sendall(self, data):
        self.replies.extend(json.loads(line) for line in data.decode().splitlines())


def joined(paths):
    return "|".join(pathlib.Path(p).name for p in paths)


class FileJobCase(unittest.TestCase):

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self._tmp.name)
        self.parts = []
        for name in ("rec-001.ogg", "rec-002.ogg"):
            (self.dir / name).write_bytes(b"opus")
            self.parts.append(str(self.dir / name))
        self.model = StubModel([Segment(0.0, 2.0, " Hello."), Segment(3.0, 3.5, "  "),
                                Segment(9.0, 11.0, " Again.")])
        patches = [mock.patch.object(server, "transcriber", server.WhisperTranscriber(self.model, "en")),
                   mock.patch.object(server, "model_name", "distil-large-v3.5"),
                   mock.patch.object(server, "file_job", None),
                   mock.patch.object(server, "session", None),
                   mock.patch.object(server, "load_audio", joined),
                   mock.patch.object(server, "log", lambda *a, **k: None)]
        for patch in patches:
            patch.start()
            self.addCleanup(patch.stop)

    def tearDown(self):
        self._tmp.cleanup()

    def command(self, name, params=None):
        sock = FakeSocket()
        server.COMMANDS[name](sock, params or {})
        self.assertEqual(len(sock.replies), 1, sock.replies)
        return sock.replies[0]

    def finished(self, since=0):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            reply = self.command("FILE_PROGRESS", {"since": since})
            if reply["status"] != "running":
                return reply
            time.sleep(0.01)
        self.fail("the file job never finished")


class FileJobTest(FileJobCase):

    def test_the_parts_are_one_stream_through_the_warm_model_with_long_form_settings(self):
        reply = self.command("FILE_START", {"paths": self.parts, "language": "en"})
        self.assertEqual(reply["status"], "running", reply)
        self.finished()
        call = self.model.calls[0]
        self.assertEqual(call["audio"], "rec-001.ogg|rec-002.ogg")
        self.assertEqual(call["language"], "en")
        self.assertTrue(call["vad_filter"])
        self.assertFalse(call["condition_on_previous_text"])
        self.assertGreaterEqual(call["beam_size"], 5)

    def test_progress_reports_the_segments_with_words_then_done(self):
        self.command("FILE_START", {"paths": self.parts, "language": "en"})
        reply = self.finished()
        self.assertEqual(reply, {"status": "done", "segments": [
            {"start": 0.0, "end": 2.0, "text": "Hello."}, {"start": 9.0, "end": 11.0, "text": "Again."}],
            "count": 2, "duration": 12.0, "language": "en", "model": "distil-large-v3.5"})
        self.assertEqual(self.command("FILE_PROGRESS", {"since": 1})["segments"],
                         [{"start": 9.0, "end": 11.0, "text": "Again."}])

    def test_no_language_is_detection(self):
        self.command("FILE_START", {"paths": self.parts, "language": None})
        self.assertEqual(self.finished()["language"], "de")
        self.assertIsNone(self.model.calls[0]["language"])

    def test_a_failure_part_way_is_failed_with_the_reason(self):
        self.model.fail_after = 2
        self.command("FILE_START", {"paths": self.parts, "language": "en"})
        reply = self.finished()
        self.assertEqual(reply["status"], "failed")
        self.assertIn("out of memory", reply["error"])

    def test_a_second_file_job_is_refused_while_one_runs(self):
        self.model.gate = threading.Semaphore(0)
        self.command("FILE_START", {"paths": self.parts, "language": "en"})
        reply = self.command("FILE_START", {"paths": self.parts, "language": "en"})
        self.assertEqual(reply["status"], "error")
        self.assertEqual(reply["busy"], "file")
        for _ in self.model.segments:
            self.model.gate.release()
        self.finished()

    def test_a_running_file_job_holds_off_the_idle_timeout_but_not_dictation(self):
        self.model.gate = threading.Semaphore(0)
        self.command("FILE_START", {"paths": self.parts, "language": "en"})
        with mock.patch.object(server, "idle_timeout", 1), mock.patch.object(server, "last_activity_time", 0):
            self.assertFalse(server.check_idle_timeout(), "the idle timeout must wait for a file job")
            self.assertFalse(server.is_busy(), "a file job must not make a dictation START refused")
            for _ in self.model.segments:
                self.model.gate.release()
            self.finished()
        # Polling is activity; idle long after, a finished job no longer holds the timeout off
        with mock.patch.object(server, "idle_timeout", 1), mock.patch.object(server, "last_activity_time", 0):
            self.assertTrue(server.check_idle_timeout())

    def test_bad_requests_are_refused_and_start_nothing(self):
        for params in ({}, {"paths": []}, {"paths": "x.ogg"}, {"paths": [str(self.dir / "gone.ogg")]},
                       {"paths": ["relative.ogg"]}, {"paths": self.parts, "language": 5}):
            with self.subTest(params=params):
                reply = self.command("FILE_START", params)
                self.assertEqual(reply["status"], "error", reply)
        self.assertEqual(self.model.calls, [])

    def test_progress_with_no_job_or_a_bad_since_is_refused(self):
        self.assertEqual(self.command("FILE_PROGRESS", {"since": 0})["status"], "error")
        self.command("FILE_START", {"paths": self.parts, "language": "en"})
        for since in (-1, "0", True):
            with self.subTest(since=since):
                self.assertEqual(self.command("FILE_PROGRESS", {"since": since})["status"], "error")
        self.finished()

    def test_no_model_loaded_is_refused(self):
        with mock.patch.object(server, "transcriber", None):
            self.assertEqual(self.command("FILE_START", {"paths": self.parts})["status"], "error")


if __name__ == "__main__":
    unittest.main()
