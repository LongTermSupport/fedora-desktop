"""Tests for helpers.play_ledger.freshness — the play-freshness verdicts (Plan 00109, Task 2.1).

Pure logic only: git is queried by the executor, and every fact it would return is
an argument here. The rules being pinned are the ones a wrong answer makes
dangerous — silence about a play never run here, and a refusal to answer at all
while the ledger is known to have a hole.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import freshness, ledger

FORTY_HEX = "e" * 40
SIXTY_FOUR_HEX = "1" * 64
OTHER_SHA = "2" * 64


def _record(play: str, *, commit: str = FORTY_HEX, dirty: bool = False,
            play_sha256: str = SIXTY_FOUR_HEX, finished: str = "2026-09-14T09:00:00Z") -> dict:
    return ledger.build_record(
        play=play, name=play.rsplit("/", 1)[-1], commit=commit, dirty=dirty,
        play_sha256=play_sha256, outcome="ok", changed=0,
        started=finished, finished=finished,
    )


class TestFresh(unittest.TestCase):
    def test_unchanged_since_the_ledgered_run_is_fresh(self) -> None:
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-x.yml"),
            changes=[], head_sha256=SIXTY_FOUR_HEX,
        )
        self.assertEqual(verdict.state, freshness.FRESH)

    def test_fresh_carries_no_changes(self) -> None:
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-x.yml"),
            changes=[], head_sha256=SIXTY_FOUR_HEX,
        )
        self.assertEqual(verdict.changes, ())


class TestStale(unittest.TestCase):
    def test_a_commit_touching_the_play_since_the_run_is_stale(self) -> None:
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-x.yml"),
            changes=[("abc1234", "Plan 00109: rework the play")],
            head_sha256=OTHER_SHA,
        )
        self.assertEqual(verdict.state, freshness.STALE)

    def test_stale_reports_WHAT_changed_not_merely_that_it_did(self) -> None:
        """'Something changed' sends a reader to git; the subjects are the point."""
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-x.yml"),
            changes=[("abc1234", "first"), ("def5678", "second")],
            head_sha256=OTHER_SHA,
        )
        self.assertEqual(
            verdict.changes, (("abc1234", "first"), ("def5678", "second"))
        )

    def test_commits_touching_the_play_win_even_when_the_hash_matches(self) -> None:
        """A revert leaves the bytes identical but the play HAS churned since the run;
        git history is the authority (DESIGN §1), the hash is only the dirty-tree guard."""
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-x.yml"),
            changes=[("abc1234", "revert")],
            head_sha256=SIXTY_FOUR_HEX,
        )
        self.assertEqual(verdict.state, freshness.STALE)


class TestDirtyRunIsNotTrusted(unittest.TestCase):
    def test_a_dirty_run_whose_hash_no_longer_matches_is_stale(self) -> None:
        """The commit is a lie for this play: it was edited and run uncommitted."""
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-x.yml", dirty=True, play_sha256=OTHER_SHA),
            changes=[], head_sha256=SIXTY_FOUR_HEX,
        )
        self.assertEqual(verdict.state, freshness.STALE)

    def test_a_dirty_run_whose_hash_still_matches_is_fresh(self) -> None:
        """The tree was dirty but THIS play was not among the edited files."""
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-x.yml", dirty=True),
            changes=[], head_sha256=SIXTY_FOUR_HEX,
        )
        self.assertEqual(verdict.state, freshness.FRESH)

    def test_a_clean_run_with_a_mismatched_hash_and_no_commits_is_unexplained(self) -> None:
        """No commit touched it and the bytes differ anyway — the ledger and the repo
        disagree about a play, and guessing which is right is how a check starts lying."""
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-x.yml", play_sha256=OTHER_SHA),
            changes=[], head_sha256=SIXTY_FOUR_HEX,
        )
        self.assertEqual(verdict.state, freshness.UNEXPLAINED)


class TestGone(unittest.TestCase):
    def test_a_ledgered_play_absent_from_HEAD_is_gone(self) -> None:
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-removed.yml"),
            changes=[("abc1234", "delete the play")], head_sha256=None,
        )
        self.assertEqual(verdict.state, freshness.GONE)

    def test_gone_wins_over_stale(self) -> None:
        """A deleted play has not merely changed; telling the user to re-run it is wrong."""
        verdict = freshness.classify(
            record=_record("playbooks/imports/play-removed.yml"),
            changes=[("a", "x"), ("b", "y")], head_sha256=None,
        )
        self.assertEqual(verdict.state, freshness.GONE)


class TestReport(unittest.TestCase):
    def test_never_mentions_a_play_the_ledger_has_not_seen(self) -> None:
        """The 43 optional plays nobody has run must be silent, or the report is noise
        on day one and gets ignored for ever after."""
        report = freshness.build_report(
            verdicts=[],
            broken_reason=None,
        )
        self.assertEqual(report.stale, ())
        self.assertTrue(report.clean)

    def test_only_stale_and_worse_reach_the_report(self) -> None:
        fresh = freshness.Verdict("playbooks/a.yml", freshness.FRESH, ())
        stale = freshness.Verdict("playbooks/b.yml", freshness.STALE, (("c", "s"),))
        report = freshness.build_report(verdicts=[fresh, stale], broken_reason=None)
        self.assertEqual([entry.play for entry in report.stale], ["playbooks/b.yml"])

    def test_gone_and_unexplained_are_reported_too(self) -> None:
        verdicts = [
            freshness.Verdict("playbooks/a.yml", freshness.GONE, ()),
            freshness.Verdict("playbooks/b.yml", freshness.UNEXPLAINED, ()),
        ]
        report = freshness.build_report(verdicts=verdicts, broken_reason=None)
        self.assertEqual(len(report.stale), 2)

    def test_a_report_with_findings_is_not_clean(self) -> None:
        stale = freshness.Verdict("playbooks/b.yml", freshness.STALE, ())
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
        stale = freshness.Verdict("playbooks/b.yml", freshness.STALE, ())
        report = freshness.build_report(verdicts=[stale], broken_reason="held")
        self.assertEqual(report.stale, ())


class TestPlaysToQuery(unittest.TestCase):
    def test_returns_the_ledgered_plays_only(self) -> None:
        latest = {
            "playbooks/a.yml": _record("playbooks/a.yml"),
            "playbooks/b.yml": _record("playbooks/b.yml"),
        }
        self.assertEqual(freshness.plays_to_query(latest), ["playbooks/a.yml", "playbooks/b.yml"])

    def test_is_sorted_so_two_runs_diff_cleanly(self) -> None:
        latest = {"playbooks/z.yml": _record("playbooks/z.yml"),
                  "playbooks/a.yml": _record("playbooks/a.yml")}
        self.assertEqual(freshness.plays_to_query(latest), ["playbooks/a.yml", "playbooks/z.yml"])

    def test_an_empty_ledger_asks_git_nothing(self) -> None:
        self.assertEqual(freshness.plays_to_query({}), [])


if __name__ == "__main__":
    unittest.main()
