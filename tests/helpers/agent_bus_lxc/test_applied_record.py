"""Unit tests for helpers/agent_bus_lxc/applied_record.py: the LXC member record's file beside
the applied stamps, readable by the stamps' group.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus_lxc.test_applied_record
"""

from __future__ import annotations

import os
import pathlib
import stat
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.agent_bus_lxc import applied_record


class WriteTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.applied = pathlib.Path(self._tmp.name) / "applied"
        self.applied.mkdir()
        self.directory = self.applied / "lxc"
        self.gid = os.getgid()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_the_directory_and_file_are_made_with_the_groups_modes(self) -> None:
        self.assertTrue(applied_record.write(self.directory, "team-a.json", "{}\n", self.gid))
        self.assertEqual(stat.S_IMODE(self.directory.stat().st_mode), 0o750)
        path = self.directory / "team-a.json"
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o640)
        self.assertEqual((path.stat().st_gid, path.read_text()), (self.gid, "{}\n"))

    def test_the_same_text_again_is_no_change(self) -> None:
        applied_record.write(self.directory, "team-a.json", "{}\n", self.gid)
        self.assertFalse(applied_record.write(self.directory, "team-a.json", "{}\n", self.gid))
        self.assertTrue(applied_record.write(self.directory, "team-a.json", "[]\n", self.gid))
        self.assertEqual(sorted(p.name for p in self.directory.iterdir()), ["team-a.json"])

    def test_none_removes_the_file_and_says_whether_there_was_one(self) -> None:
        applied_record.write(self.directory, "team-a.json", "{}\n", self.gid)
        self.assertTrue(applied_record.write(self.directory, "team-a.json", None, self.gid))
        self.assertFalse((self.directory / "team-a.json").exists())
        self.assertFalse(applied_record.write(self.directory, "team-a.json", None, self.gid))

    def test_a_missing_applied_directory_is_refused(self) -> None:
        with self.assertRaisesRegex(applied_record.RecordError, "is missing"):
            applied_record.write(self.applied / "gone" / "lxc", "team-a.json", "{}\n", self.gid)

    def test_a_symlink_in_place_of_the_directory_is_refused(self) -> None:
        (self.applied / "elsewhere").mkdir()
        self.directory.symlink_to(self.applied / "elsewhere")
        with self.assertRaisesRegex(applied_record.RecordError, "not a directory"):
            applied_record.write(self.directory, "team-a.json", "{}\n", self.gid)

    def test_names_lists_the_teams_with_a_record(self) -> None:
        self.assertEqual(applied_record.names(self.directory), [])
        applied_record.write(self.directory, "team-b.json", "{}\n", self.gid)
        applied_record.write(self.directory, "team-a.json", "{}\n", self.gid)
        (self.directory / ".team-c.json.new").write_text("{}\n")
        self.assertEqual(applied_record.names(self.directory), ["team-a", "team-b"])

    def test_names_refuses_a_symlink_in_place_of_the_directory(self) -> None:
        (self.applied / "elsewhere").mkdir()
        self.directory.symlink_to(self.applied / "elsewhere")
        with self.assertRaisesRegex(applied_record.RecordError, "not a directory"):
            applied_record.names(self.directory)

    def test_read_gives_the_text_or_none(self) -> None:
        self.assertIsNone(applied_record.read(self.directory / "team-a.json"))
        applied_record.write(self.directory, "team-a.json", "{}\n", self.gid)
        self.assertEqual(applied_record.read(self.directory / "team-a.json"), "{}\n")


if __name__ == "__main__":
    unittest.main()
