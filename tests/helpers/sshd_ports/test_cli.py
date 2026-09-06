"""Unit tests for helpers/sshd_ports/cli.py — the thin `sshd -T` executor.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.sshd_ports.test_cli

`sshd` is mocked throughout: these tests must pass on a machine with no SSH
server installed, which is exactly the machine the play guards against.
"""

from __future__ import annotations

import io
import pathlib
import subprocess
import sys
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.sshd_ports import cli


def _completed(stdout: str) -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess(args=["sshd", "-T"], returncode=0, stdout=stdout, stderr="")


class TestCollect(unittest.TestCase):
    def test_invokes_the_given_sshd_path_with_T(self):
        with mock.patch("subprocess.run", return_value=_completed("port 22\n")) as run:
            cli.collect("/usr/sbin/sshd")
        argv = run.call_args.args[0]
        self.assertEqual(argv, ["/usr/sbin/sshd", "-T"])

    def test_fails_fast_on_non_zero_exit(self):
        """check=True, so a broken sshd config stops the play rather than
        yielding an empty port list that would silently permit nothing."""
        with mock.patch("subprocess.run", return_value=_completed("")) as run:
            cli.collect("/usr/sbin/sshd")
        self.assertTrue(run.call_args.kwargs["check"])

    def test_returns_parsed_ports(self):
        text = "port 22022\nlistenaddress 0.0.0.0:2222\n"
        with mock.patch("subprocess.run", return_value=_completed(text)):
            self.assertEqual(cli.collect("/usr/sbin/sshd"), ["22022", "2222"])


class TestMain(unittest.TestCase):
    def test_prints_one_port_per_line(self):
        text = "port 22022\nlistenaddress 0.0.0.0:2222\n"
        out = io.StringIO()
        with mock.patch("subprocess.run", return_value=_completed(text)):
            with mock.patch("sys.stdout", out):
                rc = cli.main([])
        self.assertEqual(rc, 0)
        self.assertEqual(out.getvalue().split(), ["22022", "2222"])

    def test_honours_sshd_path_argument(self):
        with mock.patch("subprocess.run", return_value=_completed("port 22\n")) as run:
            with mock.patch("sys.stdout", io.StringIO()):
                cli.main(["--sshd-path", "/opt/sbin/sshd"])
        self.assertEqual(run.call_args.args[0], ["/opt/sbin/sshd", "-T"])

    def test_no_ports_prints_nothing_and_succeeds(self):
        out = io.StringIO()
        with mock.patch("subprocess.run", return_value=_completed("permitrootlogin no\n")):
            with mock.patch("sys.stdout", out):
                rc = cli.main([])
        self.assertEqual(rc, 0)
        self.assertEqual(out.getvalue(), "")


if __name__ == "__main__":
    unittest.main()
