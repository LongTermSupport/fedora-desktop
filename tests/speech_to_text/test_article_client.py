"""wsi-article as a client of the server's dictation session (Plan 00148 Task 5.1).

run_article_mode() is driven for real, in the main thread so its signal handlers work,
against a stub wsi-stream-server on a temporary Unix socket. The stub answers each command
from a script and appends segments to a real journal file as the dictation "goes on", the
way the server does. The desktop (D-Bus, notifications) is replaced by recorders.

Covered: START asks for continuous dictation with the limits from Settings; the segments
the server journals reach the window's buffer file as they commit and its raw file when
flushed; a stop drains however long the server takes (no client-side kill); a server that
fails or ends by itself is followed; a refused START, a journal that cannot be parsed and
a journal that disagrees with the server's final text each fail loudly and send STOP; the
window's marker files are removed on every exit.

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

wsi_article = stt_stubs.load_script("wsi-article", "wsi_article_client")
stream = wsi_article.stream

CONTINUOUS = {"max-recording-minutes": "60", "silence-autostop-seconds": "120"}


class StubServer:
    """Answers each command with answer(command, params); records what it was sent."""

    def __init__(self, path, answer):
        self.received = []
        self.answer = answer
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


def segment(seq, text, status="ok"):
    return {"seq": seq, "t_start": seq * 10.0, "t_end": seq * 10.0 + 9.0, "status": status,
            "cut": "silence", "text": text}


RECORDING = {"status": "recording", "elapsed_seconds": 3, "segments_pending": 0}


def done(text):
    return {"status": "done", "transcription": text, "stop_reason": "stop requested"}


class ArticleCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        tmp = pathlib.Path(self.tmp.name)
        self.cache = tmp / "cache"
        self.session_dir = tmp / "session"
        self.session_dir.mkdir()
        self.journal = self.session_dir / "journal.jsonl"
        self.settings = dict(CONTINUOUS)
        self.signals = []
        self.notes = []
        self.seen_while_recording = {}
        patch_article = mock.patch.multiple(
            wsi_article, CACHE_DIR=self.cache,
            ARTICLE_RAW_FILE=self.cache / "article-raw.txt",
            ARTICLE_BUFFER_FILE=self.cache / "article-buffer.txt",
            ARTICLE_TRIGGER_FILE=self.cache / "article-chunk.trigger",
            ARTICLE_RECORDING_ACTIVE_FILE=self.cache / "article-recording.active",
            PID_FILE=tmp / "recording.pid", FLUSH_SECONDS=3600, POLL_INTERVAL=0.05)
        patch_article.start()
        self.addCleanup(patch_article.stop)
        patch_stream = mock.patch.multiple(
            stream, SERVER_SOCKET=tmp / "wsi-stream.socket", KEEPALIVE_INTERVAL=0.2,
            PROGRESS_INTERVAL=0.1, DRAIN_POLL_INTERVAL=0.05,
            read_setting=lambda key: self.settings[key], is_server_running=lambda: True,
            emit_dbus_signal=lambda name, value: self.signals.append((name, value)),
            desktop_notification=lambda message, ms: self.notes.append(message))
        patch_stream.start()
        self.addCleanup(patch_stream.stop)
        for signum in (signal.SIGTERM, signal.SIGINT):
            self.addCleanup(signal.signal, signum, signal.getsignal(signum))

    def append_to_journal(self, *lines):
        with open(self.journal, "a", encoding="utf-8") as f:
            for line in lines:
                f.write((line if isinstance(line, str) else json.dumps(line)) + "\n")

    def scripted(self, before_stop, after_stop=(), on_start=None):
        """A server answering PROGRESS from `before_stop` until STOP, then from `after_stop`.

        Each entry is (reply, journal lines appended just before that reply). The last
        entry of each list repeats, its journal lines only once. STOP answers draining.
        """
        queues = {"before": list(before_stop), "after": list(after_stop)}
        stopped = []

        def next_entry(name):
            queue = queues[name]
            reply, lines = queue[0]
            if len(queue) > 1:
                queue.pop(0)
            else:
                queue[0] = (reply, ())
            return reply, lines

        def answer(command, params):
            if command == "START":
                if on_start:
                    return on_start()
                return {"status": "recording", "session_dir": str(self.session_dir)}
            if command == "KEEPALIVE":
                return {"status": "recording"}
            if command == "PROGRESS":
                self.seen_while_recording.setdefault(
                    "active", wsi_article.ARTICLE_RECORDING_ACTIVE_FILE.exists())
                self.seen_while_recording.setdefault("pid", wsi_article.PID_FILE.exists())
                reply, lines = next_entry("after" if stopped else "before")
                self.append_to_journal(*lines)
                return reply
            if command == "STOP":
                stopped.append(True)
                return {"status": "draining", "drain_seconds_left": 5, "segments_pending": 1}
            return {"status": "error", "message": f"unexpected {command}"}

        self.server = StubServer(stream.SERVER_SOCKET, answer)
        self.addCleanup(self.server.close)

    def run_client(self, signal_after=None, signum=signal.SIGTERM):
        if signal_after is not None:
            timer = threading.Timer(signal_after, os.kill, args=(os.getpid(), signum))
            timer.start()
            self.addCleanup(timer.cancel)
        args = argparse.Namespace(debug=False, language="en", no_notify=True)
        return wsi_article.run_article_mode(args)

    def states(self):
        return [v for name, v in self.signals if name == "StateChanged"]

    def raw(self):
        return wsi_article.ARTICLE_RAW_FILE.read_text()

    def marker_files_left(self):
        return [p.name for p in (wsi_article.ARTICLE_BUFFER_FILE,
                                 wsi_article.ARTICLE_RECORDING_ACTIVE_FILE,
                                 wsi_article.PID_FILE) if p.exists()]


class ReadNewSegmentsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = pathlib.Path(self.tmp.name) / "journal.jsonl"

    def test_a_missing_journal_is_nothing_yet(self):
        self.assertEqual(wsi_article.read_new_segments(self.path, 0), ([], 0))

    def test_complete_lines_are_returned_and_the_offset_moves_past_them(self):
        lines = [json.dumps(segment(0, "one")), json.dumps(segment(1, "two"))]
        self.path.write_text("\n".join(lines) + "\n")
        texts, offset = wsi_article.read_new_segments(self.path, 0)
        self.assertEqual(texts, ["one", "two"])
        self.assertEqual(offset, self.path.stat().st_size)
        self.assertEqual(wsi_article.read_new_segments(self.path, offset), ([], offset))

    def test_a_line_still_being_written_is_left_for_the_next_read(self):
        whole = json.dumps(segment(0, "one")) + "\n"
        half = json.dumps(segment(1, "two"))[:20]
        self.path.write_text(whole + half)
        texts, offset = wsi_article.read_new_segments(self.path, 0)
        self.assertEqual(texts, ["one"])
        self.assertEqual(offset, len(whole.encode()))
        self.path.write_text(whole + json.dumps(segment(1, "two")) + "\n")
        texts, _ = wsi_article.read_new_segments(self.path, offset)
        self.assertEqual(texts, ["two"])

    def test_empty_segments_carry_no_text_and_are_skipped(self):
        self.path.write_text(json.dumps(segment(0, "", status="empty")) + "\n"
                             + json.dumps(segment(1, "kept")) + "\n")
        self.assertEqual(wsi_article.read_new_segments(self.path, 0)[0], ["kept"])

    def test_a_complete_line_that_is_not_json_raises(self):
        self.path.write_text("{not json}\n")
        with self.assertRaises(ValueError):
            wsi_article.read_new_segments(self.path, 0)


class ArticleClientTest(ArticleCase):
    def test_start_asks_for_continuous_dictation_with_the_settings_limits(self):
        self.scripted([(RECORDING, ())], [(done(""), ())])
        self.run_client(signal_after=0.4)
        self.assertEqual(self.server.received[0],
                         ("START", {"continuous": True, "max_seconds": 3600,
                                    "silence_seconds": 120}))

    def test_segments_reach_the_window_and_the_final_flush_writes_one_paragraph(self):
        self.scripted(
            [(RECORDING, [segment(0, "first phrase")]), (RECORDING, [segment(1, "second phrase")])],
            [(done("first phrase second phrase third phrase"), [segment(2, "third phrase")])])
        rc = self.run_client(signal_after=0.5)
        self.assertEqual(rc, 0)
        self.assertEqual(self.raw(), "First phrase second phrase third phrase\n\n")
        self.assertTrue(wsi_article.ARTICLE_TRIGGER_FILE.exists(), "the window is told to polish")
        self.assertIn("STOP", self.server.commands())
        self.assertEqual(self.states()[-1], "IDLE")

    def test_the_buffer_holds_the_phrases_of_the_current_chunk_while_recording(self):
        seen = {}

        def snoop(command, params):
            if command == "KEEPALIVE":
                buffer_file = wsi_article.ARTICLE_BUFFER_FILE
                if buffer_file.exists():
                    seen["buffer"] = buffer_file.read_text()  # the latest one seen
            return None

        self.scripted([(RECORDING, [segment(0, "alpha")]), (RECORDING, [segment(1, "beta")])],
                      [(done("alpha beta"), ())])
        original = self.server.answer
        self.server.answer = lambda command, params: (snoop(command, params)
                                                      or original(command, params))
        self.run_client(signal_after=0.8)
        self.assertEqual(seen.get("buffer"), "alpha\nbeta")

    def test_a_chunk_is_flushed_when_the_flush_interval_has_passed(self):
        wsi_article.FLUSH_SECONDS = 0.3
        self.scripted([(RECORDING, [segment(0, "alpha")]), (RECORDING, ())],
                      [(done("alpha beta"), [segment(1, "beta")])])
        rc = self.run_client(signal_after=1.5)
        self.assertEqual(rc, 0)
        self.assertEqual(self.raw(), "Alpha\n\nBeta\n\n",
                         "alpha flushed on the interval, beta at the final flush")

    def test_the_marker_files_exist_while_recording_and_are_gone_after(self):
        self.scripted([(RECORDING, [segment(0, "alpha")])], [(done("alpha"), ())])
        self.run_client(signal_after=0.5)
        self.assertTrue(self.seen_while_recording["active"])
        self.assertTrue(self.seen_while_recording["pid"])
        self.assertEqual(self.marker_files_left(), [])

    def test_a_stop_waits_for_however_long_the_server_drains(self):
        draining = {"status": "draining", "drain_seconds_left": 5}
        self.scripted([(RECORDING, [segment(0, "alpha")])],
                      [(draining, ())] * 40 + [(done("alpha"), ())])
        rc = self.run_client(signal_after=0.3)
        self.assertEqual(rc, 0, "2 s of draining is far past the window's old 3 s kill")
        self.assertEqual(self.raw(), "Alpha\n\n")

    def test_a_server_that_ends_the_dictation_itself_is_followed_not_stopped(self):
        ended = dict(done("alpha"), stop_reason="no speech for 120 s")
        self.scripted([(RECORDING, [segment(0, "alpha")]), (ended, ())])
        rc = self.run_client()
        self.assertEqual(rc, 0)
        self.assertNotIn("STOP", self.server.commands())
        self.assertEqual(self.raw(), "Alpha\n\n")
        self.assertTrue(any("no speech for 120 s" in n for n in self.notes), self.notes)

    def test_a_failed_dictation_keeps_the_text_so_far_and_fails_loudly(self):
        failed = {"status": "failed", "transcription": "alpha",
                  "error": "segment 1 (10-19 s) failed to transcribe: CUDA out of memory",
                  "session_dir": str(self.session_dir), "kept_audio": ["seg-0001-failed.wav"]}
        self.scripted([(RECORDING, [segment(0, "alpha")]), (failed, ())])
        rc = self.run_client()
        self.assertEqual(rc, 1)
        self.assertEqual(self.raw(), "Alpha\n\n")
        self.assertIn("ERROR", self.states())
        self.assertTrue(any("CUDA out of memory" in n and str(self.session_dir) in n
                            for n in self.notes), self.notes)
        self.assertEqual(self.marker_files_left(), [])

    def test_a_refused_start_fails_loudly_with_nothing_left_behind(self):
        self.scripted([(RECORDING, ())],
                      on_start=lambda: {"status": "error", "message": "a dictation is running"})
        rc = self.run_client()
        self.assertEqual(rc, 1)
        self.assertIn("ERROR", self.states())
        self.assertTrue(any("a dictation is running" in n for n in self.notes), self.notes)
        self.assertNotIn("STOP", self.server.commands(), "nothing was started to stop")
        self.assertEqual(self.marker_files_left(), [])

    def test_a_journal_that_cannot_be_parsed_fails_loudly_and_stops_the_server(self):
        self.scripted([(RECORDING, ["{not json}"])], [(done(""), ())])
        rc = self.run_client()
        self.assertEqual(rc, 1)
        self.assertTrue(any("journal" in n for n in self.notes), self.notes)
        self.assertIn("STOP", self.server.commands(), "the microphone must not stay open")
        self.assertEqual(self.marker_files_left(), [])

    def test_a_journal_that_disagrees_with_the_final_text_fails_loudly(self):
        self.scripted([(RECORDING, [segment(0, "alpha")])],
                      [(done("alpha and something the journal never had"), ())])
        rc = self.run_client(signal_after=0.4)
        self.assertEqual(rc, 1)
        self.assertTrue(any("journal" in n and "final text" in n for n in self.notes), self.notes)
        self.assertEqual(self.raw(), "Alpha\n\n", "what the journal gave is still kept")

    def test_a_vanished_server_fails_loudly(self):
        self.scripted([(RECORDING, ())])
        self.server.answer = lambda command, params: (
            {"status": "recording", "session_dir": str(self.session_dir)} if command == "START"
            else {"status": "error", "message": "gone"})
        rc = self.run_client()
        self.assertEqual(rc, 1)
        self.assertIn("ERROR", self.states())
        self.assertEqual(self.marker_files_left(), [])


if __name__ == "__main__":
    unittest.main()
