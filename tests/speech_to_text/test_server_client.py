"""wsi-stream's server-mode client against a stub server (Plan 00148 Tasks 2.4, 4.2).

run_server_mode() is driven for real, in the main thread so its signal handlers work,
against a stub wsi-stream-server on a temporary Unix socket that answers each command
from a script. The desktop (D-Bus, notifications, clipboard, paste) is replaced by
recorders. Covered: START carries the limits from Settings (continuous) or the fixed cap;
there is no client-side time limit in continuous mode; KEEPALIVEs are sent and PROGRESS
is relayed to the panel; Insert drains then pastes; a server that stops by itself is
followed, not stopped again; FAILED exits non-zero with the text so far on the clipboard
and nothing pasted; Escape sends ABORT; a vanished server fails loudly. A continuous
dictation pasted in chunks: the chunks are asked for with PROGRESS {with_text} and
reported with PASTED, they join to exactly what one paste at stop pastes, and a failure
or a handed-over dictation puts on the clipboard only what was not pasted.

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
        self.note_expiry = []
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
            desktop_notification=lambda message, ms: (self.notes.append(message),
                                                      self.note_expiry.append((message, ms))),
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

    def run_client(self, signal_after=None, signum=signal.SIGTERM, auto_paste=False, timeout=120,
                   wrap_marker=False):
        if signal_after is not None:
            timer = threading.Timer(signal_after, os.kill, args=(os.getpid(), signum))
            timer.start()
            self.addCleanup(timer.cancel)
        args = argparse.Namespace(
            debug=False, no_notify=False, language="en", timeout=timeout, claude_process=False,
            wrap_marker=wrap_marker, auto_paste=auto_paste, no_auto_enter=True, paste_with_shift=1,
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

    def test_a_failed_keepalive_still_stops_the_server_and_keeps_the_text(self):
        keepalives = []
        base = scripted([RECORDING], [{"status": "draining", "drain_seconds_left": 5}, DONE])

        def answer(command, params):
            if command == "KEEPALIVE":
                keepalives.append(True)
                return {"status": "error", "message": "KEEPALIVE: timed out"}
            return base(command, params)

        self.serve(answer)
        rc = self.run_client(timeout=120)
        self.assertTrue(keepalives)
        self.assertIn("STOP", self.server.commands(), "the microphone was left open")
        self.assertEqual(rc, 0)
        self.assertEqual(self.clipboard, ["Hello there world"])

    def test_every_exit_path_tells_the_server_to_stop(self):
        self.serve(scripted([RECORDING]))
        with mock.patch.object(wsi_stream, "relay_progress", side_effect=RuntimeError("D-Bus gone")):
            with self.assertRaises(RuntimeError):
                self.run_client()
        for cleanup in self.registered:
            cleanup()
        self.assertIn("STOP", self.server.commands())

    def test_a_stop_while_the_server_is_already_stopping_completes(self):
        base = scripted([RECORDING], [{"status": "stopping"},
                                      {"status": "draining", "drain_seconds_left": 5}, DONE])

        def answer(command, params):
            if command == "STOP":
                base(command, params)
                return {"status": "stopping"}
            return base(command, params)

        self.serve(answer)
        rc = self.run_client(signal_after=0.3)
        self.assertEqual(rc, 0, self.notes)
        self.assertEqual(self.clipboard, ["Hello there world"])

    def test_the_routine_cap_without_continuous_is_a_transient_notification(self):
        reason = "reached the 120 s recording limit"
        self.serve(scripted([RECORDING, {"status": "draining", "stop_reason": reason,
                                         "drain_seconds_left": 5},
                             dict(DONE, stop_reason=reason)]))
        rc = self.run_client()
        self.assertEqual(rc, 0)
        shown = [(m, ms) for m, ms in self.note_expiry if reason in m]
        self.assertTrue(shown, self.note_expiry)
        self.assertTrue(all(ms != 0 for _, ms in shown), shown)

    def test_a_continuous_auto_stop_stays_until_dismissed(self):
        self.settings = dict(CONTINUOUS)
        reason = "no speech for 120 s"
        self.serve(scripted([RECORDING, {"status": "draining", "stop_reason": reason,
                                         "drain_seconds_left": 5},
                             dict(DONE, stop_reason=reason)]))
        self.run_client()
        self.assertIn(0, [ms for m, ms in self.note_expiry if reason in m])

    def test_text_the_server_could_not_hand_over_goes_to_the_clipboard_loudly(self):
        base = scripted([RECORDING], [DONE])
        journal = "/run/user/0/wsi-dictation/old/journal.jsonl"

        def answer(command, params):
            if command == "START":
                return {"status": "recording", "session_dir": "/run/user/0/wsi-dictation/s",
                        "undelivered": {"transcription": "words from before",
                                        "stop_reason": "heartbeat lost: no KEEPALIVE for 15 s",
                                        "journal": journal}}
            return base(command, params)

        self.serve(answer)
        rc = self.run_client(signal_after=0.5)
        self.assertEqual(rc, 0)
        self.assertEqual(self.clipboard[0], "words from before")
        loud = [m for m, ms in self.note_expiry if ms == 0 and journal in m]
        self.assertTrue(loud, self.note_expiry)
        self.assertIn("heartbeat lost", loud[0])

    def test_unreadable_dictation_settings_record_nothing(self):
        self.settings = {"continuous-dictation": "true", "max-recording-minutes": "sixty",
                         "silence-autostop-seconds": "120"}
        self.serve(scripted([RECORDING]))
        rc = self.run_client()
        self.assertEqual(rc, 1)
        self.assertEqual(self.server.received, [])


FIRST, SECOND = "so my first experience", "so my first experience was llama cpp"
WHOLE = "so my first experience was llama cpp which ran a model locally"


def chunked(snapshots, after_stop, start_extra=None):
    """A continuous dictation's server: PROGRESS {with_text} answers each of the given
    snapshots in turn as the text so far (the last repeats), STOP answers draining, PROGRESS then
    answers from `after_stop`; PASTED is acknowledged."""
    snapshots, after = list(snapshots), list(after_stop)
    stopped = []

    def answer(command, params):
        if command == "START":
            return {"status": "recording", "session_dir": "/run/user/0/wsi-dictation/s",
                    **(start_extra or {})}
        if command == "KEEPALIVE":
            return {"status": "recording"}
        if command == "PASTED":
            return {"status": "ok"}
        if command == "STOP":
            stopped.append(True)
            return {"status": "draining", "drain_seconds_left": 5, "segments_pending": 1}
        if command == "PROGRESS" and stopped:
            return after.pop(0) if len(after) > 1 else after[0]
        if command == "PROGRESS" and params.get("with_text"):
            text = snapshots.pop(0) if len(snapshots) > 1 else snapshots[0]
            return dict(RECORDING, text_so_far=text)
        if command == "PROGRESS":
            return RECORDING
        return {"status": "error", "message": f"unexpected {command}"}

    return answer


class ChunkedDictationTest(ClientCase):
    """run_server_mode pasting a continuous dictation in chunks (Plan 00148 Task 9.3),
    through the real stop path: the chunks are asked for with PROGRESS {with_text}, each
    is reported to the server with PASTED, and what is left goes out at stop."""

    def dictate(self, interval, wrap_marker=True, after_stop=(dict(DONE, transcription=WHOLE),)):
        self.settings = dict(CONTINUOUS, **{"dictation-paste-interval-seconds": interval})
        self.serve(chunked([FIRST, SECOND], after_stop))
        rc = self.run_client(signal_after=2.6, auto_paste=True, wrap_marker=wrap_marker)
        self.server.close()
        self.socket_path.unlink()
        self.registered.clear()
        return rc

    def test_the_chunks_paste_exactly_what_one_paste_at_stop_would(self):
        self.assertEqual(self.dictate("0"), 0)
        single, self.pasted = self.pasted, []
        self.assertEqual(len(single), 1, "with no interval the text is pasted once, at stop")
        self.assertEqual(self.dictate("1"), 0)
        self.assertGreater(len(self.pasted), 2, f"no chunk was pasted before the stop: {self.pasted}")
        self.assertEqual("".join(self.pasted), single[0])

    def test_each_chunk_is_asked_for_with_the_text_and_reported_as_pasted(self):
        self.dictate("1", wrap_marker=False)
        asked = [p for c, p in self.server.received if c == "PROGRESS" and p.get("with_text")]
        self.assertTrue(asked, "PROGRESS was never asked for the text so far")
        reported = [p["chars"] for c, p in self.server.received if c == "PASTED"]
        self.assertEqual(reported, [len(FIRST), len(SECOND)])

    def test_a_failure_after_chunks_copies_only_the_text_not_yet_pasted(self):
        failed = {"status": "failed", "transcription": WHOLE, "error": "CUDA out of memory",
                  "session_dir": "/run/user/0/wsi-dictation/s"}
        rc = self.dictate("1", wrap_marker=False, after_stop=(failed,))
        self.assertEqual(rc, 1)
        self.assertEqual(self.clipboard, ["which ran a model locally"])
        message = " ".join(self.notes)
        self.assertIn("already pasted", message)
        self.assertNotIn(f"({len(WHOLE.split())} words)", message)


class UndeliveredAfterChunksTest(ClientCase):
    """A dictation handed over at START whose client had pasted part of it in chunks."""

    def test_only_the_text_never_pasted_goes_to_the_clipboard(self):
        handed = {"status": "done", "transcription": WHOLE, "pasted_chars": len(SECOND),
                  "stop_reason": "heartbeat lost: no KEEPALIVE for 15 s",
                  "journal": "/run/user/0/wsi-dictation/old/journal.jsonl"}
        self.serve(chunked([""], [DONE], start_extra={"undelivered": handed}))
        rc = self.run_client(signal_after=0.5)
        self.assertEqual(rc, 0)
        self.assertEqual(self.clipboard[0], "which ran a model locally")
        loud = [m for m, ms in self.note_expiry if ms == 0 and "journal.jsonl" in m]
        self.assertTrue(loud, self.note_expiry)
        self.assertIn("already pasted", loud[0])
        self.assertIn("never pasted", loud[0])


class PreviewFallbackTest(ClientCase):
    """Standard streaming without a final transcription (Plan 00148 Task 4.2)."""

    def test_the_preview_goes_to_the_clipboard_loudly_and_is_never_pasted(self):
        wsi_stream.report_preview_fallback("words from the tiny model")
        self.assertEqual(self.clipboard, ["words from the tiny model"])
        self.assertEqual(self.pasted, [])
        self.assertIn("ERROR", self.states())
        self.assertTrue(any("nothing was pasted" in n for n in self.notes), self.notes)


REASON = ("No speech model is downloaded. Open the model manager and download at least "
          "one; base is the one suggested for this machine.")


class ModelNotDownloadedTest(ClientCase):
    """No model is downloaded by default (Plan 00156 Task 2.4). wsi-resolve-model exits
    3 when the model is not on disk; wsi-stream shows its reason as the whole message,
    records nothing and never lets faster-whisper download the model."""

    def stub_resolver(self, rc, stderr):
        script = pathlib.Path(self.tmp.name) / "wsi-resolve-model"
        script.write_text("#!/bin/sh\nprintf '%s' \"$STUB_STDERR\" >&2\nexit " + str(rc) + "\n")
        script.chmod(0o755)
        for patcher in (mock.patch.object(wsi_stream, "MODEL_RESOLVER", script),
                        mock.patch.dict(os.environ, {"STUB_STDERR": stderr})):
            patcher.start()
            self.addCleanup(patcher.stop)

    def test_exit_3_is_model_not_downloaded_with_the_reason_alone(self):
        self.stub_resolver(3, "wsi-resolve-model: auto -> base (no GPU)\n"
                              f"wsi-resolve-model: {REASON}\n")
        with self.assertRaises(wsi_stream.ModelNotDownloaded) as raised:
            wsi_stream.resolve_model("streaming", "en")
        self.assertEqual(str(raised.exception), REASON)

    def test_another_failure_is_not_model_not_downloaded(self):
        self.stub_resolver(1, "wsi-resolve-model: cannot ask CTranslate2 for CUDA devices\n")
        with self.assertRaises(RuntimeError) as raised:
            wsi_stream.resolve_model("streaming", "en")
        self.assertNotIsInstance(raised.exception, wsi_stream.ModelNotDownloaded)

    def test_server_mode_shows_the_reason_and_starts_no_server(self):
        self.stub_resolver(3, f"wsi-resolve-model: {REASON}\n")
        server_command = mock.Mock()
        with mock.patch.multiple(wsi_stream, is_server_running=lambda: False,
                                 server_pid_alive=lambda: False,
                                 read_server_idle_timeout=lambda: 0,
                                 server_command=server_command,
                                 SERVER_SCRIPT=stt_stubs.BIN / "wsi-stream-server"):
            rc = self.run_client()
        self.assertEqual(rc, 1)
        server_command.assert_not_called()
        self.assertIn("ERROR", self.states())
        self.assertIn(REASON, self.notes, "the reason is the whole message, no prefix")

    def test_a_recording_mode_shows_the_reason_not_a_crash(self):
        def missing(args):
            raise wsi_stream.ModelNotDownloaded(REASON)

        with mock.patch.object(wsi_stream, "run_streaming", missing), \
                mock.patch.object(wsi_stream.sys, "argv", ["wsi-stream"]):
            rc = wsi_stream.main()
        self.assertEqual(rc, 1)
        self.assertIn("ERROR", self.states())
        self.assertIn(REASON, self.notes)
        self.assertFalse(any("crashed" in n for n in self.notes), self.notes)


if __name__ == "__main__":
    unittest.main()
