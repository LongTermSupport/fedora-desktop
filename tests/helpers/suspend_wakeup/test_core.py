"""Unit tests for helpers/suspend_wakeup/core.py — did the wakeup policy apply?

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.suspend_wakeup.test_core

suspend_wakeup is a namespace package (no __init__.py); we put the repo root on
sys.path so `from helpers.suspend_wakeup import core` resolves. The sys.path edit
before the import is why ruff E402 is ignored for tests/** in ruff.toml.

The case that motivates the helper (Plan 00104, round-3 finding B2): the assertion
was a `grep -l '^enabled$'` over three hardcoded sysfs paths, and grep exits 2 when a
path does not exist. `failed_when: rc != 1` therefore turned "this machine has no AC
or UCSI device" into a fatal error that aborted the entire provisioning run. The form
before it had the mirror defect — it reported "all disarmed" on a host with zero
devices, i.e. blind reported as clean. Neither stated its population, which is why the
empty-host cases below are the point of the module.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.suspend_wakeup import core


class TestIsPolicyTarget(unittest.TestCase):
    """Mirrors the udev rule: KERNEL=="AC" or "ucsi-source-psy-*" in power_supply."""

    def test_ac_is_a_target(self):
        self.assertTrue(core.is_policy_target("AC"))

    def test_ucsi_source_devices_are_targets(self):
        self.assertTrue(core.is_policy_target("ucsi-source-psy-USBC000:001"))
        self.assertTrue(core.is_policy_target("ucsi-source-psy-USBC000:002"))

    def test_battery_is_not_a_target(self):
        self.assertFalse(core.is_policy_target("BAT0"))

    def test_unrelated_supply_is_not_a_target(self):
        self.assertFalse(core.is_policy_target("hidpp_battery_0"))

    def test_ucsi_sink_is_not_a_target(self):
        """The rule names the SOURCE psy devices only."""
        self.assertFalse(core.is_policy_target("ucsi-sink-psy-USBC000:001"))


class TestEvaluate(unittest.TestCase):
    def test_all_targets_disarmed_passes(self):
        result = core.evaluate(
            {"AC": "disabled", "ucsi-source-psy-USBC000:001": "disabled", "BAT0": None}
        )
        self.assertTrue(result.ok)
        self.assertEqual(result.still_armed, [])
        self.assertEqual(result.total, 2)
        self.assertEqual(result.disarmed, 2)

    def test_one_target_still_armed_fails_and_names_it(self):
        result = core.evaluate({"AC": "enabled", "ucsi-source-psy-USBC000:001": "disabled"})
        self.assertFalse(result.ok)
        self.assertEqual(result.still_armed, ["AC"])
        self.assertEqual(result.disarmed, 1)
        self.assertEqual(result.total, 2)

    def test_host_with_no_target_devices_passes(self):
        """THE REGRESSION. A desktop with no AC/UCSI device must not fail the run."""
        result = core.evaluate({"BAT0": "disabled"})
        self.assertTrue(result.ok)
        self.assertEqual(result.total, 0)
        self.assertEqual(result.still_armed, [])

    def test_completely_empty_host_passes(self):
        result = core.evaluate({})
        self.assertTrue(result.ok)
        self.assertEqual(result.total, 0)

    def test_unreadable_target_is_not_counted_as_disarmed(self):
        """An attribute we could not read is not evidence the policy applied."""
        result = core.evaluate({"AC": None})
        self.assertEqual(result.total, 1)
        self.assertEqual(result.disarmed, 0)
        self.assertEqual(result.unverifiable, ["AC"])
        self.assertFalse(result.ok)

    def test_non_target_armed_device_is_ignored(self):
        """The policy never touches BAT0, so its state must not affect the verdict."""
        result = core.evaluate({"AC": "disabled", "BAT0": "enabled"})
        self.assertTrue(result.ok)
        self.assertEqual(result.still_armed, [])

    def test_trailing_newline_from_sysfs_is_tolerated(self):
        """sysfs reads carry a trailing newline; a raw compare would miss 'enabled'."""
        result = core.evaluate({"AC": "enabled\n"})
        self.assertFalse(result.ok)
        self.assertEqual(result.still_armed, ["AC"])

    def test_unrecognised_value_is_unverifiable_not_disarmed(self):
        """Anything that is not the two known words is a state we cannot vouch for.

        A deny-list ("not 'enabled' means disarmed") passes an empty read, a truncated
        read and a garbage read as a clean result — the module's own stated failure mode,
        reporting blind as clean, surviving at per-device granularity.
        """
        for value in ("", "   \n", "\n", "potato\n", "ENABLED\n", "disable\n"):
            with self.subTest(value=value):
                result = core.evaluate({"AC": value})
                self.assertEqual(result.disarmed, 0)
                self.assertEqual(result.unverifiable, ["AC"])
                self.assertFalse(result.ok)

    def test_only_the_exact_word_disabled_counts_as_disarmed(self):
        """The positive case is an allow-list of one word, plus sysfs's trailing newline."""
        for value in ("disabled", "disabled\n"):
            with self.subTest(value=value):
                result = core.evaluate({"AC": value})
                self.assertEqual(result.disarmed, 1)
                self.assertTrue(result.ok)


class TestSummary(unittest.TestCase):
    def test_summary_states_the_population_when_devices_exist(self):
        result = core.evaluate({"AC": "disabled", "ucsi-source-psy-USBC000:001": "disabled"})
        self.assertEqual(result.summary(), "COVERAGE: 2 of 2 power-delivery devices disarmed")

    def test_summary_is_explicit_about_an_empty_population(self):
        """'0 of 0' must READ as 'nothing to do here', never as a silent pass."""
        result = core.evaluate({"BAT0": "disabled"})
        self.assertEqual(
            result.summary(),
            "COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host",
        )

    def test_summary_names_the_offenders_on_failure(self):
        result = core.evaluate({"AC": "enabled", "ucsi-source-psy-USBC000:001": "disabled"})
        self.assertIn("1 of 2", result.summary())
        self.assertIn("AC", result.summary())

    def test_summary_distinguishes_still_armed_from_unverifiable(self):
        """The two failure kinds need different fixes, so the line must not merge them."""
        result = core.evaluate({"AC": "enabled", "ucsi-source-psy-USBC000:001": "potato"})
        self.assertIn("STILL ARMED: AC", result.summary())
        self.assertIn("UNVERIFIABLE: ucsi-source-psy-USBC000:001", result.summary())


if __name__ == "__main__":
    unittest.main()
