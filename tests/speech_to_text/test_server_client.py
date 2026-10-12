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
        self.tokens = []
        self.notes = []
        self.note_expiry = []
        self.urgent = []
        self.clipboard = []
        self.pasted = []
        self.registered = []

        def sent(name, value, token):
            self.signals.append((name, value))
            self.tokens.append(token)

        # The real emit_dbus_signal runs (the token, and nothing after the session's
        # end); only the gdbus call under it is replaced
        patches = mock.patch.multiple(
            wsi_stream, SERVER_SOCKET=self.socket_path, PID_FILE=tmp / "recording.pid",
            STOPPING_FILE=tmp / "stopping.pid",
            CACHE_DIR=tmp / "cache", TRANSCRIPTION_FILE=tmp / "cache" / "last.txt",
            KEEPALIVE_INTERVAL=0.2, PROGRESS_INTERVAL=0.1, DRAIN_POLL_INTERVAL=0.05,
            START_RETRY_SECONDS=0.05,
            read_stop_grace=lambda: 0, read_setting=lambda key: self.settings[key],
            is_server_running=lambda: True,
            session_token="session-A", ended_with=None,
            send_dbus_signal=sent,
            desktop_notification=lambda message, ms: (self.notes.append(message),
                                                      self.note_expiry.append((message, ms))),
            urgent_notification=self.urgent.append,
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
                   wrap_marker=False, no_notify=False):
        if signal_after is not None:
            timer = threading.Timer(signal_after, os.kill, args=(os.getpid(), signum))
            timer.start()
            self.addCleanup(timer.cancel)
        args = argparse.Namespace(
            debug=False, no_notify=no_notify, language="en", timeout=timeout, claude_process=False,
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


class PinnedWindowUnavailableTest(ClientCase):
    """The window the dictation started in was closed, or would not take focus back, by
    the time of a paste (Task 9.10): nothing is pasted, the text not yet pasted goes to the
    clipboard, and the user is told."""

    def unavailable_after(self, pastes, reason="the window this dictation started in was closed"):
        """auto_paste pastes `pastes` times, then finds no window to paste into."""
        def paste(text, **kw):
            if len(self.pasted) >= pastes:
                raise wsi_stream.PasteTargetUnavailable(reason)
            self.pasted.append(text)
            return True
        patcher = mock.patch.object(wsi_stream, "auto_paste", paste)
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_closed_at_stop_puts_the_text_on_the_clipboard_and_says_so(self):
        self.unavailable_after(0)
        self.serve(scripted([RECORDING], [{"status": "draining", "drain_seconds_left": 5}, DONE]))
        rc = self.run_client(signal_after=0.3, auto_paste=True)
        self.assertEqual(rc, 1)
        self.assertEqual(self.pasted, [])
        self.assertEqual(self.clipboard, ["Hello there world"])
        self.assertIn("ERROR", self.states())
        self.assertNotIn("SUCCESS", self.states())
        loud = [m for m, ms in self.note_expiry if ms == 0]
        self.assertTrue(loud, self.note_expiry)
        self.assertIn("was closed", loud[0])
        self.assertIn("clipboard", loud[0])

    def test_a_window_that_will_not_take_focus_is_reported_the_same_way(self):
        self.unavailable_after(0, reason="the window this dictation started in would not take focus")
        self.serve(scripted([RECORDING], [DONE]))
        rc = self.run_client(signal_after=0.3, auto_paste=True)
        self.assertEqual(rc, 1)
        self.assertEqual(self.clipboard, ["Hello there world"])
        self.assertTrue(any("would not take focus" in m for m in self.notes), self.notes)

    def test_closed_after_a_chunk_copies_only_the_text_not_yet_pasted(self):
        self.unavailable_after(1)
        self.settings = dict(CONTINUOUS, **{"dictation-paste-interval-seconds": "1"})
        self.serve(chunked([FIRST, SECOND], (dict(DONE, transcription=WHOLE),)))
        rc = self.run_client(signal_after=2.6, auto_paste=True)
        self.assertEqual(rc, 1)
        self.assertEqual(self.pasted, ["So my first experience"])
        reported = [p["chars"] for c, p in self.server.received if c == "PASTED"]
        self.assertEqual(reported, [len(FIRST)], "a chunk not pasted was reported as pasted")
        self.assertEqual(self.clipboard, ["was llama cpp which ran a model locally"])
        loud = [m for m, ms in self.note_expiry if ms == 0]
        self.assertTrue(loud, self.note_expiry)
        self.assertIn("first 4 words were already pasted", loud[0])


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


FOREIGN_PID = "4194301"  # a PID no test process has: another client's PID file


class SessionOwnershipTest(ClientCase):
    """Plan 00171: the panel follows one client at a time.

    The incident: Insert 376 ms after a dictation's SUCCESS started a new client, then the
    old client's IDLEs (one a second after SUCCESS, two more at exit) overwrote the new
    one's RECORDING, and its exit deleted the PID file the new client had just written.
    Every signal now carries the session token the panel gave the client, nothing is sent
    after the state that ended the session, and a client removes the PID file only while
    it names this client.
    """

    def test_every_signal_carries_the_session_token(self):
        self.settings = dict(CONTINUOUS, **{"dictation-paste-interval-seconds": "0"})
        self.serve(scripted([RECORDING], [{"status": "draining", "drain_seconds_left": 5}, DONE]))
        self.assertEqual(self.run_client(signal_after=1.5, auto_paste=True), 0)
        names = {name for name, _ in self.signals}
        self.assertTrue({"StateChanged", "Progress"} <= names, self.signals)
        self.assertEqual(set(self.tokens), {"session-A"})

    def test_nothing_follows_success(self):
        self.serve(scripted([RECORDING], [{"status": "draining", "drain_seconds_left": 5}, DONE]))
        self.assertEqual(self.run_client(signal_after=0.3, auto_paste=True), 0)
        self.assertEqual(self.states()[-1], "SUCCESS", self.states())
        self.assertEqual(self.signals[-1], ("StateChanged", "SUCCESS"),
                         "a signal followed SUCCESS (the exit handlers ran too)")
        self.assertNotIn("IDLE", self.states())

    def test_nothing_follows_an_error(self):
        self.serve(scripted([RECORDING, {"status": "failed", "transcription": "",
                                         "error": "CUDA out of memory"}]))
        self.assertEqual(self.run_client(auto_paste=True), 1)
        self.assertEqual(self.signals[-1], ("StateChanged", "ERROR"), self.signals)

    def test_a_dictation_ended_without_a_result_sends_one_idle_at_exit(self):
        self.serve(scripted([RECORDING]))
        self.assertEqual(self.run_client(signal_after=0.3, signum=signal.SIGUSR1), 0)
        self.assertEqual(self.states().count("IDLE"), 1, self.states())
        self.assertEqual(self.states()[-1], "IDLE")

    def test_recording_is_announced_only_once_the_server_has_accepted_start(self):
        seen_at_start = []
        base = scripted([RECORDING], [DONE])

        def answer(command, params):
            if command == "START":
                seen_at_start.extend(self.states())
            return base(command, params)

        self.serve(answer)
        self.assertEqual(self.run_client(signal_after=0.3), 0)
        self.assertNotIn("RECORDING", seen_at_start)
        self.assertIn("RECORDING", self.states())

    def test_a_refused_start_is_shown_and_nothing_wipes_it(self):
        self.serve(lambda command, params: {"status": "error", "message": "Model not loaded"})
        self.assertEqual(self.run_client(auto_paste=True), 1)
        self.assertNotIn("RECORDING", self.states(), "RECORDING was shown for a refused START")
        self.assertEqual(self.signals[-1], ("StateChanged", "ERROR"), self.signals)
        errors = [value for name, value in self.signals if name == "Error"]
        self.assertTrue(errors and "Model not loaded" in errors[0], self.signals)
        self.assertEqual(self.server.commands(), ["START"], "a refused START is not retried")

    def test_insert_while_a_recorder_owns_the_pid_file_stops_that_recorder(self):
        # The stuck state: the panel lost track of a recording client, so Insert started
        # another, which was refused while the first kept the microphone open
        wsi_stream.PID_FILE.write_text(FOREIGN_PID)
        self.serve(scripted([RECORDING], [DONE]))
        killed = []
        with mock.patch.object(wsi_stream, "recorder_alive", lambda pid: pid == int(FOREIGN_PID)), \
                mock.patch.object(wsi_stream.os, "kill", lambda pid, signum: killed.append((pid, signum))):
            self.assertEqual(self.run_client(), 0)
        self.assertEqual(killed, [(int(FOREIGN_PID), signal.SIGTERM)])
        self.assertEqual(self.server.commands(), [], "no second dictation was started")
        self.assertEqual(wsi_stream.PID_FILE.read_text(), FOREIGN_PID)
        self.assertNotIn("ERROR", self.states())

    def test_a_dead_recorders_pid_file_is_taken_over(self):
        wsi_stream.PID_FILE.write_text(FOREIGN_PID)
        self.serve(scripted([RECORDING], [DONE]))
        with mock.patch.object(wsi_stream, "recorder_alive", lambda pid: False):
            self.assertEqual(self.run_client(signal_after=0.3), 0)
        self.assertEqual(self.server.commands()[0], "START")

    def serve_busy(self, progress_status):
        """A server refusing START for a dictation whose PROGRESS is `progress_status`."""
        def answer(command, params):
            if command == "START":
                return {"status": "error", "busy": "recording",
                        "message": "Recording already active"}
            if command == "PROGRESS":
                return {"status": progress_status}
            if command == "STOP":
                return {"status": "draining", "drain_seconds_left": 5}
            return {"status": "error", "message": f"unexpected {command}"}

        self.serve(answer)

    def test_a_start_refused_because_a_recording_runs_stops_that_recording(self):
        # A recording no live client is known to own: Insert stops it at the server, and
        # the text reaches the owner if it is still following, or the next START
        self.serve_busy("recording")
        self.assertEqual(self.run_client(), 0)
        self.assertEqual(self.server.commands(), ["START", "PROGRESS", "STOP"])
        self.assertNotIn("ERROR", self.states())
        self.assertNotIn("RECORDING", self.states())

    def assert_refused_start_leaves_the_dictation(self, status):
        self.serve_busy(status)
        self.assertEqual(self.run_client(), 0)
        self.assertEqual(self.server.commands(), ["START", "PROGRESS"], "STOP sent again")
        self.assertEqual(self.states(), ["WAITING", "IDLE"])

    def test_a_start_refused_while_the_servers_dictation_stops_sends_no_stop(self):
        self.assert_refused_start_leaves_the_dictation("stopping")

    def test_a_start_refused_while_the_servers_dictation_drains_sends_no_stop(self):
        self.assert_refused_start_leaves_the_dictation("draining")

    def stopping_marker_seen_by_the_server(self, after_stop):
        """Run a client stopped with a 1 s grace; the STOPPING_FILE contents at each
        PROGRESS, as (stopped, contents)."""
        seen = []
        base = scripted([RECORDING], after_stop)
        stopped = []

        def answer(command, params):
            if command == "STOP":
                stopped.append(True)
            if command == "PROGRESS":
                marker = wsi_stream.STOPPING_FILE
                seen.append((bool(stopped), marker.read_text() if marker.exists() else None))
            return base(command, params)

        self.serve(answer)
        with mock.patch.object(wsi_stream, "read_stop_grace", lambda: 1):
            self.assertEqual(self.run_client(signal_after=0.3), 0)
        self.assertFalse(wsi_stream.STOPPING_FILE.exists(), "the marker outlived the client")
        return seen

    def assert_insert_is_a_no_op(self):
        """A second client, with the marker naming a live client, touches nothing."""
        self.signals.clear()
        self.registered.clear()
        wsi_stream.ended_with = None
        wsi_stream.PID_FILE.write_text(FOREIGN_PID)
        wsi_stream.STOPPING_FILE.write_text(FOREIGN_PID)
        commands = len(self.server.commands())
        killed = []
        with mock.patch.object(wsi_stream, "recorder_alive", lambda pid: pid == int(FOREIGN_PID)), \
                mock.patch.object(wsi_stream.os, "kill", lambda pid, signum: killed.append((pid, signum))):
            self.assertEqual(self.run_client(), 0)
        self.assertEqual(killed, [], "the stopping client was signalled again")
        self.assertEqual(len(self.server.commands()), commands, "the server was sent a command")
        self.assertEqual(wsi_stream.PID_FILE.read_text(), FOREIGN_PID)
        self.assertEqual(wsi_stream.STOPPING_FILE.read_text(), FOREIGN_PID)
        self.assertEqual(self.states(), ["WAITING", "IDLE"])

    def test_insert_during_the_grace_is_a_no_op(self):
        seen = self.stopping_marker_seen_by_the_server([DONE])
        own = str(os.getpid())
        self.assertIn((False, own), seen, "no marker while recording on in the grace")
        self.assertEqual(seen[0], (False, None), "marked before any stop")
        self.assert_insert_is_a_no_op()

    def test_insert_during_the_drain_is_a_no_op(self):
        draining = {"status": "draining", "drain_seconds_left": 5, "segments_pending": 1}
        seen = self.stopping_marker_seen_by_the_server([draining, draining, DONE])
        self.assertIn((True, str(os.getpid())), seen, "no marker while draining")
        self.assert_insert_is_a_no_op()

    def test_a_start_refused_while_the_last_dictation_finishes_waits_then_records(self):
        starts = []
        base = scripted([RECORDING], [DONE])

        def answer(command, params):
            if command == "START":
                starts.append(True)
                if len(starts) < 3:
                    return {"status": "error", "busy": "finishing",
                            "message": "The previous dictation (draining) is still finishing"}
            return base(command, params)

        self.serve(answer)
        self.assertEqual(self.run_client(signal_after=0.6), 0)
        self.assertEqual(len(starts), 3)
        states = self.states()
        self.assertIn("WAITING", states)
        self.assertLess(states.index("WAITING"), states.index("RECORDING"))
        self.assertNotIn("ERROR", states)

    def test_insert_while_waiting_for_the_server_cancels_the_start(self):
        self.serve(lambda command, params: {"status": "error", "busy": "finishing",
                                            "message": "still finishing"})
        self.assertEqual(self.run_client(signal_after=0.3), 1)
        self.assertNotIn("RECORDING", self.states())
        self.assertEqual(self.states()[-1], "IDLE", self.states())
        self.assertNotIn("STOP", self.server.commands(), "nothing was recording to stop")
        self.assertNotIn("ABORT", self.server.commands(), "nothing was recording to abort")

    def slow_start_accepted(self, delay=0.4):
        """A server that takes `delay` to answer START, then starts recording."""
        base = scripted([RECORDING], [DONE])

        def answer(command, params):
            if command == "START":
                threading.Event().wait(delay)
            return base(command, params)

        self.serve(answer)

    def test_insert_while_start_is_in_flight_aborts_what_the_server_started(self):
        self.slow_start_accepted()
        self.assertEqual(self.run_client(signal_after=0.1), 1)
        self.assertEqual(self.server.commands(), ["START", "ABORT"],
                         "the server was left recording for no client")
        self.assertNotIn("RECORDING", self.states())
        self.assertEqual(self.states()[-1], "IDLE", self.states())

    def test_escape_while_start_is_in_flight_aborts_what_the_server_started(self):
        self.slow_start_accepted()
        self.assertEqual(self.run_client(signal_after=0.1, signum=signal.SIGUSR1), 1)
        self.assertEqual(self.server.commands(), ["START", "ABORT"])
        self.assertNotIn("RECORDING", self.states())

    def test_a_cancel_whose_abort_fails_is_retried_at_exit(self):
        base = scripted([RECORDING], [DONE])
        aborts = []

        def answer(command, params):
            if command == "START":
                threading.Event().wait(0.4)
            if command == "ABORT":
                aborts.append(True)
                if len(aborts) == 1:
                    return {"status": "error", "message": "ABORT: timed out"}
            return base(command, params)

        self.serve(answer)
        self.assertEqual(self.run_client(signal_after=0.1), 1)
        self.assertEqual(self.server.commands(), ["START", "ABORT", "ABORT"])

    def test_a_server_still_finishing_after_the_wait_is_an_error(self):
        self.serve(lambda command, params: {"status": "error", "busy": "finishing",
                                            "message": "still finishing"})
        with mock.patch.object(wsi_stream, "START_WAIT_SECONDS", 0.3):
            self.assertEqual(self.run_client(), 1)
        self.assertEqual(self.signals[-1], ("StateChanged", "ERROR"), self.signals)
        self.assertNotIn("RECORDING", self.states())

    def test_the_pid_file_is_left_alone_once_another_client_has_written_it(self):
        base = scripted([RECORDING], [DONE])

        def answer(command, params):
            if command == "STOP":
                # a newer client started meanwhile and wrote its own PID
                wsi_stream.PID_FILE.write_text(FOREIGN_PID)
            return base(command, params)

        self.serve(answer)
        self.assertEqual(self.run_client(signal_after=0.3), 0)
        self.assertEqual(wsi_stream.PID_FILE.read_text(), FOREIGN_PID)

    def test_the_pid_file_is_removed_by_the_client_it_names(self):
        self.serve(scripted([RECORDING], [DONE]))
        self.assertEqual(self.run_client(signal_after=0.3), 0)
        self.assertFalse(wsi_stream.PID_FILE.exists())

    def test_a_refused_start_leaves_another_clients_pid_file(self):
        def answer(command, params):
            wsi_stream.PID_FILE.write_text(FOREIGN_PID)
            return {"status": "error", "message": "Model not loaded"}

        self.serve(answer)
        self.assertEqual(self.run_client(), 1)
        self.assertEqual(wsi_stream.PID_FILE.read_text(), FOREIGN_PID)

    def test_the_token_comes_from_the_command_line(self):
        seen = []
        with mock.patch.object(wsi_stream, "run_server_mode",
                               lambda args: seen.append(wsi_stream.session_token) or 0), \
                mock.patch.object(wsi_stream.sys, "argv",
                                  ["wsi-stream", "--server-mode", "--session-token", "session-B"]):
            self.assertEqual(wsi_stream.main(), 0)
        self.assertEqual(seen, ["session-B"])


SHORT_MIC = {"elapsed_seconds": 24.0, "audio_seconds": 0.6}


class MicrophoneShortfallTest(ClientCase):
    """The incident: a 24 s dictation whose Bluetooth microphone delivered 0.55 s of audio
    showed "No speech detected" for a moment, then the green badge; the dictation was lost
    and nothing said why. The server reports audio_seconds; a microphone that delivered
    under half the recording's length is reported, loudly when nothing came of it."""

    def test_the_shortfall_rule(self):
        shortfall = wsi_stream.microphone_shortfall
        self.assertIsNone(shortfall({"elapsed_seconds": 24.0}), "an older server says nothing")
        self.assertIsNone(shortfall({"elapsed_seconds": 24.0, "audio_seconds": 23.9}))
        self.assertIsNone(shortfall({"elapsed_seconds": 1.5, "audio_seconds": 0.0}),
                          "too short to judge")
        self.assertIsNone(shortfall({"elapsed_seconds": 10.0, "audio_seconds": 5.0}))
        message = shortfall(SHORT_MIC)
        self.assertIn("0.6 s", message)
        self.assertIn("24.0 s", message)
        self.assertIn("microphone", message)

    def test_no_text_from_a_short_microphone_is_an_error_shown_with_notifications_off(self):
        self.serve(scripted([RECORDING], [dict(DONE, transcription="", **SHORT_MIC)]))
        rc = self.run_client(signal_after=0.3, auto_paste=True, no_notify=True)
        self.assertEqual(rc, 1)
        self.assertEqual(self.signals[-1], ("StateChanged", "ERROR"), self.signals)
        errors = [value for name, value in self.signals if name == "Error"]
        self.assertTrue(errors and "microphone" in errors[0], self.signals)
        self.assertEqual(len(self.urgent), 1, self.urgent)
        self.assertIn("0.6 s", self.urgent[0])
        self.assertEqual(self.pasted, [])

    def test_no_text_from_a_working_microphone_is_no_speech_in_the_menu_too(self):
        self.serve(scripted([RECORDING], [dict(DONE, transcription="", elapsed_seconds=10.0,
                                                audio_seconds=10.0)]))
        self.assertEqual(self.run_client(signal_after=0.3), 1)
        self.assertEqual(self.signals[-1], ("StateChanged", "ERROR"), self.signals)
        self.assertIn(("Error", "'No speech detected'"), self.signals)
        self.assertEqual(self.urgent, [])

    def test_text_from_a_short_microphone_is_pasted_with_a_lasting_warning(self):
        self.serve(scripted([RECORDING], [dict(DONE, **SHORT_MIC)]))
        rc = self.run_client(signal_after=0.3, auto_paste=True, no_notify=True)
        self.assertEqual(rc, 0)
        self.assertEqual(self.pasted, ["Hello there world"])
        self.assertEqual(self.states()[-1], "SUCCESS")
        self.assertEqual(len(self.urgent), 1, self.urgent)
        self.assertIn("may be missing", self.urgent[0])


class UrgentNotificationTest(unittest.TestCase):
    """A notification the user must see is shown even with notifications turned off."""

    def sent(self, no_notify, show):
        calls = []

        def run(argv, **kw):
            calls.append(argv)
            return wsi_stream.subprocess.CompletedProcess(argv, 0, b"", b"")

        with mock.patch.object(wsi_stream.subprocess, "run", run), \
                mock.patch.object(wsi_stream, "no_notify", no_notify):
            show("the microphone delivered nothing")
        return calls

    def test_urgent_ignores_notifications_off_and_stays(self):
        calls = self.sent(True, wsi_stream.urgent_notification)
        self.assertEqual(len(calls), 1)
        self.assertIn("the microphone delivered nothing", calls[0])
        self.assertEqual(calls[0][-1], "0", "an urgent notification stays until dismissed")

    def test_an_ordinary_notification_respects_notifications_off(self):
        self.assertEqual(self.sent(True, lambda m: wsi_stream.desktop_notification(m, 0)), [])


class SignalSenderTest(unittest.TestCase):
    """The gdbus call itself: the state first (a panel that reads only the first argument
    still works), then the token, quoted so a token of digits is not read as a number."""

    def test_the_gdbus_arguments(self):
        calls = []
        def run(argv, **kw):
            calls.append(argv)
            return wsi_stream.subprocess.CompletedProcess(argv, 0, b"", b"")

        with mock.patch.object(wsi_stream.subprocess, "run", run):
            wsi_stream.send_dbus_signal("StateChanged", "RECORDING", "12345")
        self.assertEqual(calls[0][-2:], ["RECORDING", "'12345'"])
        self.assertIn("org.fedoradesktop.SpeechToText.StateChanged", calls[0])


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
