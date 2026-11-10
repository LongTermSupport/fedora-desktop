"""Unit tests for helpers/ssh_passphrase/cli.py: ssh-keygen driven through SSH_ASKPASS.

The real ssh-keygen runs over throwaway keys in a temporary directory; the argv tests record
every command the helper starts.

    python3 -m unittest tests.helpers.ssh_passphrase.test_cli
"""

from __future__ import annotations

import io
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.ssh_passphrase import cli

PASS = "throwaway passphrase, with spaces & 'quotes'"


def run(argv: list[str], stdin: str) -> tuple[int, str]:
    err = io.StringIO()
    with mock.patch("sys.stdin", io.StringIO(stdin)), mock.patch("sys.stderr", err):
        code = cli.main(argv)
    return code, err.getvalue()


def unlocks_with_empty(key: str) -> bool:
    """Whether the key opens with no passphrase (a literal empty -P is no secret)."""
    res = subprocess.run(
        ["ssh-keygen", "-y", "-P", "", "-f", key],
        stdin=subprocess.DEVNULL, capture_output=True, check=False,
    )
    return res.returncode == 0


class TestRealSshKeygen(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.key = os.path.join(self._tmp.name, "github_test")

    def tearDown(self):
        self._tmp.cleanup()

    def test_generate_makes_a_key_the_passphrase_opens_and_nothing_else(self):
        code, err = run(["generate", self.key, "test@github"], PASS + "\n")
        self.assertEqual(code, 0, err)
        self.assertTrue(os.path.getsize(self.key) > 0)
        self.assertIn("test@github", pathlib.Path(self.key + ".pub").read_text(encoding="utf-8"))
        self.assertFalse(unlocks_with_empty(self.key))
        self.assertEqual(run(["unlocks", self.key], PASS + "\n")[0], 0)
        self.assertEqual(run(["unlocks", self.key], "wrong\n")[0], 1)

    def test_strip_leaves_a_key_with_no_passphrase(self):
        self.assertEqual(run(["generate", self.key, "c"], PASS)[0], 0)
        code, err = run(["strip", self.key], PASS + "\n")
        self.assertEqual(code, 0, err)
        self.assertTrue(unlocks_with_empty(self.key))

    def test_strip_with_the_wrong_passphrase_changes_nothing(self):
        self.assertEqual(run(["generate", self.key, "c"], PASS)[0], 0)
        self.assertEqual(run(["strip", self.key], "wrong")[0], 1)
        self.assertFalse(unlocks_with_empty(self.key))

    def test_a_missing_key_cannot_be_judged(self):
        code, err = run(["unlocks", self.key], PASS)
        self.assertEqual(code, 2)
        self.assertIn(self.key, err)


class TestTheSecretStaysOffEveryCommandLine(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.key = os.path.join(self._tmp.name, "k")
        pathlib.Path(self.key).write_text("x", encoding="utf-8")
        self.calls: list[tuple[list[str], dict[str, str]]] = []
        self.askpass_dirs: list[str] = []

        def fake_run(argv, **kwargs):
            env = kwargs["env"]
            askpass = env["SSH_ASKPASS"]
            self.askpass_dirs.append(os.path.dirname(askpass))
            self.askpass_text = pathlib.Path(askpass).read_text(encoding="utf-8")
            self.askpass_mode = os.stat(askpass).st_mode & 0o777
            self.calls.append((list(argv), dict(env)))
            return subprocess.CompletedProcess(argv, 0, b"", b"")

        patcher = mock.patch.object(cli.subprocess, "run", side_effect=fake_run)
        patcher.start()
        self.addCleanup(patcher.stop)

    def tearDown(self):
        self._tmp.cleanup()

    def test_every_mode_hands_the_passphrase_over_by_askpass_only(self):
        for argv in (["generate", self.key + ".new", "c"], ["unlocks", self.key], ["strip", self.key]):
            self.calls.clear()
            self.assertEqual(run(argv, PASS + "\n")[0], 0)
            self.assertEqual(len(self.calls), 1)
            cmd, env = self.calls[0]
            self.assertEqual(cmd[0], "ssh-keygen")
            for word in cmd:
                self.assertNotIn("passphrase", word)
                self.assertNotIn(PASS, word)
            self.assertEqual(env["SSH_ASKPASS_REQUIRE"], "force")
            self.assertEqual(env[cli.PASSPHRASE_VAR], PASS)
            self.assertNotIn(PASS, self.askpass_text)
            self.assertEqual(self.askpass_mode, 0o700)

    def test_the_askpass_directory_is_removed_and_private(self):
        self.assertEqual(run(["unlocks", self.key], PASS)[0], 0)
        self.assertFalse(os.path.exists(self.askpass_dirs[0]))

    def test_only_one_trailing_newline_is_taken_off(self):
        run(["unlocks", self.key], " pass \n\n")
        self.assertEqual(self.calls[0][1][cli.PASSPHRASE_VAR], " pass \n")

    def test_an_empty_passphrase_is_refused_before_anything_runs(self):
        code, err = run(["unlocks", self.key], "\n")
        self.assertEqual(code, 2)
        self.assertIn("no passphrase on stdin", err)
        self.assertEqual(self.calls, [])

    def test_bad_usage_is_exit_2(self):
        with mock.patch("sys.stderr", io.StringIO()), self.assertRaises(SystemExit) as cm:
            cli.main(["nope"])
        self.assertEqual(cm.exception.code, 2)


if __name__ == "__main__":
    unittest.main()
