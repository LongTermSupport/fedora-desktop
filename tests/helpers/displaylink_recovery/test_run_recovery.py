"""Unit tests for the side-effecting half of DisplayLink dock recovery.

Repo root must be on sys.path for the `helpers` namespace package to import
(matches the convention in tests/helpers/pyenv/test_resolver.py).

Most of run_recovery.py shells out and is covered by recovery.py's pure tests.
`edid_byte_count` is the exception and earns a test of its own: it is the single
input that decides whether a head looks wedged, and getting it wrong silently
arms the whole recovery ladder against healthy monitors.
"""

from __future__ import annotations

import glob
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..")))

from helpers.displaylink_recovery.run_recovery import (
    _as_user,
    edid_byte_count,
    nudged_signal_value,
)

SYSFS_DRM = "/sys/class/drm"


class TestNudgedSignalValue(unittest.TestCase):
    """Choosing a throwaway value that is guaranteed to differ from the current one.

    The refresh works by changing *any* key in org.gnome.desktop.background,
    which is what emits bg-changed. Writing a key back to the value it already
    holds emits nothing — dconf suppresses a no-op write — so the nudge value
    must never equal the current one.

    It nudges `primary-color`, not `picture-uri`, because the process can die
    between the nudge and the restore. Losing `primary-color` leaves a colour
    that is invisible behind an opaque wallpaper; losing `picture-uri` leaves a
    desktop with no wallpaper at all.
    """

    def test_differs_from_current(self):
        for current in ("'#000000'", "'#ffffff'", "'#30307171aeae'", "''", "junk"):
            with self.subTest(current=current):
                self.assertNotEqual(nudged_signal_value(current), current)

    def test_is_stable_for_a_given_input(self):
        self.assertEqual(nudged_signal_value("'#123456'"), nudged_signal_value("'#123456'"))

    def test_result_is_a_parseable_gvariant_string(self):
        for current in ("'#000000'", "'#ffffff'", "'#30307171aeae'"):
            with self.subTest(current=current):
                nudged = nudged_signal_value(current)
                self.assertTrue(nudged.startswith("'") and nudged.endswith("'"), nudged)


class TestAsUser(unittest.TestCase):
    """Reaching a user's session bus from the root recovery context.

    sudo sanitises the environment it hands the child, so setting
    DBUS_SESSION_BUS_ADDRESS on the parent process is silently dropped: dconf
    then tries to autolaunch a private bus, fails with "Failed to execute child
    process dbus-launch", and every write is lost while the command still exits
    zero. The address must therefore travel THROUGH sudo via `env`.
    """

    def test_bus_address_is_passed_through_sudo_not_inherited(self):
        argv = _as_user("someone", "1000", "gsettings", "get", "x")
        self.assertIn("env", argv)
        bus = "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus"
        self.assertIn(bus, argv)
        self.assertLess(
            argv.index("env"),
            argv.index("gsettings"),
            "env must precede the command it is setting the variable for",
        )

    def test_runs_as_the_named_user(self):
        argv = _as_user("someone", "1000", "true")
        self.assertEqual(argv[:3], ["sudo", "-u", "someone"])

    def test_command_and_arguments_are_preserved_in_order(self):
        argv = _as_user("someone", "42", "gsettings", "set", "org.x", "key", "'val'")
        self.assertEqual(argv[-5:], ["gsettings", "set", "org.x", "key", "'val'"])

    def test_uid_selects_the_bus_socket(self):
        self.assertIn(
            "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/4242/bus",
            _as_user("someone", "4242", "true"),
        )


class TestEdidByteCount(unittest.TestCase):
    def test_missing_file_is_zero(self):
        self.assertEqual(edid_byte_count("/nonexistent/connector/edid"), 0)

    def test_empty_file_is_zero(self):
        with tempfile.NamedTemporaryFile() as f:
            self.assertEqual(edid_byte_count(f.name), 0)

    def test_counts_actual_bytes(self):
        with tempfile.NamedTemporaryFile() as f:
            f.write(b"\x00" * 256)
            f.flush()
            self.assertEqual(edid_byte_count(f.name), 256)

    def test_unreadable_is_not_reported_as_absent(self):
        """-1, not 0. Returning 0 would assert the wedge signature on no evidence."""
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "edid")
            with open(path, "wb") as f:
                f.write(b"\x00" * 128)
            os.chmod(path, 0o000)
            if os.geteuid() == 0:
                self.skipTest("running as root: chmod 000 does not block the read")
            self.assertEqual(edid_byte_count(path), -1)


class TestEdidByteCountAgainstRealSysfs(unittest.TestCase):
    """The only test that could have caught the defect this function exists to fix.

    A tempfile reports an honest `st_size`, so a tempfile-only suite passes just
    as happily with `os.path.getsize()` — which is exactly how the bug survived.
    Sysfs binary attributes report **st_size 0 with content present**, and that
    behaviour reproduces nowhere else. So this asserts against the real thing and
    skips where there is none (the CCY container, CI).
    """

    def _connected_connectors_with_modes(self) -> list[str]:
        found = []
        for status_path in sorted(glob.glob(f"{SYSFS_DRM}/card*-*/status")):
            connector = os.path.dirname(status_path)
            try:
                with open(status_path, encoding="utf-8") as f:
                    if f.read().strip() != "connected":
                        continue
                with open(os.path.join(connector, "modes"), encoding="utf-8") as f:
                    if not f.read().strip():
                        continue
            except OSError:
                continue
            if os.path.exists(os.path.join(connector, "edid")):
                found.append(connector)
        return found

    def test_a_connected_display_reports_edid_bytes(self):
        connectors = self._connected_connectors_with_modes()
        if not connectors:
            self.skipTest(f"no connected DRM connector with modes under {SYSFS_DRM}")
        for connector in connectors:
            edid_path = os.path.join(connector, "edid")
            self.assertGreater(
                edid_byte_count(edid_path),
                0,
                f"{connector} is connected and has modes, so it has an EDID. "
                f"Reading zero here means every head looks wedged and the "
                f"recovery ladder will run against working monitors.",
            )

    def test_stat_disagrees_with_reading_which_is_the_whole_point(self):
        connectors = self._connected_connectors_with_modes()
        if not connectors:
            self.skipTest(f"no connected DRM connector with modes under {SYSFS_DRM}")
        edid_path = os.path.join(connectors[0], "edid")
        self.assertEqual(
            os.path.getsize(edid_path),
            0,
            "sysfs began reporting a real st_size for binary attributes. "
            "If that is now true everywhere this runs, edid_byte_count's "
            "read-the-file requirement can be revisited.",
        )
        self.assertGreater(edid_byte_count(edid_path), 0)


if __name__ == "__main__":
    unittest.main()
