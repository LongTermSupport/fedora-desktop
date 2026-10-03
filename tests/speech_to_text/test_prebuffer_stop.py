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

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_prebuffer")


class StubRecorder:
    """AudioToTextRecorder stand-in: slow to construct, counts what it is fed."""

    load_seconds = 0.0
    instance = None

    def __init__(self, **config):
        time.sleep(self.load_seconds)
        self.ready_at = time.monotonic()
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

    def run_mode(self, load_seconds, grace, term_after):
        """Run pre-buffer mode, SIGTERM at each offset in term_after; returns facts."""
        StubRecorder.load_seconds = load_seconds
        args = argparse.Namespace(
            language="en", timeout=30, claude_process=False, wrap_marker=False,
            auto_paste=False, clipboard=False, no_auto_enter=True, paste_with_shift=1,
        )
        stop = wsi_stream.GracefulStop(grace)
        terms = []
        for offset in term_after:
            def send():
                terms.append(time.monotonic())
                os.kill(os.getpid(), signal.SIGTERM)
            threading.Timer(offset, send).start()
        rc = wsi_stream.run_prebuffered_streaming(args, stop)
        mic = stt_stubs.pw_record_result(self.events)
        return {
            "rc": rc,
            "terms": terms,
            "mic_stopped": mic["stopped_at"],
            "written": mic["written"],
            "fed": StubRecorder.instance.fed_bytes,
            "ready": StubRecorder.instance.ready_at,
        }

    def test_term_during_the_load_closes_the_microphone_after_the_grace(self):
        r = self.run_mode(load_seconds=2.0, grace=0.3, term_after=[0.3])
        self.assertEqual(r["rc"], 0)
        gap = r["mic_stopped"] - r["terms"][0]
        self.assertGreaterEqual(gap, 0.25, "microphone closed before the grace ran out")
        self.assertLess(gap, 0.9, "microphone stayed open after the grace")
        self.assertLess(r["mic_stopped"], r["ready"], "waited for the model before closing the mic")
        self.assertEqual(r["fed"], r["written"], "audio captured before the stop was dropped")
        self.assertIn("TRANSCRIBING", self.states)
        self.assertNotIn("RECORDING", self.states)
        self.assertEqual(self.pasted, ["Words so far"])

    def test_second_term_during_the_load_stops_at_once(self):
        r = self.run_mode(load_seconds=2.0, grace=30, term_after=[0.3, 0.6])
        self.assertEqual(r["rc"], 0)
        self.assertLess(r["mic_stopped"] - r["terms"][1], 0.4)
        self.assertLess(r["mic_stopped"], r["ready"])
        self.assertEqual(r["fed"], r["written"])

    def test_term_after_the_load_keeps_recording_for_the_grace(self):
        r = self.run_mode(load_seconds=0.3, grace=0.5, term_after=[1.0])
        self.assertEqual(r["rc"], 0)
        gap = r["mic_stopped"] - r["terms"][0]
        self.assertGreaterEqual(gap, 0.45)
        self.assertLess(gap, 1.1)
        self.assertEqual(r["fed"], r["written"], "the pipe was not drained at stop")
        self.assertIn("RECORDING", self.states)
        self.assertEqual(self.pasted, ["Words so far"])


if __name__ == "__main__":
    unittest.main()
