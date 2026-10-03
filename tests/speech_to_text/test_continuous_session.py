"""wsi-stream-server's dictation session, run for real with stubs (Plan 00148 Tasks 2.2, 2.3).

A DictationSession is driven with a fake pw-record (stt_stubs), a stub VAD (any non-zero
sample is speech) and a stub transcriber, so the whole loop runs: capture, segmenting,
the one ordered worker, the journal, STOP's drain, and every way the session can end:
done, auto-stopped (heartbeat lost, no speech, the absolute cap) and FAILED (a segment
raises, the microphone dies, the backlog passes its ceiling, the drain deadline expires,
the journal cannot be written). A FAILED session keeps the audio it did not transcribe and
reports the text committed so far; nothing is dropped without saying so.

Also the server's commands over a real socket pair: START's parameters are required,
STOP drains and PROGRESS reports.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import importlib.util
import json
import pathlib
import socket
import tempfile
import threading
import time
import unittest
import wave
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

server = stt_stubs.load_script("wsi-stream-server", "wsi_stream_server_session")

FINISHED = ("done", "failed", "aborted")


class StubVad:
    def probabilities(self, frames):
        return [0.9 if any(f) else 0.0 for f in frames]


class StubTranscriber:
    """Returns "s<seq>" for each segment, in call order; can fail, stall or answer empty."""

    def __init__(self, fail_on=None, empty_on=None, delay=0.0, block=None):
        self.calls = []
        self.fail_on = fail_on
        self.empty_on = empty_on
        self.delay = delay
        self.block = block

    def __call__(self, samples, prompt):
        n = len(self.calls)
        self.calls.append((samples, prompt))
        if self.block is not None:
            self.block.wait()
        time.sleep(self.delay)
        if n == self.fail_on:
            raise RuntimeError("CUDA out of memory")
        if n == self.empty_on:
            return ""
        return f"s{n}"


class SessionCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        tmp = pathlib.Path(self.tmp.name)
        self.events = tmp / "events"
        self.events.mkdir()
        self.stub_bin = tmp / "bin"
        self.stub_bin.mkdir()
        self.runtime = tmp / "runtime"
        self.runtime.mkdir()
        self.saved = {k: getattr(server, k) for k in ("LOG_DIR", "LOG_FILE")}
        server.LOG_DIR = tmp
        server.LOG_FILE = tmp / "server.log"
        self.session = None

    def tearDown(self):
        if self.session is not None and self.session.busy():
            self.session.abort()
        for k, v in self.saved.items():
            setattr(server, k, v)
        self.tmp.cleanup()

    def start(self, transcriber=None, mic="pattern", env=None, **kwargs):
        if mic == "pattern":
            path = stt_stubs.install_fake_pattern_mic(self.stub_bin)
        else:
            path = stt_stubs.install_fake_pw_record(self.stub_bin)
        stub_env = {"STUB_EVENTS": str(self.events), **(env or {})}
        cmd = ["env", *(f"{k}={v}" for k, v in stub_env.items()), str(path)]
        options = dict(max_seconds=600, silence_seconds=0, continuous=True,
                       heartbeat_seconds=30, tick_seconds=0.02)
        options.update(kwargs)
        self.transcriber = transcriber or StubTranscriber()
        self.session = server.DictationSession(
            transcribe=self.transcriber, vad=StubVad(), runtime_dir=self.runtime,
            capture_cmd=cmd, **options)
        self.session.start()
        return self.session

    def wait_until(self, predicate, timeout=10.0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if predicate():
                return True
            time.sleep(0.02)
        return False

    def finished(self, timeout=10.0):
        self.assertTrue(self.wait_until(lambda: self.session.progress()["status"] in FINISHED,
                                        timeout), f"session never finished: {self.session.progress()}")
        return self.session.progress()

    def journal(self):
        path = pathlib.Path(self.session.progress()["session_dir"]) / "journal.jsonl"
        return [json.loads(line) for line in path.read_text().splitlines()]

    def mic_stopped(self):
        return self.session.capture_returncode() is not None


class StopTest(SessionCase):
    def test_stop_drains_the_microphone_and_transcribes_the_last_audio(self):
        s = self.start(mic="plain")
        time.sleep(0.4)
        s.stop("stop requested")
        p = self.finished()
        mic = stt_stubs.pw_record_result(self.events)
        self.assertEqual(p["status"], "done")
        self.assertEqual(s.captured_bytes, mic["written"], "audio left in the pipe was not read")
        audio = [x for samples, _ in self.transcriber.calls for x in samples]
        voiced = [x for x in audio if x != 0]
        self.assertEqual(len(voiced), mic["written"] // 2, "captured audio missing from segments")
        self.assertEqual(voiced[-1], 1, "the stop burst (the last words) was not transcribed")
        self.assertEqual(p["transcription"], "s0")

    def test_segments_commit_in_order_and_are_journalled(self):
        s = self.start(env={"STUB_PATTERN": "speech=1.5,silence=1.0", "STUB_SPEED": "8"})
        self.assertTrue(self.wait_until(lambda: s.progress()["segments_committed"] >= 3))
        s.stop("stop requested")
        p = self.finished()
        self.assertEqual(p["status"], "done")
        n = len(self.transcriber.calls)
        self.assertEqual(p["transcription"], " ".join(f"s{i}" for i in range(n)))
        lines = self.journal()
        self.assertEqual([line["seq"] for line in lines], list(range(n)))
        self.assertTrue(all(line["status"] == "ok" for line in lines))
        self.assertEqual(self.transcriber.calls[1][1], "s0", "the prompt is the text so far")

    def test_stop_is_idempotent(self):
        s = self.start()
        time.sleep(0.2)
        s.stop("stop requested")
        s.stop("stop requested")
        self.assertEqual(self.finished()["status"], "done")

    def test_an_empty_segment_is_counted_and_its_audio_kept(self):
        s = self.start(transcriber=StubTranscriber(empty_on=0), mic="plain")
        time.sleep(0.3)
        s.stop("stop requested")
        p = self.finished()
        self.assertEqual(p["status"], "done")
        self.assertEqual(p["empty_segments"], 1)
        self.assertEqual(len(p["kept_audio"]), 1)
        self.assertTrue(pathlib.Path(p["kept_audio"][0]).is_file())
        self.assertEqual(self.journal()[0]["status"], "empty")

    def test_abort_discards_the_text_and_the_journal(self):
        s = self.start()
        time.sleep(0.3)
        session_dir = pathlib.Path(s.progress()["session_dir"])
        s.abort()
        self.assertEqual(s.progress()["status"], "aborted")
        self.assertTrue(self.wait_until(self.mic_stopped))
        self.assertFalse(session_dir.exists(), "an aborted dictation left its text behind")

    def test_the_session_directory_is_private(self):
        s = self.start()
        mode = pathlib.Path(s.progress()["session_dir"]).stat().st_mode & 0o777
        self.assertEqual(mode, 0o700)
        s.stop("stop requested")
        self.finished()


class FailureTest(SessionCase):
    def assert_failed(self, p, words):
        self.assertEqual(p["status"], "failed")
        self.assertIn(words, p["error"])
        self.assertTrue(self.wait_until(self.mic_stopped), "the microphone was left open")

    def test_a_failing_segment_fails_loudly_keeping_text_and_audio(self):
        s = self.start(transcriber=StubTranscriber(fail_on=1),
                       env={"STUB_PATTERN": "speech=1.5,silence=1.0", "STUB_SPEED": "8"})
        p = self.finished()
        self.assert_failed(p, "segment 1")
        self.assertIn("CUDA out of memory", p["error"])
        self.assertEqual(p["transcription"], "s0", "the text before the failure is kept")
        kept = [pathlib.Path(k) for k in p["kept_audio"]]
        self.assertTrue(any("0001" in k.name for k in kept), p["kept_audio"])
        with wave.open(str(kept[0])) as w:
            self.assertEqual((w.getframerate(), w.getnchannels(), w.getsampwidth()), (16000, 1, 2))
            self.assertGreater(w.getnframes(), 0)
        self.assertIsNone(s.stop("stop requested"), "STOP after a failure changes nothing")
        self.assertEqual(s.progress()["status"], "failed")

    def test_the_microphone_dying_fails_the_session(self):
        self.start(env={"STUB_EXIT_AFTER": "0.3"})
        p = self.finished()
        self.assert_failed(p, "microphone")

    def test_a_backlog_past_the_ceiling_fails_the_session(self):
        self.start(transcriber=StubTranscriber(delay=2.0), backlog_ceiling_seconds=2,
                   env={"STUB_PATTERN": "speech=1.5,silence=1.0", "STUB_SPEED": "8"})
        p = self.finished()
        self.assert_failed(p, "cannot keep up")

    def test_the_drain_deadline_fails_the_session_and_keeps_pending_audio(self):
        block = threading.Event()
        self.addCleanup(block.set)
        s = self.start(transcriber=StubTranscriber(block=block), drain_base_seconds=0.3,
                       drain_rtf_factor=0,
                       env={"STUB_PATTERN": "speech=1.5,silence=1.0", "STUB_SPEED": "8"})
        time.sleep(0.6)
        s.stop("stop requested")
        p = self.finished()
        self.assert_failed(p, "still pending")
        self.assertGreater(len(p["kept_audio"]), 0)

    def test_a_journal_write_failure_fails_the_session(self):
        s = self.start(mic="plain")

        class BrokenJournal:
            path = pathlib.Path("/nonexistent/journal.jsonl")

            def record(self, **_entry):
                raise OSError(28, "No space left on device")

            def close(self):
                pass

        s._journal = BrokenJournal()
        time.sleep(0.3)
        s.stop("stop requested")
        p = self.finished()
        self.assert_failed(p, "journal")


class SafetyStopTest(SessionCase):
    def test_heartbeat_loss_stops_the_microphone(self):
        self.start(heartbeat_seconds=0.3)
        p = self.finished()
        self.assertEqual(p["status"], "done")
        self.assertIn("heartbeat", p["stop_reason"])
        self.assertTrue(self.mic_stopped())

    def test_keepalives_keep_it_recording(self):
        s = self.start(heartbeat_seconds=0.3)
        for _ in range(10):
            time.sleep(0.1)
            s.keepalive()
        self.assertEqual(s.progress()["status"], "recording")
        s.stop("stop requested")
        self.assertEqual(self.finished()["status"], "done")

    def test_no_speech_auto_stops(self):
        self.start(silence_seconds=0.5, env={"STUB_PATTERN": "silence=5"})
        p = self.finished()
        self.assertEqual(p["status"], "done")
        self.assertIn("no speech", p["stop_reason"])
        heard = {x for samples, _ in self.transcriber.calls for x in samples if x != 0}
        self.assertLessEqual(heard, {1}, "only the stub's stop burst may reach the transcriber")

    def test_speech_resets_the_no_speech_timer(self):
        s = self.start(silence_seconds=1,
                       env={"STUB_PATTERN": "speech=0.5,silence=0.4", "STUB_SPEED": "1"})
        time.sleep(2.5)
        self.assertEqual(s.progress()["status"], "recording")
        s.stop("stop requested")
        self.finished()

    def test_the_absolute_cap_stops_the_recording(self):
        self.start(max_seconds=0.5)
        p = self.finished()
        self.assertEqual(p["status"], "done")
        self.assertIn("limit", p["stop_reason"])


class CommandTest(SessionCase):
    """The server's command handlers over a real socket pair."""

    def setUp(self):
        super().setUp()
        path = stt_stubs.install_fake_pattern_mic(self.stub_bin)
        cmd = ["env", f"STUB_EVENTS={self.events}", str(path)]
        self.transcriber = StubTranscriber()
        patches = {
            "session": None, "transcriber": self.transcriber, "make_vad": StubVad,
            "CAPTURE_CMD": cmd, "DICTATION_DIR": self.runtime,
        }
        for name, value in patches.items():
            patcher = mock.patch.object(server, name, value, create=True)
            patcher.start()
            self.addCleanup(patcher.stop)

    def tearDown(self):
        if server.session is not None and server.session.busy():
            server.session.abort()
        super().tearDown()

    def send(self, command, params=None):
        a, b = socket.socketpair()
        with a, b:
            b.sendall((json.dumps({"command": command, "params": params or {}}) + "\n").encode())
            server.handle_client(a)
            return json.loads(b.makefile().readline())

    def test_start_requires_its_limits(self):
        reply = self.send("START", {"continuous": True})
        self.assertEqual(reply["status"], "error")
        self.assertIn("max_seconds", reply["message"])

    def test_a_dictation_over_the_socket(self):
        reply = self.send("START", {"continuous": True, "max_seconds": 600, "silence_seconds": 0})
        self.assertEqual(reply["status"], "recording")
        self.assertEqual(self.send("START", {"max_seconds": 60})["status"], "error",
                         "a second START while recording")
        self.assertEqual(self.send("KEEPALIVE")["status"], "recording")
        self.assertTrue(server.is_busy())
        time.sleep(0.3)
        reply = self.send("STOP")
        self.assertIn(reply["status"], ("draining", "done"))
        self.assertTrue(self.wait_until(lambda: self.send("PROGRESS")["status"] == "done"))
        final = self.send("PROGRESS")
        self.assertEqual(final["transcription"], "s0")
        self.assertFalse(server.is_busy())

    def test_abort_over_the_socket(self):
        self.send("START", {"max_seconds": 60})
        self.assertEqual(self.send("ABORT")["status"], "aborted")
        self.assertFalse(server.is_busy())

    def test_commands_without_a_session_are_refused(self):
        for command in ("STOP", "KEEPALIVE", "PROGRESS"):
            self.assertEqual(self.send(command)["status"], "error", command)

    def test_a_reply_longer_than_one_read_arrives_whole(self):
        long_text = "word " * 5000
        with mock.patch.object(server, "transcriber", lambda samples, prompt: long_text):
            self.send("START", {"max_seconds": 60})
            time.sleep(0.3)
            self.send("STOP")
            self.assertTrue(self.wait_until(lambda: self.send("PROGRESS")["status"] == "done"))
            self.assertEqual(self.send("PROGRESS")["transcription"], long_text.strip())


if __name__ == "__main__":
    unittest.main()
