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
# A stand-in for a DNF-installed SYSTEM extension, which lives in a different
# directory. Reserved-domain form deliberately: the allowlist the secret scanner
# derives covers only the real declared UUIDs, and a test fixture is not one.
DOCK = "system-extension@example.com"
# A stand-in for a UUID already in the list that is not ours (Fedora ships one).
# Reserved-domain form: the pre-commit secret scanner reads a real one as an email.
STOCK = "stock-extension@example.com"
THEIRS = "something-the-user-installed@example.com"


def _completed(stdout: str = "", returncode: int = 0) -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess(args=[], returncode=returncode, stdout=stdout, stderr="")


class TestMain(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.user_dir = pathlib.Path(self.tmp.name) / "user"
        self.system_dir = pathlib.Path(self.tmp.name) / "system"
        self.runtime_dir = pathlib.Path(self.tmp.name) / "runtime"
        self.runtime_dir.mkdir()
        (self.runtime_dir / "bus").touch()

    def _deploy(self, root: pathlib.Path, *uuids: str) -> None:
        for uuid in uuids:
            path = root / uuid
            path.mkdir(parents=True, exist_ok=True)
            (path / "metadata.json").write_text(json.dumps({"uuid": uuid}), encoding="utf-8")

    def _argv(self, *uuids: str, dirs: list[str] | None = None, extra: tuple = ()) -> list[str]:
        argv: list[str] = []
        for directory in dirs or [str(self.user_dir)]:
            argv += ["--extensions-dir", directory]
        for uuid in uuids:
            argv += ["--uuid", uuid]
        return argv + list(extra)

    def _run(self, argv, run_side_effect):
        stdout, stderr = io.StringIO(), io.StringIO()
        with (
            mock.patch.object(aee.subprocess, "run", side_effect=run_side_effect),
            mock.patch.dict(
                aee.session_bus.os.environ,
                {"XDG_RUNTIME_DIR": str(self.runtime_dir)},
                clear=True,
            ),
            mock.patch.object(sys, "stdout", stdout),
            mock.patch.object(sys, "stderr", stderr),
        ):
            code = aee.main(argv)
        return code, stdout.getvalue(), stderr.getvalue()

    @staticmethod
    def _responder(reads: list[str], calls: list[list[str]] | None = None):
        """A fake gsettings: `get` pops the next canned read, `set` records."""

        def fake_run(argv, **kwargs):
            if calls is not None:
                calls.append(argv)
            if "set" in argv:
                return _completed()
            if "disable-user-extensions" in argv:
                return _completed(stdout="false\n")
            return _completed(stdout=reads.pop(0) + "\n")

        return fake_run

    def test_adds_the_declared_uuids_and_reports_changed(self):
        self._deploy(self.user_dir, BLUR, CUSTOM)
        calls: list[list[str]] = []
        reads = [f"['{STOCK}']", f"['{STOCK}', '{BLUR}', '{CUSTOM}']"]

        code, out, _err = self._run(self._argv(BLUR, CUSTOM), self._responder(reads, calls))

        self.assertEqual(code, 0)
        self.assertIn(f"GNOME-EXT-DEPLOYED {BLUR},{CUSTOM}", out)
        self.assertIn("GNOME-EXT-ENABLED-CHANGED", out)
        self.assertIn(f"added={BLUR},{CUSTOM}", out)
        written = [argv for argv in calls if "set" in argv]
        self.assertEqual(len(written), 1)
        self.assertEqual(written[0][-1], f"['{STOCK}', '{BLUR}', '{CUSTOM}']")

    def test_an_undeclared_extension_on_disk_is_neither_enabled_nor_reported(self):
        # The user's own extensions live in the same directory. Sweeping them up
        # would re-enable what they deliberately disabled, and hand the play's
        # verify loop extensions this repo never installed.
        self._deploy(self.user_dir, CUSTOM, THEIRS)
        reads = [f"['{STOCK}']", f"['{STOCK}', '{CUSTOM}']"]
        calls: list[list[str]] = []

        code, out, _err = self._run(self._argv(CUSTOM), self._responder(reads, calls))

        self.assertEqual(code, 0)
        self.assertIn(f"GNOME-EXT-DEPLOYED {CUSTOM}", out)
        self.assertNotIn(THEIRS, out)
        written = [argv for argv in calls if "set" in argv]
        self.assertNotIn(THEIRS, written[0][-1])

    def test_a_declared_uuid_missing_from_disk_fails_before_any_write(self):
        # The partial-install hole: the installer's own enable is a swallowed &&,
        # so six of seven downloaded must not read as complete success.
        self._deploy(self.user_dir, BLUR)
        calls: list[list[str]] = []

        code, out, err = self._run(self._argv(BLUR, CUSTOM), self._responder([], calls))

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn(CUSTOM, err)
        self.assertEqual([argv for argv in calls if "set" in argv], [])

    def test_a_system_extension_resolves_from_the_second_search_path(self):
        # dash-to-dock is installed by DNF into /usr/share/gnome-shell/extensions
        # and is enabled through the same key as a user extension.
        self._deploy(self.user_dir, CUSTOM)
        self._deploy(self.system_dir, DOCK)
        reads = ["@as []", f"['{CUSTOM}', '{DOCK}']"]
        dirs = [str(self.user_dir), str(self.system_dir)]

        code, out, _err = self._run(
            self._argv(CUSTOM, DOCK, dirs=dirs), self._responder(reads)
        )

        self.assertEqual(code, 0)
        self.assertIn(f"GNOME-EXT-DEPLOYED {CUSTOM},{DOCK}", out)

    def test_disable_user_extensions_true_fails_rather_than_reporting_green(self):
        # That key defeats every user extension regardless of enabled-extensions,
        # so writing the list and reporting ok would be the Plan 00110 outcome one
        # key over: a green play and a session with nothing enabled.
        self._deploy(self.user_dir, CUSTOM)
        calls: list[list[str]] = []

        def fake_run(argv, **kwargs):
            calls.append(argv)
            if "disable-user-extensions" in argv:
                return _completed(stdout="true\n")
            return _completed(stdout="@as []\n")

        code, out, err = self._run(self._argv(CUSTOM), fake_run)

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn("disable-user-extensions", err)
        self.assertEqual([argv for argv in calls if "set" in argv], [])

    def test_already_declared_writes_nothing_and_reports_unchanged(self):
        self._deploy(self.user_dir, BLUR)
        calls: list[list[str]] = []

        code, out, _err = self._run(
            self._argv(BLUR), self._responder([f"['{STOCK}', '{BLUR}']"], calls)
        )

        self.assertEqual(code, 0)
        self.assertIn("GNOME-EXT-ENABLED-UNCHANGED", out)
        self.assertNotIn("GNOME-EXT-ENABLED-CHANGED", out)
        self.assertEqual([argv for argv in calls if "set" in argv], [])

    def test_a_write_that_did_not_take_is_a_failure(self):
        # dconf can accept `set` and keep the old value (a read-only or locked
        # database). Without this read-back the play would report ok and the
        # session would come up with nothing enabled — the exact defect of
        # Plan 00110's finding, reintroduced one layer down.
        self._deploy(self.user_dir, BLUR)

        code, out, err = self._run(
            self._argv(BLUR), self._responder([f"['{STOCK}']", f"['{STOCK}']"])
        )

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn(BLUR, err)

    def test_a_failing_gsettings_write_propagates(self):
        self._deploy(self.user_dir, BLUR)

        def fake_run(argv, **kwargs):
            if "set" in argv:
                raise subprocess.CalledProcessError(1, argv, stderr="dconf is read-only")
            if "disable-user-extensions" in argv:
                return _completed(stdout="false\n")
            return _completed(stdout="@as []\n")

        with self.assertRaises(subprocess.CalledProcessError):
            self._run(self._argv(BLUR), fake_run)

    def test_a_failing_gsettings_get_propagates(self):
        self._deploy(self.user_dir, BLUR)

        def fake_run(argv, **kwargs):
            raise subprocess.CalledProcessError(1, argv, stderr="No such schema")

        with self.assertRaises(subprocess.CalledProcessError):
            self._run(self._argv(BLUR), fake_run)

    def test_an_unparseable_current_value_fails_rather_than_overwriting_it(self):
        self._deploy(self.user_dir, BLUR)

        def fake_run(argv, **kwargs):
            if "disable-user-extensions" in argv:
                return _completed(stdout="false\n")
            return _completed(stdout="something that is not a list\n")

        code, out, err = self._run(self._argv(BLUR), fake_run)

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn("enabled-extensions", err)

    def test_uses_the_runtime_bus_socket_when_present(self):
        self._deploy(self.user_dir, BLUR)
        seen: dict[str, Any] = {}

        def fake_run(argv, **kwargs):
            seen.setdefault("argv", argv)
            seen.setdefault("env", kwargs.get("env"))
            if "disable-user-extensions" in argv:
                return _completed(stdout="false\n")
            return _completed(stdout=f"['{BLUR}']\n")

        code, _out, _err = self._run(self._argv(BLUR), fake_run)

        self.assertEqual(code, 0)
        self.assertEqual(seen["argv"][0], "gsettings")
        self.assertEqual(
            seen["env"]["DBUS_SESSION_BUS_ADDRESS"], f"unix:path={self.runtime_dir}/bus"
        )

    def test_falls_back_to_dbus_run_session_without_a_bus(self):
        self._deploy(self.user_dir, BLUR)
        (self.runtime_dir / "bus").unlink()
        seen: dict[str, Any] = {}

        def fake_run(argv, **kwargs):
            seen.setdefault("argv", argv)
            if "disable-user-extensions" in argv:
                return _completed(stdout="false\n")
            return _completed(stdout=f"['{BLUR}']\n")

        code, _out, _err = self._run(self._argv(BLUR), fake_run)

        self.assertEqual(code, 0)
        self.assertEqual(seen["argv"][:2], ["dbus-run-session", "--"])

    def test_schema_and_key_are_overridable(self):
        # Capturing only the FIRST gsettings call silently dropped the --key
        # assertion once disable-user-extensions became the first read. Capture
        # every call and assert against the one that carries the enabled key.
        self._deploy(self.user_dir, BLUR)
        calls: list[list[str]] = []

        def fake_run(argv, **kwargs):
            calls.append(argv)
            if "some-other-key" in argv:
                return _completed(stdout="false\n")
            return _completed(stdout=f"['{BLUR}']\n")

        code, _out, _err = self._run(
            self._argv(
                BLUR,
                extra=(
                    "--schema",
                    "org.example.thing",
                    "--key",
                    "some-key",
                    "--disable-key",
                    "some-other-key",
                ),
            ),
            fake_run,
        )

        self.assertEqual(code, 0)
        self.assertTrue(all("org.example.thing" in argv for argv in calls))
        self.assertTrue(any("some-key" in argv for argv in calls))
        self.assertTrue(any("some-other-key" in argv for argv in calls))

    def test_bad_extension_metadata_fails_before_any_write(self):
        path = self.user_dir / BLUR
        path.mkdir(parents=True)
        (path / "metadata.json").write_text("{not json", encoding="utf-8")
        calls: list[list[str]] = []

        code, out, err = self._run(self._argv(BLUR), self._responder([], calls))

        self.assertNotEqual(code, 0)
        self.assertIn("GNOME-EXT-FAIL", out)
        self.assertIn("metadata.json", err)
        self.assertEqual([argv for argv in calls if "set" in argv], [])


if __name__ == "__main__":
    unittest.main()
