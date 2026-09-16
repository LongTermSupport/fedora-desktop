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

# DRM connector types that carry no physical display link. A connector's type is
# the middle field of its sysfs directory name — `card1-DP-2` is DP,
# `card1-HDMI-A-1` is HDMI-A, `card1-Virtual-1` is Virtual. Neither of these has a
# monitor on the other end: Virtual is a framebuffer the driver invents (every
# VM GPU presents one) and Writeback renders into memory. Their modes come from
# the driver rather than from a monitor across a link, so "connected and
# advertising modes" does not imply an EDID for them the way it does for DP,
# HDMI, eDP or DVI.
#
# A DENYLIST on purpose. An unrecognised connector type is asserted against, not
# skipped, so a linkless type nobody has met yet surfaces here as a failure to
# look at rather than as a test that quietly stopped checking anything.
LINKLESS_CONNECTOR_TYPES = frozenset({"Virtual", "Writeback"})


def connector_type(connector_dir: str) -> str:
    """The DRM connector type from a sysfs connector directory name."""
    after_the_card = os.path.basename(connector_dir).partition("-")[2]
    return after_the_card.rsplit("-", 1)[0]


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


class TestConnectorType(unittest.TestCase):
    """The filter below is only as good as this parse, so it gets its own cases.

    Getting it wrong in the permissive direction excludes a real connector and
    the sysfs assertion stops running; getting it wrong in the other direction
    reddens CI again.
    """

    def test_a_single_segment_type(self):
        self.assertEqual(connector_type("/sys/class/drm/card1-DP-2"), "DP")

    def test_a_hyphenated_type_keeps_both_segments(self):
        self.assertEqual(connector_type("/sys/class/drm/card0-HDMI-A-1"), "HDMI-A")
        self.assertEqual(connector_type("/sys/class/drm/card2-DVI-I-1"), "DVI-I")

    def test_a_lowercase_type_is_preserved_exactly(self):
        # eDP, not EDP — the denylist is compared literally against this.
        self.assertEqual(connector_type("/sys/class/drm/card1-eDP-1"), "eDP")

    def test_the_linkless_types_are_recognised(self):
        for name in ("card1-Virtual-1", "card0-Writeback-1"):
            with self.subTest(name=name):
                self.assertIn(
                    connector_type(f"/sys/class/drm/{name}"), LINKLESS_CONNECTOR_TYPES
                )

    def test_a_double_digit_card_number_is_not_mistaken_for_the_type(self):
        self.assertEqual(connector_type("/sys/class/drm/card10-DP-1"), "DP")

    def test_the_connectors_this_repo_recovers_are_not_excluded(self):
        # evdi presents DisplayLink heads as DVI-I. If these ever landed in the
        # denylist the recovery ladder's own connectors would stop being checked.
        for name in ("card2-DVI-I-1", "card5-DVI-I-4"):
            with self.subTest(name=name):
                self.assertNotIn(
                    connector_type(f"/sys/class/drm/{name}"), LINKLESS_CONNECTOR_TYPES
                )


class TestEdidByteCountAgainstRealSysfs(unittest.TestCase):
    """The only test that could have caught the defect this function exists to fix.

    A tempfile reports an honest `st_size`, so a tempfile-only suite passes just
    as happily with `os.path.getsize()` — which is exactly how the bug survived.
    Sysfs binary attributes report **st_size 0 with content present**, and that
    behaviour reproduces nowhere else. So this asserts against the real thing and
    skips where there is none.

    Only connectors with a physical display link are asserted against. A GitHub
    runner is a VM whose one connected connector is `card1-Virtual-1`: connected,
    advertising modes, with an `edid` file that reads zero bytes. It met every
    condition here and failed, because a driver-invented framebuffer has no
    monitor to read an EDID from. Excluding it does not excuse a zero read — a
    connector with a real link that reads zero still fails, which is the defect
    this test exists to catch.
    """

    def _connected_connectors_with_modes(self) -> tuple[list[str], list[str]]:
        """(linked, linkless) among connectors that are connected and have modes."""
        linked: list[str] = []
        linkless: list[str] = []
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
            if not os.path.exists(os.path.join(connector, "edid")):
                continue
            if connector_type(connector) in LINKLESS_CONNECTOR_TYPES:
                linkless.append(connector)
            else:
                linked.append(connector)
        return linked, linkless

    def _no_linked_connector(self, linkless: list[str]) -> str:
        """The skip reason, naming what was excluded rather than skipping anonymously.

        `unittest` prints a skip reason only at verbosity 2, and `qa-helper-tests.bash`
        runs at the default — so this text is reached with
        `python3 -m unittest -v tests.helpers.displaylink_recovery.test_run_recovery`.
        What the suite surfaces by default is the skip COUNT, which `qa-all.bash` carries
        in the stage line precisely so a machine that skipped this pair is not mistaken
        for one that asserted it.
        """
        reason = f"no connected DRM connector with a display link under {SYSFS_DRM}"
        if linkless:
            reason += f"; ignored linkless connector(s): {', '.join(linkless)}"
        return reason

    def test_a_connected_display_reports_edid_bytes(self):
        connectors, linkless = self._connected_connectors_with_modes()
        if not connectors:
            self.skipTest(self._no_linked_connector(linkless))
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
        connectors, linkless = self._connected_connectors_with_modes()
        if not connectors:
            self.skipTest(self._no_linked_connector(linkless))
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
