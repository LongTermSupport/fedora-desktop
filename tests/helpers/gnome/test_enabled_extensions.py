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


class TestDiscoverDeployedUuids(unittest.TestCase):
    def _extension(self, root: pathlib.Path, directory: str, metadata: object) -> None:
        path = root / directory
        path.mkdir(parents=True)
        if metadata is not None:
            (path / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")

    def test_reads_the_uuid_from_metadata_not_the_directory_name(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, CUSTOM, {"uuid": CUSTOM, "name": "Workspace Names"})
            self.assertEqual(ee.discover_deployed_uuids(str(root)), [CUSTOM])

    def test_result_is_sorted_for_determinism(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            for uuid in ("c@x", "a@x", "b@x"):
                self._extension(root, uuid, {"uuid": uuid})
            self.assertEqual(ee.discover_deployed_uuids(str(root)), ["a@x", "b@x", "c@x"])

    def test_missing_directory_yields_nothing(self):
        with tempfile.TemporaryDirectory() as tmp:
            absent = str(pathlib.Path(tmp) / "no-such-dir")
            self.assertEqual(ee.discover_deployed_uuids(absent), [])

    def test_directory_without_metadata_is_not_an_extension(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, "leftover", None)
            self.assertEqual(ee.discover_deployed_uuids(str(root)), [])

    def test_uuid_disagreeing_with_the_directory_name_is_an_error(self):
        # GNOME Shell refuses to load such an extension; a silent skip here would
        # hand the play a shorter list and pass. Fail instead.
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, "a@x", {"uuid": "b@x"})
            with self.assertRaises(ValueError) as caught:
                ee.discover_deployed_uuids(str(root))
            self.assertIn("a@x", str(caught.exception))
            self.assertIn("b@x", str(caught.exception))

    def test_uuid_containing_a_comma_is_an_error(self):
        # The executor reports the list comma-separated; one would become two.
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, "a,b@x", {"uuid": "a,b@x"})
            with self.assertRaises(ValueError) as caught:
                ee.discover_deployed_uuids(str(root))
            self.assertIn("comma", str(caught.exception))

    def test_metadata_without_a_uuid_is_an_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self._extension(root, "a@x", {"name": "No UUID"})
            with self.assertRaises(ValueError):
                ee.discover_deployed_uuids(str(root))

    def test_unparseable_metadata_is_an_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            path = root / "a@x"
            path.mkdir()
            (path / "metadata.json").write_text("{not json", encoding="utf-8")
            with self.assertRaises(ValueError):
                ee.discover_deployed_uuids(str(root))


class TestCheckRequired(unittest.TestCase):
    def test_all_present_returns_nothing_missing(self):
        self.assertEqual(ee.missing_required([CUSTOM, "a@x"], [CUSTOM]), [])

    def test_absent_required_uuid_is_reported(self):
        # The custom extension's copy step failing must not pass as "7 deployed".
        self.assertEqual(ee.missing_required(["a@x"], [CUSTOM]), [CUSTOM])

    def test_missing_is_reported_in_the_order_required(self):
        self.assertEqual(ee.missing_required([], ["b@x", "a@x"]), ["b@x", "a@x"])


if __name__ == "__main__":
    unittest.main()
