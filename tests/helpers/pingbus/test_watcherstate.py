"""Unit tests for helpers/pingbus/watcherstate.py: a team's watcher record, `watcher.json`.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_watcherstate

Spec: docs/agent-bus-protocol.md §12 (`state/watcher.json`) and §13 (`watch --supervise`,
`status`); issue #116 (a watcher that keeps exiting was visible only in watch.log).
"""

from __future__ import annotations

import json
import pathlib
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import inbox, watcherstate

MS = 1_791_000_000_000


class WatcherStateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.state = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory())) / "team" / "state"
        self.state.parent.mkdir()

    def test_no_record_reads_as_none_and_creates_nothing(self):
        self.assertIsNone(watcherstate.read(self.state))
        self.assertFalse(self.state.exists())

    def test_each_restart_counts_on_from_the_first(self):
        first = watcherstate.record_exit(self.state, "exit 1 (an uncaught exception)", MS)
        self.assertEqual(first, watcherstate.Record(watcherstate.FAILING, "exit 1 (an uncaught exception)",
                                                    1, MS, MS))
        second = watcherstate.record_exit(self.state, "killed by signal SIGKILL", MS + 5)
        self.assertEqual((second.state, second.restarts, second.since_ms, second.at_ms, second.cause),
                         (watcherstate.FAILING, 2, MS, MS + 5, "killed by signal SIGKILL"))
        self.assertEqual(watcherstate.read(self.state), second)

    def test_the_file_is_private_and_written_whole(self):
        watcherstate.record_exit(self.state, "exit 7", MS)
        path = watcherstate.path(self.state)
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(json.loads(path.read_text(encoding="ascii")),
                         {"v": 1, "state": "failing", "cause": "exit 7", "restarts": 1,
                          "since_ms": MS, "at_ms": MS})
        self.assertEqual([p.name for p in self.state.iterdir()], [watcherstate.FILE])

    def test_a_stop_keeps_the_count_and_is_final(self):
        watcherstate.record_exit(self.state, "exit 7", MS)
        stopped = watcherstate.record_stop(self.state, "homeserver: forbidden", MS + 9)
        self.assertEqual(stopped, watcherstate.Record(watcherstate.STOPPED, "homeserver: forbidden", 1, MS, MS + 9))
        again = watcherstate.record_exit(self.state, "exit 7", MS + 10)
        self.assertEqual((again.restarts, again.since_ms), (1, MS + 10), "a stop is not counted on")

    def test_a_sync_clears_it(self):
        watcherstate.record_stop(self.state, "refused", MS)
        watcherstate.clear(self.state)
        self.assertIsNone(watcherstate.read(self.state))
        watcherstate.clear(self.state)  # nothing to clear is not an error

    def test_a_cause_is_one_printable_line_of_at_most_200(self):
        record = watcherstate.record_exit(self.state, "bad\tline\nnext\x1b[31m" + "x" * 400, MS)
        self.assertLessEqual(len(record.cause), watcherstate.CAUSE_MAX)
        self.assertTrue(record.cause.isascii() and record.cause.isprintable(), record.cause)
        self.assertTrue(record.cause.startswith("bad?line?next?[31m"), record.cause)
        self.assertEqual(watcherstate.record_exit(self.state, " \n", MS).cause, "-")

    def test_a_malformed_record_is_a_state_error(self):
        self.state.mkdir()
        path = watcherstate.path(self.state)
        for bad in ("not json", "[]", '{"v": 1}',
                    '{"v": 2, "state": "failing", "cause": "x", "restarts": 1, "since_ms": 1, "at_ms": 1}',
                    '{"v": 1, "state": "dead", "cause": "x", "restarts": 1, "since_ms": 1, "at_ms": 1}',
                    '{"v": 1, "state": "failing", "cause": "", "restarts": 1, "since_ms": 1, "at_ms": 1}',
                    '{"v": 1, "state": "failing", "cause": "x\\ty", "restarts": 1, "since_ms": 1, "at_ms": 1}',
                    '{"v": 1, "state": "failing", "cause": "x", "restarts": true, "since_ms": 1, "at_ms": 1}',
                    '{"v": 1, "state": "failing", "cause": "x", "restarts": -1, "since_ms": 1, "at_ms": 1}',
                    '{"v": 1, "state": "failing", "cause": "x", "restarts": 1, "since_ms": 1, "at_ms": 1, "y": 0}'):
            with self.subTest(bad=bad):
                path.write_text(bad, encoding="ascii")
                with self.assertRaises(inbox.StateError):
                    watcherstate.read(self.state)

    def test_a_malformed_record_is_replaced_by_the_next_exit(self):
        self.state.mkdir()
        watcherstate.path(self.state).write_text("not json", encoding="ascii")
        self.assertEqual(watcherstate.record_exit(self.state, "exit 1", MS).restarts, 1)


if __name__ == "__main__":
    unittest.main()
