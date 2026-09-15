"""Unit tests for helpers/gnome/session_bus.py — reaching a D-Bus session.

Shared by the applier and the verifier. They resolved the bus differently before,
and the difference mattered: the verifier's uid-derived path was never tested for
reachability, so under a sudo become that leaked a stale XDG_RUNTIME_DIR one
helper reached the live shell and the other did not.

Run from the repo root:

    python3 -m unittest tests.helpers.gnome.test_session_bus
"""

from __future__ import annotations

import pathlib
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.gnome import session_bus as sb


class TestResolveSessionBus(unittest.TestCase):
    def test_environment_address_wins(self):
        with tempfile.TemporaryDirectory() as tmp:
            bus = sb.resolve_session_bus(
                {"DBUS_SESSION_BUS_ADDRESS": "unix:path=/somewhere/bus"}, [tmp]
            )
        self.assertEqual(bus.prefix, [])
        self.assertEqual(bus.address, "unix:path=/somewhere/bus")
        self.assertEqual(bus.source, "environment")

    def test_runtime_socket_is_used_when_the_environment_is_bare(self):
        with tempfile.TemporaryDirectory() as tmp:
            (pathlib.Path(tmp) / "bus").touch()
            bus = sb.resolve_session_bus({}, [tmp])
        self.assertEqual(bus.prefix, [])
        self.assertEqual(bus.address, f"unix:path={tmp}/bus")
        self.assertEqual(bus.source, "runtime-socket")

    def test_falls_back_to_dbus_run_session_with_no_bus_at_all(self):
        with tempfile.TemporaryDirectory() as tmp:
            bus = sb.resolve_session_bus({}, [tmp])
        self.assertEqual(bus.prefix, ["dbus-run-session", "--"])
        self.assertIsNone(bus.address)
        self.assertEqual(bus.source, "dbus-run-session")

    def test_empty_environment_address_is_not_an_address(self):
        with tempfile.TemporaryDirectory() as tmp:
            bus = sb.resolve_session_bus({"DBUS_SESSION_BUS_ADDRESS": ""}, [tmp])
        self.assertEqual(bus.source, "dbus-run-session")

    def test_an_unreachable_socket_falls_back_rather_than_being_used(self):
        with tempfile.TemporaryDirectory() as tmp:
            (pathlib.Path(tmp) / "bus").touch()
            with mock.patch.object(sb.os, "access", return_value=False):
                bus = sb.resolve_session_bus({}, [tmp])
        self.assertEqual(bus.source, "dbus-run-session")

    def test_a_stale_runtime_dir_falls_through_to_the_uid_derived_one(self):
        with tempfile.TemporaryDirectory() as tmp:
            stale = pathlib.Path(tmp) / "stale"
            real = pathlib.Path(tmp) / "real"
            stale.mkdir()
            real.mkdir()
            (real / "bus").touch()
            bus = sb.resolve_session_bus({}, [str(stale), str(real)])
        self.assertEqual(bus.source, "runtime-socket")
        self.assertEqual(bus.address, f"unix:path={real}/bus")


class TestRuntimeDirs(unittest.TestCase):
    def test_xdg_runtime_dir_is_tried_before_the_uid_derived_path(self):
        dirs = sb.runtime_dirs({"XDG_RUNTIME_DIR": "/run/user/1234"}, uid=99)
        self.assertEqual(dirs, ["/run/user/1234", "/run/user/99"])

    def test_the_uid_derived_path_is_always_present(self):
        # It is the one derived from who we actually are, so it must never be
        # dropped in favour of an environment value that may be another user's.
        self.assertEqual(sb.runtime_dirs({}, uid=99), ["/run/user/99"])

    def test_a_duplicate_is_not_tried_twice(self):
        dirs = sb.runtime_dirs({"XDG_RUNTIME_DIR": "/run/user/99"}, uid=99)
        self.assertEqual(dirs, ["/run/user/99"])

    def test_an_empty_xdg_runtime_dir_is_ignored(self):
        self.assertEqual(sb.runtime_dirs({"XDG_RUNTIME_DIR": ""}, uid=99), ["/run/user/99"])


class TestEnvFor(unittest.TestCase):
    def test_the_address_is_exported_when_there_is_one(self):
        bus = sb.SessionBus(prefix=[], address="unix:path=/x/bus", source="environment")
        env = sb.env_for(bus, {"PATH": "/bin"})
        self.assertEqual(env["DBUS_SESSION_BUS_ADDRESS"], "unix:path=/x/bus")
        self.assertEqual(env["PATH"], "/bin")

    def test_nothing_is_exported_for_the_dbus_run_session_route(self):
        # dbus-run-session sets the variable itself; pre-setting it would point the
        # child at a bus that is not the one it just started.
        bus = sb.SessionBus(prefix=["dbus-run-session", "--"], address=None, source="x")
        env = sb.env_for(bus, {"PATH": "/bin"})
        self.assertNotIn("DBUS_SESSION_BUS_ADDRESS", env)

    def test_the_caller_environment_is_not_mutated(self):
        base = {"PATH": "/bin"}
        bus = sb.SessionBus(prefix=[], address="unix:path=/x/bus", source="environment")
        sb.env_for(bus, base)
        self.assertEqual(base, {"PATH": "/bin"})


class TestCurrent(unittest.TestCase):
    """The seam between the resolution above and the process this runs in.

    Everything else in this module takes its inputs as arguments. `current()` is
    the one function that reaches for `os.environ` and `os.getuid()`, and it had
    no test of its own — the only thing exercising it was an applier test that
    was really asking a different question, and that test read the host rather
    than controlling it. A change to which process facts are read here would have
    been caught nowhere.
    """

    def _refusing_access(self, tried: list[str]):
        def record(path, mode):
            tried.append(path)
            return False

        return record

    def test_the_process_environment_address_is_used(self):
        with mock.patch.dict(
            sb.os.environ, {"DBUS_SESSION_BUS_ADDRESS": "unix:path=/x/bus"}, clear=True
        ):
            bus = sb.current()
        self.assertEqual(bus.source, "environment")
        self.assertEqual(bus.address, "unix:path=/x/bus")

    def test_the_uid_derived_runtime_dir_is_consulted_with_a_bare_environment(self):
        # This is the candidate no environment change can remove, which is why a
        # test that wants the dbus-run-session branch cannot get there by
        # arranging the environment alone.
        tried: list[str] = []
        with (
            mock.patch.dict(sb.os.environ, {}, clear=True),
            mock.patch.object(sb.os, "getuid", return_value=4242),
            mock.patch.object(sb.os, "access", side_effect=self._refusing_access(tried)),
        ):
            bus = sb.current()

        self.assertEqual(tried, ["/run/user/4242/bus"])
        self.assertEqual(bus.source, "dbus-run-session")

    def test_the_environment_runtime_dir_is_tried_before_the_uid_derived_one(self):
        tried: list[str] = []
        with (
            mock.patch.dict(sb.os.environ, {"XDG_RUNTIME_DIR": "/run/user/7"}, clear=True),
            mock.patch.object(sb.os, "getuid", return_value=4242),
            mock.patch.object(sb.os, "access", side_effect=self._refusing_access(tried)),
        ):
            sb.current()

        self.assertEqual(tried, ["/run/user/7/bus", "/run/user/4242/bus"])

    def test_an_accessible_uid_derived_socket_wins_over_the_fallback(self):
        # The exact shape of the CI failure this class was added for: the
        # environment names no bus and carries no usable XDG_RUNTIME_DIR, and the
        # uid-derived socket is reachable, so there is no fallback.
        with (
            mock.patch.dict(sb.os.environ, {}, clear=True),
            mock.patch.object(sb.os, "getuid", return_value=1001),
            mock.patch.object(sb.os, "access", return_value=True),
        ):
            bus = sb.current()

        self.assertEqual(bus.source, "runtime-socket")
        self.assertEqual(bus.address, "unix:path=/run/user/1001/bus")
        self.assertEqual(bus.prefix, [])


if __name__ == "__main__":
    unittest.main()
