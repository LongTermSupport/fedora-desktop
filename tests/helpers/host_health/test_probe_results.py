"""Tests for helpers.host_health.probe_results — Plan 00109 Task 3.1's classifier.

Pure parsing and classification of what a post-boot probe collects. The executor
runs `dkms status` and `systemctl --failed`; everything it learns is an argument
here.

Two rules carry this plan's weight and are pinned throughout:

1. **Silent when clean.** A health check that speaks every login gets muted, and a
   muted check is not a check.
2. **A probe that could not run is a FINDING, not a pass.** `dkms` absent,
   `systemctl` unavailable, output in a shape this cannot read — each is reported.
   The incident this plan exists for was a green report over a broken host.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import probe_results

RUNNING_KERNEL = "7.2.4-200.fc44.x86_64"

#: Most tests care about one probe at a time; the other two are a clean nothing.
NO_UNITS = probe_results.ProbeOutcome(ok=True, text="", error="")

DKMS_HEALTHY = f"evdi/1.15.0, {RUNNING_KERNEL}, x86_64: installed"
DKMS_OTHER_KERNEL_ONLY = "evdi/1.15.0, 7.1.9-200.fc44.x86_64, x86_64: installed"
DKMS_ADDED_NOT_BUILT = "evdi/1.14.16: added"


class TestParseDkms(unittest.TestCase):
    def test_an_installed_module_for_the_running_kernel(self) -> None:
        entries = probe_results.parse_dkms(DKMS_HEALTHY)
        self.assertEqual(len(entries), 1)
        self.assertEqual(entries[0].module, "evdi")
        self.assertEqual(entries[0].version, "1.15.0")
        self.assertEqual(entries[0].kernel, RUNNING_KERNEL)
        self.assertEqual(entries[0].state, "installed")

    def test_an_added_but_unbuilt_module_has_no_kernel(self) -> None:
        """This is the shape a failed autoinstall leaves behind."""
        entries = probe_results.parse_dkms(DKMS_ADDED_NOT_BUILT)
        self.assertEqual(entries[0].module, "evdi")
        self.assertEqual(entries[0].version, "1.14.16")
        self.assertIsNone(entries[0].kernel)
        self.assertEqual(entries[0].state, "added")

    def test_several_lines_parse_independently(self) -> None:
        entries = probe_results.parse_dkms(f"{DKMS_HEALTHY}\n{DKMS_ADDED_NOT_BUILT}\n")
        self.assertEqual(len(entries), 2)

    def test_blank_lines_are_skipped(self) -> None:
        self.assertEqual(len(probe_results.parse_dkms(f"\n{DKMS_HEALTHY}\n\n")), 1)

    def test_empty_output_is_no_entries_not_an_error(self) -> None:
        """A host with no DKMS modules is a real, healthy state."""
        self.assertEqual(probe_results.parse_dkms(""), [])

    def test_a_trailing_warning_does_not_break_the_state(self) -> None:
        line = f"{DKMS_HEALTHY} (WARNING! Diff between built and installed module!)"
        self.assertEqual(probe_results.parse_dkms(line)[0].state, "installed")

    def test_an_unreadable_line_raises(self) -> None:
        """Skipping it would drop a module from the population silently, and a module
        that is not looked at reports as healthy."""
        with self.assertRaises(ValueError):
            probe_results.parse_dkms("this is not dkms output")


class TestDkmsFindings(unittest.TestCase):
    def test_a_module_installed_for_the_running_kernel_is_clean(self) -> None:
        findings = probe_results.dkms_findings(
            probe_results.parse_dkms(DKMS_HEALTHY), running_kernel=RUNNING_KERNEL)
        self.assertEqual(findings, [])

    def test_a_module_built_only_for_ANOTHER_kernel_is_a_finding(self) -> None:
        """The incident exactly: the module existed, just not for the kernel that
        booted, so `dkms status` was not empty and nothing noticed."""
        findings = probe_results.dkms_findings(
            probe_results.parse_dkms(DKMS_OTHER_KERNEL_ONLY), running_kernel=RUNNING_KERNEL)
        self.assertEqual(len(findings), 1)
        self.assertIn("evdi", findings[0])
        self.assertIn(RUNNING_KERNEL, findings[0])

    def test_an_added_but_never_built_module_is_a_finding(self) -> None:
        findings = probe_results.dkms_findings(
            probe_results.parse_dkms(DKMS_ADDED_NOT_BUILT), running_kernel=RUNNING_KERNEL)
        self.assertEqual(len(findings), 1)

    def test_one_good_kernel_entry_clears_the_module(self) -> None:
        """A module built for several kernels is healthy if the RUNNING one is among
        them; the stale entries are normal and must not be nagged about."""
        entries = probe_results.parse_dkms(f"{DKMS_OTHER_KERNEL_ONLY}\n{DKMS_HEALTHY}")
        self.assertEqual(probe_results.dkms_findings(entries, running_kernel=RUNNING_KERNEL), [])

    def test_no_modules_at_all_is_clean(self) -> None:
        self.assertEqual(probe_results.dkms_findings([], running_kernel=RUNNING_KERNEL), [])

    def test_findings_are_sorted_by_module(self) -> None:
        entries = probe_results.parse_dkms("zmod/1.0: added\namod/1.0: added")
        findings = probe_results.dkms_findings(entries, running_kernel=RUNNING_KERNEL)
        self.assertTrue(findings[0].startswith("amod"))


class TestFailedUnits(unittest.TestCase):
    def test_no_failed_units_is_clean(self) -> None:
        self.assertEqual(probe_results.failed_unit_findings("", scope="system"), [])

    def test_a_failed_unit_is_named_with_its_scope(self) -> None:
        line = "displaylink-suspend.service loaded failed failed DisplayLink suspend"
        findings = probe_results.failed_unit_findings(line, scope="system")
        self.assertEqual(len(findings), 1)
        self.assertIn("displaylink-suspend.service", findings[0])
        self.assertIn("system", findings[0])

    def test_the_user_scope_is_distinguished(self) -> None:
        line = "wsi.service loaded failed failed Speech to text"
        findings = probe_results.failed_unit_findings(line, scope="user")
        self.assertIn("user", findings[0])

    def test_several_units_each_produce_a_finding(self) -> None:
        text = ("a.service loaded failed failed A\n"
                "b.timer loaded failed failed B\n")
        self.assertEqual(len(probe_results.failed_unit_findings(text, scope="system")), 2)

    def test_blank_lines_are_ignored(self) -> None:
        self.assertEqual(probe_results.failed_unit_findings("\n  \n", scope="system"), [])

    def test_a_leading_bullet_is_stripped(self) -> None:
        """systemctl prefixes a failed unit with a Unicode bullet on a TTY."""
        line = "● broken.service loaded failed failed Broken"
        findings = probe_results.failed_unit_findings(line, scope="system")
        self.assertIn("broken.service", findings[0])
        self.assertNotIn("●", findings[0])


class TestProbeFailuresAreFindings(unittest.TestCase):
    def test_a_probe_that_could_not_run_is_a_finding(self) -> None:
        """Not a skip. The whole plan exists because a green report covered a broken
        host, so 'I could not look' must never read as 'nothing wrong'."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=False, text="", error="dkms: command not found"),
            failed_system=NO_UNITS, failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertFalse(report.clean)
        self.assertTrue(any("dkms" in f for f in report.texts))

    def test_the_error_text_reaches_the_report(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=False, text="", error="dkms: command not found"),
            failed_system=NO_UNITS, failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertTrue(any("command not found" in f for f in report.texts))

    def test_unreadable_dkms_output_is_a_finding_not_a_crash(self) -> None:
        """parse_dkms raises; the report must turn that into a finding rather than
        letting it take the whole login-time probe down."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text="garbage", error=""),
            failed_system=NO_UNITS, failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertFalse(report.clean)

    def test_a_failed_SYSTEM_unit_probe_is_a_finding(self) -> None:
        """The same rule as dkms, and it was not enforced here at first: a systemctl
        that cannot run reported an empty failed-unit list, which is exactly the
        shape of a healthy host."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_HEALTHY, error=""),
            failed_system=probe_results.ProbeOutcome(
                ok=False, text="", error="Failed to connect to bus"),
            failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertFalse(report.clean)
        self.assertTrue(any("system" in f and "Failed to connect" in f for f in report.texts))

    def test_a_failed_USER_unit_probe_is_a_finding_naming_its_scope(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_HEALTHY, error=""),
            failed_system=NO_UNITS,
            failed_user=probe_results.ProbeOutcome(
                ok=False, text="", error="Failed to connect to bus"),
            running_kernel=RUNNING_KERNEL)
        self.assertFalse(report.clean)
        self.assertTrue(any("user" in f for f in report.texts))

    def test_every_probe_failing_produces_every_finding(self) -> None:
        """One broken probe must not mask the other two."""
        broken = probe_results.ProbeOutcome(ok=False, text="", error="nope")
        report = probe_results.build_report(
            dkms=broken, failed_system=broken, failed_user=broken,
            running_kernel=RUNNING_KERNEL)
        self.assertEqual(len(report.findings), 3)


class TestAHostWithNoDkmsSubsystem(unittest.TestCase):
    """A stock server has no `dkms`, and reporting that for ever is how this gets muted.

    `dkms` is installed by exactly two plays, both optional and both desktop hardware
    (`play-displaylink.yml`, `play-virtualbox-windows.yml`), so a server provisioned by
    `playbook-main.yml` has neither the command nor a module tree. Before this rule, every
    interactive login on such a host printed "this machine needs attention" plus a line
    that could never become actionable.

    The distinction that makes staying silent honest: no command AND no registered
    modules is a positive finding of nothing. Either half alone is not.
    """

    MISSING = probe_results.ProbeOutcome(
        ok=False, text="", error="dkms: command not found (dkms status)", missing=True)

    def test_no_command_and_no_modules_is_silent(self) -> None:
        report = probe_results.build_report(
            dkms=self.MISSING, failed_system=NO_UNITS, failed_user=NO_UNITS,
            running_kernel=RUNNING_KERNEL, dkms_registered=[])
        self.assertTrue(report.clean)

    def test_no_command_but_registered_modules_is_reported(self) -> None:
        """The worse state than either half: nothing will rebuild them for the next
        kernel, and whether they are built for this one cannot be established."""
        report = probe_results.build_report(
            dkms=self.MISSING, failed_system=NO_UNITS, failed_user=NO_UNITS,
            running_kernel=RUNNING_KERNEL, dkms_registered=["evdi"])
        self.assertFalse(report.clean)
        self.assertTrue(any("evdi" in text for text in report.texts))

    def test_an_unreadable_state_directory_is_not_read_as_absence(self) -> None:
        """None means the question is open. Folding it in with `[]` would let a
        permission problem on /var/lib/dkms buy permanent silence on the axis this
        plan's incident happened on."""
        report = probe_results.build_report(
            dkms=self.MISSING, failed_system=NO_UNITS, failed_user=NO_UNITS,
            running_kernel=RUNNING_KERNEL, dkms_registered=None)
        self.assertFalse(report.clean)

    def test_omitting_the_parameter_keeps_the_old_noisy_verdict(self) -> None:
        """The default is the conservative one. A caller that forgets this argument gets
        "could not be checked" — never silence — so the omission cannot hide anything."""
        report = probe_results.build_report(
            dkms=self.MISSING, failed_system=NO_UNITS, failed_user=NO_UNITS,
            running_kernel=RUNNING_KERNEL)
        self.assertFalse(report.clean)

    def test_a_command_that_ran_and_failed_is_still_a_finding(self) -> None:
        """`missing` is the whole discriminator. A dkms that exists and returned an
        error says nothing about whether this host has a DKMS subsystem, so an empty
        module list must not silence it."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(
                ok=False, text="", error="dkms: permission denied", missing=False),
            failed_system=NO_UNITS, failed_user=NO_UNITS,
            running_kernel=RUNNING_KERNEL, dkms_registered=[])
        self.assertFalse(report.clean)

    def test_the_other_two_probes_are_still_judged(self) -> None:
        """Silencing dkms must not silence the report."""
        report = probe_results.build_report(
            dkms=self.MISSING,
            failed_system=probe_results.ProbeOutcome(
                ok=True, text="sshd.service loaded failed failed", error=""),
            failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL, dkms_registered=[])
        self.assertFalse(report.clean)
        self.assertTrue(any("sshd.service" in text for text in report.texts))


class TestFindingsSayWhetherTheCheckLooked(unittest.TestCase):
    """`Finding.checked` is the producer's own answer, carried in the data.

    The consumer that needs it — the handoff file, and next the panel — cannot
    recover it from the wording: two substrings covered seven of the messages the
    three checks emit and missed six, and every one of the six then read as a known
    fault. So each producer marks its own, and this pins that it does.
    """

    def test_a_probe_that_could_not_run_is_UNCHECKED(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=False, text="", error="dkms: not found"),
            failed_system=NO_UNITS, failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertEqual([f.checked for f in report.findings], [False])

    def test_a_real_fault_is_CHECKED(self) -> None:
        """The incident's own shape: dkms ran, and said the module is not built."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text="evdi/1.14.16: added", error=""),
            failed_system=NO_UNITS, failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertFalse(report.clean)
        self.assertEqual([f.checked for f in report.findings], [True])

    def test_a_failed_unit_is_CHECKED_and_a_failed_unit_PROBE_is_not(self) -> None:
        """Both are findings and they are not the same kind of finding: one is a fault
        on this host, the other is the fact that nobody could ask."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_HEALTHY, error=""),
            failed_system=probe_results.ProbeOutcome(
                ok=True, text="a.service loaded failed failed A", error=""),
            failed_user=probe_results.ProbeOutcome(ok=False, text="", error="no bus"),
            running_kernel=RUNNING_KERNEL)
        by_kind = {f.checked for f in report.findings}
        self.assertEqual(by_kind, {True, False})

    def test_unreadable_output_is_UNCHECKED_not_a_fault(self) -> None:
        """Output nothing could parse means the DKMS state is unknown — not healthy,
        and not established as broken either."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text="garbage", error=""),
            failed_system=NO_UNITS, failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertEqual([f.checked for f in report.findings], [False])

    def test_the_constructors_are_the_readable_way_to_say_it(self) -> None:
        self.assertTrue(probe_results.broken("x").checked)
        self.assertFalse(probe_results.unchecked("x").checked)
        self.assertEqual(probe_results.broken("x").text, "x")


class TestReport(unittest.TestCase):
    def test_a_healthy_host_produces_NO_findings(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_HEALTHY, error=""),
            failed_system=NO_UNITS, failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertTrue(report.clean)
        self.assertEqual(report.findings, ())

    def test_findings_from_every_source_are_collected(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_ADDED_NOT_BUILT, error=""),
            failed_system=probe_results.ProbeOutcome(
                ok=True, text="a.service loaded failed failed A", error=""),
            failed_user=probe_results.ProbeOutcome(
                ok=True, text="b.service loaded failed failed B", error=""),
            running_kernel=RUNNING_KERNEL)
        self.assertEqual(len(report.findings), 3)

    def test_a_healthy_host_produces_a_clean_report(self) -> None:
        """This report covers the host probes and nothing else. Play-freshness and
        installed-vs-pinned findings merge in `login_report.collect`, which can guard
        each check on its own — so a raising one becomes a finding instead of taking
        this report down."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_HEALTHY, error=""),
            failed_system=NO_UNITS, failed_user=NO_UNITS, running_kernel=RUNNING_KERNEL)
        self.assertTrue(report.clean)
        self.assertEqual(report.findings, ())


if __name__ == "__main__":
    unittest.main()
