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

import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import handoff

FINDINGS = [
    "evdi: no DKMS module installed for the running kernel 7.2.4-200.fc44.x86_64",
    "playbooks/imports/optional/hardware-specific/play-displaylink.yml — changed since it was run here",
]
NOW = "2026-09-14T16:00:00Z"


class TestRender(unittest.TestCase):
    def test_every_finding_appears(self) -> None:
        text = handoff.render(findings=FINDINGS, kernel="7.2.4", at=NOW)
        for finding in FINDINGS:
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
            findings=["the dkms probe could not run: dkms: command not found"],
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
                self.assertIn(FINDINGS[0], handle.read())

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
            path = handoff.write(base, findings=["only this one"], kernel="7.2.4", at=NOW)
            with open(path, encoding="utf-8") as handle:
                text = handle.read()
            self.assertIn("only this one", text)
            self.assertNotIn(FINDINGS[0], text)

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


if __name__ == "__main__":
    unittest.main()
