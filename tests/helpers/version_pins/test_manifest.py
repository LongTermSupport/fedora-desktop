"""Tests for helpers.version_pins.manifest — Plan 00109, Task 2.2.

The pin manifest used to be a heredoc inside `scripts/check-pinned-versions.bash`,
so a second consumer could only duplicate it. It is now `vars/version-pins.yml` and
this validates it.

Pure: it takes an already-decoded mapping — the JSON `scripts/qa-version-pins.bash`
converts the YAML into — because helpers are stdlib-only and cannot read YAML.

The properties that carry weight, all of them ways a manifest can be wrong while
still looking fine:

1. **A row is serialised pipe-delimited** for the bash consumer, so a `|` or a
   newline inside a field silently produces a different row. Rejected, not escaped.
2. **A manual pin with no note** tells the human nothing to act on, which is a
   review gate that cannot be actioned.
3. **An empty manifest validates vacuously** unless emptiness is itself an error —
   the whole gate would then pass having checked nothing.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.version_pins import manifest

GOOD_ROW = {
    "playbook": "playbooks/imports/play-nvm-install.yml",
    "var": "nvm_version",
    "github": "nvm-sh/nvm",
}
MANUAL_ROW = {
    "playbook": "playbooks/imports/optional/hardware-specific/play-nvidia.yml",
    "var": "cudnn_version",
    "note": "check the vendor site by hand",
}


def document(*rows: dict) -> dict:
    return {"version_pins": list(rows)}


class TestParse(unittest.TestCase):
    def test_a_good_row_parses(self) -> None:
        pins = manifest.parse(document(GOOD_ROW))
        self.assertEqual(len(pins), 1)
        self.assertEqual(pins[0].playbook, GOOD_ROW["playbook"])
        self.assertEqual(pins[0].var, "nvm_version")
        self.assertEqual(pins[0].github, "nvm-sh/nvm")

    def test_a_manual_row_has_no_github_and_keeps_its_note(self) -> None:
        pins = manifest.parse(document(MANUAL_ROW))
        self.assertEqual(pins[0].github, "")
        self.assertIn("vendor site", pins[0].note)

    def test_an_optional_tag_prefix_is_carried(self) -> None:
        """Some projects tag releases as `release-5.6.0`; the comparison strips it."""
        row = dict(GOOD_ROW, tag_prefix="release-")
        self.assertEqual(manifest.parse(document(row))[0].tag_prefix, "release-")

    def test_tag_prefix_defaults_to_empty(self) -> None:
        self.assertEqual(manifest.parse(document(GOOD_ROW))[0].tag_prefix, "")

    def test_several_rows_keep_their_order(self) -> None:
        pins = manifest.parse(document(GOOD_ROW, MANUAL_ROW))
        self.assertEqual([p.var for p in pins], ["nvm_version", "cudnn_version"])


class TestEmptinessIsAnError(unittest.TestCase):
    def test_no_version_pins_key_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse({})

    def test_an_empty_list_is_rejected(self) -> None:
        """A manifest with no pins would validate, report nothing, and exit 0 — a
        gate that passes having checked nothing."""
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document())

    def test_a_null_version_pins_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse({"version_pins": None})

    def test_a_non_list_version_pins_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse({"version_pins": {"playbook": "x"}})


class TestRequiredFields(unittest.TestCase):
    def test_a_row_with_no_playbook_is_rejected(self) -> None:
        row = {k: v for k, v in GOOD_ROW.items() if k != "playbook"}
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(row))

    def test_a_row_with_no_var_is_rejected(self) -> None:
        row = {k: v for k, v in GOOD_ROW.items() if k != "var"}
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(row))

    def test_an_empty_playbook_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(dict(GOOD_ROW, playbook="")))

    def test_a_row_that_is_not_a_mapping_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse({"version_pins": ["playbooks/x.yml|foo"]})

    def test_an_unknown_key_is_rejected(self) -> None:
        """A typo'd key would otherwise be ignored, so `githug:` would silently
        demote an automated pin to a manual one."""
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(dict(GOOD_ROW, githug="nvm-sh/nvm")))


class TestManualRowsMustBeActionable(unittest.TestCase):
    def test_a_row_with_neither_github_nor_note_is_rejected(self) -> None:
        """It would print `MANUAL:` with nothing after it — a review item a human
        cannot act on, which is the check-shaped version of no check."""
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document({"playbook": "playbooks/x.yml", "var": "v"}))

    def test_an_automated_row_needs_no_note(self) -> None:
        self.assertEqual(manifest.parse(document(GOOD_ROW))[0].note, "")


class TestGithubShape(unittest.TestCase):
    def test_a_github_value_without_a_slash_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(dict(GOOD_ROW, github="nvm")))

    def test_a_github_value_with_two_slashes_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(dict(GOOD_ROW, github="a/b/c")))

    def test_a_github_url_is_rejected_rather_than_silently_queried(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(dict(GOOD_ROW, github="https://github.com/a/b")))


class TestDuplicates(unittest.TestCase):
    def test_the_same_playbook_and_var_twice_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(GOOD_ROW, dict(GOOD_ROW)))

    def test_the_same_var_in_DIFFERENT_playbooks_is_fine(self) -> None:
        """Two playbooks legitimately pin their own `version`."""
        other = dict(GOOD_ROW, playbook="playbooks/imports/play-other.yml")
        self.assertEqual(len(manifest.parse(document(GOOD_ROW, other))), 2)


class TestFieldsCannotCorruptTheRowEncoding(unittest.TestCase):
    def test_a_pipe_in_any_field_is_rejected(self) -> None:
        """Rows reach bash pipe-delimited, so a `|` would shift every later field
        by one and the consumer would read a different manifest than the one
        written — silently."""
        for field in ("playbook", "var", "github", "note"):
            with self.subTest(field=field):
                with self.assertRaises(manifest.ManifestError):
                    manifest.parse(document(dict(MANUAL_ROW, **{field: "a|b"})))

    def test_a_newline_in_any_field_is_rejected(self) -> None:
        """The consumer reads one row per line."""
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(dict(MANUAL_ROW, note="line one\nline two")))


class TestInstalledDeclaration(unittest.TestCase):
    """How to resolve what is INSTALLED — the axis the incident drifted on.

    It cannot be generic, and guessing it is worse than omitting it: the
    `displaylink_version` pin is a release tag (`v6.3.0-1`) while the installed
    rpm's own version tracks evdi (`displaylink-1.14.16-2`), so a plausible-looking
    rpm resolver would have reported a permanent false finding.

    So a pin either declares a resolver or declares, with a reason, that its install
    state is not tracked. There is no third state where nobody decided.
    """

    def test_a_pin_with_no_installed_block_is_rejected(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(GOOD_ROW), require_installed=True)

    def test_it_is_optional_unless_asked_for(self) -> None:
        """The upstream-drift consumer does not need it; only the host one does."""
        self.assertEqual(manifest.parse(document(GOOD_ROW))[0].installed, None)

    def test_a_dkms_resolver_parses(self) -> None:
        row = dict(GOOD_ROW, installed={"kind": "dkms", "name": "evdi"})
        pin = manifest.parse(document(row), require_installed=True)[0]
        self.assertEqual(pin.installed.kind, "dkms")
        self.assertEqual(pin.installed.name, "evdi")

    def test_an_untracked_declaration_needs_a_reason(self) -> None:
        """'untracked' with no why is an omission wearing a decision's clothes."""
        row = dict(GOOD_ROW, installed={"kind": "untracked"})
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(row), require_installed=True)

    def test_an_untracked_declaration_with_a_reason_parses(self) -> None:
        row = dict(GOOD_ROW, installed={"kind": "untracked", "why": "no host-side value exists"})
        pin = manifest.parse(document(row), require_installed=True)[0]
        self.assertEqual(pin.installed.kind, manifest.UNTRACKED)
        self.assertIn("no host-side value", pin.installed.why)

    def test_a_resolver_kind_needs_a_name(self) -> None:
        row = dict(GOOD_ROW, installed={"kind": "dkms"})
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(row), require_installed=True)

    def test_an_unknown_resolver_kind_is_rejected(self) -> None:
        """Not silently treated as untracked — that would turn a typo into a
        decision nobody made."""
        row = dict(GOOD_ROW, installed={"kind": "magic", "name": "evdi"})
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(row), require_installed=True)

    def test_an_unknown_key_inside_installed_is_rejected(self) -> None:
        row = dict(GOOD_ROW, installed={"kind": "dkms", "naem": "evdi"})
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(row), require_installed=True)

    def test_installed_must_be_a_mapping(self) -> None:
        with self.assertRaises(manifest.ManifestError):
            manifest.parse(document(dict(GOOD_ROW, installed="dkms:evdi")), require_installed=True)

    def test_untracked_is_not_a_resolver(self) -> None:
        row = dict(GOOD_ROW, installed={"kind": "untracked", "why": "x"})
        self.assertFalse(manifest.parse(document(row), require_installed=True)[0].is_tracked)

    def test_a_real_resolver_is_tracked(self) -> None:
        row = dict(GOOD_ROW, installed={"kind": "dkms", "name": "evdi"})
        self.assertTrue(manifest.parse(document(row), require_installed=True)[0].is_tracked)

    def test_a_pin_with_no_installed_block_is_not_tracked(self) -> None:
        self.assertFalse(manifest.parse(document(GOOD_ROW))[0].is_tracked)


class TestRowEncoding(unittest.TestCase):
    def test_a_row_renders_the_five_fields_the_consumer_reads(self) -> None:
        self.assertEqual(
            manifest.to_row(manifest.parse(document(GOOD_ROW))[0]),
            "playbooks/imports/play-nvm-install.yml|nvm_version|nvm-sh/nvm||",
        )

    def test_a_manual_row_renders_an_empty_github_and_its_note(self) -> None:
        row = manifest.to_row(manifest.parse(document(MANUAL_ROW))[0])
        self.assertEqual(row.split("|")[2], "")
        self.assertEqual(row.split("|")[4], "check the vendor site by hand")

    def test_every_row_has_exactly_five_fields(self) -> None:
        for pin in manifest.parse(document(GOOD_ROW, MANUAL_ROW)):
            self.assertEqual(len(manifest.to_row(pin).split("|")), 5)


DKMS_ROW = dict(GOOD_ROW, installed={"kind": "dkms", "name": "evdi"})
UNTRACKED_ROW = dict(MANUAL_ROW, installed={"kind": "untracked", "why": "not established"})


class TestMain(unittest.TestCase):
    def test_a_valid_manifest_prints_the_OK_marker_and_the_rows(self) -> None:
        import io
        import json

        stdout = io.StringIO()
        status = main_with(json.dumps(document(DKMS_ROW, UNTRACKED_ROW)), stdout)
        self.assertEqual(status, 0)
        lines = stdout.getvalue().splitlines()
        self.assertTrue(lines[0].startswith(manifest.OK_MARKER))
        self.assertEqual(len(lines), 3)

    def test_main_requires_the_installed_declaration(self) -> None:
        """This is the only place every row is read on every qa-all run, so it is
        where a pin nobody has decided about has to be caught."""
        import io
        import json

        stdout = io.StringIO()
        self.assertNotEqual(main_with(json.dumps(document(GOOD_ROW)), stdout), 0)
        self.assertIn(manifest.INVALID_MARKER, stdout.getvalue())

    def test_the_marker_line_counts_tracked_and_untracked_separately(self) -> None:
        """The gap is printed rather than hidden: an untracked pin is not a pin
        with no findings, and the two must not read alike."""
        import io
        import json

        stdout = io.StringIO()
        main_with(json.dumps(document(DKMS_ROW, UNTRACKED_ROW)), stdout)
        first = stdout.getvalue().splitlines()[0]
        self.assertIn("1 with install state tracked", first)
        self.assertIn("1 declared untracked", first)

    def test_an_invalid_manifest_prints_the_INVALID_marker_and_fails(self) -> None:
        import io
        import json

        stdout = io.StringIO()
        status = main_with(json.dumps({"version_pins": []}), stdout)
        self.assertNotEqual(status, 0)
        self.assertIn(manifest.INVALID_MARKER, stdout.getvalue())

    def test_the_two_markers_are_distinct(self) -> None:
        """The gate greps for one; if either were a prefix of the other it could
        pass on a rejection."""
        self.assertNotIn(manifest.INVALID_MARKER, manifest.OK_MARKER)
        self.assertNotIn(manifest.OK_MARKER, manifest.INVALID_MARKER)

    def test_undecodable_input_is_INVALID_not_a_traceback(self) -> None:
        import io

        stdout = io.StringIO()
        status = main_with("not json at all", stdout)
        self.assertNotEqual(status, 0)
        self.assertIn(manifest.INVALID_MARKER, stdout.getvalue())


def main_with(text: str, stdout) -> int:
    import io

    return manifest.main(stdin=io.StringIO(text), stdout=stdout)


if __name__ == "__main__":
    unittest.main()
