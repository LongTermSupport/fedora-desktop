"""Unit tests for helpers/gnome/enabled_extensions.py — the declared enabled list.

Run from the repo root:

    python3 -m unittest tests.helpers.gnome.test_enabled_extensions
"""

from __future__ import annotations

import json
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.gnome import enabled_extensions as ee

CUSTOM = "workspace-names-overview@fedora-desktop"
# A stand-in for a UUID already in the list that is not ours (Fedora ships one).
# Reserved-domain form: the pre-commit secret scanner reads a real one as an email.
STOCK = "stock-extension@example.com"


class TestParseStringList(unittest.TestCase):
    def test_empty_typed_form(self):
        # `gsettings get` prints the type annotation when the list is empty.
        self.assertEqual(ee.parse_string_list("@as []"), [])

    def test_empty_untyped_form(self):
        self.assertEqual(ee.parse_string_list("[]"), [])

    def test_surrounding_whitespace_and_trailing_newline(self):
        self.assertEqual(ee.parse_string_list("  ['a@b']\n"), ["a@b"])

    def test_single_item(self):
        self.assertEqual(ee.parse_string_list("['" + STOCK + "']"), [STOCK])

    def test_multiple_items(self):
        self.assertEqual(
            ee.parse_string_list(f"['{STOCK}', '{CUSTOM}']"), [STOCK, CUSTOM]
        )

    def test_no_space_after_comma(self):
        self.assertEqual(ee.parse_string_list("['a@b','c@d']"), ["a@b", "c@d"])

    def test_escaped_quote_and_backslash(self):
        self.assertEqual(ee.parse_string_list(r"['it\'s', 'a\\b']"), ["it's", "a\\b"])

    def test_unterminated_string_is_an_error(self):
        with self.assertRaises(ValueError):
            ee.parse_string_list("['a@b")

    def test_missing_brackets_is_an_error(self):
        with self.assertRaises(ValueError):
            ee.parse_string_list("'a@b'")

    def test_trailing_junk_is_an_error(self):
        with self.assertRaises(ValueError):
            ee.parse_string_list("['a@b'] nonsense")

    def test_unquoted_element_is_an_error(self):
        with self.assertRaises(ValueError):
            ee.parse_string_list("[a@b]")

    def test_dangling_comma_is_an_error(self):
        with self.assertRaises(ValueError):
            ee.parse_string_list("['a@b', ]")


class TestFormatStringList(unittest.TestCase):
    def test_empty_uses_typed_form(self):
        # An untyped `[]` is ambiguous to gsettings; `@as []` is not.
        self.assertEqual(ee.format_string_list([]), "@as []")

    def test_items_are_single_quoted_and_comma_separated(self):
        self.assertEqual(
            ee.format_string_list([STOCK, CUSTOM]), f"['{STOCK}', '{CUSTOM}']"
        )

    def test_escapes_backslash_and_quote(self):
        self.assertEqual(ee.format_string_list(["it's"]), r"['it\'s']")
        self.assertEqual(ee.format_string_list(["a\\b"]), r"['a\\b']")

    def test_round_trips_through_the_parser(self):
        values = [STOCK, CUSTOM, "it's", "a\\b"]
        self.assertEqual(ee.parse_string_list(ee.format_string_list(values)), values)


class TestMerge(unittest.TestCase):
    def test_adds_missing_and_reports_changed(self):
        result = ee.merge([STOCK], [CUSTOM])
        self.assertEqual(result.values, [STOCK, CUSTOM])
        self.assertTrue(result.changed)
        self.assertEqual(result.added, [CUSTOM])

    def test_already_present_is_unchanged(self):
        result = ee.merge([STOCK, CUSTOM], [CUSTOM])
        self.assertEqual(result.values, [STOCK, CUSTOM])
        self.assertFalse(result.changed)
        self.assertEqual(result.added, [])

    def test_never_removes_what_the_user_added(self):
        # The user's own enabled extensions are not ours to revoke.
        user_extra = "something-the-user-installed@example.com"
        result = ee.merge([STOCK, user_extra], [CUSTOM])
        self.assertEqual(result.values, [STOCK, user_extra, CUSTOM])
        self.assertIn(user_extra, result.values)

    def test_preserves_existing_order_and_appends_in_deployed_order(self):
        result = ee.merge([STOCK], ["b@x", "a@x"])
        self.assertEqual(result.values, [STOCK, "b@x", "a@x"])

    def test_empty_current_yields_the_deployed_set(self):
        # The fresh-install case Plan 00110 found: the list holds only stock.
        result = ee.merge([], [CUSTOM])
        self.assertEqual(result.values, [CUSTOM])
        self.assertTrue(result.changed)

    def test_duplicates_in_current_are_collapsed_and_count_as_changed(self):
        result = ee.merge([STOCK, STOCK], [STOCK])
        self.assertEqual(result.values, [STOCK])
        self.assertTrue(result.changed)

    def test_duplicates_in_deployed_are_collapsed(self):
        result = ee.merge([], [CUSTOM, CUSTOM])
        self.assertEqual(result.values, [CUSTOM])
        self.assertEqual(result.added, [CUSTOM])

    def test_nothing_deployed_is_unchanged(self):
        result = ee.merge([STOCK], [])
        self.assertEqual(result.values, [STOCK])
        self.assertFalse(result.changed)

    def test_merge_is_idempotent(self):
        first = ee.merge([STOCK], [CUSTOM])
        second = ee.merge(first.values, [CUSTOM])
        self.assertEqual(second.values, first.values)
        self.assertFalse(second.changed)


class TestValidateUuid(unittest.TestCase):
    def test_a_plain_uuid_is_returned_unchanged(self):
        self.assertEqual(ee.validate_uuid(CUSTOM), CUSTOM)

    def test_a_comma_is_rejected(self):
        # The executor reports the declared list comma-separated; one would become two.
        with self.assertRaises(ValueError) as caught:
            ee.validate_uuid("a,b@x")
        self.assertIn("comma", str(caught.exception))

    def test_a_newline_is_rejected(self):
        # It would split the marker line in two and corrupt the play's set_fact.
        with self.assertRaises(ValueError):
            ee.validate_uuid("a\nb@x")

    def test_a_space_is_rejected(self):
        # The marker is "GNOME-EXT-DEPLOYED <csv>"; the play splits on the first space.
        with self.assertRaises(ValueError):
            ee.validate_uuid("a b@x")

    def test_empty_is_rejected(self):
        with self.assertRaises(ValueError):
            ee.validate_uuid("")


class TestResolveDeclared(unittest.TestCase):
    """The deployed set is DECLARED by the play; disk only confirms it.

    Discovering it from the extensions directory instead was wrong in both
    directions: too wide (it swept up the user's own extensions, so a stale
    third-party one aborted provisioning and a deliberately-disabled one was
    re-enabled every deploy) and too narrow (six of seven downloaded read as
    complete success, because nothing held the count against the declaration).
    """

    def _extension(self, root: pathlib.Path, directory: str, metadata: object) -> None:
        path = root / directory
        path.mkdir(parents=True)
        if metadata is not None:
            (path / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")

    def test_declared_and_present_resolves(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, CUSTOM, {"uuid": CUSTOM})
            result = ee.resolve_declared([str(root)], [CUSTOM])
        self.assertEqual(result.found, [CUSTOM])
        self.assertEqual(result.missing, [])

    def test_declared_order_is_preserved_not_sorted(self):
        # The play's order is the declaration's order; re-sorting would make the
        # marker line disagree with the play for no reason.
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            for uuid in ("c@x", "a@x", "b@x"):
                self._extension(root, uuid, {"uuid": uuid})
            result = ee.resolve_declared([str(root)], ["c@x", "a@x", "b@x"])
        self.assertEqual(result.found, ["c@x", "a@x", "b@x"])

    def test_a_declared_uuid_that_is_not_on_disk_is_missing_not_skipped(self):
        # The partial-install hole: six of seven downloaded must not read as success.
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, "a@x", {"uuid": "a@x"})
            result = ee.resolve_declared([str(root)], ["a@x", "b@x"])
        self.assertEqual(result.found, ["a@x"])
        self.assertEqual(result.missing, ["b@x"])

    def test_an_undeclared_extension_on_disk_is_ignored(self):
        # The user's own extensions are not ours to judge, enable, or fail on.
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, CUSTOM, {"uuid": CUSTOM})
            self._extension(root, "theirs@example.com", {"uuid": "theirs@example.com"})
            result = ee.resolve_declared([str(root)], [CUSTOM])
        self.assertEqual(result.found, [CUSTOM])
        self.assertNotIn("theirs@example.com", result.found)

    def test_searches_every_path_in_order(self):
        # dash-to-dock is a DNF-installed SYSTEM extension, in a second directory.
        with tempfile.TemporaryDirectory() as tmp:
            user = pathlib.Path(tmp) / "user"
            system = pathlib.Path(tmp) / "system"
            self._extension(user, CUSTOM, {"uuid": CUSTOM})
            self._extension(system, "dock@x", {"uuid": "dock@x"})
            result = ee.resolve_declared([str(user), str(system)], [CUSTOM, "dock@x"])
        self.assertEqual(result.found, [CUSTOM, "dock@x"])
        self.assertEqual(result.missing, [])
        self.assertTrue(result.locations["dock@x"].startswith(str(system)))

    def test_a_search_path_that_does_not_exist_is_not_an_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, CUSTOM, {"uuid": CUSTOM})
            absent = str(pathlib.Path(tmp) / "no-such-dir")
            result = ee.resolve_declared([absent, str(root)], [CUSTOM])
        self.assertEqual(result.found, [CUSTOM])

    def test_metadata_uuid_disagreeing_with_the_directory_is_an_error(self):
        # GNOME Shell refuses to load such an extension; treating it as present
        # would declare a UUID the shell will never enable.
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, "a@x", {"uuid": "b@x"})
            with self.assertRaises(ValueError) as caught:
                ee.resolve_declared([str(root)], ["a@x"])
            self.assertIn("a@x", str(caught.exception))
            self.assertIn("b@x", str(caught.exception))

    def test_metadata_without_a_uuid_is_an_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, "a@x", {"name": "No UUID"})
            with self.assertRaises(ValueError):
                ee.resolve_declared([str(root)], ["a@x"])

    def test_unparseable_metadata_is_an_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            path = root / "a@x"
            path.mkdir()
            (path / "metadata.json").write_text("{not json", encoding="utf-8")
            with self.assertRaises(ValueError):
                ee.resolve_declared([str(root)], ["a@x"])

    def test_a_directory_without_metadata_counts_as_missing(self):
        # An interrupted extract leaves the directory and no metadata.json.
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, "a@x", None)
            result = ee.resolve_declared([str(root)], ["a@x"])
        self.assertEqual(result.missing, ["a@x"])

    def test_an_invalid_declared_uuid_is_rejected_before_any_disk_access(self):
        result_dir = "/nonexistent-on-purpose"
        with self.assertRaises(ValueError):
            ee.resolve_declared([result_dir], ["a,b@x"])

    def test_duplicate_declarations_collapse(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, CUSTOM, {"uuid": CUSTOM})
            result = ee.resolve_declared([str(root)], [CUSTOM, CUSTOM])
        self.assertEqual(result.found, [CUSTOM])


if __name__ == "__main__":
    unittest.main()
