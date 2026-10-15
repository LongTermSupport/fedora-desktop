"""Unit tests for helpers/pingbus/inject.py: a session's delivery marks and the check that a
delivery reached the conversation.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_inject

Spec: Plan 00194 SPEC-inject.md E15, E16 and E18.
"""

from __future__ import annotations

import json
import os
import pathlib
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import inject

SOCKET = "/tmp/claude-sockets/session-one.sock"
OTHER = "/tmp/claude-sockets/session-two.sock"


class MarksTest(unittest.TestCase):
    def setUp(self) -> None:
        self.home = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.marks = inject.Marks(self.home, SOCKET)

    def test_the_key_is_the_socket_paths_hash(self):
        key = inject.session_key(SOCKET)
        self.assertRegex(key, r"^[0-9a-f]{16}$")
        self.assertNotEqual(key, inject.session_key(OTHER))

    def test_marks_start_unset_and_create_nothing_when_read(self):
        self.assertFalse(self.marks.stalled())
        self.assertFalse(self.marks.missing())
        self.assertIsNone(self.marks.transcript())
        self.assertEqual(list(self.home.iterdir()), [])

    def test_marks_set_and_clear(self):
        self.marks.set_stalled(True)
        self.marks.set_missing(True)
        self.assertTrue(self.marks.stalled())
        self.assertTrue(self.marks.missing())
        directory = self.home / inject.INJECT_DIR
        self.assertEqual(stat.S_IMODE(directory.stat().st_mode), 0o700)
        self.marks.set_stalled(False)
        self.marks.set_missing(False)
        self.assertFalse(self.marks.stalled())
        self.assertFalse(self.marks.missing())

    def test_another_sessions_marks_are_not_read(self):
        """E18: on the host PINGBUS_HOME is shared by every team session of the user."""
        self.marks.set_stalled(True)
        self.marks.set_missing(True)
        self.marks.record_transcript("/home/u/.claude/projects/p/one.jsonl")
        other = inject.Marks(self.home, OTHER)
        self.assertFalse(other.stalled())
        self.assertFalse(other.missing())
        self.assertIsNone(other.transcript())

    def test_the_transcript_path_is_recorded_privately(self):
        self.marks.record_transcript("/home/u/.claude/projects/p/one.jsonl")
        self.assertEqual(self.marks.transcript(), "/home/u/.claude/projects/p/one.jsonl")
        (path,) = (self.home / inject.INJECT_DIR).iterdir()
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)

    def test_a_bad_transcript_path_is_refused(self):
        for bad in ("relative.jsonl", "", "/a\nb", "/a\x00b", "/" + "x" * 5000, 7, None):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.marks.record_transcript(bad)

    def test_a_tampered_record_reads_as_none(self):
        self.marks.record_transcript("/t.jsonl")
        (path,) = (self.home / inject.INJECT_DIR).iterdir()
        path.write_text("relative/path", encoding="utf-8")
        self.assertIsNone(self.marks.transcript())


class CheckTest(unittest.TestCase):
    """E16: a delivery is found in the transcript, or marked missing once the transcript has
    been quiet for a while."""

    def setUp(self) -> None:
        self.dir = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.transcript = self.dir / "t.jsonl"
        self.transcript.write_text(json.dumps({"type": "user", "message": "orders"}) + "\n", encoding="utf-8")
        self.marks = inject.Marks(self.dir / "home", SOCKET)
        self.marks.record_transcript(str(self.transcript))
        self.now = [100.0]
        self.check = inject.DeliveryCheck(self.marks, lambda: self.now[0], quiet_s=180, give_up_s=3600)

    def append(self, obj: dict) -> None:
        with self.transcript.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(obj) + "\n")

    def send(self, nonce: str) -> None:
        self.check.sent(nonce, self.check.prepare())

    def test_a_delivery_in_the_transcript_is_found(self):
        self.send("3f9c2a71")
        self.append({"type": "user", "message": {"content": "agent-bus delivery 3f9c2a71: 1 message (...)"}})
        self.assertEqual(self.check.poll(), (["3f9c2a71"], []))
        self.assertEqual(self.check.outstanding(), 0)
        self.assertFalse(self.marks.missing())

    def test_a_queued_peer_attachment_counts(self):
        self.send("3f9c2a71")
        self.append({"type": "attachment", "attachment": {"type": "queued_command", "origin": {"kind": "peer"},
                                                          "prompt": "agent-bus delivery 3f9c2a71: 1 message"}})
        self.assertEqual(self.check.poll(), (["3f9c2a71"], []))

    def test_text_written_before_the_send_does_not_count(self):
        self.append({"type": "user", "message": "agent-bus delivery 3f9c2a71: an old copy"})
        self.send("3f9c2a71")
        self.now[0] += 179
        self.assertEqual(self.check.poll(), ([], []))
        self.now[0] += 1
        self.assertEqual(self.check.poll(), ([], ["3f9c2a71"]))
        self.assertTrue(self.marks.missing())

    def test_a_busy_session_is_not_quiet(self):
        """While the transcript grows the session is working: a queued message waits."""
        self.send("3f9c2a71")
        for _ in range(10):
            self.now[0] += 120
            self.append({"type": "assistant", "message": "working"})
            self.assertEqual(self.check.poll(), ([], []))
        self.now[0] += 120
        self.append({"type": "attachment", "attachment": {"prompt": "agent-bus delivery 3f9c2a71: 1 message"}})
        self.assertEqual(self.check.poll(), (["3f9c2a71"], []))

    def test_an_hour_without_it_is_missing_however_busy(self):
        self.send("3f9c2a71")
        missing: list[str] = []
        for _ in range(31):
            self.now[0] += 120
            self.append({"type": "assistant", "message": "working"})
            missing += self.check.poll()[1]
        self.assertEqual(missing, ["3f9c2a71"])

    def test_without_a_recorded_transcript_a_delivery_goes_missing(self):
        marks = inject.Marks(self.dir / "home2", SOCKET)
        check = inject.DeliveryCheck(marks, lambda: self.now[0], quiet_s=180, give_up_s=3600)
        check.sent("aaaaaaaa", check.prepare())
        self.now[0] += 180
        self.assertEqual(check.poll(), ([], ["aaaaaaaa"]))
        self.assertTrue(marks.missing())

    def test_a_later_delivery_found_clears_the_missing_mark(self):
        self.send("aaaaaaaa")
        self.now[0] += 200
        self.assertEqual(self.check.poll(), ([], ["aaaaaaaa"]))
        self.assertTrue(self.marks.missing())
        self.send("bbbbbbbb")
        self.append({"type": "user", "message": "agent-bus delivery bbbbbbbb: 1 message"})
        self.assertEqual(self.check.poll(), (["bbbbbbbb"], []))
        self.assertFalse(self.marks.missing())

    def test_a_new_transcript_after_clear_is_read_from_its_start(self):
        self.send("3f9c2a71")
        fresh = self.dir / "fresh.jsonl"
        fresh.write_text(json.dumps({"message": "agent-bus delivery 3f9c2a71: 1 message"}) + "\n", encoding="utf-8")
        self.marks.record_transcript(str(fresh))
        self.assertEqual(self.check.poll(), (["3f9c2a71"], []))

    def test_a_marker_split_across_two_writes_is_found(self):
        self.send("3f9c2a71")
        with self.transcript.open("a", encoding="utf-8") as handle:
            handle.write('{"message": "agent-bus deliv')
        self.assertEqual(self.check.poll(), ([], []))
        with self.transcript.open("a", encoding="utf-8") as handle:
            handle.write('ery 3f9c2a71: 1 message"}\n')
        self.assertEqual(self.check.poll(), (["3f9c2a71"], []))

    def test_an_unreadable_transcript_is_not_a_crash(self):
        self.send("3f9c2a71")
        os.unlink(self.transcript)
        self.now[0] += 180
        self.assertEqual(self.check.poll(), ([], ["3f9c2a71"]))


if __name__ == "__main__":
    unittest.main()
