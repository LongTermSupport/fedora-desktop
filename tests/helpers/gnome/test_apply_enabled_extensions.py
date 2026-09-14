"""Unit tests for helpers/gnome/apply_enabled_extensions.py — the gsettings executor.

Run from the repo root:

    python3 -m unittest tests.helpers.gnome.test_apply_enabled_extensions
"""

from __future__ import annotations

import io
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
from typing import Any
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.gnome import apply_enabled_extensions as aee

CUSTOM = "workspace-names-overview@fedora-desktop"
BLUR = "blur-my-shell@aunetx"
# A stand-in for a UUID already in the list that is not ours (Fedora ships one).
# Reserved-domain form: the pre-commit secret scanner reads a real one as an email.
STOCK = "stock-extension@example.com"


def _completed(stdout: str = "", returncode: int = 0) -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess(args=[], returncode=returncode, stdout=stdout, stderr="")


class TestResolveSessionBus(unittest.TestCase):
    def test_environment_address_wins(self):
        with tempfile.TemporaryDirectory() as tmp:
            bus = aee.resolve_session_bus(
                {"DBUS_SESSION_BUS_ADDRESS": "unix:path=/somewhere/bus"}, tmp
            )
        self.assertEqual(bus.prefix, [])
        self.assertEqual(bus.address, "unix:path=/somewhere/bus")
        self.assertEqual(bus.source, "environment")

    def test_runtime_socket_is_used_when_the_environment_is_bare(self):
        # Ansible's become_user gives no DBUS_SESSION_BUS_ADDRESS, but the user's
        # systemd bus socket is there — writing through it reaches the live shell.
        with tempfile.TemporaryDirectory() as tmp:
            (pathlib.Path(tmp) / "bus").touch()
            bus = aee.resolve_session_bus({}, tmp)
        self.assertEqual(bus.prefix, [])
        self.assertEqual(bus.address, f"unix:path={tmp}/bus")
        self.assertEqual(bus.source, "runtime-socket")

    def test_falls_back_to_dbus_run_session_with_no_bus_at_all(self):
        # run.bash from a TTY on a machine with no session: the write must still
        # land in the user's dconf database, or the play would have to tolerate a
        # failure — which is what this plan exists to remove.
        with tempfile.TemporaryDirectory() as tmp:
            bus = aee.resolve_session_bus({}, tmp)
        self.assertEqual(bus.prefix, ["dbus-run-session", "--"])
        self.assertIsNone(bus.address)
        self.assertEqual(bus.source, "dbus-run-session")

    def test_empty_environment_address_is_not_an_address(self):
        with tempfile.TemporaryDirectory() as tmp:
            bus = aee.resolve_session_bus({"DBUS_SESSION_BUS_ADDRESS": ""}, tmp)
        self.assertEqual(bus.source, "dbus-run-session")

    def test_an_unreachable_socket_falls_back_rather_than_being_used(self):
        # `sudo -u` can leave XDG_RUNTIME_DIR pointing at another user's 0700
        # runtime directory. The socket is there; we cannot connect to it.
        with tempfile.TemporaryDirectory() as tmp:
            (pathlib.Path(tmp) / "bus").touch()
            with mock.patch.object(aee.os, "access", return_value=False):
                bus = aee.resolve_session_bus({}, tmp)
        self.assertEqual(bus.source, "dbus-run-session")


class TestMain(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.extensions_dir = pathlib.Path(self.tmp.name) / "extensions"
        self.runtime_dir = pathlib.Path(self.tmp.name) / "runtime"
        self.runtime_dir.mkdir()
        (self.runtime_dir / "bus").touch()

    def _deploy(self, *uuids: str) -> None:
        for uuid in uuids:
            path = self.extensions_dir / uuid
            path.mkdir(parents=True)
            (path / "metadata.json").write_text(json.dumps({"uuid": uuid}), encoding="utf-8")

    def _argv(self, *extra: str) -> list[str]:
        return ["--extensions-dir", str(self.extensions_dir), *extra]

    def _run(self, argv, run_side_effect):
        stdout, stderr = io.StringIO(), io.StringIO()
        with (
            mock.patch.object(aee.subprocess, "run", side_effect=run_side_effect) as runner,
            mock.patch.dict(
                aee.os.environ, {"XDG_RUNTIME_DIR": str(self.runtime_dir)}, clear=True
            ),
            mock.patch.object(sys, "stdout", stdout),
            mock.patch.object(sys, "stderr", stderr),
        ):
            code = aee.main(argv)
        return code, stdout.getvalue(), stderr.getvalue(), runner

    def test_adds_the_deployed_uuids_and_reports_changed(self):
        self._deploy(BLUR, CUSTOM)
        reads = [f"['{STOCK}']", f"['{STOCK}', '{BLUR}', '{CUSTOM}']"]
        calls: list[list[str]] = []

        def fake_run(argv, **kwargs):
            calls.append(argv)
            if "set" in argv:
                return _completed()
            return _completed(stdout=reads.pop(0) + "\n")

        code, out, _err, _runner = self._run(self._argv(), fake_run)

        self.assertEqual(code, 0)
        self.assertIn(f"GNOME-EXT-DEPLOYED {BLUR},{CUSTOM}", out)
        self.assertIn("GNOME-EXT-ENABLED-CHANGED", out)
        self.assertIn(f"added={BLUR},{CUSTOM}", out)
        written = [argv for argv in calls if "set" in argv]
        self.assertEqual(len(written), 1)
        self.assertEqual(written[0][-1], f"['{STOCK}', '{BLUR}', '{CUSTOM}']")

    def test_already_declared_writes_nothing_and_reports_unchanged(self):
        self._deploy(BLUR)
        calls: list[list[str]] = []

        def fake_run(argv, **kwargs):
            calls.append(argv)
            return _completed(stdout=f"['{STOCK}', '{BLUR}']\n")

        code, out, _err, _runner = self._run(self._argv(), fake_run)

        self.assertEqual(code, 0)
        self.assertIn("GNOME-EXT-ENABLED-UNCHANGED", out)
        self.assertNotIn("GNOME-EXT-ENABLED-CHANGED", out)
        self.assertEqual([argv for argv in calls if "set" in argv], [])

    def test_a_write_that_did_not_take_is_a_failure(self):
        # dconf can accept `set` and keep the old value (a read-only or locked
        # database). Without this read-back the play would report ok and the
        # session would come up with nothing enabled — the exact defect of
        # Plan 00110's finding, reintroduced one layer down.
        self._deploy(BLUR)
        reads = [f"['{STOCK}']", f"['{STOCK}']"]

        def fake_run(argv, **kwargs):
            if "set" in argv:
                return _completed()
            return _completed(stdout=reads.pop(0) + "\n")

        code, out, err, _runner = self._run(self._argv(), fake_run)

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn(BLUR, err)

    def test_missing_required_uuid_fails_before_any_write(self):
        self._deploy(BLUR)
        calls: list[list[str]] = []

        def fake_run(argv, **kwargs):
            calls.append(argv)
            return _completed(stdout=f"['{STOCK}']\n")

        code, out, err, _runner = self._run(self._argv("--require", CUSTOM), fake_run)

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn(CUSTOM, err)
        self.assertEqual([argv for argv in calls if "set" in argv], [])

    def test_nothing_deployed_fails_rather_than_reporting_success(self):
        # An empty extensions directory means the install steps above did not run.
        # "0 of 0 enabled" must never read as a pass.
        self.extensions_dir.mkdir(parents=True)

        def fake_run(argv, **kwargs):
            return _completed(stdout=f"['{STOCK}']\n")

        code, out, err, _runner = self._run(self._argv(), fake_run)

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn("no extensions", err.lower())

    def test_uses_the_runtime_bus_socket_when_present(self):
        self._deploy(BLUR)
        seen: dict[str, Any] = {}

        def fake_run(argv, **kwargs):
            seen["argv"] = argv
            seen["env"] = kwargs.get("env")
            return _completed(stdout=f"['{BLUR}']\n")

        code, _out, _err, _runner = self._run(self._argv(), fake_run)

        self.assertEqual(code, 0)
        self.assertEqual(seen["argv"][0], "gsettings")
        self.assertEqual(
            seen["env"]["DBUS_SESSION_BUS_ADDRESS"], f"unix:path={self.runtime_dir}/bus"
        )

    def test_falls_back_to_dbus_run_session_without_a_bus(self):
        self._deploy(BLUR)
        (self.runtime_dir / "bus").unlink()
        seen: dict[str, Any] = {}

        def fake_run(argv, **kwargs):
            seen.setdefault("argv", argv)
            return _completed(stdout=f"['{BLUR}']\n")

        code, _out, _err, _runner = self._run(self._argv(), fake_run)

        self.assertEqual(code, 0)
        self.assertEqual(seen["argv"][:2], ["dbus-run-session", "--"])

    def test_a_failing_gsettings_get_propagates(self):
        self._deploy(BLUR)

        def fake_run(argv, **kwargs):
            raise subprocess.CalledProcessError(1, argv, stderr="No such schema")

        with self.assertRaises(subprocess.CalledProcessError):
            self._run(self._argv(), fake_run)

    def test_an_unparseable_current_value_fails_rather_than_overwriting_it(self):
        self._deploy(BLUR)

        def fake_run(argv, **kwargs):
            return _completed(stdout="something that is not a list\n")

        code, out, err, _runner = self._run(self._argv(), fake_run)

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn("enabled-extensions", err)

    def test_schema_and_key_are_overridable(self):
        self._deploy(BLUR)
        seen: dict[str, Any] = {}

        def fake_run(argv, **kwargs):
            seen.setdefault("argv", argv)
            return _completed(stdout=f"['{BLUR}']\n")

        code, _out, _err, _runner = self._run(
            self._argv("--schema", "org.example.thing", "--key", "some-key"), fake_run
        )

        self.assertEqual(code, 0)
        self.assertIn("org.example.thing", seen["argv"])
        self.assertIn("some-key", seen["argv"])


if __name__ == "__main__":
    unittest.main()
