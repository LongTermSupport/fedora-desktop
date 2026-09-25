"""Unit tests for helpers/github_scopes/cli.py — the executor every scope check calls.

gh is mocked at the subprocess boundary; nothing reaches GitHub.

    python3 -m unittest tests.helpers.github_scopes.test_cli
"""

from __future__ import annotations

import io
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.github_scopes import cli

SCOPES_TEXT = "github_required_scopes:\n  - repo\n  - gist\n  - admin:public_key\n"


def _done(args, rc=0, out="", err=""):
    return subprocess.CompletedProcess(args, rc, stdout=out, stderr=err)


def _headers(granted):
    return f"HTTP/2.0 200 OK\r\nX-Oauth-Scopes: {granted}\r\n\r\n{{}}\n"


class _CliCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.scopes_file = pathlib.Path(self._tmp.name) / "scopes.yml"
        self.scopes_file.write_text(SCOPES_TEXT, encoding="utf-8")

    def tearDown(self):
        self._tmp.cleanup()

    def run_cli(self, argv, stdin=""):
        out, err = io.StringIO(), io.StringIO()
        with mock.patch("sys.stdin", io.StringIO(stdin)), mock.patch("sys.stdout", out), mock.patch(
            "sys.stderr", err
        ):
            rc = cli.main([*argv, "--scopes-file", str(self.scopes_file)])
        return rc, out.getvalue(), err.getvalue()


class TestRequired(_CliCase):
    def test_prints_the_list_comma_joined(self):
        rc, out, _ = self.run_cli(["required"])
        self.assertEqual((rc, out), (0, "repo,gist,admin:public_key\n"))

    def test_the_default_file_is_the_repository_one(self):
        out = io.StringIO()
        with mock.patch("sys.stdout", out):
            rc = cli.main(["required"])
        self.assertEqual(rc, 0)
        self.assertIn("repo", out.getvalue().strip().split(","))

    def test_a_missing_file_fails(self):
        err = io.StringIO()
        with mock.patch("sys.stderr", err):
            rc = cli.main(["required", "--scopes-file", "/nonexistent/scopes.yml"])
        self.assertEqual(rc, 1)
        self.assertIn("/nonexistent/scopes.yml", err.getvalue())


class TestMissing(_CliCase):
    def test_prints_every_missing_scope_on_one_line(self):
        rc, out, _ = self.run_cli(["missing"], stdin=_headers("gist"))
        self.assertEqual((rc, out), (0, "repo,admin:public_key\n"))

    def test_prints_nothing_when_nothing_is_missing(self):
        rc, out, _ = self.run_cli(["missing"], stdin=_headers("repo, gist, admin:public_key"))
        self.assertEqual((rc, out), (0, ""))

    def test_marker_form(self):
        rc, out, _ = self.run_cli(["missing", "--marker"], stdin=_headers("repo, gist, admin:public_key"))
        self.assertEqual((rc, out), (0, "OK\n"))
        rc, out, _ = self.run_cli(["missing", "--marker"], stdin=_headers("repo"))
        self.assertEqual((rc, out), (0, "MISSING:gist,admin:public_key\n"))


class TestAudit(_CliCase):
    def _fake_gh(self, table):
        """table: login -> ("token", granted) | ("no-token", msg) | ("api-fails", msg)."""

        def run(args, **kwargs):
            if args[:3] == ["gh", "auth", "token"]:
                login = args[args.index("--user") + 1]
                kind, value = table[login]
                if kind == "no-token":
                    return _done(args, 1, err=value)
                return _done(args, 0, out=f"tok-{login}\n")
            if args[:3] == ["gh", "api", "-i"]:
                token = kwargs["env"]["GH_TOKEN"]
                login = token.removeprefix("tok-")
                kind, value = table[login]
                if kind == "api-fails":
                    return _done(args, 1, err=value)
                return _done(args, 0, out=_headers(value))
            raise AssertionError(f"unexpected command {args}")

        return run

    def test_one_marker_line_per_account(self):
        table = {
            "alice": ("token", "repo, gist, admin:public_key"),
            "bob": ("token", "repo"),
            "carol": ("no-token", "no oauth token found for carol"),
            "dave": ("api-fails", "HTTP 401: Bad credentials"),
        }
        with mock.patch("subprocess.run", side_effect=self._fake_gh(table)) as run:
            rc, out, _ = self.run_cli(
                ["audit", "--user", "alice", "--user", "bob", "--user", "carol", "--user", "dave"]
            )
        self.assertEqual(rc, 0)
        self.assertEqual(
            out.splitlines(),
            [
                "SCOPES-OK alice",
                "SCOPES-MISSING bob gist,admin:public_key",
                "SCOPES-NOT-AUTHENTICATED carol",
                "SCOPES-UNREADABLE dave HTTP 401: Bad credentials",
            ],
        )
        # Each account is read with its own token: the active gh account is never switched.
        for call in run.call_args_list:
            self.assertNotIn("switch", call.args[0])

    def test_the_token_never_reaches_argv_or_stdout(self):
        table = {"alice": ("token", "repo")}
        with mock.patch("subprocess.run", side_effect=self._fake_gh(table)) as run:
            _, out, err = self.run_cli(["audit", "--user", "alice"])
        for call in run.call_args_list:
            self.assertNotIn("tok-alice", " ".join(call.args[0]))
        self.assertNotIn("tok-alice", out + err)

    def test_every_gh_call_states_check(self):
        table = {"alice": ("token", "repo")}
        with mock.patch("subprocess.run", side_effect=self._fake_gh(table)) as run:
            self.run_cli(["audit", "--user", "alice"])
        for call in run.call_args_list:
            self.assertIn("check", call.kwargs)

    def test_no_user_is_a_usage_error(self):
        with self.assertRaises(SystemExit):
            self.run_cli(["audit"])


if __name__ == "__main__":
    unittest.main()
