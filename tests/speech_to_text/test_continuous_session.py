"""wsi-stream-server's dictation session, run for real with stubs (Plan 00148 Tasks 2.2, 2.3).

A DictationSession is driven with a fake pw-record (stt_stubs), a stub VAD (any non-zero
sample is speech) and a stub transcriber, so the whole loop runs: capture, segmenting,
the one ordered worker, the journal, STOP's drain, and every way the session can end:
done, auto-stopped (heartbeat lost, no speech, the absolute cap) and FAILED (a segment
raises, the microphone dies, the backlog passes its ceiling, the drain deadline expires,
the journal cannot be written). A FAILED session keeps the audio it did not transcribe and
reports the text committed so far; nothing is dropped without saying so.

Also the server's commands over a real socket pair: START's parameters are required,
STOP drains, PROGRESS reports (with the text so far when asked), and PASTED records how
much of the text a chunking client pasted, so a dictation handed over later owes only
the rest.

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
        options = dict(max_seconds=600, silence_seconds=0, continuous=True, vad=StubVad(),
                       heartbeat_seconds=30, tick_seconds=0.02)
        options.update(kwargs)
        self.transcriber = transcriber or StubTranscriber()
        self.session = server.DictationSession(
            transcribe=self.transcriber, runtime_dir=self.runtime, capture_cmd=cmd, **options)
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


class StatsLineTest(SessionCase):
    """Every finished dictation logs one line of figures, which Plan 00148's triage reads.

    It is how the speed of real dictation is measured without loading a second model.
    """

    def stats(self):
        lines = [line.split("Dictation stats: ", 1)[1]
                 for line in server.LOG_FILE.read_text().splitlines() if "Dictation stats: " in line]
        self.assertEqual(len(lines), 1, f"wanted one stats line, got {lines}")
        return dict(field.split("=", 1) for field in lines[0].split())

    def test_a_done_dictation_logs_its_figures(self):
        s = self.start(transcriber=StubTranscriber(delay=0.05),
                       env={"STUB_PATTERN": "speech=1.5,silence=1.0", "STUB_SPEED": "8"})
        self.assertTrue(self.wait_until(lambda: s.progress()["segments_committed"] >= 3))
        s.stop("stop requested")
        p = self.finished()
        stats = self.stats()
        self.assertEqual(stats["outcome"], "done")
        self.assertEqual(stats["mode"], "continuous")
        self.assertEqual(int(stats["segments"]), p["segments_committed"])
        self.assertGreater(float(stats["audio_s"]), 0)
        self.assertGreater(float(stats["rtf_max"]), 0)
        self.assertGreaterEqual(float(stats["rtf_max"]), float(stats["rtf_mean"]))
        self.assertGreater(float(stats["backlog_max_s"]), 0)
        cuts = dict(c.split(":") for c in stats["cuts"].split(","))
        self.assertEqual(set(cuts), {"pause", "soft", "hard", "stop"})
        self.assertEqual(sum(int(v) for v in cuts.values()), int(stats["segments"]))

    def test_a_failed_dictation_logs_its_figures_too(self):
        self.start(transcriber=StubTranscriber(delay=2.0), backlog_ceiling_seconds=2,
                   env={"STUB_PATTERN": "speech=1.5,silence=1.0", "STUB_SPEED": "8"})
        self.finished()
        self.assertTrue(self.wait_until(lambda: "Dictation stats: " in server.LOG_FILE.read_text()))
        self.assertEqual(self.stats()["outcome"], "failed")

    def test_a_whole_clip_says_so(self):
        s = self.start(mic="plain", continuous=False, vad=None)
        time.sleep(0.3)
        s.stop("stop requested")
        self.finished()
        stats = self.stats()
        self.assertEqual((stats["mode"], stats["segments"]), ("whole", "1"))

    def test_an_aborted_dictation_logs_no_figures(self):
        s = self.start()
        time.sleep(0.3)
        s.abort()
        self.assertTrue(self.wait_until(self.mic_stopped))
        self.assertNotIn("Dictation stats: ", server.LOG_FILE.read_text())


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

    def test_after_a_drain_deadline_the_session_stays_busy_until_the_transcription_returns(self):
        block = threading.Event()
        self.addCleanup(block.set)
        s = self.start(transcriber=StubTranscriber(block=block), drain_base_seconds=0.3,
                       drain_rtf_factor=0, mic="plain")
        time.sleep(0.3)
        s.stop("stop requested")
        self.assert_failed(self.finished(), "still pending")
        self.assertTrue(s.busy(), "a new dictation could start while the model is still in use")
        block.set()
        self.assertTrue(self.wait_until(lambda: not s.busy()), "never became free")

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

    def test_a_stop_while_stopping_reports_stopping_not_recording(self):
        s = self.start(mic="plain")
        time.sleep(0.2)
        release = threading.Event()
        self.addCleanup(release.set)
        terminate = s._proc.terminate

        def slow_terminate():
            release.wait(5)
            terminate()

        s._proc.terminate = slow_terminate
        first = threading.Thread(target=s.stop, args=("heartbeat lost",))
        first.start()
        self.assertTrue(self.wait_until(lambda: s._stopping))
        self.assertIsNone(s.stop("stop requested"))
        self.assertEqual(s.progress()["status"], "stopping")
        release.set()
        first.join()
        self.assertEqual(self.finished()["status"], "done")


class WholeClipTest(SessionCase):
    """Continuous dictation off: no VAD, the whole recording is transcribed once at stop."""

    def test_without_continuous_the_whole_clip_is_one_transcription_and_no_vad_runs(self):
        s = self.start(mic="plain", continuous=False, vad=None)
        time.sleep(0.5)
        s.stop("stop requested")
        p = self.finished()
        self.assertEqual(p["status"], "done", p)
        self.assertEqual(len(self.transcriber.calls), 1)
        samples, prompt = self.transcriber.calls[0]
        self.assertGreaterEqual(len(samples), s.captured_bytes // 2, "audio missing from the clip")
        self.assertEqual(prompt, "")
        self.assertEqual(p["transcription"], "s0")

    def test_a_vad_without_continuous_or_none_with_it_is_refused(self):
        with self.assertRaises(ValueError):
            self.start(continuous=False, vad=StubVad())
        with self.assertRaises(ValueError):
            self.start(continuous=True, vad=None)


class VadAdapterTest(unittest.TestCase):
    """The Silero adapter's checks, without faster-whisper: it fails loudly, never guesses."""

    SILENCE = [server.array.array("h", [0] * server.FRAME_SAMPLES)] * 4

    def test_a_wrong_number_of_probabilities_is_an_error(self):
        with self.assertRaisesRegex(RuntimeError, "3 probabilities for 4 frames"):
            server.checked_probabilities([0.1, 0.1, 0.1], 4, "Silero VAD")

    def test_values_that_are_not_probabilities_are_an_error(self):
        for bad in (float("nan"), 1.5, -0.1):
            with self.assertRaises(RuntimeError, msg=bad):
                server.checked_probabilities([0.1, bad], 2, "Silero VAD")

    def test_the_input_shape_that_works_on_silence_is_chosen(self):
        def wrong_shape(frames):
            return [0.0]  # one value for the whole batch: the 1.2.x-on-2-D failure
        def right_shape(frames):
            return [0.01] * len(frames)
        name, call = server.select_vad_input([("2-D", wrong_shape), ("1-D", right_shape)],
                                             self.SILENCE)
        self.assertEqual(name, "1-D")
        self.assertIs(call, right_shape)

    def test_no_working_input_shape_fails_loudly_naming_each(self):
        def asserts(frames):
            raise AssertionError("Input should be a 2D array")
        def calls_silence_speech(frames):
            return [0.9] * len(frames)
        with self.assertRaises(RuntimeError) as caught:
            server.select_vad_input([("2-D", calls_silence_speech), ("1-D", asserts)],
                                    self.SILENCE)
        message = str(caught.exception)
        self.assertIn("2-D", message)
        self.assertIn("silence as speech", message)
        self.assertIn("Input should be a 2D array", message)


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

    def wait_for_state(self, state):
        # session.progress() directly: a PROGRESS command would deliver the text
        self.assertTrue(self.wait_until(lambda: server.session.progress()["status"] == state),
                        server.session.progress())

    def test_text_nobody_collected_survives_and_the_next_start_hands_it_over(self):
        with mock.patch.object(server, "HEARTBEAT_SECONDS", 0.3):
            self.send("START", {"continuous": True, "max_seconds": 600, "silence_seconds": 0})
            first = server.session
            self.wait_for_state("done")
        self.assertIn("heartbeat", first.progress()["stop_reason"])
        journal = pathlib.Path(first.progress()["session_dir"]) / "journal.jsonl"
        with mock.patch.object(server, "idle_timeout", 1), \
                mock.patch.object(server, "last_activity_time", 0):
            self.assertFalse(server.check_idle_timeout(),
                             "the server would idle out holding text nobody collected")
        reply = self.send("START", {"continuous": True, "max_seconds": 600, "silence_seconds": 0})
        self.assertEqual(reply["status"], "recording")
        handed = reply.get("undelivered")
        self.assertIsNotNone(handed, "the next START did not hand over the undelivered text")
        self.assertEqual(handed["transcription"], "s0")
        self.assertIn("heartbeat", handed["stop_reason"])
        self.assertEqual(handed["journal"], str(journal))
        self.assertTrue(journal.is_file(), "undelivered text was deleted")
        self.send("ABORT")
        reply = self.send("START", {"max_seconds": 60})
        self.assertNotIn("undelivered", reply, "handed over twice")
        self.assertTrue(journal.is_file(), "the journal of text once undelivered is kept")

    def test_a_delivered_dictation_is_discarded_at_the_next_start(self):
        self.send("START", {"max_seconds": 60})
        first_dir = pathlib.Path(server.session.progress()["session_dir"])
        time.sleep(0.3)
        self.send("STOP")
        self.assertTrue(self.wait_until(lambda: self.send("PROGRESS")["status"] == "done"))
        reply = self.send("START", {"max_seconds": 60})
        self.assertNotIn("undelivered", reply)
        self.assertFalse(first_dir.exists())

    def test_stop_during_a_stop_already_under_way_replies_stopping(self):
        self.send("START", {"max_seconds": 60})
        with server.session._lock:
            server.session._stopping = True
        try:
            self.assertEqual(self.send("STOP")["status"], "stopping")
            self.assertEqual(self.send("KEEPALIVE")["status"], "stopping")
        finally:
            with server.session._lock:
                server.session._stopping = False

    def test_without_continuous_no_vad_is_made(self):
        def no_vad():
            raise AssertionError("the Silero VAD adapter was built for a non-continuous recording")
        with mock.patch.object(server, "make_vad", no_vad):
            reply = self.send("START", {"continuous": False, "max_seconds": 60})
        self.assertEqual(reply["status"], "recording", reply)

    def test_continuous_with_a_vad_that_will_not_load_is_refused_loudly(self):
        def broken_vad():
            raise RuntimeError("Silero VAD returned 1 probabilities for 32 frames")
        with mock.patch.object(server, "make_vad", broken_vad):
            reply = self.send("START", {"continuous": True, "max_seconds": 60})
        self.assertEqual(reply["status"], "error")
        self.assertIn("1 probabilities for 32 frames", reply["message"])
        self.assertIsNone(server.session)

    def test_start_is_refused_while_a_failed_dictation_still_holds_the_model(self):
        block = threading.Event()
        self.addCleanup(block.set)
        with mock.patch.object(server, "transcriber", StubTranscriber(block=block)), \
                mock.patch.object(server, "DRAIN_BASE_SECONDS", 0.3), \
                mock.patch.object(server, "DRAIN_RTF_FACTOR", 0):
            self.send("START", {"max_seconds": 60})
            time.sleep(0.3)
            self.send("STOP")
            self.wait_for_state("failed")
            reply = self.send("START", {"max_seconds": 60})
            self.assertEqual(reply["status"], "error")
            self.assertIn("still", reply["message"])
            block.set()
            self.assertTrue(self.wait_until(lambda: not server.is_busy()))

    def test_progress_with_text_carries_the_text_so_far_and_does_not_deliver_it(self):
        self.send("START", {"continuous": True, "max_seconds": 600, "silence_seconds": 0})
        self.assertNotIn("text_so_far", self.send("PROGRESS"), "only asked for, never by default")
        self.assertEqual(self.send("PROGRESS", {"with_text": True})["text_so_far"], "")
        time.sleep(0.3)
        self.send("STOP")
        self.wait_for_state("done")
        reply = self.send("PROGRESS", {"with_text": True})
        self.assertEqual(reply["text_so_far"], reply["transcription"])
        self.assertTrue(server.session.delivered, "the final text was delivered")
        reply = self.send("PROGRESS", {"with_text": "yes"})
        self.assertEqual(reply["status"], "error")
        self.assertIn("with_text", reply["message"])

    def end_by_heartbeat(self, text):
        """A dictation of `text` whose client vanished: done, and nobody collected it."""
        patcher = mock.patch.object(server, "transcriber", lambda samples, prompt: text)
        patcher.start()
        self.addCleanup(patcher.stop)
        with mock.patch.object(server, "HEARTBEAT_SECONDS", 0.3):
            self.send("START", {"continuous": True, "max_seconds": 600, "silence_seconds": 0})
            self.wait_for_state("done")
        return server.session

    def test_text_already_pasted_in_chunks_is_not_handed_over_again(self):
        self.end_by_heartbeat("one two three")
        self.assertEqual(self.send("PASTED", {"chars": 3})["status"], "ok")
        handed = self.send("START", {"max_seconds": 60})["undelivered"]
        self.assertEqual(handed["transcription"], "one two three")
        self.assertEqual(handed["pasted_chars"], 3)

    def test_a_dictation_pasted_whole_in_chunks_owes_nothing(self):
        first = self.end_by_heartbeat("one two three")
        self.send("PASTED", {"chars": len("one two three")})
        reply = self.send("START", {"max_seconds": 60})
        self.assertNotIn("undelivered", reply)
        self.assertFalse(first.session_dir.exists())

    def test_pasted_refuses_what_cannot_be_a_count_of_the_text(self):
        self.end_by_heartbeat("one two three")
        for chars in (-1, 14, "3", True, None):
            reply = self.send("PASTED", {"chars": chars})
            self.assertEqual(reply["status"], "error", chars)
        self.assertEqual(self.send("PASTED", {"chars": 7})["status"], "ok")
        self.assertEqual(self.send("PASTED", {"chars": 3})["status"], "error",
                         "the pasted count never goes back")

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
