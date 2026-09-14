"""Tests for helpers.version_pins.compare — repo pin vs what is installed on this host.

Plan 00109 Task 2.2, the drift axis nothing watched: a DKMS module was pinned a
minor version ahead of what the host actually had, every QA gate stayed green
because none of them compares those two things, and the monitors it drives went
dark after a reboot.

The gate the plan demands is the last class here: this must report a finding for
that exact pair. A check that cannot fail against the failure it was built for is
not a check.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.version_pins import compare


class TestParseVersion(unittest.TestCase):
    """The return is a CANONICAL form, not the literal components: trailing zeros
    are dropped, so `1.15.0` and `1.15` parse identically. That is what makes them
    compare as the same version — see TestOrdering — and asserting the raw tuple
    instead would pin an implementation detail that contradicts the contract."""

    def test_plain_dotted_version(self) -> None:
        self.assertEqual(compare.parse_version("1.15.1"), (1, 15, 1))

    def test_a_trailing_zero_is_dropped_from_the_canonical_form(self) -> None:
        self.assertEqual(compare.parse_version("1.15.0"), (1, 15))

    def test_a_leading_v_is_stripped(self) -> None:
        """Upstream tags are `v1.2.3`; an rpm query gives the bare form. Same version."""
        self.assertEqual(compare.parse_version("v1.15.1"), (1, 15, 1))

    def test_an_rpm_release_suffix_is_dropped(self) -> None:
        self.assertEqual(compare.parse_version("1.14.16-1.fc44"), (1, 14, 16))

    def test_two_and_four_component_versions_both_parse(self) -> None:
        self.assertEqual(compare.parse_version("6.2"), (6, 2))
        self.assertEqual(compare.parse_version("1.2.3.4"), (1, 2, 3, 4))

    def test_surrounding_whitespace_is_ignored(self) -> None:
        self.assertEqual(compare.parse_version("  1.15.1\n"), (1, 15, 1))

    def test_an_unparseable_version_raises(self) -> None:
        """Returning a sentinel would let an unreadable version compare as equal,
        which is a silent pass on the one axis this exists to watch."""
        with self.assertRaises(ValueError):
            compare.parse_version("not-a-version")

    def test_an_empty_string_raises(self) -> None:
        with self.assertRaises(ValueError):
            compare.parse_version("")


class TestOrdering(unittest.TestCase):
    def test_numeric_not_lexical_across_a_minor_bump(self) -> None:
        self.assertLess(compare.parse_version("1.14.16"), compare.parse_version("1.15.0"))

    def test_numeric_not_lexical_within_a_component(self) -> None:
        """The classic defect: as strings, "1.14.16" sorts BEFORE "1.14.9"."""
        self.assertLess(compare.parse_version("1.14.9"), compare.parse_version("1.14.16"))
        self.assertGreater(compare.parse_version("1.14.16"), compare.parse_version("1.14.9"))

    def test_shorter_version_compares_as_the_zero_padded_form(self) -> None:
        self.assertEqual(compare.parse_version("1.15"), compare.parse_version("1.15.0"))

    def test_a_trailing_zero_component_does_not_change_order(self) -> None:
        self.assertLess(compare.parse_version("1.15.0"), compare.parse_version("1.15.1"))


class TestClassify(unittest.TestCase):
    def test_equal_versions_match(self) -> None:
        self.assertEqual(compare.classify(pinned="1.15.0", installed="1.15.0").state, compare.MATCH)

    def test_a_v_prefixed_pin_matches_a_bare_installed_version(self) -> None:
        self.assertEqual(compare.classify(pinned="v1.15.0", installed="1.15.0").state, compare.MATCH)

    def test_installed_older_than_pinned_is_BEHIND(self) -> None:
        self.assertEqual(compare.classify(pinned="1.15.0", installed="1.14.16").state, compare.BEHIND)

    def test_installed_newer_than_pinned_is_AHEAD(self) -> None:
        """Not a pass. The host has something the repo does not describe, so the repo
        can no longer reproduce this machine — the same class of surprise."""
        self.assertEqual(compare.classify(pinned="1.15.0", installed="1.16.0").state, compare.AHEAD)

    def test_nothing_installed_is_ABSENT(self) -> None:
        self.assertEqual(compare.classify(pinned="1.15.0", installed=None).state, compare.ABSENT)

    def test_an_unparseable_installed_version_is_UNDETERMINED_not_a_pass(self) -> None:
        """`fail loudly on a pin whose install state cannot be determined rather than
        reporting a pass` — PLAN.md Task 2.2, verbatim."""
        verdict = compare.classify(pinned="1.15.0", installed="wibble")
        self.assertEqual(verdict.state, compare.UNDETERMINED)

    def test_an_unparseable_PIN_is_UNDETERMINED_too(self) -> None:
        verdict = compare.classify(pinned="latest", installed="1.15.0")
        self.assertEqual(verdict.state, compare.UNDETERMINED)

    def test_undetermined_carries_the_reason(self) -> None:
        verdict = compare.classify(pinned="1.15.0", installed="wibble")
        self.assertIn("wibble", verdict.detail)

    def test_the_verdict_carries_both_versions_for_the_report(self) -> None:
        verdict = compare.classify(pinned="1.15.0", installed="1.14.16")
        self.assertIn("1.14.16", verdict.detail)
        self.assertIn("1.15.0", verdict.detail)


class TestWhatCountsAsAFinding(unittest.TestCase):
    def test_only_MATCH_is_clean(self) -> None:
        self.assertTrue(compare.classify(pinned="1.15.0", installed="1.15.0").is_clean)

    def test_behind_ahead_absent_and_undetermined_are_all_findings(self) -> None:
        for installed in ("1.14.16", "1.16.0", None, "wibble"):
            with self.subTest(installed=installed):
                self.assertFalse(compare.classify(pinned="1.15.0", installed=installed).is_clean)


class TestTheFailureItWasBuiltFor(unittest.TestCase):
    """PLAN.md Task 2.2's gate, stated there as a requirement on this code:

    'must report FAIL against the ... state (evdi 1.14.16 installed, 1.15.0
    pinned). A check that cannot fail against the incident it was built for is
    not a check.'
    """

    def test_a_host_a_minor_version_behind_its_pin_is_a_finding(self) -> None:
        verdict = compare.classify(pinned="1.15.0", installed="1.14.16")
        self.assertFalse(verdict.is_clean)
        self.assertEqual(verdict.state, compare.BEHIND)

    def test_and_the_state_after_the_fix_is_clean(self) -> None:
        """The other half: a check that fires on the fixed state too is just noise."""
        self.assertTrue(compare.classify(pinned="1.15.0", installed="1.15.0").is_clean)

    def test_the_dkms_form_of_the_installed_version_is_also_caught(self) -> None:
        """`dkms status` reports `evdi/1.14.16`; the resolver may hand that through."""
        verdict = compare.classify(pinned="1.15.0", installed="evdi/1.14.16")
        self.assertEqual(verdict.state, compare.BEHIND)


class TestReport(unittest.TestCase):
    def test_a_clean_report_names_nothing(self) -> None:
        findings = compare.findings([
            compare.Pin("evdi", "1.15.0", "1.15.0"),
            compare.Pin("nvm", "0.40.1", "0.40.1"),
        ])
        self.assertEqual(findings, [])

    def test_only_the_drifting_pin_is_named(self) -> None:
        findings = compare.findings([
            compare.Pin("evdi", "1.15.0", "1.14.16"),
            compare.Pin("nvm", "0.40.1", "0.40.1"),
        ])
        self.assertEqual([f.name for f in findings], ["evdi"])

    def test_an_undeterminable_pin_is_reported_not_skipped(self) -> None:
        findings = compare.findings([compare.Pin("cudnn", "9.1.0", None)])
        self.assertEqual([f.name for f in findings], ["cudnn"])

    def test_findings_are_sorted_so_two_runs_diff_cleanly(self) -> None:
        findings = compare.findings([
            compare.Pin("zlib", "1.0", "0.9"),
            compare.Pin("alpha", "1.0", "0.9"),
        ])
        self.assertEqual([f.name for f in findings], ["alpha", "zlib"])


if __name__ == "__main__":
    unittest.main()
