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


def render(
    sections: dict, *, at: str = NOW, now: str = NOW, running_kernel: str = KERNEL
) -> str:
    return login_message.render(
        document(sections, at=at), now=now, running_kernel=running_kernel
    )


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

    def test_a_document_stamped_in_the_future_is_not_treated_as_fresh(self) -> None:
        """`(now - then).days` floors, so a stamp six days ahead gives -6, and
        `-6 >= STALE_AFTER_DAYS` is False — a wrong clock buying unlimited silence.
        `fetch_clock.offline_finding` treats exactly this condition on the sibling clock
        as a finding; an impossible age is no more a fresh one than an unknown age is."""
        message = render({"health": []}, at="2026-09-20T18:00:00Z", now=NOW)
        self.assertNotEqual(message, "")
        self.assertIn("future", message)

    def test_ordinary_clock_skew_is_not_called_a_broken_clock(self) -> None:
        """`.days` floors, so a stamp twenty minutes ahead reads as -1. The producer and
        the consumer are the same host, so an NTP correction landing between the write
        and the read must not cry "wrong clock" at every login — that is how a health
        surface earns being ignored. Caught by the clean-document test, which stamps a
        fixed NOW and reads it back against the real clock."""
        message = render({"health": []}, at="2026-09-14T18:20:00Z", now=NOW)
        self.assertEqual(message, "")

    def test_the_future_tolerance_is_a_declared_constant_too(self) -> None:
        self.assertIsInstance(login_message.FUTURE_TOLERANCE_DAYS, int)
        self.assertGreater(login_message.FUTURE_TOLERANCE_DAYS, 0)


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
            status_document.read("/nowhere/at/all/status.json"),
            now=NOW, running_kernel=KERNEL)
        self.assertNotEqual(message, "")

    def test_it_does_not_claim_an_age_it_cannot_know(self) -> None:
        """An unreadable document has no `generated_at`, and printing an age derived
        from an empty string would be a measurement of nothing."""
        message = login_message.render(
            status_document.read("/nowhere/at/all/status.json"),
            now=NOW, running_kernel=KERNEL)
        self.assertNotIn("days ago", message)

    def test_it_does_not_claim_a_kernel_mismatch_it_cannot_know(self) -> None:
        """The unavailable shape carries `kernel: ""`. Comparing that against the
        running kernel would report a mismatch on every login on a host whose only
        problem is that nothing has run yet, and the document already says so."""
        message = login_message.render(
            status_document.read("/nowhere/at/all/status.json"),
            now=NOW, running_kernel=KERNEL)
        self.assertNotIn("now running", message)


class TestADocumentFromAnotherBootIsNotAboutThisOne(unittest.TestCase):
    """The exposure the SERVER route introduces, and the desktop route does not have.

    On a desktop the producer re-runs at every graphical login, so the document always
    describes the boot the reader is in. On a server it runs on a timer, so a document
    can outlive a reboot — and after a reboot into a NEW kernel, a document collected
    under the old one is still fresh, still says `ok`, and the report stays silent about
    a host whose DKMS modules are not built for the kernel it is now running.

    That is this plan's founding incident exactly: a reboot into kernel 7.2.4 left both
    DisplayLink monitors dark while every check stayed green. Staleness does not cover
    it — the document can be minutes old and still be about a different kernel.
    """

    OTHER = "7.1.9-200.fc44.x86_64"

    def test_a_fresh_clean_document_from_another_kernel_is_not_silent(self) -> None:
        self.assertNotEqual(render({"health": []}, running_kernel=self.OTHER), "")

    def test_it_names_both_kernels(self) -> None:
        """Which one it was collected under and which one is running — a reader has to
        be able to tell a reboot from a kernel that was removed under them."""
        message = render({"health": []}, running_kernel=self.OTHER)
        self.assertIn(KERNEL, message)
        self.assertIn(self.OTHER, message)

    def test_it_is_reported_as_not_checked_rather_than_as_a_fault(self) -> None:
        """Nothing is known to be broken. What is known is that these results do not
        describe the running kernel, which is the not-checked group's whole meaning."""
        message = render({"health": []}, running_kernel=self.OTHER)
        self.assertIn("not checked", message.lower())

    def test_the_same_kernel_is_silent(self) -> None:
        self.assertEqual(render({"health": []}, running_kernel=KERNEL), "")

    def test_it_is_reported_alongside_real_findings_not_instead_of_them(self) -> None:
        message = render(
            {"play-freshness": [probe_results.broken("play-x.yml has changed")]},
            running_kernel=self.OTHER,
        )
        self.assertIn("play-x.yml has changed", message)
        self.assertIn(self.OTHER, message)

    def test_the_boot_scoped_findings_stop_being_present_tense_faults(self) -> None:
        """THE defect this rule nearly introduced. `dkms_findings` bakes the collecting
        kernel into its text — "no DKMS module installed for the running kernel 7.1.9" —
        which is written at collection time and read later. Left in the fault list after
        a reboot it put two different values for "the running kernel" on consecutive
        lines of one report, one of them wrong, in precisely the scenario the mismatch
        rule exists for. Demoted, they are what they now are: nobody has looked."""
        stale = f"evdi: no DKMS module installed for the running kernel {KERNEL}"
        message = render(
            {status_document.BOOT_SCOPED_SECTION: [probe_results.broken(stale)]},
            running_kernel=self.OTHER,
        )
        self.assertIn(stale, message)
        self.assertLess(
            message.lower().index("not checked"),
            message.index(stale),
            "a finding about a kernel that is no longer running must sit under the "
            "not-checked heading, not above it as a known fault",
        )

    def test_a_section_that_survives_a_reboot_keeps_its_faults(self) -> None:
        """Only `post-boot-health` is boot-scoped. Play freshness, the ledger and
        installed-vs-pinned are unchanged by a reboot, so demoting them would be its
        own overclaim — the mirror image of the defect above."""
        message = render(
            {
                status_document.BOOT_SCOPED_SECTION: [probe_results.broken("dkms thing")],
                "installed-vs-pinned": [probe_results.broken("evdi: pinned 1.15.0")],
            },
            running_kernel=self.OTHER,
        )
        self.assertLess(
            message.index("evdi: pinned 1.15.0"),
            message.lower().index("not checked"),
            "a pin comparison is not invalidated by a reboot",
        )

    def test_the_same_boot_leaves_the_boot_scoped_findings_as_faults(self) -> None:
        message = render(
            {status_document.BOOT_SCOPED_SECTION: [probe_results.broken("dkms thing")]},
            running_kernel=KERNEL,
        )
        self.assertIn("dkms thing", message)
        self.assertNotIn("not checked", message.lower())

    def test_it_does_not_claim_the_other_three_sections_are_invalidated(self) -> None:
        """"Nothing here describes the running kernel" was the first wording and it
        overclaimed about three sections out of four."""
        message = render({"play-freshness": []}, running_kernel=self.OTHER)
        self.assertIn("post-boot checks", message)
        self.assertNotIn("nothing here describes", message)

    def test_the_explanation_comes_before_what_it_explains(self) -> None:
        message = render(
            {status_document.BOOT_SCOPED_SECTION: [probe_results.broken("dkms thing")]},
            running_kernel=self.OTHER,
        )
        self.assertLess(
            message.index("different boot"),
            message.index("dkms thing"),
            "a demoted line read before its reason is a line with no reason",
        )

    def test_a_stale_document_from_another_kernel_reports_both(self) -> None:
        """Independent conditions. A timer that died before a reboot produces both, and
        collapsing them would hide whichever was reported second."""
        message = render(
            {"health": []}, at="2026-08-01T18:00:00Z", now=NOW, running_kernel=self.OTHER
        )
        self.assertIn("days ago", message)
        self.assertIn(self.OTHER, message)

    def test_an_unknown_running_kernel_claims_no_mismatch(self) -> None:
        """`probe.running_kernel` reads `os.uname()` and cannot realistically return
        empty, but "I could not tell" must not render as "they differ" — that would be
        a finding manufactured out of ignorance, which is the inverse of this plan's
        rule and just as wrong."""
        self.assertEqual(render({"health": []}, running_kernel=""), "")


class TestAMalformedDocumentIsNotAHealthyHost(unittest.TestCase):
    """Not raising was pinned; not going silent was not, and the two are different.

    Each shape below carries a current timestamp, the running kernel and a schema this
    reader knows, so nothing else flags it — and every defensive guard in `render`
    answers "unreadable" and "genuinely empty" the same way, which on this surface means
    healthy. `status_document.read` refuses to make that trade for an absent file; this
    is the same refusal one layer in.
    """

    def malformed(self, sections: object) -> str:
        return login_message.render(
            {"schema": 1, "generated_at": NOW, "kernel": KERNEL, "sections": sections},
            now=NOW, running_kernel=KERNEL)

    def test_sections_that_are_not_a_mapping_are_reported(self) -> None:
        for sections in ("post-boot-health", [{"state": "findings"}]):
            with self.subTest(sections=sections):
                self.assertNotEqual(self.malformed(sections), "")

    def test_a_section_that_is_not_a_mapping_is_reported(self) -> None:
        self.assertNotEqual(self.malformed({"post-boot-health": "broken"}), "")

    def test_a_document_that_says_findings_and_shows_none_is_reported(self) -> None:
        """The shape worth naming: `state` says `findings`, the list is unreadable, and
        a reader that only reads the list prints nothing — agreeing with the wrong half
        of a document that contradicts itself."""
        message = self.malformed(
            {"post-boot-health": {"state": "findings", "findings": "evdi is dead"}})
        self.assertNotEqual(message, "")
        self.assertIn("not checked", message.lower())

    def test_findings_holding_things_that_are_not_strings_are_reported(self) -> None:
        self.assertNotEqual(
            self.malformed(
                {"post-boot-health": {"state": "findings", "findings": [{"t": "x"}]}}),
            "",
        )

    def test_a_genuinely_clean_document_is_still_silent(self) -> None:
        """The control. Without it this class would pass with `render` shouting at
        every login, which is the failure it is guarding the other side of."""
        self.assertEqual(
            self.malformed({"health": {"state": "ok", "findings": [], "unchecked": []}}),
            "",
        )

    def test_it_is_reported_as_not_checked_rather_than_as_a_fault(self) -> None:
        """Nothing has been shown to be wrong with the host. What is wrong is that
        nothing about it could be read."""
        message = self.malformed({"post-boot-health": "broken"})
        self.assertIn("not checked", message.lower())


class TestItNeverRaises(unittest.TestCase):
    """This runs from a login shell. Losing the user's prompt to a traceback is worse
    than any report it could have printed — and is a different claim from not going
    silent, which `TestAMalformedDocumentIsNotAHealthyHost` above pins."""

    def test_a_document_missing_its_sections_does_not_raise(self) -> None:
        message = login_message.render({"schema": 1}, now=NOW, running_kernel=KERNEL)
        self.assertIsInstance(message, str)
        self.assertNotEqual(message, "", "a document with no sections is not a clean host")

    def test_a_document_that_is_not_a_dict_does_not_raise(self) -> None:
        message = login_message.render("nonsense", now=NOW, running_kernel=KERNEL)
        self.assertIsInstance(message, str)
        self.assertNotEqual(message, "")

    def test_a_document_whose_kernel_is_not_a_string_does_not_raise(self) -> None:
        """The kernel comparison reads a value off a file that may have been written by
        another version, truncated or hand-edited — the same defensiveness every other
        field in here already has."""
        odd = {
            "schema": status_document.SCHEMA_VERSION,
            "generated_at": NOW,
            "kernel": ["not", "a", "string"],
            "sections": {},
        }
        self.assertIsInstance(
            login_message.render(odd, now=NOW, running_kernel=KERNEL), str)

    def test_an_unparseable_timestamp_does_not_raise_and_is_reported(self) -> None:
        """A timestamp that cannot be read means the age is unknown, which by this
        plan's own rule is not the same as fresh."""
        message = render({"health": []}, at="not-a-timestamp", now=NOW)
        self.assertNotEqual(message, "")

    def test_a_timestamp_carrying_no_timezone_does_not_raise(self) -> None:
        """The one input that defeated the `except ValueError`. `fromisoformat` accepts
        a naive stamp *and* an aware one, so the subtraction that follows raises
        TypeError — out of `_age_days`, `render`, `read_and_render` and `main`, from
        inside the function written to keep a traceback away from a login shell.

        The producer always writes `Z`, so this is a document from another version, a
        hand-edit, or a truncation — every one of which reaches this code path."""
        message = render({"health": []}, at="2026-08-01T18:00:00", now=NOW)
        self.assertIsInstance(message, str)
        self.assertNotEqual(message, "", "an unreadable age must be reported, not assumed fresh")

    def test_a_section_whose_lists_are_the_wrong_type_does_not_raise(self) -> None:
        broken_document = {
            "schema": status_document.SCHEMA_VERSION,
            "generated_at": NOW,
            "kernel": KERNEL,
            "sections": {"health": {"state": "findings", "findings": "not a list"}},
        }
        self.assertIsInstance(
            login_message.render(broken_document, now=NOW, running_kernel=KERNEL), str)


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
