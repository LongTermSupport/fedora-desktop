"""wsi-stream-server's stop order, run for real (Plan 00148 Phase 0).

Drives stop_recording_pipeline() with a fake pw-record on PATH, the server's own
feeding thread, and a stub recorder that publishes a realtime pass every 0.1 s while
recording. The fake pw-record writes a final burst when told to stop, so the test
fails if the pipe is not drained to EOF, or if recorder.stop() comes before the last
audio is fed or before two realtime passes have run over it.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import importlib.util
import os
import pathlib
import subprocess
import tempfile
import threading
import time
import unittest

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

server = stt_stubs.load_script("wsi-stream-server", "wsi_stream_server")


class StubRecorder:
    def __init__(self):
        self.fed_bytes = 0
        self.last_fed_at = None
        self.stopped_at = None
        self.updates_at_stop = None
        self.recording = True
        self.pass_times = []
        self._ticker = threading.Thread(target=self._passes, daemon=True)
        self._ticker.start()

    def _passes(self):
        while self.recording:
            time.sleep(0.1)
            if self.recording:
                self.pass_times.append(time.monotonic())
                server.on_realtime_transcription_update("words so far")

    def feed_audio(self, audio):
        self.fed_bytes += len(audio) * 2
        self.last_fed_at = time.monotonic()

    def stop(self):
        self.stopped_at = time.monotonic()
        with server.transcription_lock:
            self.updates_at_stop = server.realtime_updates
        self.recording = False


class ServerStopOrderTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        tmp = pathlib.Path(self.tmp.name)
        self.events = tmp / "events"
        self.events.mkdir()
        stub_bin = tmp / "bin"
        stub_bin.mkdir()
        pw_record = stt_stubs.install_fake_pw_record(stub_bin)

        self.saved = {k: getattr(server, k) for k in (
            "LOG_DIR", "LOG_FILE", "recorder", "audio_process", "audio_feed_thread",
            "audio_feed_stop", "transcription_text")}
        server.LOG_DIR = tmp
        server.LOG_FILE = tmp / "server.log"

        env = dict(os.environ, STUB_EVENTS=str(self.events))
        self.recorder = StubRecorder()
        server.recorder = self.recorder
        server.audio_process = subprocess.Popen([str(pw_record)], stdout=subprocess.PIPE, env=env)
        server.audio_feed_stop = threading.Event()
        server.audio_feed_thread = threading.Thread(
            target=server.audio_feeding_thread_func,
            args=(server.audio_process, server.audio_feed_stop), daemon=True)
        server.audio_feed_thread.start()

    def tearDown(self):
        self.recorder.recording = False
        for k, v in self.saved.items():
            setattr(server, k, v)
        self.tmp.cleanup()

    def test_stop_drains_the_pipe_then_waits_for_two_passes_then_stops(self):
        proc = server.audio_process
        time.sleep(0.4)
        text = server.stop_recording_pipeline()
        proc.stdout.close()
        mic = stt_stubs.pw_record_result(self.events)

        self.assertEqual(self.recorder.fed_bytes, mic["written"],
                         "audio left in the pipe at stop was not fed")
        self.assertIsNotNone(self.recorder.stopped_at, "recorder.stop() was never called")
        self.assertGreater(self.recorder.stopped_at, self.recorder.last_fed_at,
                           "recorder.stop() came before the last audio was fed")
        passes_after_eof = [t for t in self.recorder.pass_times
                            if self.recorder.last_fed_at < t < self.recorder.stopped_at]
        self.assertGreaterEqual(len(passes_after_eof), server.FINAL_REALTIME_PASSES,
                                "recorder.stop() did not wait for passes over the last audio")
        self.assertEqual(text, "words so far")


if __name__ == "__main__":
    unittest.main()
