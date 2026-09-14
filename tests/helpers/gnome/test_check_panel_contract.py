"""Tests for helpers.gnome.check_panel_contract — Plan 00109, Tasks 4.1 and 4.5.

The status document is written by Python and read by JavaScript, so its contract is
spelled out twice. Nothing at runtime notices when the two copies disagree: a panel
looking for the wrong file name, or refusing an unexpected schema number, reports
`unavailable` for ever — and `unavailable` is indistinguishable from a producer that has
never run, which is the one state this whole plan says must not be guessed at.

So the agreement is a gate. What is pinned here:

1. **It fails when they disagree**, naming which constant and both values. A
   cross-language gate that only ever passes is the defect class this plan is about.
2. **It fails when the constant is missing**, rather than treating absent as equal.
3. **It reads the JavaScript as text**, because there is no JS runtime in the QA path —
   and it therefore has to fail loudly when the text stops matching its expectations,
   not quietly find nothing to compare.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.gnome import check_panel_contract

JS = """
export const OK = 'ok';
export const UNAVAILABLE = 'unavailable';
export const SCHEMA_VERSION = 1;
const FILE_NAME = 'host-status.json';
"""


class TestAgreement(unittest.TestCase):
    def test_matching_constants_produce_no_findings(self) -> None:
        self.assertEqual(
            check_panel_contract.mismatches(
                JS, {"OK": "ok", "SCHEMA_VERSION": "1", "FILE_NAME": "host-status.json"}
            ),
            [],
        )

    def test_a_differing_value_is_a_finding_naming_both_sides(self) -> None:
        findings = check_panel_contract.mismatches(JS, {"FILE_NAME": "status.json"})
        self.assertEqual(len(findings), 1)
        self.assertIn("FILE_NAME", findings[0])
        self.assertIn("host-status.json", findings[0])
        self.assertIn("status.json", findings[0])

    def test_a_differing_schema_number_is_a_finding(self) -> None:
        """The number is the one that will actually drift, because bumping the Python
        constant is the documented way to change the shape."""
        findings = check_panel_contract.mismatches(JS, {"SCHEMA_VERSION": "2"})
        self.assertEqual(len(findings), 1)
        self.assertIn("SCHEMA_VERSION", findings[0])

    def test_every_disagreement_is_reported_not_just_the_first(self) -> None:
        findings = check_panel_contract.mismatches(
            JS, {"OK": "fine", "FILE_NAME": "elsewhere.json"}
        )
        self.assertEqual(len(findings), 2)


class TestAMissingConstantIsNotAgreement(unittest.TestCase):
    """The failure mode of a text-matching gate: the pattern stops matching, nothing is
    compared, and silence reads as a pass."""

    def test_a_constant_absent_from_the_javascript_is_a_finding(self) -> None:
        findings = check_panel_contract.mismatches(JS, {"SELF_SECTION": "status"})
        self.assertEqual(len(findings), 1)
        self.assertIn("SELF_SECTION", findings[0])

    def test_the_finding_says_it_could_not_be_found_rather_than_a_wrong_value(
        self,
    ) -> None:
        findings = check_panel_contract.mismatches(JS, {"SELF_SECTION": "status"})
        self.assertIn("not declared", findings[0])

    def test_an_empty_javascript_file_fails_every_constant(self) -> None:
        findings = check_panel_contract.mismatches("", {"OK": "ok", "FILE_NAME": "x"})
        self.assertEqual(len(findings), 2)


class TestTheRealPair(unittest.TestCase):
    """Against the actual files, so the gate cannot pass on a fixture while the shipped
    pair disagrees."""

    def test_the_shipped_javascript_agrees_with_the_shipped_python(self) -> None:
        root = os.path.join(os.path.dirname(__file__), "..", "..", "..")
        self.assertEqual(check_panel_contract.check(root), [])

    def test_the_expected_set_is_not_empty(self) -> None:
        """Zero constants compared would make `check` a gate that cannot fail — this
        plan's cardinal defect, in the gate written to prevent one."""
        self.assertGreater(len(check_panel_contract.expected()), 0)

    def test_it_covers_the_file_name_and_the_schema_and_all_three_states(self) -> None:
        names = set(check_panel_contract.expected())
        self.assertLessEqual(
            {"FILE_NAME", "SCHEMA_VERSION", "SELF_SECTION", "OK", "FINDINGS",
             "UNAVAILABLE", "STATE_DIR_NAME"},
            names,
        )


class TestTheKeysAreDerivedNotListed(unittest.TestCase):
    """The set of names compared is derived from the producer, never hand-listed.

    A hand-written list covers whatever its author thought of and keeps passing while
    the document grows a key the panel never learned to read. That matters most for the
    section ids, which `sections/health.js` itself calls interface: *"rename one here
    and the section silently reports unavailable for ever."* Deriving the set means a
    key the gate would have forgotten cannot exist.
    """

    def test_the_document_keys_come_from_a_real_built_document(self) -> None:
        keys = check_panel_contract.document_keys()
        self.assertLessEqual(
            {"schema", "generated_at", "kernel", "sections",
             "state", "findings", "unchecked"},
            keys,
        )

    def test_the_section_ids_come_from_the_real_seam(self) -> None:
        self.assertEqual(
            sorted(check_panel_contract.section_ids()),
            sorted(["post-boot-health", "play-freshness", "installed-vs-pinned"]),
        )

    def test_a_name_the_javascript_never_mentions_is_a_finding(self) -> None:
        self.assertEqual(
            check_panel_contract.unmentioned("const state = 1;", {"state"}), []
        )
        self.assertEqual(
            check_panel_contract.unmentioned("const state = 1;", {"not_checked"}),
            ["not_checked"],
        )

    def test_a_renamed_document_key_is_caught_against_the_shipped_javascript(self) -> None:
        """Renaming `unchecked` on the Python side leaves `health.js` reading a key the
        document does not carry: it sees `length === 0`, skips the caveat block, and
        renders a section in the `unavailable` state with no findings and no
        explanation — the "neutral icon over an empty menu" that file exists to
        prevent. This is the mutation that must produce a finding."""
        javascript = self._shipped_javascript()
        self.assertEqual(check_panel_contract.unmentioned(javascript, {"unchecked"}), [])
        self.assertEqual(
            check_panel_contract.unmentioned(javascript, {"not_checked_after_rename"}),
            ["not_checked_after_rename"],
        )

    def test_the_shipped_javascript_mentions_every_derived_name(self) -> None:
        root = os.path.join(os.path.dirname(__file__), "..", "..", "..")
        self.assertEqual(check_panel_contract.check(root), [])

    @staticmethod
    def _shipped_javascript() -> str:
        root = os.path.join(os.path.dirname(__file__), "..", "..", "..")
        return check_panel_contract.panel_javascript(root)


if __name__ == "__main__":
    unittest.main()
