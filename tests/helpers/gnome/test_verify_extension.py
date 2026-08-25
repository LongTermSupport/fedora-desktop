"""Unit tests for helpers/gnome/verify_extension.py.

Thin executor around the pure classifier (test_extension_state.py). subprocess
and the filesystem are mocked so no GNOME session is needed.
"""

from __future__ import annotations

import contextlib
import io
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.gnome import verify_extension as ve

UUID = "workspace-names-overview@fedora-desktop"


def _write_extension(tmp, shell_versions):
    ext_dir = pathlib.Path(tmp) / UUID
    ext_dir.mkdir(parents=True)
    (ext_dir / "metadata.json").write_text(
        json.dumps({"uuid": UUID, "shell-version": shell_versions})
    )
    return str(tmp)


def _fake_run(*, info_rc, info_stdout, shell_version="GNOME Shell 50.1"):
    def run(cmd, **_kwargs):
        prog = pathlib.Path(cmd[0]).name
        if prog == "gnome-shell":
            return subprocess.CompletedProcess(cmd, 0, shell_version + "\n", "")
        if prog == "gnome-extensions":
            if info_rc != 0:
                return subprocess.CompletedProcess(cmd, info_rc, "", "no session")
            return subprocess.CompletedProcess(cmd, 0, info_stdout, "")
        raise AssertionError(f"unexpected command: {cmd}")

    return run


class TestVerifyMain(unittest.TestCase):
    def _main(self, *, shell_versions, info_rc, info_stdout, shell_version="GNOME Shell 50.1"):
        with tempfile.TemporaryDirectory() as tmp:
            ext_root = _write_extension(tmp, shell_versions)
            with mock.patch.object(
                ve.subprocess,
                "run",
                _fake_run(info_rc=info_rc, info_stdout=info_stdout, shell_version=shell_version),
            ):
                out = io.StringIO()
                with contextlib.redirect_stdout(out):
                    rc = ve.main(["--uuid", UUID, "--extensions-dir", ext_root])
        return rc, out.getvalue()

    _ACTIVE = (
        "workspace-names-overview@fedora-desktop\n  Name: X\n  State: ACTIVE\n"
    )
    _OUT_OF_DATE = (
        "workspace-names-overview@fedora-desktop\n  Name: X\n  State: OUT OF DATE\n"
    )
    _ERROR = "workspace-names-overview@fedora-desktop\n  Name: X\n  State: ERROR\n"

    def test_active_passes(self):
        rc, _out = self._main(shell_versions=["50"], info_rc=0, info_stdout=self._ACTIVE)
        self.assertEqual(rc, 0)

    def test_out_of_date_but_metadata_current_passes_with_reload_notice(self):
        rc, out = self._main(
            shell_versions=["48", "49", "50"], info_rc=0, info_stdout=self._OUT_OF_DATE
        )
        self.assertEqual(rc, 0)
        self.assertIn("log out", out.lower())

    def test_out_of_date_and_metadata_stale_fails(self):
        rc, _out = self._main(
            shell_versions=["48", "49"], info_rc=0, info_stdout=self._OUT_OF_DATE
        )
        self.assertEqual(rc, 1)

    def test_error_state_fails(self):
        rc, _out = self._main(shell_versions=["50"], info_rc=0, info_stdout=self._ERROR)
        self.assertEqual(rc, 1)

    def test_no_session_skips(self):
        rc, out = self._main(shell_versions=["50"], info_rc=1, info_stdout="")
        self.assertEqual(rc, 0)
        self.assertIn("no GNOME session", out)

    def test_missing_metadata_fails(self):
        with (
            tempfile.TemporaryDirectory() as tmp,
            mock.patch.object(
                ve.subprocess, "run", _fake_run(info_rc=0, info_stdout=self._ACTIVE)
            ),
            self.assertRaises(SystemExit),
        ):
            ve.main(["--uuid", UUID, "--extensions-dir", tmp])


if __name__ == "__main__":
    unittest.main()
