"""Tests for helpers.play_ledger.ledger_presence — Plan 00109, Task 4.2.

An empty state directory makes `check_freshness` publish `play-freshness: ok`. That is
the right answer to the question that check asks — no play has drifted, because no play
is known — and a green tick on a host whose ledger is gone. The two are the same picture
and mean opposite things, which is this plan's incident in miniature.

So the emptiness is a **separate** question with its own section, never a
reinterpretation of freshness. What is pinned here:

1. **A ledger with records says nothing.** Silence is only earned by a real answer.
2. **An empty or absent ledger is a FAULT on this host, not an unknown.** `run.bash`
   ledgers every play and a play is what deploys the unit that runs this check, so by
   the time anything reads it there must be at least one record. Nothing is the one
   answer that cannot be honestly reached.
3. **The BROKEN sentinel silences it**, because that says the same absence with more
   detail, and two voices on one fact read as two problems.
"""

from __future__ import annotations

import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import ledger, ledger_presence

RECORD = (
    '{"schema": 1, "play": "playbooks/imports/play-basic-configs.yml", '
    '"name": "Basic Configs", "commit": "abc1234", "dirty": false, '
    '"play_sha256": "f" , "outcome": "ok", "changed": 0, '
    '"started": "2026-09-14T10:00:00Z", "finished": "2026-09-14T10:00:05Z"}'
)


def _ledger(base: str, *lines: str) -> None:
    with open(ledger.runs_path(base), "w", encoding="utf-8") as handle:
        for line in lines:
            handle.write(f"{line}\n")


class TestALedgerWithRecordsIsSilent(unittest.TestCase):
    def test_one_record_is_enough(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _ledger(base, RECORD)
            self.assertEqual(ledger_presence.findings(base), [])

    def test_several_records_are_silent_too(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _ledger(base, RECORD, RECORD, RECORD)
            self.assertEqual(ledger_presence.findings(base), [])

    def test_blank_lines_around_a_real_record_do_not_hide_it(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _ledger(base, "", RECORD, "  ", "")
            self.assertEqual(ledger_presence.findings(base), [])


class TestAnEmptyLedgerIsAFault(unittest.TestCase):
    """Not `unchecked`. Nobody failed to look — this looked, and found that nothing has
    ever been recorded on a host that must have run at least one play to be running this
    check at all."""

    def test_a_missing_directory_is_reported(self) -> None:
        with tempfile.TemporaryDirectory() as parent:
            findings = ledger_presence.findings(os.path.join(parent, "absent"))
            self.assertEqual(len(findings), 1)
            self.assertTrue(findings[0].checked)

    def test_a_missing_runs_file_is_reported(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            findings = ledger_presence.findings(base)
            self.assertEqual(len(findings), 1)
            self.assertTrue(findings[0].checked)

    def test_a_file_of_only_blank_lines_is_reported(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _ledger(base, "", "   ", "")
            self.assertEqual(len(findings := ledger_presence.findings(base)), 1)
            self.assertTrue(findings[0].checked)

    def test_it_says_what_is_wrong_rather_than_naming_a_path(self) -> None:
        """A path tells the user where to look. This has to tell them what it means —
        that nothing on this host has been recorded, so every drift check downstream is
        answering from an empty set."""
        with tempfile.TemporaryDirectory() as base:
            text = ledger_presence.findings(base)[0].text
            self.assertIn("no play run has ever been recorded", text)


class TestTheBrokenSentinelSilencesIt(unittest.TestCase):
    def test_a_sentinel_beside_an_empty_ledger_is_left_to_freshness(self) -> None:
        """`check_freshness` already refuses to answer while the sentinel exists and says
        why. Reporting the emptiness as well would describe one absence twice."""
        with tempfile.TemporaryDirectory() as base:
            with open(ledger.sentinel_path(base), "w", encoding="utf-8") as handle:
                handle.write("a write failed\n")
            self.assertEqual(ledger_presence.findings(base), [])


class TestItNeverRaises(unittest.TestCase):
    """This runs inside the login report, where an exception costs the user every other
    check in the same run."""

    def test_an_unreadable_ledger_is_a_finding_not_an_exception(self) -> None:
        """A directory where the records file should be. Chosen over `chmod 0o000`
        because the QA container runs as root, which reads a mode-zero file happily —
        the permission version of this test passes without reaching the branch, which
        is the failure it was written to catch, one level up."""
        with tempfile.TemporaryDirectory() as base:
            os.mkdir(ledger.runs_path(base))
            findings = ledger_presence.findings(base)
            self.assertEqual(len(findings), 1)
            self.assertFalse(
                findings[0].checked,
                "a ledger that could not be read is unknown, not proven empty",
            )


if __name__ == "__main__":
    unittest.main()
