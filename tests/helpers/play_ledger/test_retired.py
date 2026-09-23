"""Tests for helpers.play_ledger.retired — the map of removed plays to their successors.

A play the ledger has seen and HEAD no longer has is GONE, and GONE has no remedy of
its own: there is nothing to re-run. The map names the play that absorbed it, so the
advice becomes "run that one", and the finding can end once it has been run. A map
that is wrong must be refused, never read around — a mistyped path would otherwise
hide nothing and say nothing.
"""

from __future__ import annotations

import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import retired

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
GONE = "playbooks/imports/play-old.yml"
SUCCESSOR = "playbooks/imports/play-new.yml"


class TestParse(unittest.TestCase):
    def test_a_flat_object_of_paths_is_the_map(self) -> None:
        self.assertEqual(retired.parse(f'{{"{GONE}": "{SUCCESSOR}"}}'), {GONE: SUCCESSOR})

    def test_an_empty_object_is_an_empty_map(self) -> None:
        self.assertEqual(retired.parse("{}"), {})

    def test_invalid_json_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            retired.parse("{not json")

    def test_a_non_object_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            retired.parse(f'["{GONE}"]')

    def test_a_non_string_successor_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            retired.parse(f'{{"{GONE}": 1}}')

    def test_an_empty_path_is_refused(self) -> None:
        for text in (f'{{"": "{SUCCESSOR}"}}', f'{{"{GONE}": ""}}'):
            with self.subTest(text=text), self.assertRaises(ValueError):
                retired.parse(text)

    def test_an_absolute_or_escaping_path_is_refused(self) -> None:
        """The ledger records repo-relative paths; any other form can never match one,
        so the entry would silently do nothing."""
        for bad in ("/abs/play.yml", "../outside.yml", "playbooks/../x.yml"):
            with self.subTest(path=bad), self.assertRaises(ValueError):
                retired.parse(f'{{"{bad}": "{SUCCESSOR}"}}')
            with self.subTest(successor=bad), self.assertRaises(ValueError):
                retired.parse(f'{{"{GONE}": "{bad}"}}')

    def test_a_play_cannot_succeed_itself(self) -> None:
        with self.assertRaises(ValueError):
            retired.parse(f'{{"{GONE}": "{GONE}"}}')

    def test_a_successor_that_is_itself_retired_is_refused(self) -> None:
        """A chain would advise running a play that is also gone. Point the entry at
        the play that exists instead."""
        with self.assertRaises(ValueError):
            retired.parse('{"a.yml": "b.yml", "b.yml": "c.yml"}')

    def test_a_duplicate_key_is_refused(self) -> None:
        """json.loads keeps the last duplicate silently; the first entry would vanish."""
        with self.assertRaises(ValueError):
            retired.parse(f'{{"{GONE}": "{SUCCESSOR}", "{GONE}": "other.yml"}}')


class TestLoad(unittest.TestCase):
    def test_reads_the_map_from_the_checkout(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            path = os.path.join(root, retired.MAP_PATH)
            os.makedirs(os.path.dirname(path))
            with open(path, "w", encoding="utf-8") as handle:
                handle.write(f'{{"{GONE}": "{SUCCESSOR}"}}\n')
            self.assertEqual(retired.load(root), {GONE: SUCCESSOR})

    def test_a_missing_map_is_an_error_not_an_empty_map(self) -> None:
        """The file is tracked. Its absence means the checkout is not what this code
        expects, and reading that as 'nothing retired' would hide it."""
        with tempfile.TemporaryDirectory() as root, self.assertRaises(OSError):
            retired.load(root)

    def test_the_tracked_map_parses(self) -> None:
        """The shipped file itself, so a malformed edit fails the helper tests rather
        than a login on some later day."""
        retired.load(REPO_ROOT)


class TestValidate(unittest.TestCase):
    def test_a_map_consistent_with_head_passes(self) -> None:
        retired.validate({GONE: SUCCESSOR}, exists_at_head=lambda path: path == SUCCESSOR)

    def test_a_retired_play_still_at_head_is_refused(self) -> None:
        """It is not retired. Either the entry is premature or the file was meant to go."""
        with self.assertRaisesRegex(ValueError, GONE):
            retired.validate({GONE: SUCCESSOR}, exists_at_head=lambda path: True)

    def test_a_successor_missing_at_head_is_refused(self) -> None:
        """The advice would be to run a play that does not exist."""
        with self.assertRaisesRegex(ValueError, SUCCESSOR):
            retired.validate({GONE: SUCCESSOR}, exists_at_head=lambda path: False)


if __name__ == "__main__":
    unittest.main()
