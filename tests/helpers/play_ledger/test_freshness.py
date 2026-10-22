"""Tests for helpers.play_ledger.freshness — the play-freshness report (Plan 00109, Task 2.1).

Pure logic only: which plays have to run again is decided by `changed_plays.judge` and
tested there. The rules pinned here are the ones a wrong answer makes dangerous — silence
about a play never run here, faults for exactly the plays `run.bash --changed` runs (and
the removed ones), and a refusal to answer at all while the ledger is known to have a hole.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import freshness


class TestReport(unittest.TestCase):
    def test_never_mentions_a_play_the_ledger_has_not_seen(self) -> None:
        """The 43 optional plays nobody has run must be silent, or the report is noise
        on day one and gets ignored for ever after."""
        report = freshness.build_report(verdicts=[], broken_reason=None)
        self.assertEqual(report.stale, ())
        self.assertTrue(report.clean)

    def test_stale_failed_and_gone_are_the_faults(self) -> None:
        verdicts = [
            freshness.Verdict("playbooks/imports/a.yml", freshness.FRESH),
            freshness.Verdict("playbooks/imports/b.yml", freshness.STALE, ("changed: x",)),
            freshness.Verdict("playbooks/imports/c.yml", freshness.FAILED),
            freshness.Verdict("playbooks/imports/d.yml", freshness.UNRESOLVED),
            freshness.Verdict("playbooks/imports/e.yml", freshness.GONE),
        ]
        report = freshness.build_report(verdicts=verdicts, broken_reason=None)
        self.assertEqual([entry.play for entry in report.stale],
                         ["playbooks/imports/b.yml", "playbooks/imports/c.yml", "playbooks/imports/e.yml"])

    def test_an_unresolved_play_alone_is_clean(self) -> None:
        """run.bash --changed does not run it, so it is not a fault the report counts."""
        report = freshness.build_report(
            verdicts=[freshness.Verdict("playbooks/imports/d.yml", freshness.UNRESOLVED)],
            broken_reason=None)
        self.assertTrue(report.clean)

    def test_a_report_with_findings_is_not_clean(self) -> None:
        stale = freshness.Verdict("playbooks/b.yml", freshness.STALE)
        report = freshness.build_report(verdicts=[stale], broken_reason=None)
        self.assertFalse(report.clean)


class TestBrokenLedgerRefusesToAnswer(unittest.TestCase):
    def test_a_sentinel_makes_the_report_not_clean_even_with_no_verdicts(self) -> None:
        """DESIGN §5: while the sentinel exists, nothing at all may be concluded. A
        clean report from a ledger known to have holes is the defect this plan exists
        to catch, reproduced by its own check."""
        report = freshness.build_report(verdicts=[], broken_reason="2026-09-14T09:00:00Z disk full")
        self.assertFalse(report.clean)

    def test_the_reason_is_carried_so_the_user_can_act(self) -> None:
        report = freshness.build_report(verdicts=[], broken_reason="2026-09-14T09:00:00Z disk full")
        self.assertIn("disk full", report.broken_reason or "")

    def test_verdicts_are_withheld_while_broken(self) -> None:
        """Not merely flagged alongside: a per-play answer from an incomplete history
        is a specific false statement, which is worse than the general warning."""
        stale = freshness.Verdict("playbooks/b.yml", freshness.STALE)
        report = freshness.build_report(verdicts=[stale], broken_reason="held")
        self.assertEqual(report.stale, ())


if __name__ == "__main__":
    unittest.main()
