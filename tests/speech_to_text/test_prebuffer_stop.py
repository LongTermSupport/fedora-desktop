"""Pre-buffer streaming mode's stop path, run for real (Plan 00148 Phase 0).

Runs wsi-stream's run_prebuffered_streaming() with a stub RealtimeSTT whose model takes
a set time to load and a fake pw-record on PATH, and sends this process real SIGTERMs.
Checks when the microphone closes and that every byte it wrote reached the recorder.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import argparse
import importlib.util
import os
import pathlib
import signal
import sys
import tempfile
import threading
import time
import types
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_prebuffer")


class StubRecorder:
    """AudioToTextRecorder stand-in: slow to construct, counts what it is fed.

    load_seconds None is a model that finishes loading only once the microphone has closed
    (its result file exists), at most LOAD_CAP_SECONDS: a stop that waited for the model
    shows as a microphone closed after `ready`, however loaded the host.
    """

    LOAD_CAP_SECONDS = 30
    load_seconds = 0.0
    mic_result = None
    instance = None

    def __init__(self, **config):
        if self.load_seconds is None:
            cap = time.monotonic() + self.LOAD_CAP_SECONDS
            while not self.mic_result.exists() and time.monotonic() < cap:
                time.sleep(0.01)
        else:
            time.sleep(self.load_seconds)
        self.ready_at = time.monotonic()
        self.config = config
        self.fed_bytes = 0
        self.started = False
        self.stopped = False
        StubRecorder.instance = self

    def start(self):
        self.started = True

    def feed_audio(self, audio):
        self.fed_bytes += len(audio) * 2
        wsi_stream.on_realtime_update("words so far")

    def stop(self):
        self.stopped = True

    def shutdown(self):
        pass


class PrebufferStopTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        tmp = pathlib.Path(self.tmp.name)
        self.events = tmp / "events"
        self.events.mkdir()
        stub_bin = tmp / "bin"
        stub_bin.mkdir()
        stt_stubs.install_fake_pw_record(stub_bin)

        self.saved_env = {k: os.environ.get(k) for k in ("PATH", "STUB_EVENTS")}
        os.environ["PATH"] = f"{stub_bin}:{os.environ['PATH']}"
        os.environ["STUB_EVENTS"] = str(self.events)

        self.saved_signals = {s: signal.getsignal(s) for s in (signal.SIGTERM, signal.SIGINT)}
        self.saved_modules = sys.modules.get("RealtimeSTT")
        sys.modules["RealtimeSTT"] = types.SimpleNamespace(AudioToTextRecorder=StubRecorder)

        self.states = []
        self.pasted = []
        self.patches = {
            "check_dependencies": lambda: True,
            "resolve_model": lambda mode, language: "base",
            "resolve_realtime_model": lambda language: f"realtime-for-{language}",
            "notify": lambda state, message: self.states.append(state),
            "emit_dbus_signal": lambda *a: None,
            "desktop_notification": lambda *a: None,
            "copy_to_clipboard": lambda text, use_clipboard=False: self.pasted.append(text) or True,
            "PID_FILE": tmp / "stt.pid",
            "CACHE_DIR": tmp / "cache",
            "BUFFER_FILE": tmp / "cache" / "buffer.txt",
            "TRANSCRIPTION_FILE": tmp / "cache" / "last.txt",
            "FINAL_REALTIME_WAIT_SECONDS": 0.2,
            "current_text": "",
            "recorder": None,
            "stop_requested": False,
        }
        self.saved_attrs = {k: getattr(wsi_stream, k) for k in self.patches}
        for k, v in self.patches.items():
            setattr(wsi_stream, k, v)

    def tearDown(self):
        for k, v in self.saved_attrs.items():
            setattr(wsi_stream, k, v)
        for sig, handler in self.saved_signals.items():
            signal.signal(sig, handler)
        if self.saved_modules is None:
            sys.modules.pop("RealtimeSTT", None)
        else:
            sys.modules["RealtimeSTT"] = self.saved_modules
        for k, v in self.saved_env.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
        self.tmp.cleanup()

    def run_mode(self, load_seconds, grace, term_after, after="mic"):
        """Run pre-buffer mode, SIGTERM at each offset in term_after; returns facts.

        The offsets count from the moment `after` is ready: "mic", the fake pw-record's stop
        handler installed (its ready file), or "model", that and the stub recorder loaded.
        A timer from the start raced both on a loaded host. If it never becomes ready no
        TERM is sent at all (one before the handler would end this test process).
        """
        StubRecorder.load_seconds = load_seconds
        StubRecorder.mic_result = self.events / "pw-record.json"
        StubRecorder.instance = None
        args = argparse.Namespace(
            language="en", timeout=30, claude_process=False, wrap_marker=False,
            auto_paste=False, clipboard=False, no_auto_enter=True, paste_with_shift=1,
        )
        stop = wsi_stream.GracefulStop(grace)
        terms = []
        seen = []
        mic_ready = (self.events / "pw-record.ready").exists
        ready = {"mic": mic_ready,
                 "model": lambda: mic_ready() and StubRecorder.instance is not None}[after]
        finished = threading.Event()

        def send_terms():
            cap = time.monotonic() + 30
            while not ready() and not finished.is_set() and time.monotonic() < cap:
                time.sleep(0.005)
            seen.append(ready())
            if not seen[0]:
                return
            start = time.monotonic()
            for offset in term_after:
                if finished.wait(max(0.0, start + offset - time.monotonic())):
                    return
                terms.append(time.monotonic())
                os.kill(os.getpid(), signal.SIGTERM)

        sender = threading.Thread(target=send_terms, daemon=True)
        sender.start()
        try:
            rc = wsi_stream.run_prebuffered_streaming(args, stop)
        finally:
            finished.set()
            sender.join(5)
        self.assertEqual(seen, [True], f"the {after} never became ready, so no TERM was timed from it")
        mic = stt_stubs.pw_record_result(self.events)
        return {
            "rc": rc,
            "terms": terms,
            "mic_stopped": mic["stopped_at"],
            "written": mic["written"],
            "fed": StubRecorder.instance.fed_bytes,
            "ready": StubRecorder.instance.ready_at,
            "config": StubRecorder.instance.config,
        }

    # The model below loads only once the microphone has closed, so a stop that waited for
    # it shows as mic_stopped after ready. The ceilings on a gap are 3 s past what the code
    # waits, ten times the 0.3 s grace and far below the 30 s one (CLAUDE/QA.md
    # test-wall-clock-ceiling-under-a-second).
    SLACK_SECONDS = 3.0

    def test_term_during_the_load_closes_the_microphone_after_the_grace(self):
        r = self.run_mode(load_seconds=None, grace=0.3, term_after=[0.3])
        self.assertEqual(r["rc"], 0)
        gap = r["mic_stopped"] - r["terms"][0]
        self.assertGreaterEqual(gap, 0.25, "microphone closed before the grace ran out")
        self.assertLess(gap, 0.3 + self.SLACK_SECONDS, "microphone stayed open after the grace")
        self.assertLess(r["mic_stopped"], r["ready"], "waited for the model before closing the mic")
        self.assertEqual(r["fed"], r["written"], "audio captured before the stop was dropped")
        self.assertIn("TRANSCRIBING", self.states)
        self.assertNotIn("RECORDING", self.states)
        self.assertEqual(self.pasted, ["Words so far"])

    def test_second_term_during_the_load_stops_at_once(self):
        r = self.run_mode(load_seconds=None, grace=30, term_after=[0.3, 0.6])
        self.assertEqual(r["rc"], 0)
        self.assertLess(r["mic_stopped"] - r["terms"][1], self.SLACK_SECONDS,
                        "the second TERM did not stop at once (the grace is 30 s)")
        self.assertLess(r["mic_stopped"], r["ready"])
        self.assertEqual(r["fed"], r["written"])

    def test_term_after_the_load_keeps_recording_for_the_grace(self):
        # The TERM comes 1 s after the model is ready, so the recording loop has taken over
        r = self.run_mode(load_seconds=0.3, grace=0.5, term_after=[1.0], after="model")
        self.assertEqual(r["rc"], 0)
        gap = r["mic_stopped"] - r["terms"][0]
        self.assertGreaterEqual(gap, 0.45)
        self.assertLess(gap, 0.5 + self.SLACK_SECONDS)
        self.assertEqual(r["fed"], r["written"], "the pipe was not drained at stop")
        self.assertIn("RECORDING", self.states)
        self.assertEqual(self.pasted, ["Words so far"])

    def test_the_live_preview_model_is_the_resolvers_and_no_log_file_is_written(self):
        # Plan 00234 Task 4.1: RealtimeSTT's default real-time model is one nothing
        # downloads, and its realtimesst.log lands in the working directory (the home
        # directory, when the panel starts wsi-stream)
        r = self.run_mode(load_seconds=0.0, grace=0, term_after=[0.3], after="model")
        self.assertEqual(r["rc"], 0)
        self.assertTrue(r["config"]["enable_realtime_transcription"])
        self.assertEqual(r["config"]["realtime_model_type"], "realtime-for-en")
        self.assertIs(r["config"]["no_log_file"], True)
        self.assertEqual(r["config"]["model"], "base")


class RecorderLoadFailureTest(unittest.TestCase):
    """Plan 00234 Task 4.1: a recorder that fails to load ends wsi-stream at once, the panel
    told why. RealtimeSTT 1.0.0's main model loads in a worker thread that is not a daemon
    (`thread.deamon = True`, misspelt): an error there ends only that thread and the
    constructor waits for ever, and an error the constructor raises leaves that thread
    keeping the process alive. Either way the dictation hung without a word."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.exits = []
        self.exited = threading.Event()
        self.signals = []
        self.urgent = []
        self.pid_file = pathlib.Path(self.tmp.name) / "stt.pid"
        self.pid_file.write_text(str(os.getpid()))
        self.hook = threading.excepthook
        self.addCleanup(setattr, threading, "excepthook", self.hook)
        for patcher in (
                mock.patch.dict(sys.modules, {"RealtimeSTT": None}),
                mock.patch.multiple(
                    wsi_stream, PID_FILE=self.pid_file, ended_with=None,
                    emit_dbus_signal=lambda name, value: self.signals.append(value),
                    urgent_notification=self.urgent.append),
                mock.patch.object(wsi_stream.os, "_exit", self.fake_exit)):
            patcher.start()
            self.addCleanup(patcher.stop)

    def fake_exit(self, code):
        self.exits.append(code)
        self.exited.set()

    def recorder_class(self, build):
        sys.modules["RealtimeSTT"] = types.SimpleNamespace(AudioToTextRecorder=build)

    def assert_ended_with(self, reason):
        self.assertEqual(self.exits, [1], "wsi-stream did not end at once")
        self.assertIn("ERROR", self.signals)
        self.assertEqual(len(self.urgent), 1, "the user is not told")
        self.assertIn(reason, self.urgent[0])
        self.assertFalse(self.pid_file.exists(), "the PID file outlived the process")
        self.assertIs(threading.excepthook, self.hook, "the thread hook was left installed")

    def test_a_constructor_that_raises_ends_the_process_and_releases_the_microphone(self):
        def build(**config):
            raise RuntimeError("CUDA failed with error out of memory")

        self.recorder_class(build)
        release = mock.Mock()
        wsi_stream.load_recorder({"model": "base"}, release)
        self.assert_ended_with("CUDA failed with error out of memory")
        release.assert_called_once_with()

    def test_a_model_thread_that_dies_while_the_constructor_waits_ends_the_process(self):
        def build(**config):
            def load():
                raise OSError("LocalEntryNotFoundError: Systran/faster-whisper-tiny")

            threading.Thread(target=load).start()
            # RealtimeSTT waits here for the model for ever; the stand-in stops waiting
            # once the process has been ended
            self.exited.wait(10)
            return object()

        self.recorder_class(build)
        wsi_stream.load_recorder({"model": "base"})
        self.assert_ended_with("LocalEntryNotFoundError")

    def test_a_recorder_that_loads_is_returned_and_nothing_ends(self):
        built = object()
        self.recorder_class(lambda **config: built)
        self.assertIs(wsi_stream.load_recorder({"model": "base"}), built)
        self.assertEqual(self.exits, [])
        self.assertIs(threading.excepthook, self.hook)


if __name__ == "__main__":
    unittest.main()
