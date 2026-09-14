"""Unit tests for helpers/gnome/verify_extension.py.

Thin executor around the pure classifier (test_extension_state.py). subprocess
and the filesystem are mocked so no GNOME session is needed.

The fake distinguishes `gnome-extensions list` (does a session exist at all?)
from `gnome-extensions info <uuid>` (does the shell know THIS one?), because
conflating those two was the defect Plan 00112 fixed here: both exit non-zero,
and reading the second as the first reported "no session" for extensions sitting
on disk in a live session.
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


def _write_extension(root, shell_versions, uuid=UUID):
    ext_dir = pathlib.Path(root) / uuid
    ext_dir.mkdir(parents=True)
    (ext_dir / "metadata.json").write_text(
        json.dumps({"uuid": uuid, "shell-version": shell_versions})
    )
    return str(root)


def _fake_run(*, info_rc, info_stdout, shell_version="GNOME Shell 50.1", list_rc=0):
    def run(cmd, **_kwargs):
        argv = [part for part in cmd if part not in ("dbus-run-session", "--")]
        prog = pathlib.Path(argv[0]).name
        if prog == "gnome-shell":
            return subprocess.CompletedProcess(cmd, 0, shell_version + "\n", "")
        if prog == "gnome-extensions":
            if argv[1] == "list":
                return subprocess.CompletedProcess(cmd, list_rc, "", "")
            if info_rc != 0:
                return subprocess.CompletedProcess(cmd, info_rc, "", "no such extension")
            return subprocess.CompletedProcess(cmd, 0, info_stdout, "")
        raise AssertionError(f"unexpected command: {cmd}")

    return run


class TestVerifyMain(unittest.TestCase):
    def _main(
        self,
        *,
        shell_versions,
        info_rc,
        info_stdout,
        shell_version="GNOME Shell 50.1",
        list_rc=0,
    ):
        with tempfile.TemporaryDirectory() as tmp:
            ext_root = _write_extension(tmp, shell_versions)
            with mock.patch.object(
                ve.subprocess,
                "run",
                _fake_run(
                    info_rc=info_rc,
                    info_stdout=info_stdout,
                    shell_version=shell_version,
                    list_rc=list_rc,
                ),
            ):
                out = io.StringIO()
                with contextlib.redirect_stdout(out):
                    rc = ve.main(["--uuid", UUID, "--extensions-dir", ext_root])
        return rc, out.getvalue()

    _ACTIVE = "workspace-names-overview@fedora-desktop\n  Name: X\n  State: ACTIVE\n"
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
        # `list` fails: there is genuinely nothing to ask.
        rc, out = self._main(
            shell_versions=["50"], info_rc=1, info_stdout="", list_rc=1
        )
        self.assertEqual(rc, 0)
        self.assertIn("no GNOME session", out)

    def test_a_live_session_that_does_not_know_the_uuid_is_pending_scan_not_no_session(self):
        # THE regression this split exists to prevent. `list` succeeds, `info`
        # fails: the extension is on disk and the shell has not scanned it. Saying
        # "no session" here is how a nine-iteration gate reported OK having judged
        # nothing on a fresh install.
        rc, out = self._main(
            shell_versions=["50"], info_rc=1, info_stdout="", list_rc=0
        )
        self.assertEqual(rc, 0)
        self.assertIn("pending_scan", out)
        self.assertNotIn("no GNOME session", out)
        self.assertIn("has no record", out)

    def test_missing_metadata_fails(self):
        with (
            tempfile.TemporaryDirectory() as tmp,
            mock.patch.object(
                ve.subprocess, "run", _fake_run(info_rc=0, info_stdout=self._ACTIVE)
            ),
            self.assertRaises(SystemExit),
        ):
            ve.main(["--uuid", UUID, "--extensions-dir", tmp])

    def test_the_missing_metadata_message_names_every_directory_searched(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(
            ve.subprocess, "run", _fake_run(info_rc=0, info_stdout=self._ACTIVE)
        ):
            first = str(pathlib.Path(tmp) / "a")
            second = str(pathlib.Path(tmp) / "b")
            with self.assertRaises(SystemExit) as caught:
                ve.main(
                    [
                        "--uuid",
                        UUID,
                        "--extensions-dir",
                        first,
                        "--extensions-dir",
                        second,
                    ]
                )
        message = str(caught.exception)
        self.assertIn(first, message)
        self.assertIn(second, message)
        # The old remedy named one play step, which was wrong for seven of the
        # eight extensions this now runs over.
        self.assertNotIn("Deploy Custom Extension", message)


class TestSearchPath(unittest.TestCase):
    """dash-to-dock is a DNF system extension, in a directory of its own."""

    def test_an_extension_in_the_second_directory_is_found(self):
        with tempfile.TemporaryDirectory() as tmp:
            user = pathlib.Path(tmp) / "user"
            system = pathlib.Path(tmp) / "system"
            user.mkdir()
            _write_extension(system, ["50"])
            with mock.patch.object(
                ve.subprocess,
                "run",
                _fake_run(info_rc=0, info_stdout=TestVerifyMain._ACTIVE),
            ):
                out = io.StringIO()
                with contextlib.redirect_stdout(out):
                    rc = ve.main(
                        [
                            "--uuid",
                            UUID,
                            "--extensions-dir",
                            str(user),
                            "--extensions-dir",
                            str(system),
                        ]
                    )
        self.assertEqual(rc, 0)

    def test_the_first_directory_holding_it_wins(self):
        with tempfile.TemporaryDirectory() as tmp:
            first = pathlib.Path(tmp) / "first"
            second = pathlib.Path(tmp) / "second"
            # Only the first covers the running GNOME 50; if the second were
            # consulted the verdict would flip to a version failure.
            _write_extension(first, ["50"])
            _write_extension(second, ["48"])
            with mock.patch.object(
                ve.subprocess,
                "run",
                _fake_run(info_rc=0, info_stdout=TestVerifyMain._OUT_OF_DATE),
            ):
                out = io.StringIO()
                with contextlib.redirect_stdout(out):
                    rc = ve.main(
                        [
                            "--uuid",
                            UUID,
                            "--extensions-dir",
                            str(first),
                            "--extensions-dir",
                            str(second),
                        ]
                    )
        self.assertEqual(rc, 0)


if __name__ == "__main__":
    unittest.main()
