"""Tests for helpers.host_health.status_document — Plan 00109, Tasks 3.2 and 4.1.

The machine-readable form of the login report, and the single producer behind two
consumers that cannot share a delivery: the GNOME panel on a desktop, and a login-shell
message on a server where `notify-send` has no session bus to reach.

What is pinned here:

1. **Three states, and they are distinct in the data.** `ok`, `findings`, `unavailable`.
   A neutral icon over an empty menu is what a healthy host looks like AND what a
   missing file, an unparseable one and a crashed producer look like. Collapsing those
   is the incident, rebuilt in the UI layer.
2. **`unavailable` is read from `Finding.checked`, never from the wording.** Two
   substrings once covered seven of the messages the checks emit and missed six.
3. **A section that reported nothing still says when it was collected**, because a
   panel presenting login-time findings at teatime states something it did not measure.
4. **Merged, not chained**: a raising producer becomes its own section's `unavailable`,
   never a missing key and never an empty findings list.
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import probe_results, status_document

NOW = "2026-09-14T18:00:00Z"
KERNEL = "7.2.4-200.fc44.x86_64"


class TestTheThreeStates(unittest.TestCase):
    def test_nothing_at_all_is_ok(self) -> None:
        section = status_document.section([])
        self.assertEqual(section["state"], status_document.OK)
        self.assertEqual(section["findings"], [])
        self.assertEqual(section["unchecked"], [])

    def test_a_real_fault_is_findings(self) -> None:
        section = status_document.section([probe_results.broken("evdi: no DKMS module")])
        self.assertEqual(section["state"], status_document.FINDINGS)
        self.assertEqual(section["findings"], ["evdi: no DKMS module"])
        self.assertEqual(section["unchecked"], [])

    def test_only_unchecked_is_UNAVAILABLE_not_ok(self) -> None:
        """The whole point. A check that could not run tells you nothing about the
        thing it was meant to look at, and must not render as a clean result."""
        section = status_document.section([probe_results.unchecked("dkms: not found")])
        self.assertEqual(section["state"], status_document.UNAVAILABLE)
        self.assertEqual(section["findings"], [])
        self.assertEqual(section["unchecked"], ["dkms: not found"])

    def test_both_kinds_reports_findings_AND_keeps_the_unchecked_list(self) -> None:
        """The headline is the fault, because something known-wrong outranks something
        unknown — but the unchecked list must survive, or the panel shows a partial
        picture as a complete one."""
        section = status_document.section([
            probe_results.broken("evdi: no DKMS module"),
            probe_results.unchecked("dkms: not found"),
        ])
        self.assertEqual(section["state"], status_document.FINDINGS)
        self.assertEqual(section["findings"], ["evdi: no DKMS module"])
        self.assertEqual(section["unchecked"], ["dkms: not found"])

    def test_the_three_states_are_distinct_values(self) -> None:
        states = {status_document.OK, status_document.FINDINGS, status_document.UNAVAILABLE}
        self.assertEqual(len(states), 3)


class TestTheDocument(unittest.TestCase):
    def test_it_records_when_and_on_which_kernel(self) -> None:
        """A panel presenting login-time findings hours later states something it did
        not measure. The collection time is what lets a consumer say how old it is."""
        document = status_document.build(sections={}, kernel=KERNEL, at=NOW)
        self.assertEqual(document["generated_at"], NOW)
        self.assertEqual(document["kernel"], KERNEL)

    def test_it_carries_a_schema_version(self) -> None:
        """A consumer that cannot tell which shape it is reading has to guess."""
        document = status_document.build(sections={}, kernel=KERNEL, at=NOW)
        self.assertEqual(document["schema"], status_document.SCHEMA_VERSION)

    def test_sections_are_keyed_by_id(self) -> None:
        document = status_document.build(
            sections={"health": [probe_results.broken("x")]}, kernel=KERNEL, at=NOW)
        self.assertEqual(list(document["sections"]), ["health"])
        self.assertEqual(document["sections"]["health"]["state"], status_document.FINDINGS)

    def test_it_round_trips_through_json(self) -> None:
        """It is read by JavaScript across a file, so it has to be plain JSON — a
        NamedTuple that serialises today and stops when a field is added would be a
        break the tests never see."""
        document = status_document.build(
            sections={"health": [probe_results.unchecked("dkms: not found")]},
            kernel=KERNEL, at=NOW)
        decoded = json.loads(json.dumps(document))
        self.assertEqual(decoded, document)


class TestCollectGuardsEachProducer(unittest.TestCase):
    """Merged, not chained — the same rule `login_report.collect` needed, one layer on.

    A producer that raises must become its own section's `unavailable` carrying the
    reason. Never a missing key, which a consumer would have to invent a meaning for,
    and never an empty findings list, which reads as health.
    """

    def test_a_raising_producer_becomes_its_own_section_unavailable(self) -> None:
        def explode() -> list[probe_results.Finding]:
            raise RuntimeError("the ledger is unreadable")

        sections = status_document.collect({"health": explode})
        self.assertEqual(sections["health"][0].checked, False)
        self.assertIn("unreadable", sections["health"][0].text)

    def test_the_section_is_present_not_missing(self) -> None:
        def explode() -> list[probe_results.Finding]:
            raise RuntimeError("boom")

        document = status_document.build(
            sections=status_document.collect({"health": explode}), kernel=KERNEL, at=NOW)
        self.assertIn("health", document["sections"])
        self.assertEqual(document["sections"]["health"]["state"], status_document.UNAVAILABLE)

    def test_one_raising_producer_does_not_hide_another(self) -> None:
        def explode() -> list[probe_results.Finding]:
            raise RuntimeError("boom")

        sections = status_document.collect({
            "health": explode,
            "plays": lambda: [probe_results.broken("playbooks/a.yml — changed")],
        })
        self.assertEqual(len(sections), 2)
        self.assertEqual(sections["plays"][0].text, "playbooks/a.yml — changed")

    def test_the_failing_section_is_named_so_the_reader_knows_which(self) -> None:
        def explode() -> list[probe_results.Finding]:
            raise RuntimeError("boom")

        sections = status_document.collect({"health": explode})
        self.assertIn("health", sections["health"][0].text)


class TestTheWrite(unittest.TestCase):
    def test_what_is_written_reads_back(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "status.json")
            status_document.write_atomic(path, {"schema": 1})
            with open(path, encoding="utf-8") as handle:
                self.assertEqual(json.load(handle), {"schema": 1})

    def test_a_write_that_fails_partway_leaves_the_last_good_one_intact(self) -> None:
        """The atomicity claim, stated as something that can be observed.

        Writing straight to the destination passes every other test in this class —
        contents match, directory created, second write wins, no stray file. It only
        parts company with the real thing when the write does not finish: the
        destination has already been truncated and half a document sits where a whole
        one was. `json.dump` with an indent serialises incrementally, so an
        unserialisable value at the end emits the opening lines and then raises, which
        is exactly that shape.

        A consumer reading it would not report a parse error either — by rule 3 it
        would report `unavailable`, meaning "nothing is known about this host", about a
        host whose last real report said a DKMS module was missing.
        """
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "status.json")
            good = status_document.build(
                sections={"health": [probe_results.broken("evdi: no DKMS module")]},
                kernel=KERNEL, at=NOW)
            status_document.write_atomic(path, good)

            with self.assertRaises(TypeError):
                status_document.write_atomic(
                    path, {"schema": 1, "padding": "x" * 4096, "bad": object()})

            self.assertEqual(status_document.read(path), good)
            self.assertEqual(sorted(os.listdir(base)), ["status.json"])

    def test_it_creates_its_directory(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "nested", "deeper", "status.json")
            status_document.write_atomic(path, {"schema": 1})
            self.assertTrue(os.path.exists(path))

    def test_a_second_write_replaces_the_first(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "status.json")
            status_document.write_atomic(path, {"schema": 1, "first": True})
            status_document.write_atomic(path, {"schema": 1, "second": True})
            with open(path, encoding="utf-8") as handle:
                self.assertEqual(json.load(handle), {"schema": 1, "second": True})

    def test_no_temporary_file_is_left_behind(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "status.json")
            status_document.write_atomic(path, {"schema": 1})
            self.assertEqual(sorted(os.listdir(base)), ["status.json"])


class TestReadingItBack(unittest.TestCase):
    """The consumer half, and the rule that decides whether the panel is honest.

    An absent or unreadable document is **not** an empty one. `container-watch` falls
    back to an empty findings array and is right to — its subject is live processes, so
    "the scanner has not run" genuinely means nothing is flagged right now. These facts
    are not live: a dead DKMS module for the running kernel stays true, so absence here
    is ignorance and has to read as such.
    """

    def test_a_missing_file_is_unavailable_not_clean(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            document = status_document.read(os.path.join(base, "absent.json"))
        self.assertEqual(document["sections"]["status"]["state"], status_document.UNAVAILABLE)
        self.assertTrue(document["sections"]["status"]["unchecked"])

    def test_an_unparseable_file_is_unavailable_and_says_why(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "status.json")
            with open(path, "w", encoding="utf-8") as handle:
                handle.write("{not json")
            document = status_document.read(path)
        section = document["sections"]["status"]
        self.assertEqual(section["state"], status_document.UNAVAILABLE)
        self.assertTrue(any("could not" in line for line in section["unchecked"]))

    def test_a_good_file_reads_back_as_itself(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "status.json")
            written = status_document.build(
                sections={"health": [probe_results.broken("evdi: no DKMS module")]},
                kernel=KERNEL, at=NOW)
            status_document.write_atomic(path, written)
            self.assertEqual(status_document.read(path), written)

    def test_a_document_from_a_FUTURE_schema_is_unavailable_not_guessed_at(self) -> None:
        """Reading an unknown shape and rendering whatever happens to parse is how a
        consumer reports confidently about a document it did not understand."""
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "status.json")
            status_document.write_atomic(
                path, {"schema": status_document.SCHEMA_VERSION + 1, "sections": {}})
            document = status_document.read(path)
        self.assertEqual(document["sections"]["status"]["state"], status_document.UNAVAILABLE)
        self.assertTrue(any("schema" in line for line in document["sections"]["status"]["unchecked"]))


if __name__ == "__main__":
    unittest.main()
