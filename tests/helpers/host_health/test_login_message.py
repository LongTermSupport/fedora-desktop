"""Tests for helpers.host_health.login_message — Plan 00109, Task 3.2 server route.

The second consumer of the status document, and the one a server gets. `notify-send` has
no session bus to reach on a server and `graphical-session.target` never activates, so the
desktop delivery ends its play there and a server would otherwise get no drift reporting
at all.

The split that makes this affordable: the checks run on a schedule and write the document;
this renders what they left. A `git fetch` at every SSH login would add latency to every
login and can hang.

What is pinned here:

1. **Silent when clean AND fresh.** A message on every login gets ignored, and then it is
   not a report. Both conditions — a clean document nobody has updated for a month is not
   a clean host.
2. **Stale is a finding, not a reason to stay quiet.** The worst outcome available is a
   host that quietly stops being checked, which is this plan's subject exactly.
   `DESIGN-host-health.md` §8 settled the same question for the fetch clock.
3. **`unavailable` is reported and labelled as not-checked**, never folded in with the
   faults and never silently dropped.
4. **It never raises.** This runs from a login shell. A renderer that throws on a
   malformed document costs the user their prompt, and an error there is far worse than
   the report it replaces.
"""

from __future__ import annotations

import io
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import login_message, probe_results, status_document

NOW = "2026-09-14T18:00:00Z"
KERNEL = "7.2.4-200.fc44.x86_64"


def document(sections: dict, *, at: str = NOW) -> dict:
    return status_document.build(sections=sections, kernel=KERNEL, at=at)


def render(sections: dict, *, at: str = NOW, now: str = NOW) -> str:
    return login_message.render(document(sections, at=at), now=now)


class TestSilentWhenCleanAndFresh(unittest.TestCase):
    def test_a_clean_fresh_document_says_nothing_at_all(self) -> None:
        self.assertEqual(render({"health": []}), "")

    def test_not_even_a_reassuring_line(self) -> None:
        """"Everything is fine" on every single login is the same noise as a finding on
        every login, and trains the same blindness."""
        self.assertEqual(render({"health": [], "pins": []}), "")

    def test_an_empty_document_is_still_silent_if_fresh(self) -> None:
        self.assertEqual(render({}), "")


class TestStalenessIsReported(unittest.TestCase):
    """A clean document nobody has updated for a month is not a clean host. The same
    question `DESIGN-host-health.md` §8 settled for the fetch clock, same answer."""

    def test_a_stale_clean_document_is_not_silent(self) -> None:
        message = render({"health": []}, at="2026-08-01T18:00:00Z", now=NOW)
        self.assertNotEqual(message, "")

    def test_it_says_how_long_rather_than_just_that_it_is_old(self) -> None:
        """"Stale" tells the user to go and find out. A duration tells them whether the
        timer died today or six weeks ago."""
        message = render({"health": []}, at="2026-08-15T18:00:00Z", now=NOW)
        self.assertIn("30 days", message)

    def test_just_inside_the_bound_stays_silent(self) -> None:
        inside = f"2026-09-{14 - login_message.STALE_AFTER_DAYS + 1:02d}T19:00:00Z"
        self.assertEqual(render({"health": []}, at=inside, now=NOW), "")

    def test_the_bound_is_a_declared_constant_not_a_buried_branch(self) -> None:
        self.assertIsInstance(login_message.STALE_AFTER_DAYS, int)
        self.assertGreater(login_message.STALE_AFTER_DAYS, 0)


class TestFindingsAreReported(unittest.TestCase):
    def test_a_fault_is_named(self) -> None:
        message = render({"health": [probe_results.broken("evdi: no DKMS module")]})
        self.assertIn("evdi: no DKMS module", message)

    def test_every_section_with_a_fault_is_reported_not_just_the_first(self) -> None:
        message = render({
            "health": [probe_results.broken("evdi: no DKMS module")],
            "pins": [probe_results.broken("evdi: pinned 1.15.0, installed 1.14.0")],
        })
        self.assertIn("no DKMS module", message)
        self.assertIn("1.14.0", message)

    def test_an_unchecked_finding_is_reported_and_marked_as_not_checked(self) -> None:
        """The distinction the handoff file exists to keep, on this channel too. A list
        that mixes them and marks neither reads like a complete picture of a machine."""
        message = render({"health": [probe_results.unchecked("dkms: not found")]})
        self.assertIn("dkms: not found", message)
        self.assertIn("not checked", message.lower())

    def test_faults_and_unchecked_are_not_run_together_in_one_list(self) -> None:
        message = render({"health": [
            probe_results.broken("evdi: no DKMS module"),
            probe_results.unchecked("dkms: not found"),
        ]})
        self.assertLess(
            message.index("evdi: no DKMS module"),
            message.lower().index("not checked"),
            "known faults come first; the not-checked group is introduced after them",
        )


class TestAnAbsentDocumentIsNotAHealthyHost(unittest.TestCase):
    """`status_document.read` already reports absent, unparseable and unknown-schema as
    `unavailable`. What matters here is that the renderer does not then go quiet."""

    def test_an_unavailable_document_is_reported(self) -> None:
        message = login_message.render(
            status_document.read("/nowhere/at/all/status.json"), now=NOW)
        self.assertNotEqual(message, "")

    def test_it_does_not_claim_an_age_it_cannot_know(self) -> None:
        """An unreadable document has no `generated_at`, and printing an age derived
        from an empty string would be a measurement of nothing."""
        message = login_message.render(
            status_document.read("/nowhere/at/all/status.json"), now=NOW)
        self.assertNotIn("days ago", message)


class TestItNeverRaises(unittest.TestCase):
    """This runs from a login shell. Losing the user's prompt to a traceback is worse
    than any report it could have printed."""

    def test_a_document_missing_its_sections_does_not_raise(self) -> None:
        self.assertIsInstance(login_message.render({"schema": 1}, now=NOW), str)

    def test_a_document_that_is_not_a_dict_does_not_raise(self) -> None:
        self.assertIsInstance(login_message.render("nonsense", now=NOW), str)

    def test_an_unparseable_timestamp_does_not_raise_and_is_reported(self) -> None:
        """A timestamp that cannot be read means the age is unknown, which by this
        plan's own rule is not the same as fresh."""
        message = render({"health": []}, at="not-a-timestamp", now=NOW)
        self.assertNotEqual(message, "")

    def test_a_section_whose_lists_are_the_wrong_type_does_not_raise(self) -> None:
        broken_document = {
            "schema": status_document.SCHEMA_VERSION,
            "generated_at": NOW,
            "kernel": KERNEL,
            "sections": {"health": {"state": "findings", "findings": "not a list"}},
        }
        self.assertIsInstance(login_message.render(broken_document, now=NOW), str)


class TestTheEntryPointALoginShellCalls(unittest.TestCase):
    """`main` is what a login shell runs, so its contract is narrower than a report's.

    It must exit 0 whatever it finds. A non-zero status from a login shell snippet
    can trip `set -e` in a sourced profile and, on a strict shell, cost the user the
    login itself — a health reporter that locks you out of the host it reports on.
    """

    def test_it_exits_zero_even_with_findings(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            status_document.write_atomic(
                status_document.path(base),
                document({"health": [probe_results.broken("evdi: no DKMS module")]}))
            out = io.StringIO()
            self.assertEqual(login_message.main(["--state-dir", base], stdout=out), 0)
            self.assertIn("evdi", out.getvalue())

    def test_it_exits_zero_and_prints_nothing_when_clean(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            status_document.write_atomic(
                status_document.path(base), document({"health": []}))
            out = io.StringIO()
            self.assertEqual(login_message.main(["--state-dir", base], stdout=out), 0)
            self.assertEqual(out.getvalue(), "")

    def test_it_exits_zero_when_there_is_no_document_at_all(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            out = io.StringIO()
            self.assertEqual(login_message.main(["--state-dir", base], stdout=out), 0)
            self.assertIn("no host status", out.getvalue())


if __name__ == "__main__":
    unittest.main()
