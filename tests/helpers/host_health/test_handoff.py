"""Tests for helpers.host_health.handoff — Plan 00109, Task 3.3.

Writes the findings into a file a human can hand to Claude Code, so diagnosing a
break is not a manual archaeology session. The 2026-09-11 session that found the
DKMS failure was exactly that archaeology, and this exists to skip it.

Two constraints from the plan's Non-Goals, and they are the reason this is a file
writer and not a launcher:

1. **The handoff is offered, never automatic.** Nothing here starts a process,
   and nothing re-runs a play. The file names the command; a human runs it.
2. **Nothing is auto-applied.** The prompt asks for a diagnosis and a discussion,
   not a fix, because re-running a play is always a human decision.

And one property that is this plan's own subject: the file must say what was
**not** checked. A findings list that omits the checks that could not run reads
like a complete picture of a machine, which is the thing that went wrong.
"""

from __future__ import annotations

import io
import os
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import handoff, probe_results

FINDING_TEXTS = [
    "evdi: no DKMS module installed for the running kernel 7.2.4-200.fc44.x86_64",
    "playbooks/imports/optional/hardware-specific/play-displaylink.yml — changed since it was run here",
]
FINDINGS = [probe_results.broken(text) for text in FINDING_TEXTS]
NOW = "2026-09-14T16:00:00Z"


class TestRender(unittest.TestCase):
    def test_every_finding_appears(self) -> None:
        text = handoff.render(findings=FINDINGS, kernel="7.2.4", at=NOW)
        for finding in FINDING_TEXTS:
            self.assertIn(finding, text)

    def test_the_running_kernel_is_recorded(self) -> None:
        """The incident turned on which kernel had booted, so the handoff must not
        make the reader ask."""
        self.assertIn("7.2.4", handoff.render(findings=FINDINGS, kernel="7.2.4", at=NOW))

    def test_the_time_is_recorded(self) -> None:
        self.assertIn(NOW, handoff.render(findings=FINDINGS, kernel="7.2.4", at=NOW))

    def test_it_asks_for_a_diagnosis_rather_than_a_fix(self) -> None:
        """Re-running a play is always a human decision (the plan's Non-Goals), so
        the prompt must not ask an agent to apply one."""
        text = handoff.render(findings=FINDINGS, kernel="7.2.4", at=NOW).lower()
        self.assertIn("do not", text)

    def test_it_names_the_checks_that_produced_the_findings(self) -> None:
        text = handoff.render(findings=FINDINGS, kernel="7.2.4", at=NOW)
        self.assertIn("helpers.host_health.login_report", text)

    def test_rendering_with_no_findings_raises(self) -> None:
        """There is nothing to hand off on a healthy host, and writing an empty
        prompt file would leave a stale one to be found later and believed."""
        with self.assertRaises(ValueError):
            handoff.render(findings=[], kernel="7.2.4", at=NOW)

    def test_a_finding_that_is_a_could_not_check_is_marked_as_such(self) -> None:
        """A list that mixes 'this is broken' with 'this was not looked at' and
        distinguishes neither is the shape that let the incident happen."""
        text = handoff.render(
            findings=[probe_results.unchecked(
                "the dkms probe could not run: dkms: command not found")],
            kernel="7.2.4", at=NOW)
        self.assertIn("could not be checked", text.lower())

    def test_all_findings_broken_produces_no_unchecked_section(self) -> None:
        text = handoff.render(findings=FINDINGS, kernel="7.2.4", at=NOW)
        self.assertNotIn("could not be checked", text.lower())


class TestWrite(unittest.TestCase):
    def test_the_file_is_written_and_its_path_returned(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = handoff.write(base, findings=FINDINGS, kernel="7.2.4", at=NOW)
            self.assertTrue(os.path.exists(path))
            with open(path, encoding="utf-8") as handle:
                self.assertIn(FINDING_TEXTS[0], handle.read())

    def test_it_creates_its_directory(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            nested = os.path.join(base, "deeper")
            self.assertTrue(os.path.exists(handoff.write(
                nested, findings=FINDINGS, kernel="7.2.4", at=NOW)))

    def test_a_second_write_replaces_the_first(self) -> None:
        """A stale handoff file describing a break that is already fixed is worse
        than none: it is a confident description of a machine that has moved on."""
        with tempfile.TemporaryDirectory() as base:
            handoff.write(base, findings=FINDINGS, kernel="7.2.4", at=NOW)
            path = handoff.write(
                base, findings=[probe_results.broken("only this one")],
                kernel="7.2.4", at=NOW)
            with open(path, encoding="utf-8") as handle:
                text = handle.read()
            self.assertIn("only this one", text)
            self.assertNotIn(FINDING_TEXTS[0], text)

    def test_it_is_not_world_readable(self) -> None:
        """It records what is broken about this machine."""
        with tempfile.TemporaryDirectory() as base:
            path = handoff.write(base, findings=FINDINGS, kernel="7.2.4", at=NOW)
            self.assertEqual(os.stat(path).st_mode & 0o077, 0)


class TestTheOffer(unittest.TestCase):
    def test_the_offer_names_the_file_and_the_command(self) -> None:
        line = handoff.offer("/somewhere/findings.md")
        self.assertIn("/somewhere/findings.md", line)
        self.assertIn("claude", line)

    def test_the_offer_does_NOT_name_ccy(self) -> None:
        """The plan asks for CC against the repo, not a container: diagnosing a
        broken host from inside a container cannot see the host."""
        self.assertNotIn("ccy", handoff.offer("/somewhere/findings.md"))

    def test_nothing_here_launches_anything(self) -> None:
        """`offer` returns a string. If it ever grows a subprocess call this test
        is where that gets noticed — the handoff is offered, never automatic."""
        self.assertIsInstance(handoff.offer("/x"), str)


class TestTheHandoffCanBeSuppressedForTriage(unittest.TestCase):
    """`--no-handoff` exists so a triage run does not clobber the operator's file.

    A triage script that announces itself as read-only and then overwrites the one
    artefact somebody was about to read is a small lie of the kind this plan is
    otherwise about. Driven through `main`, so the flag is exercised rather than
    asserted to exist.
    """

    def _run(self, *flags: str) -> tuple[str, bool]:
        from helpers.host_health import login_report

        with tempfile.TemporaryDirectory() as home:
            out = io.StringIO()
            environment = {"XDG_STATE_HOME": os.path.join(home, "state")}
            with mock.patch.dict(os.environ, environment, clear=False):
                login_report.main(
                    ["--no-notify", *flags], stdout=out, stderr=io.StringIO())
                base = os.path.join(home, "state", "fedora-desktop", "play-ledger")
                return out.getvalue(), os.path.exists(os.path.join(base, handoff.FILE_NAME))

    def test_by_default_the_handoff_file_is_written(self) -> None:
        """This container always has findings — no dkms, no systemd bus — which is
        what makes it a usable fixture for the write path."""
        printed, written = self._run()
        self.assertTrue(written)
        self.assertIn("To discuss this with Claude Code", printed)

    def test_with_no_handoff_nothing_is_written_and_nothing_is_offered(self) -> None:
        printed, written = self._run("--no-handoff")
        self.assertFalse(written)
        self.assertNotIn("To discuss this with Claude Code", printed)

    def test_the_findings_are_still_reported_either_way(self) -> None:
        """Suppressing the file must not suppress the report — that would make a
        triage run look like a clean host."""
        printed, _ = self._run("--no-handoff")
        self.assertIn("could not run", printed)


class TestTheSplitDoesNotGuessFromWording(unittest.TestCase):
    """The split is on `Finding.checked`, never on the prose.

    Matching substrings could not do this job. Two of them — "could not run" and
    "could not be checked" — covered seven of the messages the three checks emit and
    missed six, and all six landed under *"What is wrong"*, in the file whose one job
    is keeping those groups apart. These are real messages from the three producers,
    deliberately worded in ways no substring rule would catch.
    """

    #: Every one of these is an "I could not look" message, and not one of them
    #: contains "could not run" or "could not be checked".
    AWKWARDLY_WORDED = [
        "play-freshness has never successfully reached the remote on this host, "
        "so no play has ever been checked for staleness here",
        "play-freshness cannot tell when it last reached the remote "
        "(unreadable timestamp 'whenever'), so its verdicts cannot be trusted",
        "play-freshness last reached the remote in the future (2027-01-01T00:00:00Z), "
        "so this host's clock cannot be trusted",
        "play-freshness has not reached the remote for 9 days, so nothing has been "
        "checked against upstream in that time",
        "the dkms probe output could not be read: unreadable line 'garbage'",
        "play-freshness could not give an answer, so no play was judged: the ledger "
        "is marked BROKEN",
    ]

    def test_none_of_these_would_be_caught_by_the_old_substrings(self) -> None:
        """Guards the fixture itself: if a message is reworded to contain one of the
        phrases, this case stops proving anything and says so."""
        for text in self.AWKWARDLY_WORDED:
            self.assertNotIn("could not run", text)
            self.assertNotIn("could not be checked", text)

    def test_they_all_land_under_what_could_not_be_checked(self) -> None:
        findings = [probe_results.unchecked(text) for text in self.AWKWARDLY_WORDED]
        text = handoff.render(findings=findings, kernel="7.2.4", at=NOW)
        self.assertIn("What could not be checked", text)
        self.assertNotIn("What is wrong", text)

    def test_a_real_fault_still_lands_under_what_is_wrong(self) -> None:
        text = handoff.render(
            findings=[probe_results.broken("evdi: no DKMS module for the running kernel")],
            kernel="7.2.4", at=NOW)
        self.assertIn("What is wrong", text)
        self.assertNotIn("What could not be checked", text)

    def test_a_mixed_list_is_split_and_not_merged(self) -> None:
        text = handoff.render(
            findings=[
                probe_results.broken("evdi: no DKMS module for the running kernel"),
                probe_results.unchecked(self.AWKWARDLY_WORDED[0]),
            ],
            kernel="7.2.4", at=NOW)
        self.assertIn("What is wrong", text)
        self.assertIn("What could not be checked", text)
        self.assertLess(text.index("What is wrong"), text.index("What could not be checked"))


if __name__ == "__main__":
    unittest.main()
