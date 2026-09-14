"""Tests for helpers.host_health.login_report — Plan 00109, Tasks 3.1 and 3.2.

The login surface. It runs the three checks this plan built — post-boot health,
play freshness, installed-vs-pinned — merges their findings into ONE report, and
notifies only if there is something to say.

What is pinned here, all of it about the ways a health surface stops being one:

1. **Silent when clean.** No findings, no notification, no output, exit 0. A check
   that speaks on every login gets muted, and a muted check is not a check.
2. **One notification, not three.** A host with a stale play and a failed unit and
   a drifted pin gets a single message listing three things. Three notifications is
   the other way this gets ignored.
3. **A check that could not run is a finding**, and one broken check must not
   suppress the other two — the whole point of merging rather than chaining.
4. **The notification failing does not lose the findings.** stdout still carries
   them, and the exit status still says there were some.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import login_report, probe_results

RUNNING_KERNEL = "7.2.4-200.fc44.x86_64"
HEALTHY_DKMS = f"evdi/1.15.0, {RUNNING_KERNEL}, x86_64: installed"

CLEAN_PROBE = probe_results.Report(())
DIRTY_PROBE = probe_results.Report(("evdi: no DKMS module for the running kernel",))


def run(**overrides) -> tuple[int, list[str], list[str]]:
    """Drive `collect` with every check stubbed clean unless overridden."""
    sent: list[str] = []
    arguments = {
        "health": lambda: CLEAN_PROBE,
        "freshness": lambda: [],
        "pins": lambda: [],
        "notify": sent.append,
    }
    arguments.update(overrides)
    findings = login_report.collect(
        health=arguments["health"], freshness=arguments["freshness"], pins=arguments["pins"])
    status = login_report.emit(findings, notify=arguments["notify"], write=lambda _: None)
    return status, findings, sent


class TestSilentWhenClean(unittest.TestCase):
    def test_a_healthy_host_notifies_NOTHING(self) -> None:
        status, findings, sent = run()
        self.assertEqual(status, login_report.EXIT_OK)
        self.assertEqual(findings, [])
        self.assertEqual(sent, [])

    def test_a_healthy_host_writes_nothing_to_stdout(self) -> None:
        written: list[str] = []
        login_report.emit([], notify=lambda _: None, write=written.append)
        self.assertEqual(written, [])


class TestOneNotificationNotThree(unittest.TestCase):
    def test_findings_from_all_three_checks_are_merged(self) -> None:
        _status, findings, _sent = run(
            health=lambda: DIRTY_PROBE,
            freshness=lambda: ["playbooks/a.yml — changed since it was run here"],
            pins=lambda: ["evdi_version (behind): pinned 1.15.0, installed 1.14.16"],
        )
        self.assertEqual(len(findings), 3)

    def test_exactly_one_notification_is_sent(self) -> None:
        _status, _findings, sent = run(
            health=lambda: DIRTY_PROBE,
            freshness=lambda: ["playbooks/a.yml — changed"],
            pins=lambda: ["evdi_version (behind)"],
        )
        self.assertEqual(len(sent), 1)

    def test_the_one_notification_mentions_every_finding(self) -> None:
        _status, _findings, sent = run(
            health=lambda: DIRTY_PROBE,
            freshness=lambda: ["playbooks/a.yml — changed"],
            pins=lambda: ["evdi_version (behind)"],
        )
        self.assertIn("evdi", sent[0])
        self.assertIn("playbooks/a.yml", sent[0])
        self.assertIn("evdi_version", sent[0])

    def test_the_health_findings_come_first(self) -> None:
        """Something broken on this host outranks something that merely drifted."""
        _status, findings, _sent = run(
            health=lambda: DIRTY_PROBE, pins=lambda: ["evdi_version (behind)"])
        self.assertIn("no DKMS module", findings[0])


class TestOneBrokenCheckDoesNotSuppressTheOthers(unittest.TestCase):
    def test_a_raising_freshness_check_becomes_a_finding(self) -> None:
        def explode() -> list[str]:
            raise RuntimeError("the ledger is unreadable")

        _status, findings, _sent = run(freshness=explode)
        self.assertEqual(len(findings), 1)
        self.assertIn("unreadable", findings[0])

    def test_a_raising_check_does_not_hide_another_check_findings(self) -> None:
        """Chaining would have lost these. Merging is why it cannot."""
        def explode() -> list[str]:
            raise RuntimeError("boom")

        _status, findings, _sent = run(freshness=explode, health=lambda: DIRTY_PROBE)
        self.assertEqual(len(findings), 2)

    def test_every_check_raising_produces_every_finding(self) -> None:
        def explode() -> list[str]:
            raise RuntimeError("boom")

        def explode_health() -> probe_results.Report:
            raise RuntimeError("boom")

        _status, findings, _sent = run(
            health=explode_health, freshness=explode, pins=explode)
        self.assertEqual(len(findings), 3)

    def test_each_broken_check_is_named_so_the_user_knows_which(self) -> None:
        def explode() -> list[str]:
            raise RuntimeError("boom")

        _status, findings, _sent = run(freshness=explode)
        self.assertIn("play-freshness", findings[0])


class TestTheNotificationIsNotTheOnlyChannel(unittest.TestCase):
    def test_a_failing_notifier_still_reports_the_findings(self) -> None:
        """A broken notifier must not turn a broken host into a silent one."""
        def explode(_message: str) -> None:
            raise RuntimeError("no session bus")

        written: list[str] = []
        status = login_report.emit(
            ["evdi: broken"], notify=explode, write=written.append)
        self.assertEqual(status, login_report.EXIT_FINDINGS)
        self.assertTrue(any("evdi: broken" in line for line in written))

    def test_a_failing_notifier_is_itself_reported(self) -> None:
        def explode(_message: str) -> None:
            raise RuntimeError("no session bus")

        written: list[str] = []
        login_report.emit(["evdi: broken"], notify=explode, write=written.append)
        self.assertTrue(any("no session bus" in line for line in written))

    def test_findings_reach_stdout_one_per_line(self) -> None:
        written: list[str] = []
        login_report.emit(
            ["one", "two"], notify=lambda _: None, write=written.append)
        self.assertEqual([line.strip() for line in written], ["one", "two"])


class TestTheDkmsSeamDoesNotFabricateAnAnswer(unittest.TestCase):
    """A probe that could not run must not be handed on as empty output.

    `ProbeOutcome.text` is `""` when the probe failed, and empty `dkms status` is a
    legitimate healthy state — no modules. Passing the text straight through would
    turn "dkms is not installed" into "pinned 1.15.0, nothing installed": a
    confident claim about this host that nothing measured. Not a silent pass, but
    the same family — an unmeasured fact reported as a measured one.
    """

    def test_a_failed_probe_raises_rather_than_returning_empty_text(self) -> None:
        failed = probe_results.ProbeOutcome(
            ok=False, text="", error="dkms: command not found")
        with self.assertRaises(Exception) as caught:
            login_report.dkms_text(lambda _argv: failed)
        self.assertIn("command not found", str(caught.exception))

    def test_a_successful_probe_returns_its_text(self) -> None:
        ok = probe_results.ProbeOutcome(ok=True, text=HEALTHY_DKMS, error="")
        self.assertEqual(login_report.dkms_text(lambda _argv: ok), HEALTHY_DKMS)

    def test_genuinely_empty_output_is_returned_not_rejected(self) -> None:
        """A host with no DKMS modules is real and healthy, and must stay
        distinguishable from a host where dkms could not be asked."""
        empty = probe_results.ProbeOutcome(ok=True, text="", error="")
        self.assertEqual(login_report.dkms_text(lambda _argv: empty), "")

    def test_it_asks_dkms_for_its_status(self) -> None:
        seen: list[list[str]] = []

        def runner(argv: list[str]) -> probe_results.ProbeOutcome:
            seen.append(argv)
            return probe_results.ProbeOutcome(ok=True, text="", error="")

        login_report.dkms_text(runner)
        self.assertEqual(seen, [["dkms", "status"]])


class TestExitStatus(unittest.TestCase):
    def test_clean_and_findings_are_distinct(self) -> None:
        self.assertNotEqual(login_report.EXIT_OK, login_report.EXIT_FINDINGS)

    def test_findings_produce_the_findings_status(self) -> None:
        status, _findings, _sent = run(health=lambda: DIRTY_PROBE)
        self.assertEqual(status, login_report.EXIT_FINDINGS)


class TestTheNotificationText(unittest.TestCase):
    def test_the_summary_counts_the_findings(self) -> None:
        self.assertIn("3", login_report.message(["a", "b", "c"]))

    def test_one_finding_is_not_pluralised(self) -> None:
        self.assertNotIn("findings", login_report.message(["a"]))

    def test_several_findings_are(self) -> None:
        self.assertIn("findings", login_report.message(["a", "b"]))

    def test_an_empty_list_never_produces_a_message(self) -> None:
        """Belt and braces: `emit` already refuses, and a caller that reached here
        with nothing would otherwise send an empty notification."""
        with self.assertRaises(ValueError):
            login_report.message([])


if __name__ == "__main__":
    unittest.main()
