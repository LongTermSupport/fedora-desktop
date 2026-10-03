"""wsi-stream's server-mode client against a stub server (Plan 00148 Task 2.4).

run_server_mode() is driven for real, in the main thread so its signal handlers work,
against a stub wsi-stream-server on a temporary Unix socket that answers each command
from a script. The desktop (D-Bus, notifications, clipboard, paste) is replaced by
recorders. Covered: START carries the limits from Settings (continuous) or the fixed cap;
there is no client-side time limit in continuous mode; KEEPALIVEs are sent and PROGRESS
is relayed to the panel; Insert drains then pastes; a server that stops by itself is
followed, not stopped again; FAILED exits non-zero with the text so far on the clipboard
and nothing pasted; Escape sends ABORT; a vanished server fails loudly.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import argparse
import importlib.util
import json
import os
import pathlib
import signal
import socket
import tempfile
import threading
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_server_client")

CONTINUOUS = {"continuous-dictation": "true", "max-recording-minutes": "60",
              "silence-autostop-seconds": "120"}


class StubServer:
    """Answers each command with answer(command, params); records what it was sent."""

    def __init__(self, path, answer):
        self.path = path
        self.answer = answer
        self.received = []
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.bind(str(path))
        self.sock.listen(5)
        self.sock.settimeout(0.1)
        self.running = True
        self.thread = threading.Thread(target=self._serve, daemon=True)
        self.thread.start()

    def _serve(self):
        while self.running:
            try:
                conn, _ = self.sock.accept()
            except socket.timeout:
                continue
            with conn:
                request = json.loads(conn.makefile().readline())
                command, params = request["command"], request.get("params", {})
                self.received.append((command, params))
                reply = self.answer(command, params)
                conn.sendall((json.dumps(reply) + "\n").encode())

    def commands(self):
        return [c for c, _ in self.received]

    def close(self):
        self.running = False
        self.thread.join()
        self.sock.close()


class ClientCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        tmp = pathlib.Path(self.tmp.name)
        self.socket_path = tmp / "wsi-stream.socket"
        self.settings = {"continuous-dictation": "false"}
        self.signals = []
        self.notes = []
        self.clipboard = []
        self.pasted = []
        self.registered = []
        patches = mock.patch.multiple(
            wsi_stream, SERVER_SOCKET=self.socket_path, PID_FILE=tmp / "recording.pid",
            CACHE_DIR=tmp / "cache", TRANSCRIPTION_FILE=tmp / "cache" / "last.txt",
            KEEPALIVE_INTERVAL=0.2, PROGRESS_INTERVAL=0.1, DRAIN_POLL_INTERVAL=0.05,
            read_stop_grace=lambda: 0, read_setting=lambda key: self.settings[key],
            is_server_running=lambda: True,
            emit_dbus_signal=lambda name, value: self.signals.append((name, value)),
            desktop_notification=lambda message, ms: self.notes.append(message),
            copy_to_clipboard=lambda text, use_clipboard=False: self.clipboard.append(text) or True,
            auto_paste=lambda text, **kw: self.pasted.append(text) or True)
        patches.start()
        self.addCleanup(patches.stop)
        register = mock.patch.object(wsi_stream.atexit, "register", self.registered.append)
        register.start()
        self.addCleanup(register.stop)
        for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGUSR1):
            self.addCleanup(signal.signal, signum, signal.getsignal(signum))

    def serve(self, answer):
        self.server = StubServer(self.socket_path, answer)
        self.addCleanup(self.server.close)

    def run_client(self, signal_after=None, signum=signal.SIGTERM, auto_paste=False, timeout=120):
        if signal_after is not None:
            timer = threading.Timer(signal_after, os.kill, args=(os.getpid(), signum))
            timer.start()
            self.addCleanup(timer.cancel)
        args = argparse.Namespace(
            debug=False, no_notify=False, language="en", timeout=timeout, claude_process=False,
            wrap_marker=False, auto_paste=auto_paste, no_auto_enter=True, paste_with_shift=1,
            clipboard=True)
        rc = wsi_stream.run_server_mode(args)
        for cleanup in self.registered:
            cleanup()
        return rc

    def states(self):
        return [v for name, v in self.signals if name == "StateChanged"]


def scripted(before_stop, after_stop=()):
    """A server answering PROGRESS from `before_stop` until STOP, then from `after_stop`.

    The last reply of each list repeats. STOP itself answers draining.
    """
    replies = {"before": list(before_stop), "after": list(after_stop)}
    stopped = []

    def next_reply(name):
        queue = replies[name]
        return queue.pop(0) if len(queue) > 1 else queue[0]

    def answer(command, params):
        if command == "START":
            return {"status": "recording", "session_dir": "/run/user/0/wsi-dictation/s"}
        if command == "KEEPALIVE":
            return {"status": "recording"}
        if command == "PROGRESS":
            return next_reply("after" if stopped else "before")
        if command == "STOP":
            stopped.append(True)
            return {"status": "draining", "drain_seconds_left": 5, "segments_pending": 1}
        if command == "ABORT":
            return {"status": "aborted"}
        return {"status": "error", "message": f"unexpected {command}"}

    return answer


RECORDING = {"status": "recording", "elapsed_seconds": 3, "segments_pending": 2,
             "backlog_seconds": 9, "rtf": 0.4}
DONE = {"status": "done", "transcription": "hello there world", "stop_reason": "stop requested"}


class ServerClientTest(ClientCase):
    def test_continuous_start_carries_the_settings_and_has_no_client_time_limit(self):
        self.settings = dict(CONTINUOUS)
        self.serve(scripted([RECORDING], [{"status": "draining", "drain_seconds_left": 5}, DONE]))
        rc = self.run_client(signal_after=1.5, timeout=1)
        self.assertEqual(rc, 0)
        start = self.server.received[0]
        self.assertEqual(start, ("START", {"continuous": True, "max_seconds": 3600,
                                           "silence_seconds": 120}))
        commands = self.server.commands()
        self.assertIn("KEEPALIVE", commands)
        self.assertIn("STOP", commands, "the client stopped on Insert, not on --timeout")
        self.assertEqual(self.clipboard, ["Hello there world"])
        progress = [v for name, v in self.signals if name == "Progress"]
        self.assertIn("'elapsed=3 pending=2 backlog=9 behind=0'", progress)

    def test_without_continuous_the_fixed_cap_goes_to_the_server(self):
        self.serve(scripted([RECORDING], [DONE]))
        self.run_client(signal_after=0.5, timeout=120)
        self.assertEqual(self.server.received[0],
                         ("START", {"continuous": False, "max_seconds": 120, "silence_seconds": 0}))

    def test_insert_drains_then_pastes(self):
        self.serve(scripted([RECORDING], [{"status": "draining", "drain_seconds_left": 5}, DONE]))
        rc = self.run_client(signal_after=0.3, auto_paste=True)
        self.assertEqual(rc, 0)
        self.assertEqual(self.pasted, ["Hello there world"])
        self.assertIn("TRANSCRIBING", self.states())

    def test_a_server_that_stopped_itself_is_followed_and_its_reason_shown(self):
        auto = {"status": "draining", "stop_reason": "no speech for 120 s", "drain_seconds_left": 5}
        done = dict(DONE, stop_reason="no speech for 120 s")
        self.serve(scripted([RECORDING, auto, done]))
        rc = self.run_client()
        self.assertEqual(rc, 0)
        self.assertNotIn("STOP", self.server.commands())
        self.assertTrue(any("no speech for 120 s" in n for n in self.notes), self.notes)
        self.assertEqual(self.clipboard, ["Hello there world"])

    def test_failed_copies_the_text_so_far_and_never_pastes(self):
        failed = {"status": "failed", "transcription": "the words so far",
                  "error": "segment 3 (60-84 s) failed to transcribe: CUDA out of memory",
                  "kept_audio": ["/run/user/0/wsi-dictation/s/seg-0003-failed.wav"],
                  "session_dir": "/run/user/0/wsi-dictation/s"}
        self.serve(scripted([RECORDING, failed]))
        rc = self.run_client(auto_paste=True)
        self.assertEqual(rc, 1)
        self.assertEqual(self.clipboard, ["the words so far"])
        self.assertEqual(self.pasted, [], "a transcript with a hole must never be pasted")
        self.assertIn("ERROR", self.states())
        message = " ".join(self.notes)
        self.assertIn("CUDA out of memory", message)
        self.assertIn("/run/user/0/wsi-dictation/s", message)
        self.assertIn(("Error", "'segment 3 (60-84 s) failed to transcribe: CUDA out of memory'"),
                      self.signals)

    def test_escape_aborts_and_pastes_nothing(self):
        self.serve(scripted([RECORDING]))
        rc = self.run_client(signal_after=0.3, signum=signal.SIGUSR1, auto_paste=True)
        self.assertEqual(rc, 0)
        self.assertIn("ABORT", self.server.commands())
        self.assertNotIn("STOP", self.server.commands())
        self.assertEqual(self.pasted, [])
        self.assertEqual(self.clipboard, [])

    def test_a_vanished_server_fails_loudly(self):
        def answer(command, params):
            if command == "START":
                return {"status": "recording", "session_dir": "/run/user/0/wsi-dictation/s"}
            return {"status": "error", "message": "No active recording"}

        self.serve(answer)
        rc = self.run_client()
        self.assertEqual(rc, 1)
        self.assertIn("ERROR", self.states())
        self.assertTrue(any("journal.jsonl" in n for n in self.notes), self.notes)

    def test_unreadable_dictation_settings_record_nothing(self):
        self.settings = {"continuous-dictation": "true", "max-recording-minutes": "sixty",
                         "silence-autostop-seconds": "120"}
        self.serve(scripted([RECORDING]))
        rc = self.run_client()
        self.assertEqual(rc, 1)
        self.assertEqual(self.server.received, [])


if __name__ == "__main__":
    unittest.main()
