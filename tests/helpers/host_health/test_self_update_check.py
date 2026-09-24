"""Tests for helpers.host_health.self_update_check — the cycle's result in the report (Plan 00137 T4.3).

The unattended cycle runs as root at night and nobody watches it. This check is how a
failed, stopped or unverified cycle reaches the person who logs in next. Pinned here:

1. **A host without self-update says nothing.** No directory, no finding, no noise.
2. **A failed cycle is a fault, naming the phase and the detail.**
3. **Silence is a claim.** A cycle that has stopped recording, or never started, is
   reported once the nightly cadence allows no other explanation.
4. **A reboot whose post-boot check never ran is reported**, after the check's own time.
5. **A record this cannot read is reported as not checked**, never as clean.
"""

from __future__ import annotations

import os
import sys
import tempfile
import time
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import self_update_check
from helpers.self_update import published

NOW = "2026-09-23T09:00:00Z"
BOOT = "boot-now"
LONG_UP = 24 * 3600.0


def record(**overrides: str) -> dict[str, str]:
    base = {
        "at": "2026-09-23T03:30:00Z", "phase": "update", "outcome": "nothing",
        "old": "a" * 40, "new": "a" * 40, "plays": "", "detail": "",
    }
    base.update(overrides)
    return base


class CheckCase(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.directory = os.path.join(self._tmp.name, "self-update-status")
        os.mkdir(self.directory)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def publish(self, owed_boot: str = "", **overrides: str) -> None:
        published.write(self.directory, record(**overrides), owed_boot=owed_boot)

    def findings(self, *, now: str = NOW, boot: str = BOOT, uptime: float = LONG_UP):
        return self_update_check.findings(self.directory, now=now, boot_id=boot, uptime_seconds=uptime)

    def texts(self, **kwargs) -> list[str]:
        return [finding.text for finding in self.findings(**kwargs)]


class TestNotEnabled(CheckCase):
    def test_no_status_directory_means_no_finding(self) -> None:
        os.rmdir(self.directory)
        self.assertEqual(self.findings(), [])


class TestTheLastOutcome(CheckCase):
    def test_a_cycle_with_nothing_to_do_is_clean(self) -> None:
        self.publish()
        self.assertEqual(self.findings(), [])

    def test_a_verified_deployment_is_clean(self) -> None:
        self.publish(phase="verify", outcome="deployed", detail="every restored session is running")
        self.assertEqual(self.findings(), [])

    def test_a_failed_play_is_a_fault_naming_the_phase_and_the_detail(self) -> None:
        self.publish(phase="play", outcome="play-failed", detail="play-x.yml exited 2; no reboot")
        found = self.findings()
        self.assertEqual(len(found), 1)
        self.assertTrue(found[0].checked)
        self.assertIn("play-failed", found[0].text)
        self.assertIn("play-x.yml exited 2", found[0].text)
        self.assertIn("2026-09-23T03:30:00Z", found[0].text)

    def test_every_failed_outcome_is_a_fault(self) -> None:
        for outcome in sorted(published.FAILED_OUTCOMES):
            with self.subTest(outcome=outcome):
                self.publish(phase="warn", outcome=outcome, detail="why")
                found = self.findings()
                self.assertEqual([f.checked for f in found], [True])

    def test_a_reboot_in_progress_in_this_boot_is_not_a_fault(self) -> None:
        self.publish(owed_boot=BOOT, phase="warn", outcome="rebooting", detail="reboot in 3 minute(s)")
        self.assertEqual(self.findings(), [])

    def test_an_outcome_this_reader_does_not_know_is_not_checked(self) -> None:
        self.publish(outcome="exploded")
        found = self.findings()
        self.assertEqual([f.checked for f in found], [False])
        self.assertIn("exploded", found[0].text)


class TestTheAlertChannel(CheckCase):
    def test_an_alert_that_could_not_be_delivered_is_a_fault(self) -> None:
        self.publish(phase="play", outcome="play-failed", detail="x", alert="slack: HTTP 500")
        found = self.findings()
        self.assertEqual([f.checked for f in found], [True, True])
        self.assertIn("slack: HTTP 500", found[1].text)

    def test_a_lost_alert_is_reported_even_for_a_clean_cycle(self) -> None:
        self.publish(phase="verify", outcome="deployed", alert="slack: not delivered (timed out)")
        found = self.findings()
        self.assertEqual([f.checked for f in found], [True])
        self.assertIn("could not be delivered", found[0].text)


class TestSilenceIsAClaim(CheckCase):
    def test_a_result_inside_the_bound_is_fresh(self) -> None:
        self.publish(at="2026-09-21T03:30:00Z")
        self.assertEqual(self.findings(), [])

    def test_a_result_older_than_the_bound_is_a_fault(self) -> None:
        self.publish(at="2026-09-19T03:30:00Z")
        found = self.findings()
        self.assertEqual([f.checked for f in found], [True])
        self.assertIn("4 days", found[0].text)

    def test_a_stale_failure_reports_both_the_failure_and_the_silence(self) -> None:
        self.publish(at="2026-09-10T03:30:00Z", phase="play", outcome="play-failed", detail="x")
        self.assertEqual(len(self.findings()), 2)

    def test_enabled_with_no_result_is_quiet_while_it_is_new(self) -> None:
        # The directory's mtime is the real clock, so `now` must be too: a fixed NOW
        # earlier than today reads that mtime as stamped in the future.
        self.assertEqual(self.findings(now=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())), [])

    def test_enabled_with_no_result_past_the_bound_is_a_fault(self) -> None:
        old = time.time() - (self_update_check.STALE_AFTER_DAYS + 1) * 86400
        os.utime(self.directory, (old, old))
        found = self.findings(now=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
        self.assertEqual([f.checked for f in found], [True])
        self.assertIn("no cycle has recorded a result since", found[0].text)

    def test_a_result_stamped_in_the_future_is_not_checked(self) -> None:
        self.publish(at="2026-09-30T03:30:00Z")
        found = self.findings()
        self.assertEqual([f.checked for f in found], [False])
        self.assertIn("future", found[0].text)

    def test_an_unreadable_stamp_is_not_checked(self) -> None:
        self.publish(at="yesterday")
        found = self.findings()
        self.assertEqual([f.checked for f in found], [False])


class TestTheOwedPostBootCheck(CheckCase):
    def test_owed_by_an_earlier_boot_and_past_its_time_is_a_fault(self) -> None:
        self.publish(owed_boot="boot-before", phase="warn", outcome="rebooting", detail="reboot in 3")
        found = self.findings()
        self.assertEqual([f.checked for f in found], [True])
        self.assertIn("post-boot check", found[0].text)

    def test_owed_by_an_earlier_boot_but_inside_its_time_is_quiet(self) -> None:
        """The verify unit may still be waiting for the sessions to settle."""
        self.publish(owed_boot="boot-before", phase="warn", outcome="rebooting")
        uptime = self_update_check.VERIFY_GRACE_SECONDS - 60
        self.assertEqual(self.findings(uptime=uptime), [])

    def test_an_unknown_uptime_does_not_buy_silence(self) -> None:
        self.publish(owed_boot="boot-before", phase="warn", outcome="rebooting")
        found = self.findings(uptime=-1.0)
        self.assertEqual(len(found), 1)

    def test_an_unknown_boot_id_is_not_checked_rather_than_guessed(self) -> None:
        self.publish(owed_boot="boot-before", phase="warn", outcome="rebooting")
        found = self.findings(boot="")
        self.assertEqual([f.checked for f in found], [False])


class TestAnUnreadableRecord(CheckCase):
    def test_a_malformed_record_is_not_checked(self) -> None:
        with open(published.path(self.directory), "w", encoding="utf-8") as handle:
            handle.write("nonsense\n")
        found = self.findings()
        self.assertEqual([f.checked for f in found], [False])
        self.assertIn("could not be read", found[0].text)

    @unittest.skipIf(os.geteuid() == 0, "root reads a 0000 file regardless")
    def test_a_record_the_user_cannot_open_is_not_checked(self) -> None:
        self.publish()
        os.chmod(published.path(self.directory), 0)
        found = self.findings()
        self.assertEqual([f.checked for f in found], [False])


class TestUptime(unittest.TestCase):
    def test_uptime_is_the_first_field_of_proc_uptime(self) -> None:
        with tempfile.NamedTemporaryFile("w", delete=False) as handle:
            handle.write("12345.67 99999.00\n")
        try:
            self.assertEqual(self_update_check.read_uptime_seconds(handle.name), 12345.67)
        finally:
            os.unlink(handle.name)

    def test_an_unreadable_uptime_is_negative_meaning_unknown(self) -> None:
        self.assertLess(self_update_check.read_uptime_seconds("/nonexistent/uptime"), 0)

    def test_an_unreadable_boot_id_is_empty_meaning_unknown(self) -> None:
        self.assertEqual(self_update_check.read_boot_id("/nonexistent/boot_id"), "")


if __name__ == "__main__":
    unittest.main()
