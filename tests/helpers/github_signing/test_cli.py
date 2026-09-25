"""Unit tests for helpers/github_signing/cli.py — registration and git's key selection.

gh and ssh-keygen are mocked at the subprocess boundary; nothing reaches GitHub. The
selection check runs the real git against a throwaway HOME.

    python3 -m unittest tests.helpers.github_signing.test_cli
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

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.github_signing import cli

MACHINE_PUB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMACHINE machine"
ALICE_PUB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIALICE alice"
BOB_PUB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBOB bob"


def _done(args, rc=0, out="", err=""):
    return subprocess.CompletedProcess(args, rc, stdout=out, stderr=err)


class FakeGitHub:
    """gh and ssh-keygen as the CLI sees them. Accounts hold auth and signing key lines."""

    def __init__(self, keys):
        self.emails = {
            "alice": ["me@example.com", "a@example.com"],
            "bob": ["b@example.com"],
        }
        self.signing = {"alice": [], "bob": []}
        self.tokens = {"alice": "tok-alice", "bob": "tok-bob"}
        self.keys = keys  # private path -> public line ssh-keygen -y derives
        self.added = []

    def __call__(self, argv, **kwargs):
        env = kwargs.get("env") or {}
        if argv[0] == "ssh-keygen":
            path = argv[-1]
            if path in self.keys:
                return _done(argv, out=self.keys[path] + "\n")
            return _done(
                argv, rc=1, err="incorrect passphrase supplied to decrypt private key"
            )
        if argv[:3] == ["gh", "auth", "token"]:
            login = argv[argv.index("--user") + 1]
            if login in self.tokens:
                return _done(argv, out=self.tokens[login] + "\n")
            return _done(argv, rc=1, err="no oauth token found")
        login = next(
            user for user, tok in self.tokens.items() if tok == env.get("GH_TOKEN")
        )
        if argv[:2] == ["gh", "api"] and "user/emails" in argv:
            return _done(argv, out="".join(e + "\n" for e in self.emails[login]))
        if argv[:2] == ["gh", "api"] and "user/ssh_signing_keys" in argv:
            return _done(argv, out="".join(line + "\n" for line in self.signing[login]))
        if argv[:3] == ["gh", "ssh-key", "add"]:
            line = pathlib.Path(argv[3]).read_text(encoding="utf-8").strip()
            self.signing[login].append(line)
            self.added.append(
                (login, argv[argv.index("--title") + 1], argv[argv.index("--type") + 1])
            )
            return _done(argv)
        raise AssertionError(f"unexpected command {argv}")


class _Case(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self._tmp.name)
        self.machine = self._key("id_ed25519_git_signing", MACHINE_PUB)
        self.alice = self._key("github_a_signing", ALICE_PUB)
        self.bob = self._key("github_b_signing", BOB_PUB)
        self.gh = FakeGitHub(
            {
                str(p): pub
                for p, pub in (
                    (self.machine, MACHINE_PUB),
                    (self.alice, ALICE_PUB),
                    (self.bob, BOB_PUB),
                )
            }
        )

    def tearDown(self):
        self._tmp.cleanup()

    def _key(self, name, pub):
        private = self.dir / name
        private.write_text("placeholder private key bytes\n", encoding="utf-8")
        private.chmod(0o600)
        (self.dir / f"{name}.pub").write_text(pub + "\n", encoding="utf-8")
        return private

    def register(self, accounts='{"a": "alice", "b": "bob"}'):
        argv = [
            "register",
            "--title",
            "box",
            "--ssh-dir",
            str(self.dir),
            "--accounts",
            accounts,
            "--machine-key",
            str(self.machine),
            "--email",
            "me@example.com",
        ]
        out, err = io.StringIO(), io.StringIO()
        with (
            mock.patch("subprocess.run", self.gh),
            mock.patch("sys.stdout", out),
            mock.patch("sys.stderr", err),
        ):
            rc = cli.main(argv)
        return rc, out.getvalue(), err.getvalue()


class TestRegister(_Case):
    def test_every_account_key_and_the_machine_key_are_added(self):
        rc, out, _ = self.register()
        self.assertEqual(rc, 0)
        self.assertEqual(
            sorted(self.gh.added),
            [
                ("alice", "box github_a_signing", "signing"),
                ("alice", "box id_ed25519_git_signing", "signing"),
                ("bob", "box github_b_signing", "signing"),
            ],
        )
        self.assertIn("SIGNING-ADDED alice github_a_signing", out)
        self.assertIn("SIGNING-ADDED alice id_ed25519_git_signing", out)

    def test_the_machine_key_goes_on_the_account_with_the_commit_email(self):
        self.gh.emails = {"alice": ["a@example.com"], "bob": ["me@example.com"]}
        rc, _, _ = self.register()
        self.assertEqual(rc, 0)
        self.assertIn(("bob", "box id_ed25519_git_signing", "signing"), self.gh.added)

    def test_a_second_run_adds_nothing(self):
        self.register()
        self.gh.added.clear()
        rc, out, _ = self.register()
        self.assertEqual((rc, self.gh.added), (0, []))
        self.assertNotIn("SIGNING-ADDED", out)
        self.assertIn("SIGNING-PRESENT bob github_b_signing", out)

    def test_an_account_gh_holds_no_token_for_is_refused_before_anything_is_added(self):
        del self.gh.tokens["bob"]
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("bob", err)

    def test_a_commit_email_on_no_account_is_refused_before_anything_is_added(self):
        self.gh.emails = {"alice": ["a@example.com"], "bob": []}
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("me@example.com", err)
        self.assertIn("none of", err)

    def test_an_empty_key_is_refused_as_empty_not_as_a_passphrase(self):
        self.bob.write_text("", encoding="utf-8")
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("is empty", err)
        self.assertNotIn("passphrase", err)

    def test_a_key_with_a_passphrase_is_refused(self):
        del self.gh.keys[str(self.bob)]
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("passphrase", err)

    def test_a_private_key_readable_by_others_is_refused(self):
        self.alice.chmod(0o644)
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("0600", err)

    def test_a_public_half_that_does_not_match_is_refused(self):
        (self.dir / "github_a_signing.pub").write_text(BOB_PUB + "\n", encoding="utf-8")
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("does not match", err)

    def test_a_missing_public_half_is_refused(self):
        (self.dir / "github_b_signing.pub").unlink()
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("github_b_signing.pub", err)

    def test_accounts_that_are_not_a_map_of_alias_to_login_are_refused(self):
        for bad in ("[]", "{}", '{"a": ""}', '{"-x": "alice"}', "not json"):
            with self.subTest(bad=bad):
                rc, _, err = self.register(bad)
                self.assertEqual((rc, self.gh.added), (1, []))
                self.assertIn("--accounts", err)

    def test_a_failed_upload_fails_the_run(self):
        real = self.gh

        def failing(argv, **kwargs):
            if argv[:3] == ["gh", "ssh-key", "add"]:
                return _done(argv, rc=1, err="HTTP 422: key is already in use")
            return real(argv, **kwargs)

        with mock.patch("subprocess.run", failing):
            out, err = io.StringIO(), io.StringIO()
            with mock.patch("sys.stdout", out), mock.patch("sys.stderr", err):
                rc = cli.main(
                    [
                        "register",
                        "--title",
                        "box",
                        "--ssh-dir",
                        str(self.dir),
                        "--accounts",
                        '{"a": "alice"}',
                        "--machine-key",
                        str(self.machine),
                        "--email",
                        "me@example.com",
                    ]
                )
        self.assertEqual(rc, 1)
        self.assertIn("already in use", err.getvalue())


class TestCheckSelection(unittest.TestCase):
    """The real git, reading a throwaway global config, picks the key for each remote."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        home = pathlib.Path(self._tmp.name)
        (home / "work.gitconfig").write_text(
            "[user]\n\tsigningkey = /keys/github_work_signing\n", encoding="utf-8"
        )
        self.accounts = home / "accounts.gitconfig"
        self.accounts.write_text(
            f'[includeIf "hasconfig:remote.*.url:git@github.com-work:*/**"]\n\tpath = {home}/work.gitconfig\n'
            f'[includeIf "hasconfig:remote.*.url:ssh://git@github.com-work/**"]\n\tpath = {home}/work.gitconfig\n',
            encoding="utf-8",
        )
        self.gitconfig = home / "gitconfig"
        self.gitconfig.write_text(
            f"[user]\n\tsigningkey = /keys/machine\n[include]\n\tpath = {home}/accounts.gitconfig\n",
            encoding="utf-8",
        )
        self.env = {
            k: v for k, v in os.environ.items() if not k.startswith("GIT_CONFIG")
        }
        self.env.update(GIT_CONFIG_GLOBAL=str(self.gitconfig), GIT_CONFIG_NOSYSTEM="1")

    def tearDown(self):
        self._tmp.cleanup()

    def check(self, accounts, fallback, *extra):
        argv = ["--ssh-dir", "/keys", "--accounts", accounts, "--fallback", fallback]
        out, err = io.StringIO(), io.StringIO()
        with (
            mock.patch.dict(os.environ, self.env, clear=True),
            mock.patch("sys.stdout", out),
            mock.patch("sys.stderr", err),
        ):
            rc = cli.main(["check-selection", *argv, *extra])
        return rc, out.getvalue(), err.getvalue()

    def test_each_alias_remote_gets_its_key_and_any_other_the_fallback(self):
        rc, out, err = self.check('{"work": "w"}', "/keys/machine")
        self.assertEqual((rc, out.strip()), (0, "SELECTION-OK"), err)

    def test_a_repo_with_remotes_on_two_accounts_signs_as_the_one_included_last(self):
        # What docs/configuration.md "Commit Signing" tells the owner: one account per
        # repository, or the account listed last in github_accounts signs.
        home = self.gitconfig.parent
        (home / "home.gitconfig").write_text(
            "[user]\n\tsigningkey = /keys/github_home_signing\n", encoding="utf-8"
        )
        with self.accounts.open("a", encoding="utf-8") as handle:
            handle.write(
                f'[includeIf "hasconfig:remote.*.url:git@github.com-home:*/**"]\n'
                f"\tpath = {home}/home.gitconfig\n"
            )
        repo = home / "repo"
        for argv in (
            ["git", "init", "-q", str(repo)],
            [
                "git",
                "-C",
                str(repo),
                "remote",
                "add",
                "origin",
                "git@github.com-work:o/r.git",
            ],
            [
                "git",
                "-C",
                str(repo),
                "remote",
                "add",
                "fork",
                "git@github.com-home:o/r.git",
            ],
        ):
            subprocess.run(argv, check=True, env=self.env, capture_output=True)
        picked = subprocess.run(
            ["git", "-C", str(repo), "config", "--get", "user.signingkey"],
            check=True,
            env=self.env,
            capture_output=True,
            text=True,
        ).stdout.strip()
        self.assertEqual(picked, "/keys/github_home_signing")

    def test_an_alias_whose_ssh_url_form_is_not_picked_is_refused(self):
        text = self.accounts.read_text(encoding="utf-8")
        self.accounts.write_text(
            text.split("[includeIf", 2)[1].join(["[includeIf", ""]), encoding="utf-8"
        )
        rc, _, err = self.check('{"work": "w"}', "/keys/machine")
        self.assertEqual(rc, 1)
        self.assertIn("ssh://git@github.com-work/", err)

    def test_an_alias_whose_key_git_does_not_pick_is_refused(self):
        rc, _, err = self.check('{"work": "w", "home": "h"}', "/keys/machine")
        self.assertEqual(rc, 1)
        self.assertIn("github.com-home", err)
        self.assertIn("/keys/machine", err)

    def test_a_fallback_that_git_does_not_use_is_refused(self):
        rc, _, err = self.check('{"work": "w"}', "/keys/other")
        self.assertEqual(rc, 1)
        self.assertIn("/keys/other", err)

    def test_a_later_top_level_key_that_overrides_the_accounts_is_caught(self):
        with self.gitconfig.open("a", encoding="utf-8") as handle:
            handle.write("[user]\n\tsigningkey = /keys/machine\n")
        rc, _, err = self.check('{"work": "w"}', "/keys/machine")
        self.assertEqual(rc, 1)
        self.assertIn("github.com-work", err)


if __name__ == "__main__":
    unittest.main()
