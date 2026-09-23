"""Tests for helpers.self_update.published — the result the user side may read (Plan 00137 T4.3).

The cycle's own state is root-only, and the host-health report runs as the user. This is
the one derived copy between them, so what is pinned here is its shape (the record keys
plus the owed boot), its permissions (group-readable, never writable) and that a reader
refuses a record it cannot read instead of guessing.
"""

from __future__ import annotations

import os
import re
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import cycle, published

RECORD = {
    "at": "2026-09-23T03:30:00Z", "phase": "play", "outcome": "play-failed",
    "old": "a" * 40, "new": "b" * 40, "plays": "playbooks/imports/play-claude-yolo.yml",
    "detail": "playbooks/imports/play-claude-yolo.yml exited 2; no reboot, retried next cycle",
}


class TestTheFormat(unittest.TestCase):
    def test_a_written_record_reads_back_with_the_owed_boot(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            published.write(directory, RECORD, owed_boot="boot-1")
            record = published.read(directory)
        self.assertEqual(record, {**RECORD, "owed_boot": "boot-1"})

    def test_the_keys_are_the_cycles_result_keys_plus_the_owed_boot(self) -> None:
        self.assertEqual(published.KEYS, (*cycle.RESULT_KEYS, "owed_boot"))

    def test_nothing_published_reads_as_none(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            self.assertIsNone(published.read(directory))

    def test_a_line_that_is_not_key_value_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with open(published.path(directory), "w", encoding="utf-8") as handle:
                handle.write("nonsense\n")
            with self.assertRaises(ValueError):
                published.read(directory)

    def test_a_record_missing_a_key_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with open(published.path(directory), "w", encoding="utf-8") as handle:
                handle.write("at=2026-09-23T03:30:00Z\noutcome=nothing\n")
            with self.assertRaises(ValueError):
                published.read(directory)

    def test_a_value_with_a_newline_is_refused_before_it_is_written(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                published.write(directory, {**RECORD, "detail": "two\nlines"}, owed_boot="")
            self.assertEqual(os.listdir(directory), [])


class TestThePermissions(unittest.TestCase):
    def test_the_file_is_group_readable_and_writable_by_nobody_but_its_owner(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            published.write(directory, RECORD, owed_boot="")
            mode = stat.S_IMODE(os.stat(published.path(directory)).st_mode)
        self.assertEqual(mode, 0o640)

    def test_no_temporary_file_is_left_behind(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            published.write(directory, RECORD, owed_boot="")
            self.assertEqual(os.listdir(directory), [published.FILE_NAME])


class TestTheOutcomeVocabulary(unittest.TestCase):
    def test_every_outcome_the_cycle_writes_is_one_the_reader_knows(self) -> None:
        """A new outcome in the cycle that the reader has never heard of would be read as
        "unknown" at every login. Read from the cycle's source, so adding one there
        without classifying it here fails this test."""
        source_path = os.path.join(os.path.dirname(cycle.__file__), "cycle.py")
        with open(source_path, encoding="utf-8") as handle:
            source = handle.read()
        written = set(re.findall(r'outcome="([a-z-]+)"', source))
        written |= set(re.findall(r'"outcome": "([a-z-]+)"', source))
        self.assertGreater(len(written), 5, "the pattern no longer matches the cycle's source")
        known = published.OK_OUTCOMES | published.IN_PROGRESS_OUTCOMES | published.FAILED_OUTCOMES
        self.assertEqual(written - known, set())

    def test_the_three_classes_do_not_overlap(self) -> None:
        classes = (published.OK_OUTCOMES, published.IN_PROGRESS_OUTCOMES, published.FAILED_OUTCOMES)
        for index, first in enumerate(classes):
            for second in classes[index + 1:]:
                self.assertEqual(first & second, frozenset())


if __name__ == "__main__":
    unittest.main()
