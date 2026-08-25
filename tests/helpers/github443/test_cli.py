"""Unit tests for helpers/github443/cli.py — the thin side-effecting executor.

Network and SSH calls are mocked; file operations run against a tmp directory.

    python3 -m unittest tests.helpers.github443.test_cli
"""

from __future__ import annotations

import os
import pathlib
import stat
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.github443 import cli, core


class TestApplyState(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.ssh_dir = os.path.join(self._tmp.name, ".ssh")

    def tearDown(self):
        self._tmp.cleanup()

    def _config(self):
        with open(os.path.join(self.ssh_dir, "config"), encoding="utf-8") as fh:
            return fh.read()

    def test_present_creates_dir_and_writes_blocks(self):
        res = cli.apply_state(self.ssh_dir, present=True, aliases=["deploy_*"], keys=["ssh-ed25519 AAAA"])
        self.assertTrue(res["config_changed"])
        self.assertTrue(res["known_hosts_changed"])
        cfg = self._config()
        self.assertIn(core.SSH_CONFIG_BEGIN, cfg)
        self.assertIn("Host github.com ssh.github.com github.com-* deploy_*", cfg)
        with open(os.path.join(self.ssh_dir, "known_hosts"), encoding="utf-8") as fh:
            self.assertIn("[ssh.github.com]:443 ssh-ed25519 AAAA", fh.read())

    def test_dir_is_0700_and_files_0600(self):
        cli.apply_state(self.ssh_dir, present=True, aliases=[], keys=["ssh-ed25519 AAAA"])
        dir_mode = stat.S_IMODE(os.stat(self.ssh_dir).st_mode)
        cfg_mode = stat.S_IMODE(os.stat(os.path.join(self.ssh_dir, "config")).st_mode)
        self.assertEqual(dir_mode, 0o700)
        self.assertEqual(cfg_mode, 0o600)

    def test_present_is_idempotent(self):
        cli.apply_state(self.ssh_dir, present=True, aliases=[], keys=["ssh-ed25519 AAAA"])
        res = cli.apply_state(self.ssh_dir, present=True, aliases=[], keys=["ssh-ed25519 AAAA"])
        self.assertFalse(res["config_changed"])
        self.assertFalse(res["known_hosts_changed"])

    def test_absent_removes_block_and_preserves_user_content(self):
        cfg_path = os.path.join(self.ssh_dir, "config")
        os.makedirs(self.ssh_dir, mode=0o700)
        with open(cfg_path, "w", encoding="utf-8") as fh:
            fh.write("Host github.com-work\n    HostName github.com\n")
        cli.apply_state(self.ssh_dir, present=True, aliases=[], keys=["ssh-ed25519 AAAA"])
        res = cli.apply_state(self.ssh_dir, present=False, aliases=[], keys=[])
        self.assertTrue(res["config_changed"])
        cfg = self._config()
        self.assertNotIn(core.SSH_CONFIG_BEGIN, cfg)
        self.assertIn("Host github.com-work", cfg)


class TestFetchKeys(unittest.TestCase):
    def test_uses_meta_api_when_available(self):
        payload = b'{"ssh_keys": ["ssh-ed25519 AAAA", "ssh-rsa BBBB"]}'
        fake = mock.MagicMock()
        fake.read.return_value = payload
        fake.__enter__.return_value = fake
        with mock.patch.object(cli.urllib.request, "urlopen", return_value=fake) as uo:
            keys = cli.fetch_keys()
        uo.assert_called_once()
        self.assertEqual(keys, ["ssh-ed25519 AAAA", "ssh-rsa BBBB"])

    def test_falls_back_to_keyscan_when_meta_fails(self):
        scan = mock.MagicMock()
        scan.returncode = 0
        scan.stdout = "[ssh.github.com]:443 ssh-ed25519 AAAA\n"
        with (
            mock.patch.object(cli.urllib.request, "urlopen", side_effect=OSError("no net")),
            mock.patch.object(cli.subprocess, "run", return_value=scan) as run,
        ):
            keys = cli.fetch_keys()
        run.assert_called_once()
        self.assertEqual(keys, ["ssh-ed25519 AAAA"])

    def test_raises_when_both_sources_fail(self):
        scan = mock.MagicMock()
        scan.returncode = 1
        scan.stdout = ""
        with (
            mock.patch.object(cli.urllib.request, "urlopen", side_effect=OSError("no net")),
            mock.patch.object(cli.subprocess, "run", return_value=scan),
            self.assertRaises(cli.KeyFetchError),
        ):
            cli.fetch_keys()


class TestProbe(unittest.TestCase):
    def _result(self, rc, text):
        r = mock.MagicMock()
        r.returncode = rc
        r.stdout = text
        r.stderr = ""
        return r

    def test_true_on_successful_auth(self):
        out = self._result(1, "Hi octocat! You've successfully authenticated, but GitHub does not provide shell access.")
        with mock.patch.object(cli.subprocess, "run", return_value=out):
            self.assertTrue(cli.probe("github.com", "22"))

    def test_false_on_timeout_or_refusal(self):
        out = self._result(255, "ssh: connect to host github.com port 22: Connection timed out")
        with mock.patch.object(cli.subprocess, "run", return_value=out):
            self.assertFalse(cli.probe("github.com", "22"))


class TestMainEnv(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.ssh_dir = os.path.join(self._tmp.name, ".ssh")

    def tearDown(self):
        self._tmp.cleanup()

    def test_env_reports_unset_when_no_block(self):
        out = []
        with mock.patch("builtins.print", side_effect=lambda *a, **k: out.append(" ".join(map(str, a)))):
            rc = cli.main(["env", "--ssh-dir", self.ssh_dir])
        self.assertEqual(rc, 0)
        self.assertIn("unset GITHUB_SSH_443", out)

    def test_env_reports_export_after_on(self):
        cli.apply_state(self.ssh_dir, present=True, aliases=[], keys=["ssh-ed25519 AAAA"])
        out = []
        with mock.patch("builtins.print", side_effect=lambda *a, **k: out.append(" ".join(map(str, a)))):
            cli.main(["env", "--ssh-dir", self.ssh_dir])
        self.assertIn("export GITHUB_SSH_443=1", out)


if __name__ == "__main__":
    unittest.main()
