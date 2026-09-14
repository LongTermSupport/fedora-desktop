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

import io
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import login_report, probe_results, status_document
from helpers.play_ledger import check_freshness

RUNNING_KERNEL = "7.2.4-200.fc44.x86_64"
HEALTHY_DKMS = f"evdi/1.15.0, {RUNNING_KERNEL}, x86_64: installed"

CLEAN_PROBE = probe_results.Report(())
DIRTY_PROBE = probe_results.Report(
    (probe_results.broken("evdi: no DKMS module for the running kernel"),))


def found(*texts: str) -> list[probe_results.Finding]:
    """Stub findings from a check that ran and found something."""
    return [probe_results.broken(text) for text in texts]


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
            freshness=lambda: found("playbooks/a.yml — changed since it was run here"),
            pins=lambda: found("evdi_version (behind): pinned 1.15.0, installed 1.14.16"),
        )
        self.assertEqual(len(findings), 3)

    def test_exactly_one_notification_is_sent(self) -> None:
        _status, _findings, sent = run(
            health=lambda: DIRTY_PROBE,
            freshness=lambda: found("playbooks/a.yml — changed"),
            pins=lambda: found("evdi_version (behind)"),
        )
        self.assertEqual(len(sent), 1)

    def test_the_one_notification_mentions_every_finding(self) -> None:
        _status, _findings, sent = run(
            health=lambda: DIRTY_PROBE,
            freshness=lambda: found("playbooks/a.yml — changed"),
            pins=lambda: found("evdi_version (behind)"),
        )
        self.assertIn("evdi", sent[0])
        self.assertIn("playbooks/a.yml", sent[0])
        self.assertIn("evdi_version", sent[0])

    def test_the_health_findings_come_first(self) -> None:
        """Something broken on this host outranks something that merely drifted."""
        _status, findings, _sent = run(
            health=lambda: DIRTY_PROBE, pins=lambda: found("evdi_version (behind)"))
        self.assertIn("no DKMS module", findings[0].text)


class TestOneBrokenCheckDoesNotSuppressTheOthers(unittest.TestCase):
    def test_a_raising_freshness_check_becomes_a_finding(self) -> None:
        def explode() -> list[str]:
            raise RuntimeError("the ledger is unreadable")

        _status, findings, _sent = run(freshness=explode)
        self.assertEqual(len(findings), 1)
        self.assertIn("unreadable", findings[0].text)

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
        self.assertIn("play-freshness", findings[0].text)


class TestTheNotificationIsNotTheOnlyChannel(unittest.TestCase):
    def test_a_failing_notifier_still_reports_the_findings(self) -> None:
        """A broken notifier must not turn a broken host into a silent one."""
        def explode(_message: str) -> None:
            raise RuntimeError("no session bus")

        written: list[str] = []
        status = login_report.emit(
            found("evdi: broken"), notify=explode, write=written.append)
        self.assertEqual(status, login_report.EXIT_FINDINGS)
        self.assertTrue(any("evdi: broken" in line for line in written))

    def test_a_failing_notifier_is_itself_reported(self) -> None:
        def explode(_message: str) -> None:
            raise RuntimeError("no session bus")

        written: list[str] = []
        login_report.emit(found("evdi: broken"), notify=explode, write=written.append)
        self.assertTrue(any("no session bus" in line for line in written))

    def test_findings_reach_stdout_one_per_line(self) -> None:
        written: list[str] = []
        login_report.emit(
            found("one", "two"), notify=lambda _: None, write=written.append)
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
    def test_findings_cannot_be_confused_with_an_interpreter_crash(self) -> None:
        """The unit declares the findings status a SUCCESS, so a drifted host does not
        also read as a broken service. Python exits 1 for any uncaught exception, so if
        findings were 1 the unit would call a permanently dead health surface a success
        and say nothing — this plan's own incident, inside the code preventing it.

        2 is excluded as well: `argparse` exits 2 on a usage error, so a unit started
        with a bad flag must not read as findings either.
        """
        self.assertNotIn(login_report.EXIT_FINDINGS, (1, 2))
        self.assertNotEqual(login_report.EXIT_FINDINGS, login_report.EXIT_OK)

    def test_clean_and_findings_are_distinct(self) -> None:
        self.assertNotEqual(login_report.EXIT_OK, login_report.EXIT_FINDINGS)

    def test_findings_produce_the_findings_status(self) -> None:
        status, _findings, _sent = run(health=lambda: DIRTY_PROBE)
        self.assertEqual(status, login_report.EXIT_FINDINGS)


class TestTheNotificationText(unittest.TestCase):
    def test_the_summary_counts_the_findings(self) -> None:
        self.assertIn("3", login_report.message(found("a", "b", "c")))

    def test_one_finding_is_not_pluralised(self) -> None:
        self.assertNotIn("findings", login_report.message(found("a")))

    def test_several_findings_are(self) -> None:
        self.assertIn("findings", login_report.message(found("a", "b")))

    def test_an_empty_list_never_produces_a_message(self) -> None:
        """Belt and braces: `emit` already refuses, and a caller that reached here
        with nothing would otherwise send an empty notification."""
        with self.assertRaises(ValueError):
            login_report.message([])


class TestTheFreshnessSeamKeepsItsChannelsApart(unittest.TestCase):
    """The seam between this module and `check_freshness`, which had no tests at all.

    `check_freshness` answers on two channels that mean different things: stdout
    carries findings, stderr carries the reason it could not answer. Handed a single
    sink for both, every diagnostic became a user-facing finding — "git fetch failed"
    reported as a problem with the host, which `DESIGN-host-health.md` §8 decided it
    must not be — and every commit line beneath a stale play became a peer finding, so
    one stale play with three commits counted as four problems.

    The same seam one file over (`dkms_text`) got its own class because this lesson had
    already been paid for. This one had not, which is why it was where the defects were.
    """

    @staticmethod
    def _fake(status: int, out: str, err: str):
        """Stands in for `check_freshness.run`, writing to the channels it is given."""

        def run(*, stdout, stderr, **_arguments) -> int:
            stdout.write(out)
            stderr.write(err)
            return status

        return run

    def _findings(self, status: int, out: str = "", err: str = "") -> tuple[list[str], str]:
        errors = io.StringIO()
        findings = login_report.freshness_findings(
            "/state/base", "/repo", stderr=errors, run=self._fake(status, out, err)
        )
        return findings, errors.getvalue()

    def test_a_clean_run_reports_nothing(self) -> None:
        self.assertEqual(self._findings(check_freshness.EXIT_OK)[0], [])

    def test_a_diagnostic_is_not_a_finding(self) -> None:
        findings, _ = self._findings(
            check_freshness.EXIT_FINDINGS,
            out="playbooks/a.yml — changed since it was run here\n",
            err="play-freshness: git fetch failed, judging on the refs on hand: boom\n",
        )
        self.assertEqual(
            [f.text for f in findings],
            ["playbooks/a.yml — changed since it was run here"],
        )

    def test_a_diagnostic_still_reaches_stderr(self) -> None:
        """Kept out of the payload, not thrown away — it is a diagnostic, so it goes
        where diagnostics go (`CLAUDE/StderrHygiene.md`)."""
        _, errors = self._findings(
            check_freshness.EXIT_FINDINGS,
            out="playbooks/a.yml — changed\n",
            err="play-freshness: git fetch failed: boom\n",
        )
        self.assertIn("git fetch failed: boom", errors)

    def test_the_untrustworthy_reason_reaches_the_user(self) -> None:
        """The BROKEN sentinel exists to say WHY. A finding that dropped the reason and
        pointed at "its stderr output above" sent the reader looking for something that
        was never shown to them."""
        findings, _ = self._findings(
            check_freshness.EXIT_UNTRUSTWORTHY,
            err=(
                "play-freshness: the ledger is marked BROKEN and cannot be trusted.\n"
                "  reason: the callback could not write a record: disk full\n"
            ),
        )
        self.assertEqual(len(findings), 1)
        self.assertIn("disk full", findings[0].text)

    def test_one_stale_play_is_one_finding_carrying_its_commits(self) -> None:
        findings, _ = self._findings(
            check_freshness.EXIT_FINDINGS,
            out=(
                "playbooks/a.yml — changed since it was run here\n"
                "    aaa1111  first change\n"
                "    bbb2222  second change\n"
            ),
        )
        self.assertEqual(len(findings), 1)
        self.assertIn("aaa1111", findings[0].text)
        self.assertIn("bbb2222", findings[0].text)

    def test_every_finding_is_a_single_line(self) -> None:
        """`emit` writes one finding per line and `test_probe.py` pins that as a
        contract; this is the one place that produced multi-line findings."""
        findings, _ = self._findings(
            check_freshness.EXIT_FINDINGS,
            out=(
                "playbooks/a.yml — changed\n"
                "    aaa1111  first change\n"
                "playbooks/b.yml — changed\n"
            ),
        )
        self.assertEqual(len(findings), 2)
        for finding in findings:
            self.assertNotIn("\n", finding)

    def test_an_orphan_detail_line_is_kept_not_dropped(self) -> None:
        """The fold must not be able to lose a line. An indented line with no headline
        before it becomes its own finding rather than being swallowed."""
        findings, _ = self._findings(
            check_freshness.EXIT_FINDINGS, out="    aaa1111  an orphan\n"
        )
        self.assertEqual([f.text for f in findings], ["aaa1111  an orphan"])


if __name__ == "__main__":
    unittest.main()


class TestSectionsKeepTheirIdentity(unittest.TestCase):
    """The notification flattens three checks into one list; the status document must
    not, because the panel's registry matches a section by id and a section it cannot
    find renders `unavailable`.

    Both come from ONE call, so the notification and the document cannot disagree about
    which checks ran. Two copies of the merged-not-chained guard would be two chances to
    fix only one of them.
    """

    def _sections(self, **overrides):
        arguments = {"health": lambda: CLEAN_PROBE, "freshness": lambda: [], "pins": lambda: []}
        arguments.update(overrides)
        return login_report.collect_sections(**arguments)

    def test_every_check_gets_its_own_named_section(self) -> None:
        self.assertEqual(
            list(self._sections()),
            [login_report.HEALTH, login_report.FRESHNESS, login_report.PINS],
        )

    def test_health_comes_first_because_broken_now_outranks_drifted(self) -> None:
        """Order is the report's priority, and the flattened list inherits it."""
        self.assertEqual(list(self._sections())[0], login_report.HEALTH)

    def test_a_findings_stay_under_the_check_that_produced_them(self) -> None:
        sections = self._sections(
            health=lambda: DIRTY_PROBE, pins=lambda: found("evdi: pinned 1.15.0, installed 1.14.0"))
        self.assertEqual(
            [finding.text for finding in sections[login_report.HEALTH]],
            ["evdi: no DKMS module for the running kernel"])
        self.assertEqual(sections[login_report.FRESHNESS], [])
        self.assertIn("1.14.0", sections[login_report.PINS][0].text)

    def test_a_raising_check_becomes_its_own_section_unchecked_and_names_itself(self) -> None:
        def explode() -> list[probe_results.Finding]:
            raise RuntimeError("the ledger is unreadable")

        sections = self._sections(freshness=explode)
        finding = sections[login_report.FRESHNESS][0]
        self.assertFalse(finding.checked)
        self.assertIn(login_report.FRESHNESS, finding.text)
        self.assertIn("unreadable", finding.text)

    def test_the_flattened_list_holds_exactly_the_sections_findings(self) -> None:
        """`collect` must be the same findings in the same order, or the notification
        and the document describe different hosts."""
        arguments = {
            "health": lambda: DIRTY_PROBE,
            "freshness": lambda: found("playbooks/a.yml — changed since it last ran"),
            "pins": lambda: found("evdi: pinned 1.15.0, installed 1.14.0"),
        }
        sections = login_report.collect_sections(**arguments)
        flattened = [finding.text for group in sections.values() for finding in group]
        self.assertEqual([f.text for f in login_report.collect(**arguments)], flattened)


class TestTheDocumentIsWrittenWhetherOrNotAnythingIsWrong(unittest.TestCase):
    """A document that only appears when something is wrong makes a clean host look
    exactly like a host nothing has ever checked — this plan's own defect, moved into
    the file format. `ok` is a result and has to be recorded as one.

    The handoff file is the opposite case and correctly only written when there are
    findings: it exists to be handed to Claude Code, and there is nothing to diagnose
    about a healthy host.
    """

    def _published(self, base: str, **overrides) -> dict:
        arguments = {"health": lambda: CLEAN_PROBE, "freshness": lambda: [], "pins": lambda: []}
        arguments.update(overrides)
        login_report.publish(
            base,
            sections=login_report.collect_sections(**arguments),
            kernel=RUNNING_KERNEL,
            at="2026-09-14T18:00:00Z",
        )
        return status_document.read(status_document.path(base))

    def test_a_clean_host_still_publishes_and_says_ok(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            document = self._published(base)
        self.assertEqual(document["sections"][login_report.HEALTH]["state"], status_document.OK)
        self.assertEqual(document["kernel"], RUNNING_KERNEL)

    def test_findings_reach_the_document_under_their_own_section(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            document = self._published(base, health=lambda: DIRTY_PROBE)
        section = document["sections"][login_report.HEALTH]
        self.assertEqual(section["state"], status_document.FINDINGS)
        self.assertIn("evdi", section["findings"][0])

    def test_a_check_that_could_not_run_is_unavailable_in_the_document(self) -> None:
        def explode() -> list[probe_results.Finding]:
            raise RuntimeError("boom")

        with tempfile.TemporaryDirectory() as base:
            document = self._published(base, freshness=explode)
        self.assertEqual(
            document["sections"][login_report.FRESHNESS]["state"], status_document.UNAVAILABLE)

    def test_it_records_when_it_was_collected(self) -> None:
        """The panel shows the age. Presenting login-time findings at teatime as
        current states something the checks did not measure."""
        with tempfile.TemporaryDirectory() as base:
            document = self._published(base)
        self.assertEqual(document["generated_at"], "2026-09-14T18:00:00Z")
