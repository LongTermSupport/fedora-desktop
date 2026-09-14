"""Tests for helpers.play_ledger.fetch_clock — Plan 00109, DESIGN-host-health.md §8.

Settles what an offline login says. The two rules this plan keeps invoking conflict
head-on here: *"I could not look" must never render as "nothing is wrong"* says
report a failed fetch, and *a check that speaks on every login gets muted* says do
not, because offline at login is ordinary.

The resolution is that the question was posed wrongly. "Can I reach the remote right
now" is a fact about the network. **"How long is it since I last could"** is a fact
about this host, and it is the one worth reporting — a freshness answer from refs
fetched an hour ago is worth having, the same answer from refs three weeks old is not.

So what is pinned here:

1. Within the bound, an offline run is **silent** and judges on the refs it has.
2. Beyond it, a **finding** naming how long it has been.
3. Never fetched at all is its own finding — a different problem from one that
   fetched last month, and one message for both would describe neither.
"""

from __future__ import annotations

import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import fetch_clock

NOW = "2026-09-14T12:00:00Z"


class TestRecordAndRead(unittest.TestCase):
    def setUp(self) -> None:
        self.dir = tempfile.mkdtemp()

    def test_nothing_recorded_reads_as_None(self) -> None:
        self.assertIsNone(fetch_clock.last_success(self.dir))

    def test_a_recorded_success_reads_back(self) -> None:
        fetch_clock.record_success(self.dir, at=NOW)
        self.assertEqual(fetch_clock.last_success(self.dir), NOW)

    def test_the_latest_recorded_success_wins(self) -> None:
        fetch_clock.record_success(self.dir, at="2026-09-01T00:00:00Z")
        fetch_clock.record_success(self.dir, at=NOW)
        self.assertEqual(fetch_clock.last_success(self.dir), NOW)

    def test_an_unreadable_stamp_reads_as_None_rather_than_raising(self) -> None:
        """A corrupt stamp must degrade to 'never fetched', which is a finding —
        not to 'fetched just now', which would be a silent pass."""
        with open(os.path.join(self.dir, fetch_clock.STAMP_NAME), "w", encoding="utf-8") as handle:
            handle.write("not a timestamp")
        self.assertIsNone(fetch_clock.last_success(self.dir))

    def test_recording_does_not_need_the_directory_to_pre_exist(self) -> None:
        nested = os.path.join(self.dir, "deeper")
        fetch_clock.record_success(nested, at=NOW)
        self.assertEqual(fetch_clock.last_success(nested), NOW)


class TestOfflineVerdict(unittest.TestCase):
    def test_a_recent_fetch_is_silent(self) -> None:
        """An ordinary offline login — a train, a hotel — emits nothing."""
        self.assertIsNone(
            fetch_clock.offline_finding(last=fetch_clock.shift(NOW, days=-1), now=NOW))

    def test_the_boundary_itself_is_still_silent(self) -> None:
        """An off-by-one here turns an ordinary day into a nag."""
        at_bound = fetch_clock.shift(NOW, days=-fetch_clock.STALE_AFTER_DAYS)
        self.assertIsNone(fetch_clock.offline_finding(last=at_bound, now=NOW))

    def test_the_FIRST_day_past_the_bound_is_a_finding(self) -> None:
        """The bound plus one, not the bound plus three. With a comfortable margin a
        bound widened by a day still satisfies both this and the silence test above,
        so the pair would agree that a boundary was pinned while leaving it free."""
        first_past = fetch_clock.shift(NOW, days=-(fetch_clock.STALE_AFTER_DAYS + 1))
        finding = fetch_clock.offline_finding(last=first_past, now=NOW)
        self.assertIsNotNone(finding)
        self.assertIn(str(fetch_clock.STALE_AFTER_DAYS + 1), finding)

    def test_beyond_the_bound_is_a_finding_naming_the_gap(self) -> None:
        past = fetch_clock.shift(NOW, days=-(fetch_clock.STALE_AFTER_DAYS + 3))
        finding = fetch_clock.offline_finding(last=past, now=NOW)
        self.assertIsNotNone(finding)
        self.assertIn(str(fetch_clock.STALE_AFTER_DAYS + 3), finding)

    def test_never_fetched_is_its_OWN_finding(self) -> None:
        """A host that has never fetched has a different problem from one that
        fetched last month; one message for both would describe neither."""
        finding = fetch_clock.offline_finding(last=None, now=NOW)
        self.assertIsNotNone(finding)
        self.assertIn("never", finding.lower())

    def test_the_two_findings_do_not_read_alike(self) -> None:
        past = fetch_clock.shift(NOW, days=-90)
        self.assertNotEqual(
            fetch_clock.offline_finding(last=None, now=NOW),
            fetch_clock.offline_finding(last=past, now=NOW),
        )

    def test_an_unparseable_stamp_is_a_finding_not_silence(self) -> None:
        """An input guard on a public function, and NOT the route a corrupt stamp on
        disk takes: `last_success` filters an unreadable one to `None`, so via `run()`
        a corrupt stamp reports "never fetched" instead. Both are findings, so neither
        is a silent pass — this pins the guard for a caller that passes the raw text."""
        finding = fetch_clock.offline_finding(last="whenever", now=NOW)
        self.assertIsNotNone(finding)

    def test_a_stamp_in_the_FUTURE_is_a_finding(self) -> None:
        """A clock that ran backwards would otherwise buy unlimited silence."""
        future = fetch_clock.shift(NOW, days=+2)
        self.assertIsNotNone(fetch_clock.offline_finding(last=future, now=NOW))

    def test_the_bound_is_a_declared_constant(self) -> None:
        self.assertIsInstance(fetch_clock.STALE_AFTER_DAYS, int)
        self.assertGreater(fetch_clock.STALE_AFTER_DAYS, 0)


class TestShift(unittest.TestCase):
    """The test helper itself, because a broken one would make every case above
    vacuous in the same direction."""

    def test_shifting_back_gives_an_earlier_timestamp(self) -> None:
        self.assertLess(fetch_clock.shift(NOW, days=-1), NOW)

    def test_shifting_forward_gives_a_later_one(self) -> None:
        self.assertGreater(fetch_clock.shift(NOW, days=1), NOW)

    def test_a_zero_shift_is_the_same_instant(self) -> None:
        self.assertEqual(fetch_clock.shift(NOW, days=0), NOW)


if __name__ == "__main__":
    unittest.main()
