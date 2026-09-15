"""Tests for helpers.version_pins.check_pins — Plan 00109, Task 2.2's executor.

The third drift axis: repo pin versus what is actually installed here. This is the
axis the 2026-09-11 failure happened on, with every other check green.

What is pinned here:

1. **The gate this task demands.** The check must report a finding against the
   incident's own state — evdi 1.14.16 installed, 1.15.0 pinned — *and* be clean
   against the state after the fix. A check that cannot fail against the failure it
   was built for is not a check; one that cannot pass is noise that gets muted.
2. **A tracked pin whose resolution fails is a finding**, never a pass. That is the
   whole reason `compare.UNDETERMINED` exists.
3. **An untracked pin is silent** — the decision not to compare it is recorded in
   the manifest, and repeating it every login is how a report gets ignored.
"""

from __future__ import annotations

import os
import subprocess
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.version_pins import check_pins, manifest

RUNNING_KERNEL = "7.2.4-200.fc44.x86_64"

#: Exactly as recorded in the plan's journal for the incident.
DKMS_INCIDENT = "evdi/1.14.16, 7.1.9-200.fc44.x86_64, x86_64: installed"
#: Exactly as recorded after the fix, release suffix and all.
DKMS_FIXED = f"evdi/1.15.0-1.github_evdi, {RUNNING_KERNEL}, x86_64: installed"

PLAYBOOK = """---
- name: DisplayLink
  vars:
    displaylink_version: "v6.3.0-1"
    evdi_version: "1.15.0"
"""


def pin(**overrides) -> manifest.Pin:
    row = {
        "playbook": "playbooks/imports/optional/hardware-specific/play-displaylink.yml",
        "var": "evdi_version",
        "github": "DisplayLink/evdi",
        "installed": {"kind": "dkms", "name": "evdi"},
    }
    row.update(overrides)
    return manifest.parse({"version_pins": [row]}, require_installed=True)[0]


class TestPinnedValue(unittest.TestCase):
    def test_the_pinned_value_is_read_from_the_playbook_text(self) -> None:
        self.assertEqual(check_pins.pinned_value(PLAYBOOK, "evdi_version"), "1.15.0")

    def test_quotes_and_whitespace_are_stripped(self) -> None:
        self.assertEqual(check_pins.pinned_value(PLAYBOOK, "displaylink_version"), "v6.3.0-1")

    def test_a_var_that_is_not_there_raises(self) -> None:
        """Returning None would let a renamed var read as an absent install."""
        with self.assertRaises(check_pins.ResolutionError):
            check_pins.pinned_value(PLAYBOOK, "not_a_var")

    def test_a_var_name_that_is_a_prefix_of_another_is_not_matched(self) -> None:
        with self.assertRaises(check_pins.ResolutionError):
            check_pins.pinned_value(PLAYBOOK, "evdi")


class TestDkmsResolution(unittest.TestCase):
    def test_the_module_version_is_taken_from_dkms_status(self) -> None:
        self.assertEqual(check_pins.installed_from_dkms(DKMS_INCIDENT, "evdi"), "1.14.16")

    def test_a_release_suffix_does_not_defeat_it(self) -> None:
        self.assertEqual(
            check_pins.installed_from_dkms(DKMS_FIXED, "evdi"), "1.15.0-1.github_evdi")

    def test_a_module_that_is_absent_resolves_to_None(self) -> None:
        """Absent is a real state — ABSENT, not UNDETERMINED — and the incident's
        worst hour was exactly this."""
        self.assertIsNone(check_pins.installed_from_dkms(DKMS_INCIDENT, "nvidia"))

    def test_no_dkms_modules_at_all_resolves_to_None(self) -> None:
        self.assertIsNone(check_pins.installed_from_dkms("", "evdi"))

    def test_unreadable_dkms_output_raises(self) -> None:
        with self.assertRaises(check_pins.ResolutionError):
            check_pins.installed_from_dkms("this is not dkms output", "evdi")

    def test_the_highest_installed_version_wins_when_several_are_present(self) -> None:
        """Several kernels' builds list the same module repeatedly; the question is
        what version this host HAS, and numerically 1.14.16 is above 1.14.9."""
        text = "evdi/1.14.9, k1, x86_64: installed\nevdi/1.14.16, k2, x86_64: installed"
        self.assertEqual(check_pins.installed_from_dkms(text, "evdi"), "1.14.16")


class TestTheGateThisTaskDemands(unittest.TestCase):
    """Both directions, against the real recorded states."""

    def test_the_INCIDENT_state_is_a_finding(self) -> None:
        findings = check_pins.check(
            pins=[pin()],
            playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: DKMS_INCIDENT,
        )
        self.assertEqual(len(findings), 1)
        self.assertIn("evdi_version", findings[0].text)
        self.assertIn("1.14.16", findings[0].text)
        self.assertIn("1.15.0", findings[0].text)

    def test_the_state_AFTER_the_fix_is_clean(self) -> None:
        """The other half of the gate. A check that cannot pass gets muted, and a
        muted check is not a check."""
        self.assertEqual(
            check_pins.check(
                pins=[pin()],
                playbook_text=lambda _: PLAYBOOK,
                dkms_status=lambda: DKMS_FIXED,
            ),
            [],
        )

    def test_the_module_missing_entirely_is_a_finding(self) -> None:
        findings = check_pins.check(
            pins=[pin()], playbook_text=lambda _: PLAYBOOK, dkms_status=lambda: "")
        self.assertEqual(len(findings), 1)
        self.assertIn("nothing installed", findings[0].text)


class TestFailuresAreFindings(unittest.TestCase):
    def test_a_probe_that_could_not_run_is_a_finding(self) -> None:
        def explode() -> str:
            raise check_pins.ResolutionError("dkms: command not found")

        findings = check_pins.check(
            pins=[pin()], playbook_text=lambda _: PLAYBOOK, dkms_status=explode)
        self.assertEqual(len(findings), 1)
        self.assertIn("command not found", findings[0].text)

    def test_an_unreadable_playbook_is_a_finding_not_a_pass(self) -> None:
        def explode(_path: str) -> str:
            raise check_pins.ResolutionError("no such file")

        findings = check_pins.check(
            pins=[pin()], playbook_text=explode, dkms_status=lambda: DKMS_FIXED)
        self.assertEqual(len(findings), 1)

    def test_an_unexpected_exception_is_still_a_finding(self) -> None:
        """This runs at login. An escaping exception takes the whole surface down,
        and a user who sees nothing cannot tell that from a healthy host."""
        def explode() -> str:
            raise OSError("permission denied")

        findings = check_pins.check(
            pins=[pin()], playbook_text=lambda _: PLAYBOOK, dkms_status=explode)
        self.assertEqual(len(findings), 1)
        self.assertIn("permission denied", findings[0].text)

    def test_the_dkms_probe_runs_at_most_once_for_several_pins(self) -> None:
        calls = []

        def counted() -> str:
            calls.append(1)
            return DKMS_FIXED

        check_pins.check(
            pins=[pin(), pin(var="displaylink_version")],
            playbook_text=lambda _: PLAYBOOK,
            dkms_status=counted,
        )
        self.assertEqual(len(calls), 1)


class TestUntrackedPinsAreSilent(unittest.TestCase):
    """A pin declared `untracked` with a reason contributes nothing.

    Each fixture keeps one TRACKED pin alongside, so these cases test what they claim
    — that an untracked pin adds nothing to a real comparison — rather than that an
    empty comparison comes back empty. `TestZeroCoverageIsItsOwnFinding` owns the case
    where nothing is tracked at all.
    """

    def test_an_untracked_pin_produces_nothing(self) -> None:
        untracked = pin(
            var="displaylink_version",
            installed={"kind": "untracked", "why": "no host-side value exists"})
        self.assertEqual(
            check_pins.check(
                pins=[pin(), untracked],
                playbook_text=lambda _: PLAYBOOK,
                dkms_status=lambda: DKMS_FIXED,
            ),
            [],
        )

    def test_an_untracked_pin_does_not_even_probe_for_itself(self) -> None:
        """Otherwise a host with no dkms would report a finding for a pin nobody
        decided to track. The tracked pin beside it is resolved from one cached probe."""
        probes: list[int] = []

        def counted() -> str:
            probes.append(1)
            return DKMS_FIXED

        untracked = pin(var="displaylink_version", installed={"kind": "untracked", "why": "x"})
        findings = check_pins.check(
            pins=[pin(), untracked], playbook_text=lambda _: PLAYBOOK, dkms_status=counted)
        self.assertEqual(findings, [])
        self.assertEqual(len(probes), 1)


class TestZeroCoverageIsItsOwnFinding(unittest.TestCase):
    """Partial coverage is a decision; zero coverage is a check that cannot fail.

    Every pin may be declared `untracked` one at a time, each with a good reason, and
    at the end of that road this check compares nothing, returns nothing, and looks
    exactly like a host whose every version matches. That is a whole drift axis gone
    quiet — on the axis the incident happened on.
    """

    @staticmethod
    def _all_untracked(count: int) -> list[manifest.Pin]:
        rows = [
            {
                "playbook": "playbooks/imports/optional/hardware-specific/play-displaylink.yml",
                "var": f"pin_{index}_version",
                "github": "DisplayLink/evdi",
                "installed": {"kind": "untracked", "why": "nothing host-side to compare"},
            }
            for index in range(count)
        ]
        return manifest.parse({"version_pins": rows}, require_installed=True)

    def test_nothing_tracked_is_reported_with_the_number(self) -> None:
        findings = check_pins.check(
            pins=self._all_untracked(9),
            playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: DKMS_FIXED,
        )
        self.assertEqual(len(findings), 1)
        self.assertIn("0 of 9", findings[0].text)

    def test_it_is_UNCHECKED_not_a_fault_on_this_host(self) -> None:
        """Nobody has shown anything wrong here. What is wrong is the coverage."""
        findings = check_pins.check(
            pins=self._all_untracked(3),
            playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: DKMS_FIXED,
        )
        self.assertFalse(findings[0].checked)

    def test_one_tracked_pin_is_enough_to_stay_silent(self) -> None:
        """The floor is one. A deliberate 1-of-9 split is the state today, and the QA
        gate is where that number is printed on every run — verbatim:

            VERSION-PINS-OK 9 pin(s), 1 with install state tracked, 8 declared
            untracked — COVERAGE: 9 of 9 resolve to a live playbook var

        The `COVERAGE:` token is a DIFFERENT population — rows resolving to a live
        playbook var, not pins whose installed version was compared — so read the
        `with install state tracked` clause for this number, not that one. A review
        pass mistook the two and concluded partial coverage went unreported anywhere.

        Kept off the login surface deliberately. Coverage is a property of the repo's
        manifest, identical on every host and not actionable by whoever is reading a
        login prompt; a permanent line there is how a surface earns being ignored.
        Zero coverage is different in kind, and `check` reports it — see this class's
        docstring."""
        pins = [pin(), *self._all_untracked(8)]
        self.assertEqual(
            check_pins.check(
                pins=pins, playbook_text=lambda _: PLAYBOOK, dkms_status=lambda: DKMS_FIXED),
            [],
        )

    def test_an_empty_manifest_is_the_manifest_validator_job_not_this_one(self) -> None:
        """`manifest.parse` already refuses an empty document, and `qa-version-pins`
        controls for it. Reporting it here too would be a second voice on one fact."""
        self.assertEqual(
            check_pins.check(
                pins=[], playbook_text=lambda _: PLAYBOOK, dkms_status=lambda: DKMS_FIXED),
            [],
        )


class TestAPinIsOnlyAboutAHostThatRanItsPlay(unittest.TestCase):
    """The rule Task 1.3 settled for freshness, applied to the axis that forgot it.

    A pin describes software that one play installs. On a host that has never run that
    play there is nothing for the pin to describe, and `compare.classify` answers a
    resolver's `None` with `ABSENT` — *"pinned 1.15.0, nothing installed"* — a fault
    nobody can act on. On a server, `evdi` is the DisplayLink module and the answer is
    permanent.

    So: applicability comes from the play ledger, exactly as freshness's does — a play
    with no record has never been run here, and silence is correct for it.
    """

    DISPLAYLINK = "playbooks/imports/optional/hardware-specific/play-displaylink.yml"

    def test_a_pin_whose_play_never_ran_here_is_silent(self) -> None:
        findings = check_pins.check(
            pins=[pin()],
            playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: "",
            ran_plays=set(),
        )
        self.assertEqual(findings, [])

    def test_a_pin_whose_play_DID_run_here_is_still_checked(self) -> None:
        """The rule must not silence the axis it was written to keep working: this is
        the incident's own state, and it has to stay a finding."""
        findings = check_pins.check(
            pins=[pin()],
            playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: DKMS_INCIDENT,
            ran_plays={self.DISPLAYLINK},
        )
        self.assertEqual(len(findings), 1)
        self.assertIn("1.14.16", findings[0].text)

    def test_an_unreadable_ledger_keeps_every_pin_applicable(self) -> None:
        """None is not the empty set. A ledger that could not be read leaves the
        question open, and an open question must never buy silence on a whole drift
        axis — the failure this plan exists for, one level up."""
        findings = check_pins.check(
            pins=[pin()],
            playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: DKMS_INCIDENT,
            ran_plays=None,
        )
        self.assertEqual(len(findings), 1)

    def test_the_dkms_probe_is_not_run_for_a_pin_that_does_not_apply(self) -> None:
        """`dkms_status` is the expensive, failing-on-a-server call. A pin filtered out
        must not pay for it — and must not turn its failure into a finding."""
        calls: list[int] = []

        def dkms() -> str:
            calls.append(1)
            raise check_pins.ResolutionError("dkms: command not found")

        self.assertEqual(
            check_pins.check(
                pins=[pin()], playbook_text=lambda _: PLAYBOOK,
                dkms_status=dkms, ran_plays=set()),
            [],
        )
        self.assertEqual(calls, [])

    def test_zero_coverage_counts_only_applicable_pins(self) -> None:
        """Otherwise the coverage finding replaces the noise it just removed: a server
        would trade one permanent line for another."""
        untracked = TestZeroCoverageIsItsOwnFinding._all_untracked(9)
        self.assertEqual(
            check_pins.check(
                pins=untracked, playbook_text=lambda _: PLAYBOOK,
                dkms_status=lambda: "", ran_plays=set()),
            [],
        )

    def test_zero_coverage_still_fires_when_the_applicable_pins_are_untracked(self) -> None:
        """The guard must survive the filter. A host that HAS run the play and tracks
        none of its pins is the case it was written for."""
        untracked = TestZeroCoverageIsItsOwnFinding._all_untracked(9)
        findings = check_pins.check(
            pins=untracked, playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: "", ran_plays={self.DISPLAYLINK})
        self.assertEqual(len(findings), 1)
        self.assertIn("0 of 9", findings[0].text)


class TestTheRealResolvers(unittest.TestCase):
    """`_rpm_version` and `_command_version`, which had no tests — so the fallbacks the
    module falls back TO were asserted to work and never exercised.

    The one that matters is ABSENT. `installed_from_dkms`'s own docstring calls a
    missing package "a real, reportable state, and the incident's own worst case", and
    `_rpm_version` is meant to return `None` for it. It could not: the error it matches
    on is built from the probe's output, and a failing `rpm -q` reports on stdout.
    """

    @staticmethod
    def _completed(returncode: int, stdout: str = "", stderr: str = ""):
        return subprocess.CompletedProcess(
            args=["probe"], returncode=returncode, stdout=stdout, stderr=stderr
        )

    def _with_probe(self, completed):
        """Patch the ONE subprocess call, so the real `_run` is what gets exercised."""
        return mock.patch.object(check_pins.subprocess, "run", return_value=completed)

    def test_an_absent_package_is_None_not_an_error(self) -> None:
        """`rpm -q` says so on STDOUT and exits non-zero, which is the shape that made
        this branch unreachable."""
        absent = self._completed(1, stdout="package nope is not installed\n")
        with self._with_probe(absent):
            self.assertIsNone(check_pins._rpm_version("nope"))

    def test_an_installed_package_returns_its_version(self) -> None:
        with self._with_probe(self._completed(0, stdout="1.14.16")):
            self.assertEqual(check_pins._rpm_version("evdi"), "1.14.16")

    def test_a_genuine_rpm_failure_still_raises(self) -> None:
        """ABSENT must not become the catch-all for every non-zero exit — that would
        report a broken rpm database as "nothing installed"."""
        with self._with_probe(self._completed(1, stderr="rpmdb: BDB0113 corrupt")):
            with self.assertRaises(check_pins.ResolutionError):
                check_pins._rpm_version("evdi")

    def test_the_probe_runs_in_a_predictable_locale(self) -> None:
        """The ABSENT check matches English text, so a translated message would make it
        miss — working on a developer's machine and failing on a user's."""
        with self._with_probe(self._completed(0, stdout="1.0")) as run:
            check_pins._rpm_version("evdi")
        self.assertEqual(run.call_args.kwargs["env"]["LC_ALL"], "C")

    def test_command_version_returns_the_output(self) -> None:
        with self._with_probe(self._completed(0, stdout="evdi 1.15.0\n")):
            self.assertEqual(check_pins._command_version("evdi"), "evdi 1.15.0")

    def test_command_version_with_no_output_is_None(self) -> None:
        with self._with_probe(self._completed(0, stdout="   \n")):
            self.assertIsNone(check_pins._command_version("evdi"))


class TestExitStatus(unittest.TestCase):
    def test_clean_is_zero_and_findings_are_not(self) -> None:
        self.assertEqual(check_pins.EXIT_OK, 0)
        self.assertNotEqual(check_pins.EXIT_FINDINGS, check_pins.EXIT_OK)


if __name__ == "__main__":
    unittest.main()
