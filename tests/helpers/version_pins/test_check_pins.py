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
import shutil
import subprocess
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import probe_results
from helpers.version_pins import check_pins, manifest

REPO_ROOT = os.path.join(os.path.dirname(__file__), "..", "..", "..")

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


class TestWhatThisHostKnowsAboutItself(unittest.TestCase):
    """Two host facts act on this check, and NEITHER narrows the population.

    A first attempt filtered every pin by the play ledger, on the theory that a pin
    describes software one play installs. It silenced this plan's founding incident:
    Task 1.3 chose no backfill, so no host has a `play-displaylink.yml` record until
    that play next runs, and `evdi_version (behind): pinned 1.15.0, installed 1.14.16`
    — the 2026-09-11 state exactly — stopped being reported on every desktop. Task 1.3's
    rule was derived for the FRESHNESS axis, where "has this play been run here" is the
    whole question. On the install-state axis it is not: an installed version that
    disagrees with the pin is drift whatever the ledger has seen.

    So the two facts are scoped to what each can actually answer:

    * `registry.present is False` — no DKMS state directory at all — makes a
      DKMS-resolved pin unanswerable here. That is an answer, and it is the whole reason
      a stock server spoke at every login. **Not** merely an empty module list: the
      `dkms` rpm owns that directory, so a DisplayLink host whose module was removed has
      the directory and an empty registry, and that is precisely what this axis exists
      to report.
    * `ran_plays` disambiguates the one ambiguous verdict, ABSENT. "Pinned 1.15.0,
      nothing installed" is a fault on a host that ran the play and expected on one
      that never did.
    """

    DISPLAYLINK = "playbooks/imports/optional/hardware-specific/play-displaylink.yml"
    NO_SUBSYSTEM = probe_results.DkmsRegistry(present=False)
    EMPTY_REGISTRY = probe_results.DkmsRegistry(present=True)

    def test_the_incident_is_reported_whatever_the_ledger_has_seen(self) -> None:
        """The regression test for the mistake above, driven across every ledger state.
        `BEHIND` means the software is here and was compared; no ledger answer can make
        that uninteresting."""
        for ran in (None, set(), {"playbooks/imports/play-python.yml"},
                    {self.DISPLAYLINK}):
            with self.subTest(ran_plays=ran):
                findings = check_pins.check(
                    pins=[pin()], playbook_text=lambda _: PLAYBOOK,
                    dkms_status=lambda: DKMS_INCIDENT, ran_plays=ran)
                self.assertEqual(len(findings), 1)
                self.assertIn("1.14.16", findings[0].text)

    def test_the_pin_playbook_and_the_ledger_key_are_the_same_spelling(self) -> None:
        """The filter is only wired if these two agree, and both are repo-relative
        paths by construction — `collector._resolve` strips the repo root, and
        `qa-version-pins.bash` requires each `playbook:` to resolve from it. Pinned
        because a check driven only by the SILENT case would pass either way."""
        real = check_pins.declared_pins(REPO_ROOT)
        tracked = [p for p in real if p.is_tracked]
        self.assertTrue(tracked, "the manifest tracks no pin, so this proves nothing")
        for candidate in tracked:
            with self.subTest(pin=candidate.var):
                self.assertFalse(candidate.playbook.startswith("/"))
                self.assertTrue(os.path.exists(
                    os.path.join(REPO_ROOT, candidate.playbook)))

    def test_absent_on_a_host_that_never_ran_the_play_is_silent(self) -> None:
        """The one verdict the ledger disambiguates. `dkms status` succeeds and simply
        does not list the module — a host with DKMS but no DisplayLink."""
        self.assertEqual(
            check_pins.check(
                pins=[pin()], playbook_text=lambda _: PLAYBOOK,
                dkms_status=lambda: f"vboxhost/7.0.14, {RUNNING_KERNEL}, x86_64: installed",
                ran_plays=set()),
            [],
        )

    def test_absent_on_a_host_that_DID_run_the_play_is_a_fault(self) -> None:
        """Software the play installed and that is now gone is exactly a finding."""
        findings = check_pins.check(
            pins=[pin()], playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: f"vboxhost/7.0.14, {RUNNING_KERNEL}, x86_64: installed",
            ran_plays={self.DISPLAYLINK})
        self.assertEqual(len(findings), 1)
        self.assertIn("nothing installed", findings[0].text)

    def test_an_unreadable_ledger_reports_absent_rather_than_assuming(self) -> None:
        """None is not the empty set. An open question must not buy silence."""
        findings = check_pins.check(
            pins=[pin()], playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: "", ran_plays=None)
        self.assertEqual(len(findings), 1)

    def test_a_host_with_no_dkms_subsystem_does_not_resolve_a_dkms_pin(self) -> None:
        """The server case, and the probe must not even be called: it is the expensive
        call, it raises "command not found" there, and that raise is what produced the
        permanent "could not be checked" line at every login."""
        calls: list[int] = []

        def dkms() -> str:
            calls.append(1)
            raise check_pins.ResolutionError("dkms: command not found")

        self.assertEqual(
            check_pins.check(
                pins=[pin()], playbook_text=lambda _: PLAYBOOK,
                dkms_status=dkms, registry=self.NO_SUBSYSTEM),
            [],
        )
        self.assertEqual(calls, [])

    def test_a_dkms_directory_with_no_modules_STILL_reports_the_missing_module(self) -> None:
        """H4. The `dkms` rpm owns `/var/lib/dkms`, so every host that ran
        `play-displaylink.yml` has the directory — and an emptied registry there means
        the module was removed, which is exactly what this axis exists to say. Task 0.2
        of this plan sets out to create that state. Skipping on "no modules" instead of
        "no directory" silences it, and one unrelated module would have masked the bug."""
        findings = check_pins.check(
            pins=[pin()], playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: "", registry=self.EMPTY_REGISTRY,
            ran_plays={self.DISPLAYLINK})
        self.assertEqual(len(findings), 1)
        self.assertIn("nothing installed", findings[0].text)

    def test_an_unreadable_dkms_state_directory_still_resolves_the_pin(self) -> None:
        """`present is None` is "could not tell", and must not be folded in with "there
        is no DKMS here" — the same tri-state `probe_results.build_report` keeps."""
        findings = check_pins.check(
            pins=[pin()], playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: DKMS_INCIDENT,
            registry=probe_results.DkmsRegistry(present=None))
        self.assertEqual(len(findings), 1)

    def test_a_host_WITH_dkms_modules_resolves_the_pin(self) -> None:
        findings = check_pins.check(
            pins=[pin()], playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: DKMS_INCIDENT,
            registry=probe_results.DkmsRegistry(present=True, modules=("evdi",)))
        self.assertEqual(len(findings), 1)

    def test_a_non_dkms_pin_is_untouched_by_the_dkms_answer(self) -> None:
        """The rule is about one resolver, not about this host in general."""
        rpm_pin = pin(installed={"kind": "rpm", "name": "displaylink"})
        findings = check_pins.check(
            pins=[rpm_pin], playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: "", registry=self.NO_SUBSYSTEM,
            rpm_version=lambda _: "1.14.16")
        self.assertEqual(len(findings), 1)

    def test_a_command_that_is_not_installed_resolves_to_absent_not_an_exception(self) -> None:
        """H6. `_command_version` had no ABSENT branch, so a missing command raised, the
        broad `except` turned it into an *unchecked* finding, and the original permanent
        noise returned through the one resolver the ABSENT scoping cannot reach — it
        never gets as far as `classify`. Not live today, and invisible when it stops
        being true."""
        command_pin = pin(installed={"kind": "command", "name": "there-is-no-such-cmd"})
        self.assertEqual(
            check_pins.check(
                pins=[command_pin], playbook_text=lambda _: PLAYBOOK,
                dkms_status=lambda: "", ran_plays=set()),
            [],
        )

    def test_zero_coverage_counts_the_whole_manifest_again(self) -> None:
        """Counting only a host-narrowed population left the population empty and
        skipped the guard entirely, so such a host produced NO output at all —
        indistinguishable from every pin matching. The guard is about the manifest,
        which is identical on every host."""
        untracked = TestZeroCoverageIsItsOwnFinding._all_untracked(9)
        findings = check_pins.check(
            pins=untracked, playbook_text=lambda _: PLAYBOOK,
            dkms_status=lambda: "", ran_plays=set(), registry=self.NO_SUBSYSTEM)
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

    def test_the_absent_phrase_about_ANOTHER_package_does_not_count(self) -> None:
        """Matched against the package this call asked about, on the stream rpm uses.
        A phrase found anywhere in a merged blob would resolve someone else's absence
        as this pin's — and ABSENT renders as "pinned X, nothing installed"."""
        with self._with_probe(
                self._completed(1, stdout="package other-thing is not installed\n")):
            with self.assertRaises(check_pins.ResolutionError):
                check_pins._rpm_version("evdi")

    def test_the_absent_phrase_on_STDERR_does_not_count(self) -> None:
        """rpm reports it on stdout. On stderr it came from something else."""
        with self._with_probe(
                self._completed(1, stderr="package evdi is not installed")):
            with self.assertRaises(check_pins.ResolutionError):
                check_pins._rpm_version("evdi")

    def test_a_missing_command_is_NotInstalled_established_by_the_OS(self) -> None:
        """The absence this can answer structurally: the exec itself failed."""
        with mock.patch.object(
                check_pins.subprocess, "run", side_effect=FileNotFoundError()):
            self.assertIsNone(check_pins._command_version("there-is-no-such-cmd"))

    def test_a_tool_that_RAN_and_printed_command_not_found_is_not_absent(self) -> None:
        """THE conflation. A wrapper script that exists, exits non-zero and reports that
        phrase about something INSIDE itself is a broken tool, not an absent one — and
        reading it as absent renders "pinned X, nothing installed", a confident claim
        about the host from a probe that failed. Discriminated by exception TYPE, which
        is what `probe.run_probe` already does with `ProbeOutcome.missing`."""
        broken_wrapper = self._completed(127, stderr="inner-thing: command not found")
        with self._with_probe(broken_wrapper):
            with self.assertRaises(check_pins.ResolutionError):
                check_pins._command_version("wrapper")

    def test_an_installed_binary_off_this_PATH_reaches_the_same_verdict(self) -> None:
        """The limit of what `exec` establishes, pinned so the claim cannot quietly
        widen again. `/usr/bin/env` is on disk; under a narrow PATH the resolver still
        answers "absent", which renders as "pinned X, nothing installed".

        Not closable with `shutil.which` — measured: it consults the same PATH and
        returns None for the same input. The consumer is a systemd --user unit, whose
        PATH is narrower than the shell an operator tests in, so the docstrings say
        "did not resolve on this process's PATH" rather than "is not installed"."""
        self.assertTrue(os.path.exists("/usr/bin/env"), "fixture assumes a real binary")
        with mock.patch.dict(os.environ, {"PATH": "/nonexistent"}, clear=False):
            self.assertIsNone(check_pins._command_version("env"))
            self.assertIsNone(shutil.which("env"))

    def test_NotInstalled_is_still_a_ResolutionError(self) -> None:
        """The type exists to let a caller be MORE specific, never to let one escape the
        broad handler in `check` that keeps a login shell from seeing a traceback."""
        self.assertTrue(issubclass(check_pins.NotInstalled, check_pins.ResolutionError))

    def test_the_failure_carries_the_structure_a_caller_needs(self) -> None:
        """Discriminating on a substring of a merged blob is what produced the two cases
        above; the streams and the exit status are kept apart so a caller need not."""
        with self._with_probe(self._completed(3, stdout="out", stderr="err")):
            with self.assertRaises(check_pins.ResolutionError) as caught:
                check_pins._command_version("thing")
        self.assertEqual(caught.exception.returncode, 3)
        self.assertEqual(caught.exception.stdout, "out")
        self.assertEqual(caught.exception.stderr, "err")

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
