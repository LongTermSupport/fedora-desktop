"""Unit tests for helpers/suspend_wakeup/cli.py — the sysfs read, against real files.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.suspend_wakeup.test_cli

These use tmp directories rather than mocks because the behaviour under test is
precisely how the filesystem answers, and every defect this module has had so far came
from guessing that rather than measuring it.

THE DISTINCTION THAT MATTERS (Plan 00104, round-4 finding 2): a `power/wakeup`
attribute that is ABSENT means the device is not wakeup-capable — there is nothing to
disarm and nothing wrong. An attribute that EXISTS but cannot be read is a real
problem. Collapsing the two makes a perfectly healthy host hard-fail its whole
provisioning run, which is the same defect class as the `grep -l` that exited 2 on a
missing path.
"""

from __future__ import annotations

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
        device as UNREADABLE would hard-fail the run on a healthy machine.
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
    def test_exits_zero_on_a_host_with_no_target_devices(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "BAT0", "disabled\n")
            self.assertEqual(cli.main(["--power-supply-dir", str(root)]), 0)

    def test_exits_zero_when_every_target_is_disarmed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", "disabled\n")
            _make_device(root, "ucsi-source-psy-USBC000:001", "disabled\n")
            self.assertEqual(cli.main(["--power-supply-dir", str(root)]), 0)

    def test_exits_nonzero_when_a_target_is_still_armed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", "enabled\n")
            self.assertEqual(cli.main(["--power-supply-dir", str(root)]), 1)

    def test_exits_zero_when_a_target_device_is_not_wakeup_capable(self):
        """THE ROUND-4 REGRESSION: this must not fail a healthy host."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _make_device(root, "AC", None)
            self.assertEqual(cli.main(["--power-supply-dir", str(root)]), 0)


if __name__ == "__main__":
    unittest.main()
