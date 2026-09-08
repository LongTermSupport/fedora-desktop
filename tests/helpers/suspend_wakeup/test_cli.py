"""Unit tests for helpers/suspend_wakeup/cli.py — the sysfs read, against real files.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.suspend_wakeup.test_cli

These use tmp directories rather than mocks because the behaviour under test is
precisely how the filesystem answers, and every defect this module has had so far came
from guessing that rather than measuring it.

THE DISTINCTION THAT MATTERS: a `power/wakeup` attribute that is ABSENT means the device
is not wakeup-capable — there is nothing to disarm and nothing wrong. An attribute that
EXISTS but cannot be read is a real problem. Collapsing the two makes a perfectly healthy
host hard-fail its whole provisioning run, which is the same defect class as the `grep -l`
that exited 2 on a missing path.
"""

from __future__ import annotations

import contextlib
import io
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.suspend_wakeup import cli


def _make_device(root: pathlib.Path, name: str, wakeup: str | None) -> None:
    """Create <root>/<name>/power/, with a wakeup file only if `wakeup` is not None."""
    power = root / name / "power"
    power.mkdir(parents=True)
    if wakeup is not None:
        (power / "wakeup").write_text(wakeup)


class TestReadWakeupStates(unittest.TestCase):
    def test_reads_the_attribute_when_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", "disabled\n")
            states = cli.read_wakeup_states(str(root))
            self.assertEqual(states, {"AC": "disabled\n"})

    def test_device_without_a_wakeup_attribute_is_omitted_entirely(self):
        """Not wakeup-capable is not a fault — there is nothing to disarm.

        BAT0 on the reference host genuinely has no power/wakeup. Reporting such a
        device as UNVERIFIABLE would hard-fail the run on a healthy machine.
        """
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "BAT0", None)
            self.assertEqual(cli.read_wakeup_states(str(root)), {})

    def test_a_target_device_without_the_attribute_is_also_omitted(self):
        """The same must hold for a device the policy targets, or it fails the run."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", None)
            self.assertEqual(cli.read_wakeup_states(str(root)), {})

    def test_unreadable_attribute_is_reported_as_None(self):
        """Present but unreadable is a genuine problem and must NOT be silently dropped."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", "disabled\n")
            # A directory where a file is expected: exists, but reading it raises
            # IsADirectoryError (an OSError that is not FileNotFoundError).
            (root / "AC" / "power" / "wakeup").unlink()
            (root / "AC" / "power" / "wakeup").mkdir()
            self.assertEqual(cli.read_wakeup_states(str(root)), {"AC": None})

    def test_absent_power_supply_directory_yields_an_empty_mapping(self):
        with tempfile.TemporaryDirectory() as tmp:
            missing = str(pathlib.Path(tmp) / "definitely-not-here")
            self.assertEqual(cli.read_wakeup_states(missing), {})

    def test_a_dangling_device_symlink_is_unverifiable_not_dropped(self):
        """A device that vanished mid-enumeration must stay in the population.

        `read_text` on a dangling symlink raises FileNotFoundError, which the
        not-wakeup-capable branch swallows — so the device disappears from the COVERAGE
        line entirely and the run reports a clean, smaller population. Under-matches are
        silent, which is exactly the failure this module exists to prevent, so resolve
        the device itself before blaming its attribute.
        """
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            os.symlink(root / "nowhere", root / "AC")
            self.assertEqual(cli.read_wakeup_states(str(root)), {"AC": None})

    def test_mixed_host_is_classified_correctly(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", "enabled\n")
            _make_device(root, "ucsi-source-psy-USBC000:001", "disabled\n")
            _make_device(root, "BAT0", None)
            states = cli.read_wakeup_states(str(root))
            self.assertEqual(
                states,
                {"AC": "enabled\n", "ucsi-source-psy-USBC000:001": "disabled\n"},
            )


class TestMain(unittest.TestCase):
    """`main` prints the COVERAGE line, so every case here captures stdout.

    Capturing keeps fixture output out of the gate's own report, where a COVERAGE line
    printed after `OK` reads like a finding from the run rather than a test's payload.
    It also lets each case assert what was SAID, not merely the exit code — the line is
    the payload, and an exit code alone cannot catch it going wrong.
    """

    def _run(self, root: pathlib.Path) -> tuple[int, str]:
        buffer = io.StringIO()
        with contextlib.redirect_stdout(buffer):
            code = cli.main(["--power-supply-dir", str(root)])
        return code, buffer.getvalue()

    def test_exits_zero_on_a_host_with_no_target_devices(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "BAT0", "disabled\n")
            code, out = self._run(root)
            self.assertEqual(code, 0)
            self.assertIn("no power-delivery wakeup devices on this host", out)

    def test_exits_zero_when_every_target_is_disarmed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", "disabled\n")
            _make_device(root, "ucsi-source-psy-USBC000:001", "disabled\n")
            code, out = self._run(root)
            self.assertEqual(code, 0)
            self.assertIn("COVERAGE: 2 of 2", out)

    def test_exits_nonzero_when_a_target_is_still_armed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", "enabled\n")
            code, out = self._run(root)
            self.assertEqual(code, 1)
            self.assertIn("STILL ARMED: AC", out)

    def test_exits_zero_when_a_target_device_is_not_wakeup_capable(self):
        """A healthy host must not be failed for hardware it simply does not have."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", None)
            code, _ = self._run(root)
            self.assertEqual(code, 0)

    def test_exits_nonzero_on_a_value_it_cannot_interpret(self):
        """A garbage read must not be reported as a disarmed device."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", "")
            code, out = self._run(root)
            self.assertEqual(code, 1)
            self.assertIn("UNVERIFIABLE: AC", out)


if __name__ == "__main__":
    unittest.main()
