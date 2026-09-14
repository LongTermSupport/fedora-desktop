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
            failed_system="", failed_user="", running_kernel=RUNNING_KERNEL)
        self.assertFalse(report.clean)
        self.assertTrue(any("dkms" in f for f in report.findings))

    def test_the_error_text_reaches_the_report(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=False, text="", error="dkms: command not found"),
            failed_system="", failed_user="", running_kernel=RUNNING_KERNEL)
        self.assertTrue(any("command not found" in f for f in report.findings))

    def test_unreadable_dkms_output_is_a_finding_not_a_crash(self) -> None:
        """parse_dkms raises; the report must turn that into a finding rather than
        letting it take the whole login-time probe down."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text="garbage", error=""),
            failed_system="", failed_user="", running_kernel=RUNNING_KERNEL)
        self.assertFalse(report.clean)


class TestReport(unittest.TestCase):
    def test_a_healthy_host_produces_NO_findings(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_HEALTHY, error=""),
            failed_system="", failed_user="", running_kernel=RUNNING_KERNEL)
        self.assertTrue(report.clean)
        self.assertEqual(report.findings, ())

    def test_findings_from_every_source_are_collected(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_ADDED_NOT_BUILT, error=""),
            failed_system="a.service loaded failed failed A",
            failed_user="b.service loaded failed failed B",
            running_kernel=RUNNING_KERNEL)
        self.assertEqual(len(report.findings), 3)

    def test_extra_findings_from_phase_2_are_merged_in(self) -> None:
        """Play-freshness and installed-vs-pinned are separate checks; this is where
        their findings join the login report rather than being a second notification."""
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_HEALTHY, error=""),
            failed_system="", failed_user="", running_kernel=RUNNING_KERNEL,
            extra=["evdi: pinned 1.15.0, installed 1.14.16"])
        self.assertFalse(report.clean)
        self.assertIn("evdi: pinned 1.15.0, installed 1.14.16", report.findings)

    def test_no_extra_findings_keeps_a_clean_host_silent(self) -> None:
        report = probe_results.build_report(
            dkms=probe_results.ProbeOutcome(ok=True, text=DKMS_HEALTHY, error=""),
            failed_system="", failed_user="", running_kernel=RUNNING_KERNEL, extra=[])
        self.assertTrue(report.clean)


if __name__ == "__main__":
    unittest.main()
