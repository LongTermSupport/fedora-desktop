"""Unit tests for helpers/gnome/fedora_compat.py — pure Fedora↔GNOME compat logic.

Run from the repo root:

    python3 -m unittest tests.helpers.gnome.test_fedora_compat
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.gnome import fedora_compat as fc

UUID = "speech-to-text@fedora-desktop"


class TestGnomeMajorForFedora(unittest.TestCase):
    def test_known_releases_map_to_expected_gnome_major(self):
        # The shipping reality this gate exists to protect: F44 → GNOME Shell 50.
        self.assertEqual(fc.gnome_major_for_fedora(44), 50)
        self.assertEqual(fc.gnome_major_for_fedora(43), 49)
        self.assertEqual(fc.gnome_major_for_fedora(42), 48)

    def test_unknown_release_returns_none(self):
        # Fail-fast signal for the caller: an unmapped Fedora version must not
        # silently pass. A new F<N> branch has to add its GNOME major explicitly.
        self.assertIsNone(fc.gnome_major_for_fedora(999))


class TestClassify(unittest.TestCase):
    def test_metadata_covers_running_gnome_is_ok(self):
        c = fc.classify(
            uuid=UUID,
            fedora_version=44,
            metadata_shell_versions=["45", "46", "47", "48", "49", "50"],
        )
        self.assertEqual(c.verdict, fc.Verdict.OK)
        self.assertFalse(c.is_failure)
        self.assertEqual(c.exit_code, 0)

    def test_metadata_string_versions_compare_correctly(self):
        # metadata.json lists strings; the lookup yields an int. Comparison must
        # not be tripped by the type difference.
        c = fc.classify(
            uuid=UUID, fedora_version=44, metadata_shell_versions=["50"]
        )
        self.assertEqual(c.verdict, fc.Verdict.OK)

    def test_metadata_integer_versions_compare_correctly(self):
        # Defensive: a metadata file could list ints rather than strings.
        c = fc.classify(
            uuid=UUID, fedora_version=44, metadata_shell_versions=[48, 49, 50]
        )
        self.assertEqual(c.verdict, fc.Verdict.OK)

    def test_metadata_missing_running_gnome_is_failure(self):
        # The exact drift the user hit: deployed metadata stops at 49 while the
        # branch targets F44 / GNOME 50.
        c = fc.classify(
            uuid=UUID, fedora_version=44, metadata_shell_versions=["47", "48", "49"]
        )
        self.assertEqual(c.verdict, fc.Verdict.FAIL_UNCOVERED)
        self.assertTrue(c.is_failure)
        self.assertEqual(c.exit_code, 1)
        self.assertIn("50", c.message)
        self.assertIn("metadata.json", c.message)

    def test_unknown_fedora_version_is_failure(self):
        c = fc.classify(
            uuid=UUID, fedora_version=999, metadata_shell_versions=["50"]
        )
        self.assertEqual(c.verdict, fc.Verdict.FAIL_UNKNOWN_FEDORA)
        self.assertTrue(c.is_failure)
        self.assertEqual(c.exit_code, 1)
        self.assertIn("fedora_compat.py", c.message)

    def test_empty_metadata_is_failure(self):
        c = fc.classify(uuid=UUID, fedora_version=44, metadata_shell_versions=[])
        self.assertEqual(c.verdict, fc.Verdict.FAIL_UNCOVERED)
        self.assertTrue(c.is_failure)


if __name__ == "__main__":
    unittest.main()
