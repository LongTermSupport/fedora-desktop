"""Delayed stop in wsi-stream (Plan 00148 Phase 0).

The extension stops a recording with SIGTERM the moment Insert is pressed. The
first TERM now starts a grace during which recording continues; a second TERM,
or a grace of 0, stops at once; SIGINT and SIGUSR1 (Escape) never wait. These
tests drive that state, the real signal handlers, the grace reader and the
end-of-recording drain without RealtimeSTT, a microphone or GNOME.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import array
import importlib.util
import os
import pathlib
import signal
import subprocess
import sys
import tempfile
import threading
import unittest
from importlib.machinery import SourceFileLoader

_REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
_SCRIPT = _REPO_ROOT / "files" / "home" / ".local" / "bin" / "wsi-stream"

_loader = SourceFileLoader("wsi_stream", str(_SCRIPT))
_spec = importlib.util.spec_from_loader("wsi_stream", _loader)
wsi_stream = importlib.util.module_from_spec(_spec)
_loader.exec_module(wsi_stream)


class FakeClock:
    def __init__(self):
        self.now = 100.0

    def __call__(self):
        return self.now


class GracefulStopTest(unittest.TestCase):
    def setUp(self):
        self.clock = FakeClock()

    def make(self, grace):
        return wsi_stream.GracefulStop(grace, clock=self.clock)

    def test_first_request_starts_the_grace_and_recording_continues(self):
        stop = self.make(3)
        self.assertTrue(stop.request())
        self.assertTrue(stop.requested)
        self.assertFalse(stop.due())
        self.clock.now += 2.9
        self.assertFalse(stop.due())

    def test_stop_is_due_when_the_grace_ends(self):
        stop = self.make(3)
        stop.request()
        self.clock.now += 3
        self.assertTrue(stop.due())

    def test_second_request_during_the_grace_stops_at_once(self):
        stop = self.make(3)
        stop.request()
        self.clock.now += 1
        self.assertFalse(stop.request())
        self.assertTrue(stop.due())

    def test_zero_grace_stops_at_once(self):
        stop = self.make(0)
        self.assertFalse(stop.request())
        self.assertTrue(stop.due())

    def test_stop_now_never_waits_and_can_discard(self):
        stop = self.make(3)
        stop.stop_now(discard=True)
        self.assertTrue(stop.due())
        self.assertTrue(stop.discard)

    def test_stop_now_during_a_grace_cuts_it_short_without_discarding(self):
        stop = self.make(3)
        stop.request()
        stop.stop_now()
        self.assertTrue(stop.due())
        self.assertFalse(stop.discard)

    def test_nothing_is_due_before_any_request(self):
        stop = self.make(3)
        self.assertFalse(stop.requested)
        self.assertFalse(stop.due())

    def test_pending_stop_is_announced_exactly_once(self):
        stop = self.make(3)
        self.assertFalse(stop.take_notice())
        stop.request()
        self.assertTrue(stop.take_notice())
        self.assertFalse(stop.take_notice())

    def test_no_announcement_once_the_stop_is_already_due(self):
        stop = self.make(3)
        stop.request()
        stop.request()
        self.assertFalse(stop.take_notice())

    def test_negative_grace_is_refused(self):
        with self.assertRaises(ValueError):
            self.make(-1)


class SignalHandlerTest(unittest.TestCase):
    """The real handlers, driven by real signals to this process."""

    def setUp(self):
        self.saved = {s: signal.getsignal(s) for s in (signal.SIGTERM, signal.SIGINT, signal.SIGUSR1)}

    def tearDown(self):
        for sig, handler in self.saved.items():
            signal.signal(sig, handler)

    def test_term_then_term(self):
        stop = wsi_stream.GracefulStop(30)
        wsi_stream.install_stop_handlers(stop, abort_on_usr1=True)
        os.kill(os.getpid(), signal.SIGTERM)
        self.assertTrue(stop.requested)
        self.assertFalse(stop.due(), "first TERM must keep recording for the grace")
        os.kill(os.getpid(), signal.SIGTERM)
        self.assertTrue(stop.due(), "second TERM must stop at once")
        self.assertFalse(stop.discard)

    def test_usr1_discards_at_once(self):
        stop = wsi_stream.GracefulStop(30)
        wsi_stream.install_stop_handlers(stop, abort_on_usr1=True)
        os.kill(os.getpid(), signal.SIGUSR1)
        self.assertTrue(stop.due())
        self.assertTrue(stop.discard)

    def test_int_stops_at_once_without_discarding(self):
        stop = wsi_stream.GracefulStop(30)
        wsi_stream.install_stop_handlers(stop)
        os.kill(os.getpid(), signal.SIGINT)
        self.assertTrue(stop.due())
        self.assertFalse(stop.discard)

    def test_usr1_is_left_alone_unless_asked(self):
        before = signal.getsignal(signal.SIGUSR1)
        wsi_stream.install_stop_handlers(wsi_stream.GracefulStop(3))
        self.assertIs(signal.getsignal(signal.SIGUSR1), before)


class ReadStopGraceTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.saved = wsi_stream.STOP_GRACE_READER

    def tearDown(self):
        wsi_stream.STOP_GRACE_READER = self.saved
        self.tmp.cleanup()

    def reader(self, body):
        path = pathlib.Path(self.tmp.name) / "wsi-stop-grace"
        path.write_text(f"#!/usr/bin/bash\n{body}\n")
        path.chmod(0o755)
        wsi_stream.STOP_GRACE_READER = path

    def test_reads_the_value(self):
        self.reader("echo 4")
        self.assertEqual(wsi_stream.read_stop_grace(), 4)

    def test_reader_failure_is_raised_with_its_message(self):
        self.reader("echo 'schema missing' >&2; exit 1")
        with self.assertRaisesRegex(RuntimeError, "schema missing"):
            wsi_stream.read_stop_grace()

    def test_non_number_is_raised(self):
        self.reader("echo 'uint32 3'")
        with self.assertRaisesRegex(RuntimeError, "non-number"):
            wsi_stream.read_stop_grace()

    def test_missing_reader_is_raised(self):
        wsi_stream.STOP_GRACE_READER = pathlib.Path(self.tmp.name) / "absent"
        with self.assertRaises(OSError):
            wsi_stream.read_stop_grace()

    def test_deployed_reader_sits_beside_the_script(self):
        self.assertEqual(self.saved.name, "wsi-stop-grace")
        self.assertEqual(self.saved.parent, _SCRIPT.resolve().parent)
        self.assertTrue(self.saved.exists())


class DrainTest(unittest.TestCase):
    """At stop, audio already captured is fed, not dropped."""

    def test_remaining_audio_is_fed_up_to_eof(self):
        # A recorder that has 5 chunks plus an odd byte queued and exits on SIGTERM.
        payload_chunks = 5
        chunk = 64
        with tempfile.TemporaryDirectory() as tmp:
            ready = pathlib.Path(tmp) / "ready"
            writer = (
                "import pathlib, signal, sys, time\n"
                "signal.signal(signal.SIGTERM, lambda *a: sys.exit(0))\n"
                f"sys.stdout.buffer.write(b'\\x01\\x00' * {payload_chunks * chunk // 2} + b'\\x07')\n"
                "sys.stdout.buffer.flush()\n"
                f"pathlib.Path({str(ready)!r}).touch()\n"
                "time.sleep(30)\n"
            )
            proc = subprocess.Popen([sys.executable, "-c", writer], stdout=subprocess.PIPE)
            for _ in range(100):
                if ready.exists():
                    break
                threading.Event().wait(0.05)
            self.assertTrue(ready.exists(), "writer never became ready")
        fed = []
        wsi_stream.read_remaining_audio(proc, chunk, fed.append)
        self.assertEqual(proc.returncode, 0)
        self.assertEqual(len(fed), payload_chunks)
        self.assertTrue(all(isinstance(a, array.array) and a.typecode == "h" for a in fed))
        self.assertEqual(sum(len(a) for a in fed), payload_chunks * chunk // 2)

    def test_a_recorder_that_ignores_sigterm_is_killed(self):
        writer = (
            "import signal, sys, time\n"
            "signal.signal(signal.SIGTERM, signal.SIG_IGN)\n"
            "sys.stdout.buffer.write(b'r')\n"
            "sys.stdout.buffer.flush()\n"
            "time.sleep(30)\n"
        )
        proc = subprocess.Popen([sys.executable, "-c", writer], stdout=subprocess.PIPE)
        # Wait for the ready byte so SIGTERM lands after the handler is set to ignore
        self.assertEqual(proc.stdout.read(1), b"r")
        wsi_stream.read_remaining_audio(proc, 64, lambda a: None)
        self.assertEqual(proc.returncode, -signal.SIGKILL)


class FinalPassesTest(unittest.TestCase):
    def setUp(self):
        self.saved = wsi_stream.realtime_updates

    def tearDown(self):
        wsi_stream.realtime_updates = self.saved

    def test_returns_once_two_passes_follow_the_drain(self):
        wsi_stream.realtime_updates = 10

        def two_passes():
            for _ in range(2):
                wsi_stream.on_realtime_update("")
        threading.Timer(0.1, two_passes).start()
        wsi_stream.wait_for_final_realtime_passes(10)
        self.assertEqual(wsi_stream.realtime_updates, 12)

    def test_is_bounded_when_no_pass_arrives(self):
        wsi_stream.realtime_updates = 0
        saved = wsi_stream.FINAL_REALTIME_WAIT_SECONDS
        wsi_stream.FINAL_REALTIME_WAIT_SECONDS = 0.2
        try:
            wsi_stream.wait_for_final_realtime_passes(0)
        finally:
            wsi_stream.FINAL_REALTIME_WAIT_SECONDS = saved
        self.assertEqual(wsi_stream.realtime_updates, 0)


if __name__ == "__main__":
    unittest.main()
