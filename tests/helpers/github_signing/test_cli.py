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


RETIRED_PUB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIRETIRED retired"


class FakeGitHub:
    """gh and ssh-keygen as the CLI sees them. Accounts hold signing keys, with ids.

    The keys are login keys, so by default each has a passphrase: ssh-keygen -y -P ""
    fails with "incorrect passphrase", as it does for ~/.ssh/id and github_<alias>.
    """

    def __init__(self):
        self.emails = {
            "alice": ["me@example.com", "a@example.com"],
            "bob": ["b@example.com"],
        }
        self.signing = {"alice": [], "bob": []}  # login -> [(id, public line)]
        self.tokens = {"alice": "tok-alice", "bob": "tok-bob"}
        self.unlocked = {}  # private path -> public line, for a key with no passphrase
        self.not_keys = set()  # private paths ssh-keygen cannot read as a key at all
        self.added = []
        self.deleted = []
        self.delete_error = ""
        self._next_id = 100

    def __call__(self, argv, **kwargs):
        env = kwargs.get("env") or {}
        if argv[0] == "ssh-keygen":
            path = argv[-1]
            if path in self.not_keys:
                return _done(argv, rc=255, err=f'Load key "{path}": invalid format')
            if path in self.unlocked:
                return _done(argv, out=self.unlocked[path] + "\n")
            return _done(
                argv, rc=255, err="incorrect passphrase supplied to decrypt private key"
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
        if argv[:4] == ["gh", "api", "-X", "DELETE"]:
            if self.delete_error:
                return _done(argv, rc=1, err=self.delete_error)
            key_id = int(argv[4].rsplit("/", 1)[1])
            self.signing[login] = [k for k in self.signing[login] if k[0] != key_id]
            self.deleted.append((login, key_id))
            return _done(argv)
        if argv[:2] == ["gh", "api"] and "user/ssh_signing_keys" in argv:
            jq = argv[argv.index("--jq") + 1]
            rows = (
                f"{key_id} {line}" if ".id" in jq else line
                for key_id, line in self.signing[login]
            )
            return _done(argv, out="".join(row + "\n" for row in rows))
        if argv[:3] == ["gh", "ssh-key", "add"]:
            self.hold(login, pathlib.Path(argv[3]).read_text(encoding="utf-8"))
            self.added.append(
                (login, argv[argv.index("--title") + 1], argv[argv.index("--type") + 1])
            )
            return _done(argv)
        raise AssertionError(f"unexpected command {argv}")

    def hold(self, login, line):
        self._next_id += 1
        self.signing[login].append((self._next_id, line.strip()))
        return self._next_id


class _Case(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self._tmp.name)
        self.machine = self._key("id", MACHINE_PUB)
        self.alice = self._key("github_a", ALICE_PUB)
        self.bob = self._key("github_b", BOB_PUB)
        self.gh = FakeGitHub()

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
                ("alice", "box github_a", "signing"),
                ("alice", "box id", "signing"),
                ("bob", "box github_b", "signing"),
            ],
        )
        self.assertIn("SIGNING-ADDED alice github_a", out)
        self.assertIn("SIGNING-ADDED alice id", out)

    def test_the_machine_key_goes_on_the_account_with_the_commit_email(self):
        self.gh.emails = {"alice": ["a@example.com"], "bob": ["me@example.com"]}
        rc, _, _ = self.register()
        self.assertEqual(rc, 0)
        self.assertIn(("bob", "box id", "signing"), self.gh.added)

    def test_a_second_run_adds_nothing(self):
        self.register()
        self.gh.added.clear()
        rc, out, _ = self.register()
        self.assertEqual((rc, self.gh.added), (0, []))
        self.assertNotIn("SIGNING-ADDED", out)
        self.assertIn("SIGNING-PRESENT bob github_b", out)

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

    def test_a_login_key_with_a_passphrase_is_registered(self):
        # The signer's agent holds the unlocked key, so a passphrase is no obstacle.
        rc, out, err = self.register()
        self.assertEqual(rc, 0, err)
        self.assertIn("SIGNING-ADDED bob github_b", out)

    def test_a_file_that_is_not_a_private_key_is_refused(self):
        self.gh.not_keys.add(str(self.bob))
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("not a private key", err)

    def test_a_private_key_readable_by_others_is_refused(self):
        self.alice.chmod(0o644)
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("0600", err)

    def test_a_public_half_that_does_not_match_is_refused(self):
        # Checkable only for a key with no passphrase; with one, the agent's signature
        # fails to verify instead, which acceptance catches.
        self.gh.unlocked[str(self.alice)] = ALICE_PUB
        (self.dir / "github_a.pub").write_text(BOB_PUB + "\n", encoding="utf-8")
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("does not match", err)

    def test_a_missing_public_half_is_refused(self):
        (self.dir / "github_b.pub").unlink()
        rc, _, err = self.register()
        self.assertEqual((rc, self.gh.added), (1, []))
        self.assertIn("github_b.pub", err)

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


class TestRetire(_Case):
    """The passphrase-free signing keys D5 withdraws come off GitHub, and nothing else."""

    def setUp(self):
        super().setUp()
        self.old_key = self._key("github_a_signing", RETIRED_PUB)
        self.gh.hold("alice", RETIRED_PUB)
        self.gh.hold("bob", RETIRED_PUB)
        self.kept = self.gh.hold("alice", ALICE_PUB)

    def retire(self, paths=None):
        argv = [
            "retire",
            "--ssh-dir",
            str(self.dir),
            "--accounts",
            '{"a": "alice", "b": "bob"}',
            "--machine-key",
            str(self.machine),
        ]
        for path in paths or [self.old_key]:
            argv += ["--retired", str(path)]
        out, err = io.StringIO(), io.StringIO()
        with (
            mock.patch("subprocess.run", self.gh),
            mock.patch("sys.stdout", out),
            mock.patch("sys.stderr", err),
        ):
            rc = cli.main(argv)
        return rc, out.getvalue(), err.getvalue()

    def test_a_retired_key_is_deleted_from_every_account_holding_it(self):
        rc, out, err = self.retire()
        self.assertEqual(rc, 0, err)
        self.assertEqual(
            sorted(login for login, _ in self.gh.deleted), ["alice", "bob"]
        )
        self.assertIn("SIGNING-RETIRED alice github_a_signing", out)
        self.assertIn("SIGNING-RETIRED bob github_a_signing", out)

    def test_the_keys_in_use_are_left_registered(self):
        self.retire()
        self.assertNotIn(("alice", self.kept), self.gh.deleted)
        self.assertEqual([line for _, line in self.gh.signing["alice"]], [ALICE_PUB])

    def test_a_second_run_deletes_nothing(self):
        self.retire()
        self.gh.deleted.clear()
        rc, out, _ = self.retire()
        self.assertEqual((rc, self.gh.deleted), (0, []))
        self.assertNotIn("SIGNING-RETIRED", out)

    def test_a_retired_file_that_is_already_gone_is_nothing_to_do(self):
        rc, _, err = self.retire([self.dir / "github_zz_signing"])
        self.assertEqual((rc, self.gh.deleted), (0, []), err)

    def test_retiring_a_key_still_in_use_is_refused_before_anything_is_deleted(self):
        rc, _, err = self.retire([self.old_key, self.alice])
        self.assertEqual((rc, self.gh.deleted), (1, []))
        self.assertIn("github_a", err)
        self.assertIn("in use", err)

    def test_a_failed_delete_fails_the_run(self):
        self.gh.delete_error = "HTTP 404: Not Found"
        rc, _, err = self.retire()
        self.assertEqual(rc, 1)
        self.assertIn("404", err)


class TestCheckSelection(unittest.TestCase):
    """The real git, reading a throwaway global config, picks the key for each remote."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        home = pathlib.Path(self._tmp.name)
        (home / "work.gitconfig").write_text(
            "[user]\n\tsigningkey = /keys/github_work\n", encoding="utf-8"
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
            "[user]\n\tsigningkey = /keys/github_home\n", encoding="utf-8"
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
        self.assertEqual(picked, "/keys/github_home")

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
