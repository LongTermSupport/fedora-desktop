"""Tests for helpers.self_update.update — the safe updater and the signed-tip gate, for real.

Every case builds real repositories: a bare "origin", an author clone that pushes to it,
and the deploy clone under test. Signing uses throwaway ed25519 keys made by ssh-keygen,
so what is exercised is git's real verification, not a stand-in for it. Global and
system git config are cut off, so a developer's own settings cannot change a verdict.
"""

from __future__ import annotations

import io
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import signers, update

PRINCIPAL = "owner@example.com"
BRANCH = "F44"
REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))


class Fixture:
    """origin (bare), author (pushes), deploy (the clone the updater runs on)."""

    def __init__(self, root: str) -> None:
        self.root = root
        self.home = os.path.join(root, "home")
        os.makedirs(self.home)
        # Config passed through the environment outranks GIT_CONFIG_GLOBAL, so it goes too.
        inherited = {k: v for k, v in os.environ.items()
                     if k not in ("GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS")}
        self.env = {
            **inherited,
            "HOME": self.home,
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_CONFIG_SYSTEM": os.devnull,
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_TERMINAL_PROMPT": "0",
        }
        self.owner_key = self._key("owner")
        self.other_key = self._key("other")
        self.account_key = self._key("account")
        self.allowed = os.path.join(root, "allowed_signers")
        with open(self.owner_key + ".pub", encoding="utf-8") as handle:
            kind, blob = handle.read().split()[:2]
        with open(self.allowed, "w", encoding="utf-8") as handle:
            handle.write(f'{PRINCIPAL} namespaces="git" {kind} {blob}\n')
        os.chmod(self.allowed, 0o644)
        os.chmod(root, 0o755)
        self.os_release = os.path.join(root, "os-release")
        self.set_running_fedora(44)

        self.origin = os.path.join(root, "origin.git")
        self.author = os.path.join(root, "author")
        self.deploy = os.path.join(root, "deploy")
        self.git(root, "init", "-q", "--bare", self.origin)
        self.git(self.origin, "symbolic-ref", "HEAD", f"refs/heads/{BRANCH}")
        self.git(root, "clone", "-q", self.origin, self.author)
        self.git(self.author, "checkout", "-q", "-b", BRANCH)
        self._identity(self.author)
        self.commit("vars/fedora-version.yml", "---\nfedora_version: 44\n", "initial", sign="owner")
        self.git(self.author, "push", "-q", "origin", BRANCH)
        self.git(root, "clone", "-q", "-b", BRANCH, self.origin, self.deploy)
        self._identity(self.deploy)

    def _key(self, name: str) -> str:
        path = os.path.join(self.root, name)
        subprocess.run(
            ["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", name, "-f", path],
            check=True, capture_output=True, env=self.env,
        )
        return path

    def _identity(self, repo: str) -> None:
        self.git(repo, "config", "user.email", "a@example.com")
        self.git(repo, "config", "user.name", "a")
        self.git(repo, "config", "gpg.format", "ssh")

    def git(self, cwd: str, *args: str) -> str:
        result = subprocess.run(
            ["git", *args], cwd=cwd, check=True, capture_output=True, text=True, env=self.env,
        )
        return result.stdout.strip()

    def commit(self, path: str, content: str, message: str, *, sign: str | None = None) -> str:
        full = os.path.join(self.author, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w", encoding="utf-8") as handle:
            handle.write(content)
        self.git(self.author, "add", path)
        args = ["commit", "-q", "-m", message]
        if sign is not None:
            key = {"owner": self.owner_key, "account": self.account_key}.get(sign, self.other_key)
            args = ["-c", f"user.signingkey={key}", *args, "-S"]
        self.git(self.author, *args)
        return self.git(self.author, "rev-parse", "HEAD")

    def write_object(self, raw: str) -> str:
        return subprocess.run(
            ["git", "hash-object", "-t", "commit", "-w", "--stdin"],
            cwd=self.author, input=raw, check=True, capture_output=True, text=True, env=self.env,
        ).stdout.strip()

    def force_branch_to(self, sha: str) -> None:
        self.git(self.author, "update-ref", f"refs/heads/{BRANCH}", sha)
        self.push("--force")

    def push(self, *extra: str) -> None:
        self.git(self.author, "push", "-q", *extra, "origin", BRANCH)

    def tag(
        self, name: str, sha: str | None = None, *, sign: str | None = "owner", annotated: bool = True,
        message: str | None = None,
    ) -> str:
        """Tag `sha` (default: the author's HEAD). `sign` names the key; None is an unsigned
        annotated tag, and `annotated=False` a lightweight one. Returns the tag ref's value."""
        target = sha or self.git(self.author, "rev-parse", "HEAD")
        if not annotated:
            self.git(self.author, "tag", name, target)
        elif sign is None:
            self.git(self.author, "tag", "-a", "-m", message or f"release {name}", name, target)
        else:
            key = {"owner": self.owner_key, "account": self.account_key}.get(sign, self.other_key)
            self.git(self.author, "-c", f"user.signingkey={key}", "tag", "-s", "-m", message or f"release {name}",
                     name, target)
        return self.git(self.author, "rev-parse", f"refs/tags/{name}")

    def push_tag(self, name: str, *extra: str) -> None:
        self.git(self.author, "push", "-q", *extra, "origin", f"refs/tags/{name}")

    def withdraw_tag(self, name: str) -> None:
        self.git(self.author, "push", "-q", "origin", f":refs/tags/{name}")

    def release(self, name: str, *, path: str = "a.txt", content: str | None = None) -> str:
        """An owner-signed commit on the branch, pushed, and an owner-signed tag on it, pushed.
        Returns the commit."""
        sha = self.commit(path, content or f"{name}\n", f"release {name}", sign="owner")
        self.push()
        self.tag(name, sha)
        self.push_tag(name)
        return sha

    def deployed(self) -> str:
        return self.git(self.deploy, "rev-parse", "HEAD")

    def set_running_fedora(self, version: int) -> None:
        with open(self.os_release, "w", encoding="utf-8") as handle:
            handle.write(f'NAME="Fedora Linux"\nVERSION_ID={version}\n')

    def run(self, **overrides: object) -> tuple[int, str, str]:
        args: dict[str, object] = {
            "checkout": self.deploy,
            "remote": "origin",
            "branch": BRANCH,
            "allowed_signers": self.allowed,
            "principal": PRINCIPAL,
            "os_release": self.os_release,
            "channel": update.CHANNEL_BRANCH,
        }
        args.update(overrides)
        out, err = io.StringIO(), io.StringIO()
        code = update.run(**args, stdout=out, stderr=err, env=self.env)
        return code, out.getvalue(), err.getvalue()


def _evil_program(path: str, marker: str, rc: int) -> None:
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(f"#!/bin/sh\ntouch {marker}\nexit {rc}\n")
    os.chmod(path, 0o755)


class UpdateCase(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.fx = Fixture(self._tmp.name)

    def tearDown(self) -> None:
        self._tmp.cleanup()


class TestTheGate(UpdateCase):
    def test_nothing_new_is_nothing_to_do(self) -> None:
        before = self.fx.deployed()
        code, out, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertIn("SELF-UPDATE-NOTHING", out)
        self.assertEqual(self.fx.deployed(), before)

    def test_a_signed_tip_is_deployed(self) -> None:
        old = self.fx.deployed()
        new = self.fx.commit("a.txt", "a\n", "owner change", sign="owner")
        self.fx.push()
        code, out, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertIn(f"SELF-UPDATE-OLD {old}", out)
        self.assertIn(f"SELF-UPDATE-NEW {new}", out)
        self.assertEqual(self.fx.deployed(), new)

    def test_a_dry_run_names_the_target_and_moves_nothing(self) -> None:
        old = self.fx.deployed()
        new = self.fx.commit("a.txt", "a\n", "owner change", sign="owner")
        self.fx.push()
        code, out, _ = self.fx.run(dry_run=True)
        self.assertEqual(code, update.EXIT_OK)
        self.assertIn(f"SELF-UPDATE-OLD {old}", out)
        self.assertIn(f"SELF-UPDATE-TARGET {new}", out)
        self.assertNotIn("SELF-UPDATE-NEW", out)
        self.assertEqual(self.fx.deployed(), old)

    def test_only_unsigned_commits_waits(self) -> None:
        before = self.fx.deployed()
        self.fx.commit("a.txt", "a\n", "agent change")
        self.fx.push()
        code, out, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertIn("SELF-UPDATE-NOTHING", out)
        self.assertEqual(self.fx.deployed(), before)

    def test_unsigned_commits_above_a_signed_one_wait_and_the_signed_one_deploys(self) -> None:
        signed = self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        self.fx.commit("b.txt", "b\n", "agent on top")
        self.fx.push()
        code, out, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(self.fx.deployed(), signed)
        self.assertIn(f"SELF-UPDATE-NEW {signed}", out)

    def test_a_signed_tip_vouches_for_unsigned_commits_below_it(self) -> None:
        self.fx.commit("a.txt", "a\n", "agent")
        tip = self.fx.commit("b.txt", "b\n", "owner vouches", sign="owner")
        self.fx.push()
        code, _, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(self.fx.deployed(), tip)

    def test_a_key_that_is_not_pinned_is_not_trusted(self) -> None:
        before = self.fx.deployed()
        self.fx.commit("a.txt", "a\n", "someone else signs", sign="other")
        self.fx.push()
        code, out, err = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertIn("SELF-UPDATE-NOTHING", out)
        self.assertEqual(self.fx.deployed(), before)
        self.assertIn("not trusted", err)

    def test_a_tampered_signed_commit_refuses_the_cycle(self) -> None:
        before = self.fx.deployed()
        signed = self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        raw = self.fx.git(self.fx.author, "cat-file", "commit", signed)
        forged = self.fx.write_object(raw.replace("\n\nowner", "\n\nowner, altered") + "\n")
        self.fx.force_branch_to(forged)
        code, out, err = self.fx.run()
        self.assertEqual(code, update.EXIT_BAD_SIGNATURE)
        self.assertNotIn("SELF-UPDATE-NEW", out)
        self.assertIn(forged[:12], err)
        self.assertEqual(self.fx.deployed(), before)

    def test_a_pgp_signed_commit_never_runs_the_repos_gpg_program(self) -> None:
        """A repo-local gpg.program must never execute: the gate only verifies SSH."""
        marker = os.path.join(self.fx.root, "gpg-ran")
        evil = os.path.join(self.fx.root, "evil-gpg")
        _evil_program(evil, marker, 1)
        for key in ("gpg.program", "gpg.openpgp.program", "gpg.ssh.program"):
            self.fx.git(self.fx.deploy, "config", key, evil)
        unsigned = self.fx.commit("a.txt", "a\n", "pgp-looking")
        raw = self.fx.git(self.fx.author, "cat-file", "commit", unsigned)
        head, _, body = raw.partition("\n\n")
        forged = self.fx.write_object(
            f"{head}\ngpgsig -----BEGIN PGP SIGNATURE-----\n iQ==\n -----END PGP SIGNATURE-----\n"
            f"\n{body}\n"
        )
        self.fx.force_branch_to(forged)
        code, out, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertIn("SELF-UPDATE-NOTHING", out)
        self.assertFalse(os.path.exists(marker), "a repo-configured signing program was executed")

    def test_every_git_call_pins_the_signing_programs(self) -> None:
        """The second layer under the PGP test above: even a call that did reach git's
        OpenPGP verifier would run `false`, never a program the repo's config names."""
        git = update._Git(self.fx.deploy, self.fx.allowed, self.fx.env)
        pinned = dict(pair.split("=", 1) for pair in git._config if pair != "-c")
        for key in ("gpg.program", "gpg.openpgp.program", "gpg.x509.program"):
            self.assertEqual(pinned[key], "false", key)
        self.assertEqual(pinned["core.hooksPath"], "/dev/null")
        self.assertEqual(pinned["core.fsmonitor"], "false")
        self.assertTrue(os.path.isabs(pinned["gpg.ssh.program"]))
        self.assertEqual(pinned["gpg.ssh.allowedSignersFile"], os.path.abspath(self.fx.allowed))

    def test_the_repos_own_ssh_program_and_signers_file_are_not_used(self) -> None:
        marker = os.path.join(self.fx.root, "ssh-program-ran")
        evil = os.path.join(self.fx.root, "evil-ssh")
        _evil_program(evil, marker, 0)
        self.fx.git(self.fx.deploy, "config", "gpg.ssh.program", evil)
        self.fx.git(self.fx.deploy, "config", "gpg.ssh.allowedSignersFile", os.devnull)
        new = self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        self.fx.push()
        code, _, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(self.fx.deployed(), new)
        self.assertFalse(os.path.exists(marker))

    def test_hooks_in_the_deploy_clone_do_not_run(self) -> None:
        marker = os.path.join(self.fx.root, "hook-ran")
        hooks = os.path.join(self.fx.deploy, ".git", "hooks")
        for name in ("post-merge", "post-checkout", "reference-transaction"):
            _evil_program(os.path.join(hooks, name), marker, 0)
        self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        self.fx.push()
        code, _, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertFalse(os.path.exists(marker), "a hook in the deploy clone ran")


class TestRefusals(UpdateCase):
    def _signed_change_pending(self) -> None:
        self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        self.fx.push()

    def test_a_modified_tracked_file_refuses(self) -> None:
        self._signed_change_pending()
        with open(os.path.join(self.fx.deploy, "vars", "fedora-version.yml"), "a", encoding="utf-8") as handle:
            handle.write("# local edit\n")
        before = self.fx.deployed()
        code, out, err = self.fx.run()
        self.assertEqual(code, update.EXIT_DIRTY)
        self.assertEqual(out, "")
        self.assertIn("fedora-version.yml", err)
        self.assertEqual(self.fx.deployed(), before)

    def test_an_untracked_file_refuses(self) -> None:
        """It could shadow a file the incoming commit adds, or be read by a play's glob."""
        self._signed_change_pending()
        with open(os.path.join(self.fx.deploy, "planted.yml"), "w", encoding="utf-8") as handle:
            handle.write("x: 1\n")
        code, _, err = self.fx.run()
        self.assertEqual(code, update.EXIT_DIRTY)
        self.assertIn("planted.yml", err)

    def test_an_ignored_file_is_not_dirt(self) -> None:
        """Gitignored local state (the vault password file, run logs) lives in the checkout."""
        self.fx.commit(".gitignore", "ignored-local.dat\n", "ignore", sign="owner")
        self.fx.push()
        self.assertEqual(self.fx.run()[0], update.EXIT_OK)
        with open(os.path.join(self.fx.deploy, "ignored-local.dat"), "w", encoding="utf-8") as handle:
            handle.write("x\n")
        self._signed_change_pending()
        self.assertEqual(self.fx.run()[0], update.EXIT_OK)

    def test_a_detached_head_refuses(self) -> None:
        self._signed_change_pending()
        self.fx.git(self.fx.deploy, "checkout", "-q", "--detach")
        self.assertEqual(self.fx.run()[0], update.EXIT_DETACHED)

    def test_the_wrong_branch_refuses(self) -> None:
        self._signed_change_pending()
        self.fx.git(self.fx.deploy, "checkout", "-q", "-b", "other")
        code, _, err = self.fx.run()
        self.assertEqual(code, update.EXIT_WRONG_BRANCH)
        self.assertIn("other", err)

    def test_local_commits_refuse(self) -> None:
        with open(os.path.join(self.fx.deploy, "local.txt"), "w", encoding="utf-8") as handle:
            handle.write("x\n")
        self.fx.git(self.fx.deploy, "add", "local.txt")
        self.fx.git(self.fx.deploy, "commit", "-q", "-m", "local")
        self.assertEqual(self.fx.run()[0], update.EXIT_AHEAD)

    def test_a_rewritten_remote_refuses(self) -> None:
        self._signed_change_pending()
        self.assertEqual(self.fx.run()[0], update.EXIT_OK)
        deployed = self.fx.deployed()
        self.fx.git(self.fx.author, "reset", "-q", "--hard", "HEAD~1")
        self.fx.commit("c.txt", "c\n", "rewritten history", sign="owner")
        self.fx.push("--force")
        code, _, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_DIVERGED)
        self.assertEqual(self.fx.deployed(), deployed)

    def test_a_remote_rewound_behind_the_deployed_commit_refuses(self) -> None:
        self._signed_change_pending()
        self.assertEqual(self.fx.run()[0], update.EXIT_OK)
        self.fx.git(self.fx.author, "reset", "-q", "--hard", "HEAD~1")
        self.fx.push("--force")
        self.assertEqual(self.fx.run()[0], update.EXIT_AHEAD)

    def test_a_fetch_failure_refuses(self) -> None:
        self.fx.git(self.fx.deploy, "remote", "set-url", "origin", os.path.join(self.fx.root, "nowhere.git"))
        code, _, err = self.fx.run()
        self.assertEqual(code, update.EXIT_FETCH_FAILED)
        self.assertIn("fetch", err)

    def test_a_missing_allowed_signers_file_refuses(self) -> None:
        self._signed_change_pending()
        code, _, _ = self.fx.run(allowed_signers=os.path.join(self.fx.root, "absent"))
        self.assertEqual(code, update.EXIT_SIGNERS_FILE)

    def test_an_empty_allowed_signers_file_refuses(self) -> None:
        self._signed_change_pending()
        empty = os.path.join(self.fx.root, "empty")
        with open(empty, "w", encoding="utf-8") as handle:
            handle.write("# nothing but a comment\n\n")
        os.chmod(empty, 0o644)
        self.assertEqual(self.fx.run(allowed_signers=empty)[0], update.EXIT_SIGNERS_FILE)

    def test_a_group_or_world_writable_allowed_signers_file_refuses(self) -> None:
        self._signed_change_pending()
        for mode in (0o664, 0o646):
            with self.subTest(mode=oct(mode)):
                os.chmod(self.fx.allowed, mode)
                self.assertEqual(self.fx.run()[0], update.EXIT_SIGNERS_FILE)
        os.chmod(self.fx.allowed, 0o644)

    def test_an_allowed_signers_file_in_a_writable_directory_refuses(self) -> None:
        """Whoever can write the directory can replace the file."""
        self._signed_change_pending()
        directory = os.path.dirname(self.fx.allowed)
        mode = stat.S_IMODE(os.stat(directory).st_mode)
        os.chmod(directory, mode | stat.S_IWOTH)
        try:
            self.assertEqual(self.fx.run()[0], update.EXIT_SIGNERS_FILE)
        finally:
            os.chmod(directory, mode)

    def test_a_fedora_pin_change_refuses_before_anything_moves(self) -> None:
        before = self.fx.deployed()
        self.fx.commit("vars/fedora-version.yml", "---\nfedora_version: 45\n", "F45", sign="owner")
        self.fx.push()
        code, out, err = self.fx.run()
        self.assertEqual(code, update.EXIT_FEDORA_MISMATCH)
        self.assertNotIn("SELF-UPDATE-NEW", out)
        self.assertIn("45", err)
        self.assertEqual(self.fx.deployed(), before)

    def test_an_empty_principal_is_a_usage_error(self) -> None:
        self.assertEqual(self.fx.run(principal="")[0], update.EXIT_USAGE)


class TestSeveralTrustedKeys(UpdateCase):
    """Plan 00137 Task 4.8: the owner signs with more than one key. The desktop's `~/.ssh/id`
    signs on the host, and a ccy session signs with its GitHub account's key (Plan 00139 D5).
    A server trusting only the first never deploys a ccy commit, so the play writes every
    listed key, through helpers.self_update.signers, and either key deploys."""

    def _pub(self, key: str) -> str:
        with open(key + ".pub", encoding="utf-8") as handle:
            return handle.read().strip()

    def _trust(self, *keys: str) -> None:
        listed = signers.effective_keys([self._pub(key) for key in keys], None)
        with open(self.fx.allowed, "w", encoding="utf-8") as handle:
            handle.write(signers.render(PRINCIPAL, listed))

    def test_a_commit_signed_by_the_second_listed_key_is_deployed(self) -> None:
        self._trust(self.fx.owner_key, self.fx.account_key)
        new = self.fx.commit("a.txt", "a\n", "made in a ccy session", sign="account")
        self.fx.push()
        code, out, err = self.fx.run()
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertIn(f"SELF-UPDATE-NEW {new}", out)
        self.assertEqual(self.fx.deployed(), new)

    def test_the_first_listed_key_still_deploys(self) -> None:
        self._trust(self.fx.owner_key, self.fx.account_key)
        new = self.fx.commit("a.txt", "a\n", "made on the host", sign="owner")
        self.fx.push()
        self.assertEqual(self.fx.run()[0], update.EXIT_OK)
        self.assertEqual(self.fx.deployed(), new)

    def test_a_list_of_one_does_not_deploy_the_other_key(self) -> None:
        self._trust(self.fx.owner_key)
        before = self.fx.deployed()
        self.fx.commit("a.txt", "a\n", "made in a ccy session", sign="account")
        self.fx.push()
        code, out, _ = self.fx.run()
        self.assertEqual(code, update.EXIT_OK)
        self.assertIn("SELF-UPDATE-NOTHING", out)
        self.assertEqual(self.fx.deployed(), before)

    def test_a_key_outside_the_list_still_waits(self) -> None:
        self._trust(self.fx.owner_key, self.fx.account_key)
        before = self.fx.deployed()
        self.fx.commit("a.txt", "a\n", "someone else", sign="other")
        self.fx.push()
        self.assertEqual(self.fx.run()[0], update.EXIT_OK)
        self.assertEqual(self.fx.deployed(), before)

    def test_verify_head_accepts_a_head_the_second_key_signed(self) -> None:
        self._trust(self.fx.owner_key, self.fx.account_key)
        self.fx.commit("a.txt", "a\n", "made in a ccy session", sign="account")
        self.fx.push()
        self.assertEqual(self.fx.run()[0], update.EXIT_OK)
        err = io.StringIO()
        code = update.verify_head(checkout=self.fx.deploy, allowed_signers=self.fx.allowed,
                                  principal=PRINCIPAL, stderr=err, env=self.fx.env)
        self.assertEqual(code, update.EXIT_OK, err.getvalue())


class TestAnchor(UpdateCase):
    """A fresh clone is at whatever the remote's tip is, which nobody vouched for. The play
    anchors it: HEAD must be a commit the pinned key signed before root runs any of it."""

    def _fresh_clone(self) -> str:
        path = os.path.join(self.fx.root, "fresh")
        self.fx.git(self.fx.root, "clone", "-q", "-b", BRANCH, self.fx.origin, path)
        return path

    def _anchor(self, checkout: str, **overrides: object) -> tuple[int, str, str]:
        args: dict[str, object] = {
            "checkout": checkout, "branch": BRANCH, "allowed_signers": self.fx.allowed,
            "principal": PRINCIPAL, "os_release": self.fx.os_release, "channel": update.CHANNEL_BRANCH,
        }
        args.update(overrides)
        out, err = io.StringIO(), io.StringIO()
        code = update.anchor(**args, stdout=out, stderr=err, env=self.fx.env)
        return code, out.getvalue(), err.getvalue()

    def _head(self, checkout: str) -> str:
        return self.fx.git(checkout, "rev-parse", "HEAD")

    def test_a_clone_at_an_unsigned_tip_moves_back_to_the_newest_signed_commit(self) -> None:
        signed = self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        unsigned = self.fx.commit("b.txt", "b\n", "agent on top")
        self.fx.push()
        fresh = self._fresh_clone()
        self.assertEqual(self._head(fresh), unsigned)
        code, out, err = self._anchor(fresh)
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertEqual(self._head(fresh), signed)
        self.assertIn(f"SELF-UPDATE-ANCHORED {signed}", out)
        self.assertIn(f"SELF-UPDATE-ANCHOR-MOVED {unsigned}", out)
        self.assertEqual(self.fx.git(fresh, "symbolic-ref", "--short", "HEAD"), BRANCH)
        self.assertEqual(self.fx.git(fresh, "status", "--porcelain"), "")

    def test_a_signed_head_is_left_where_it_is(self) -> None:
        before = self.fx.deployed()
        code, out, _ = self._anchor(self.fx.deploy)
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(self.fx.deployed(), before)
        self.assertIn(f"SELF-UPDATE-ANCHORED {before}", out)
        self.assertNotIn("SELF-UPDATE-ANCHOR-MOVED", out)

    def test_it_never_moves_forward_past_the_gate(self) -> None:
        before = self.fx.deployed()
        self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        self.fx.push()
        self.fx.git(self.fx.deploy, "fetch", "-q", "origin")
        code, _, _ = self._anchor(self.fx.deploy)
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(self.fx.deployed(), before)

    def test_no_trusted_commit_in_the_history_refuses_and_moves_nothing(self) -> None:
        self.fx.commit("a.txt", "a\n", "agent")
        self.fx.push()
        fresh = self._fresh_clone()
        before = self._head(fresh)
        code, out, err = self._anchor(fresh, principal="someone-else@example.com")
        self.assertEqual(code, update.EXIT_UNTRUSTED)
        self.assertEqual(self._head(fresh), before)
        self.assertEqual(out, "")
        self.assertIn("no commit", err)

    def test_the_target_must_match_the_running_fedora(self) -> None:
        self.fx.commit("a.txt", "a\n", "agent")
        self.fx.push()
        fresh = self._fresh_clone()
        before = self._head(fresh)
        self.fx.set_running_fedora(43)
        code, _, _ = self._anchor(fresh)
        self.assertEqual(code, update.EXIT_FEDORA_MISMATCH)
        self.assertEqual(self._head(fresh), before)

    def test_a_dirty_clone_refuses(self) -> None:
        with open(os.path.join(self.fx.deploy, "stray.txt"), "w", encoding="utf-8") as handle:
            handle.write("x\n")
        self.assertEqual(self._anchor(self.fx.deploy)[0], update.EXIT_DIRTY)

    def _ignore(self, pattern: str) -> None:
        """Sign a .gitignore naming `pattern`, and bring the deploy clone up to it."""
        self.fx.commit(".gitignore", f"{pattern}\n", "ignore", sign="owner")
        self.fx.push()
        self.fx.git(self.fx.deploy, "pull", "-q", "--ff-only")

    def test_an_ignored_bytecode_file_refuses(self) -> None:
        """Python imports a pyc beside its source without checking it, and git status never
        lists an ignored file. Root imports from this tree, so an ignored file is code."""
        self._ignore("__pycache__/")
        cache = os.path.join(self.fx.deploy, "helpers", "__pycache__")
        os.makedirs(cache)
        with open(os.path.join(cache, "cycle.cpython-311.pyc"), "wb") as handle:
            handle.write(b"planted")
        code, out, err = self._anchor(self.fx.deploy)
        self.assertEqual(code, update.EXIT_DIRTY)
        self.assertEqual(out, "")
        self.assertIn("helpers/__pycache__/cycle.cpython-311.pyc", err)

    def test_a_leftover_nested_repository_refuses(self) -> None:
        """What a recursive clone of an unsigned tip's submodule leaves behind, at a path
        the signed commit ignores, so git status says nothing about it."""
        self._ignore("vendor-sub/")
        nested = os.path.join(self.fx.deploy, "vendor-sub")
        self.fx.git(self.fx.root, "init", "-q", nested)
        with open(os.path.join(nested, "code.py"), "w", encoding="utf-8") as handle:
            handle.write("x = 1\n")
        code, _, err = self._anchor(self.fx.deploy)
        self.assertEqual(code, update.EXIT_DIRTY)
        self.assertIn("vendor-sub", err)

    def test_the_allowed_file_is_the_only_exception(self) -> None:
        self._ignore("host_vars.yml")
        with open(os.path.join(self.fx.deploy, "host_vars.yml"), "w", encoding="utf-8") as handle:
            handle.write("x: 1\n")
        self.assertEqual(self._anchor(self.fx.deploy)[0], update.EXIT_DIRTY, "not allowed unless named")
        code, _, err = self._anchor(self.fx.deploy, allow_untracked=("host_vars.yml",))
        self.assertEqual(code, update.EXIT_OK, err)

    def test_a_stray_file_is_refused_before_anything_moves(self) -> None:
        signed = self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        self.fx.commit(".gitignore", "*.pyc\n", "agent on top")
        self.fx.push()
        fresh = self._fresh_clone()
        unsigned = self._head(fresh)
        with open(os.path.join(fresh, "planted.pyc"), "wb") as handle:
            handle.write(b"planted")
        code, _, _ = self._anchor(fresh)
        self.assertEqual(code, update.EXIT_DIRTY)
        self.assertEqual(self._head(fresh), unsigned)
        self.assertNotEqual(unsigned, signed)


class TestVerifyHead(UpdateCase):
    def _verify(self, **overrides: object) -> int:
        args: dict[str, object] = {
            "checkout": self.fx.deploy, "allowed_signers": self.fx.allowed, "principal": PRINCIPAL,
        }
        args.update(overrides)
        return update.verify_head(**args, stderr=io.StringIO(), env=self.fx.env)

    def test_a_head_signed_by_the_pinned_key_is_trusted(self) -> None:
        self.assertEqual(self._verify(), update.EXIT_OK)

    def test_an_unsigned_head_is_not(self) -> None:
        self.fx.commit("a.txt", "a\n", "agent")
        self.fx.push()
        self.fx.git(self.fx.deploy, "pull", "-q", "--ff-only")
        self.assertEqual(self._verify(), update.EXIT_UNTRUSTED)

    def test_a_head_signed_by_another_key_is_not(self) -> None:
        self.fx.commit("a.txt", "a\n", "other", sign="other")
        self.fx.push()
        self.fx.git(self.fx.deploy, "pull", "-q", "--ff-only")
        self.assertEqual(self._verify(), update.EXIT_UNTRUSTED)

    def test_the_wrong_principal_is_not_trusted(self) -> None:
        self.assertEqual(self._verify(principal="someone-else@example.com"), update.EXIT_UNTRUSTED)


class TagCase(UpdateCase):
    """The tags channel: what is deployed is the newest signed release tag, never the tip."""

    def run_tags(self, **overrides: object) -> tuple[int, str, str]:
        return self.fx.run(channel=update.CHANNEL_TAGS, **overrides)

    def assert_refused_unmoved(self, code: int, *, before: str | None = None, **overrides: object) -> str:
        before = before or self.fx.deployed()
        got, out, err = self.run_tags(**overrides)
        self.assertEqual(got, code, err)
        self.assertNotIn("SELF-UPDATE-NEW", out)
        self.assertNotIn("SELF-UPDATE-TARGET", out)
        self.assertEqual(self.fx.deployed(), before)
        return err


class TestTagsDeploy(TagCase):
    def test_the_newest_release_tag_is_deployed(self) -> None:
        old = self.fx.deployed()
        self.fx.release("44.0.0")
        new = self.fx.release("44.1.0", path="b.txt")
        code, out, err = self.run_tags()
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertEqual(out, f"SELF-UPDATE-OLD {old}\nSELF-UPDATE-NEW {new}\nSELF-UPDATE-TAG 44.1.0\n")
        self.assertEqual(self.fx.deployed(), new)

    def test_the_tip_above_the_tag_is_not_deployed_though_the_owner_signed_it(self) -> None:
        tagged = self.fx.release("44.0.0")
        self.fx.commit("b.txt", "b\n", "signed work after the release", sign="owner")
        self.fx.push()
        code, out, err = self.run_tags()
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertEqual(self.fx.deployed(), tagged)
        self.assertIn("SELF-UPDATE-TAG 44.0.0", out)

    def test_the_branch_channel_is_unchanged_and_deploys_that_tip(self) -> None:
        self.fx.release("44.0.0")
        tip = self.fx.commit("b.txt", "b\n", "signed work after the release", sign="owner")
        self.fx.push()
        code, out, _ = self.fx.run(channel=update.CHANNEL_BRANCH)
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(self.fx.deployed(), tip)
        self.assertNotIn("SELF-UPDATE-TAG", out)

    def test_the_default_channel_is_tags(self) -> None:
        self.fx.commit("a.txt", "a\n", "signed tip, no release", sign="owner")
        self.fx.push()
        before = self.fx.deployed()
        out, err = io.StringIO(), io.StringIO()
        code = update.run(
            checkout=self.fx.deploy, remote="origin", branch=BRANCH, allowed_signers=self.fx.allowed,
            principal=PRINCIPAL, os_release=self.fx.os_release, stdout=out, stderr=err, env=self.fx.env,
        )
        self.assertEqual(code, update.EXIT_NO_RELEASE, err.getvalue())
        self.assertEqual(self.fx.deployed(), before)

    def test_the_numerically_highest_tag_wins_not_the_latest_made(self) -> None:
        highest = self.fx.release("44.10.0")
        self.fx.release("44.9.0", path="b.txt")
        code, out, err = self.run_tags()
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertIn("SELF-UPDATE-TAG 44.10.0", out)
        self.assertEqual(self.fx.deployed(), highest)

    def test_a_name_outside_the_scheme_is_passed_over_with_a_line_on_stderr(self) -> None:
        good = self.fx.release("44.1.0")
        draft = self.fx.commit("b.txt", "b\n", "draft", sign="owner")
        self.fx.push()
        for name in ("44.2.0-rc1", "44.01.0"):
            self.fx.tag(name, draft)
            self.fx.push_tag(name)
        code, out, err = self.run_tags()
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertEqual(self.fx.deployed(), good)
        self.assertIn("SELF-UPDATE-TAG 44.1.0", out)
        self.assertIn("44.2.0-rc1", err)
        self.assertIn("44.01.0", err)

    def test_another_majors_tags_are_not_this_branchs_releases(self) -> None:
        good = self.fx.release("44.0.0")
        self.fx.release("45.0.0", path="b.txt")
        code, _, err = self.run_tags()
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertEqual(self.fx.deployed(), good)

    def test_a_tag_by_the_second_listed_key_is_deployed(self) -> None:
        """The same pinned principal covers every listed key, for the tag and its commit."""
        with open(self.fx.owner_key + ".pub", encoding="utf-8") as handle:
            owner_pub = handle.read().strip()
        with open(self.fx.account_key + ".pub", encoding="utf-8") as handle:
            account_pub = handle.read().strip()
        with open(self.fx.allowed, "w", encoding="utf-8") as handle:
            handle.write(signers.render(PRINCIPAL, signers.effective_keys([owner_pub, account_pub], None)))
        sha = self.fx.commit("a.txt", "a\n", "from a ccy session", sign="account")
        self.fx.push()
        self.fx.tag("44.0.0", sha, sign="account")
        self.fx.push_tag("44.0.0")
        code, _, err = self.run_tags()
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertEqual(self.fx.deployed(), sha)

    def test_the_tag_at_head_is_nothing_to_do_and_still_names_the_release(self) -> None:
        commit = self.fx.release("44.0.0")
        self.assertEqual(self.run_tags()[0], update.EXIT_OK)
        code, out, _ = self.run_tags()
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(out, f"SELF-UPDATE-NOTHING {commit}\nSELF-UPDATE-TAG 44.0.0\n")

    def test_a_dry_run_names_the_target_and_the_tag_and_moves_nothing(self) -> None:
        old = self.fx.deployed()
        commit = self.fx.release("44.0.0")
        code, out, _ = self.run_tags(dry_run=True)
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(out, f"SELF-UPDATE-OLD {old}\nSELF-UPDATE-TARGET {commit}\nSELF-UPDATE-TAG 44.0.0\n")
        self.assertEqual(self.fx.deployed(), old)

    def test_the_tag_refspec_does_not_force_and_prunes(self) -> None:
        """Pinned from the argv: both properties are what the refusals below depend on."""
        calls: list[tuple[str, ...]] = []
        original = update._Git.run

        def spy(git: update._Git, *args: str, **kwargs: int) -> subprocess.CompletedProcess:
            calls.append(args)
            return original(git, *args, **kwargs)

        self.fx.release("44.0.0")
        with mock.patch.object(update._Git, "run", spy):
            self.run_tags()
        fetch = next(call for call in calls if call[0] == "fetch")
        self.assertIn("--prune", fetch)
        self.assertIn("refs/tags/44.*:refs/tags/44.*", fetch)
        self.assertNotIn("+refs/tags/44.*:refs/tags/44.*", fetch)


class TestTagsRefused(TagCase):
    def test_no_tag_at_all_refuses_and_never_falls_back_to_the_tip(self) -> None:
        self.fx.commit("a.txt", "a\n", "signed tip", sign="owner")
        self.fx.push()
        err = self.assert_refused_unmoved(update.EXIT_NO_RELEASE)
        self.assertIn("release", err)

    def test_only_names_outside_the_scheme_is_no_release(self) -> None:
        sha = self.fx.commit("a.txt", "a\n", "draft", sign="owner")
        self.fx.push()
        self.fx.tag("44.1.0-rc1", sha)
        self.fx.push_tag("44.1.0-rc1")
        self.assert_refused_unmoved(update.EXIT_NO_RELEASE)

    def test_a_lightweight_tag_refuses(self) -> None:
        sha = self.fx.commit("a.txt", "a\n", "signed", sign="owner")
        self.fx.push()
        self.fx.tag("44.0.0", sha, annotated=False)
        self.fx.push_tag("44.0.0")
        err = self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)
        self.assertIn("44.0.0", err)

    def test_an_unsigned_annotated_tag_refuses(self) -> None:
        sha = self.fx.commit("a.txt", "a\n", "signed", sign="owner")
        self.fx.push()
        self.fx.tag("44.0.0", sha, sign=None)
        self.fx.push_tag("44.0.0")
        self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)

    def test_a_tag_signed_by_a_key_that_is_not_pinned_refuses(self) -> None:
        sha = self.fx.commit("a.txt", "a\n", "signed", sign="owner")
        self.fx.push()
        self.fx.tag("44.0.0", sha, sign="other")
        self.fx.push_tag("44.0.0")
        self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)

    def test_a_good_signature_for_another_principal_refuses(self) -> None:
        """A tag by a listed key that is not the pinned principal's is not a release, though
        the commit under it is the owner's and git calls the tag signature good."""
        with open(self.fx.other_key + ".pub", encoding="utf-8") as handle:
            kind, blob = handle.read().split()[:2]
        with open(self.fx.allowed, "a", encoding="utf-8") as handle:
            handle.write(f'colleague@example.com namespaces="git" {kind} {blob}\n')
        sha = self.fx.commit("a.txt", "a\n", "signed", sign="owner")
        self.fx.push()
        self.fx.tag("44.0.0", sha, sign="other")
        self.fx.push_tag("44.0.0")
        err = self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)
        self.assertIn("colleague@example.com", err)

    def test_a_bad_newest_tag_refuses_though_an_older_one_is_good(self) -> None:
        self.fx.release("44.0.0")
        sha = self.fx.commit("b.txt", "b\n", "signed", sign="owner")
        self.fx.push()
        self.fx.tag("44.1.0", sha, sign="other")
        self.fx.push_tag("44.1.0")
        err = self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)
        self.assertIn("44.1.0", err)

    def test_a_good_tag_on_a_commit_the_principal_did_not_sign_refuses(self) -> None:
        for number, sign in enumerate((None, "other")):
            with self.subTest(commit_signed_by=sign):
                name = f"44.{number}.0"
                sha = self.fx.commit("a.txt", f"{sign}\n", "not the owner", sign=sign)
                self.fx.push()
                self.fx.tag(name, sha, sign="owner")
                self.fx.push_tag(name)
                self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)

    def test_a_tag_on_a_commit_a_side_branch_holds_refuses(self) -> None:
        self.fx.git(self.fx.author, "checkout", "-q", "-b", "side")
        sha = self.fx.commit("a.txt", "a\n", "owner, on a side branch", sign="owner")
        self.fx.tag("44.0.0", sha)
        self.fx.push_tag("44.0.0")
        self.fx.git(self.fx.author, "checkout", "-q", BRANCH)
        err = self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)
        self.assertIn(BRANCH, err)

    def test_a_tag_signed_for_another_name_refuses(self) -> None:
        """A signed tag object re-pointed under a higher name is not that release."""
        sha = self.fx.commit("a.txt", "a\n", "signed", sign="owner")
        self.fx.push()
        object_name = self.fx.tag("44.0.0", sha)
        self.fx.git(self.fx.author, "update-ref", "refs/tags/44.9.9", object_name)
        self.fx.push_tag("44.9.9")
        err = self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)
        self.assertIn("44.9.9", err)

    def test_a_tag_whose_commit_pins_another_fedora_refuses_before_anything_moves(self) -> None:
        sha = self.fx.commit("vars/fedora-version.yml", "---\nfedora_version: 45\n", "F45", sign="owner")
        self.fx.push()
        self.fx.tag("44.0.0", sha)
        self.fx.push_tag("44.0.0")
        err = self.assert_refused_unmoved(update.EXIT_FEDORA_MISMATCH)
        self.assertIn("45", err)

    def test_a_tag_moved_upstream_refuses_without_force(self) -> None:
        first = self.fx.release("44.0.0")
        self.assertEqual(self.run_tags()[0], update.EXIT_OK)
        second = self.fx.commit("b.txt", "b\n", "owner, the tag is moved here", sign="owner")
        self.fx.push()
        self.fx.git(self.fx.author, "-c", f"user.signingkey={self.fx.owner_key}", "tag", "-f", "-s", "-m", "moved",
                    "44.0.0", second)
        self.fx.push_tag("44.0.0", "--force")
        err = self.assert_refused_unmoved(update.EXIT_TAG_MOVED, before=first)
        self.assertIn("44.0.0", err)
        self.assertEqual(self.fx.git(self.fx.deploy, "rev-parse", "refs/tags/44.0.0^{commit}"), first)

    def test_a_withdrawn_tag_is_pruned_and_the_clone_refuses_as_ahead(self) -> None:
        self.fx.release("44.0.0")
        newest = self.fx.release("44.1.0", path="b.txt")
        self.assertEqual(self.run_tags()[0], update.EXIT_OK)
        self.fx.withdraw_tag("44.1.0")
        self.assert_refused_unmoved(update.EXIT_AHEAD, before=newest)
        self.assertNotIn("44.1.0", self.fx.git(self.fx.deploy, "tag", "--list"))

    def test_local_commits_above_the_tag_refuse_as_ahead(self) -> None:
        self.fx.release("44.0.0")
        self.assertEqual(self.run_tags()[0], update.EXIT_OK)
        with open(os.path.join(self.fx.deploy, "local.txt"), "w", encoding="utf-8") as handle:
            handle.write("x\n")
        self.fx.git(self.fx.deploy, "add", "local.txt")
        self.fx.git(self.fx.deploy, "commit", "-q", "-m", "local")
        self.assert_refused_unmoved(update.EXIT_AHEAD)

    def test_diverged_histories_refuse(self) -> None:
        with open(os.path.join(self.fx.deploy, "local.txt"), "w", encoding="utf-8") as handle:
            handle.write("x\n")
        self.fx.git(self.fx.deploy, "add", "local.txt")
        self.fx.git(self.fx.deploy, "commit", "-q", "-m", "local")
        self.fx.release("44.0.0")
        self.assert_refused_unmoved(update.EXIT_DIVERGED)

    def test_a_dirty_clone_refuses(self) -> None:
        self.fx.release("44.0.0")
        with open(os.path.join(self.fx.deploy, "planted.yml"), "w", encoding="utf-8") as handle:
            handle.write("x: 1\n")
        self.assert_refused_unmoved(update.EXIT_DIRTY)

    def test_a_branch_that_is_not_a_fedora_release_branch_is_a_usage_error(self) -> None:
        self.fx.git(self.fx.deploy, "checkout", "-q", "-b", "main")
        self.assert_refused_unmoved(update.EXIT_USAGE, branch="main")

    def test_a_tag_that_is_not_a_signed_one_never_runs_a_program_the_repo_names(self) -> None:
        marker = os.path.join(self.fx.root, "gpg-ran")
        evil = os.path.join(self.fx.root, "evil-gpg")
        _evil_program(evil, marker, 0)
        for key in ("gpg.program", "gpg.openpgp.program", "gpg.ssh.program"):
            self.fx.git(self.fx.deploy, "config", key, evil)
        sha = self.fx.commit("a.txt", "a\n", "signed", sign="owner")
        self.fx.push()
        pgp = "-----BEGIN PGP SIGNATURE-----\n\niQ==\n-----END PGP SIGNATURE-----"
        self.fx.tag("44.0.0", sha, sign=None, message=f"release\n{pgp}")
        self.fx.push_tag("44.0.0")
        self.assert_refused_unmoved(update.EXIT_TAG_REFUSED)
        self.assertFalse(os.path.exists(marker), "a repo-configured signing program was executed")

    def test_a_fetch_failure_is_not_a_moved_tag(self) -> None:
        self.fx.git(self.fx.deploy, "remote", "set-url", "origin", os.path.join(self.fx.root, "nowhere.git"))
        self.assert_refused_unmoved(update.EXIT_FETCH_FAILED)


class TestTagsAnchor(UpdateCase):
    """After the play clones, HEAD is the branch tip, which nobody vouched for. In the tags
    channel the anchor lands the clone on the newest valid release, not on the newest signed commit."""

    def _fresh_clone(self) -> str:
        path = os.path.join(self.fx.root, "fresh")
        self.fx.git(self.fx.root, "clone", "-q", "-b", BRANCH, self.fx.origin, path)
        return path

    def _anchor(self, checkout: str, **overrides: object) -> tuple[int, str, str]:
        args: dict[str, object] = {
            "checkout": checkout, "branch": BRANCH, "allowed_signers": self.fx.allowed,
            "principal": PRINCIPAL, "os_release": self.fx.os_release, "channel": update.CHANNEL_TAGS,
        }
        args.update(overrides)
        out, err = io.StringIO(), io.StringIO()
        code = update.anchor(**args, stdout=out, stderr=err, env=self.fx.env)
        return code, out.getvalue(), err.getvalue()

    def _head(self, checkout: str) -> str:
        return self.fx.git(checkout, "rev-parse", "HEAD")

    def test_a_fresh_clone_at_the_tip_lands_on_the_newest_valid_tag(self) -> None:
        self.fx.release("44.0.0")
        tagged = self.fx.release("44.1.0", path="b.txt")
        tip = self.fx.commit("c.txt", "c\n", "owner, signed, not released", sign="owner")
        self.fx.push()
        fresh = self._fresh_clone()
        self.assertEqual(self._head(fresh), tip)
        code, out, err = self._anchor(fresh)
        self.assertEqual(code, update.EXIT_OK, err)
        self.assertEqual(self._head(fresh), tagged)
        self.assertEqual(
            out, f"SELF-UPDATE-ANCHORED {tagged}\nSELF-UPDATE-ANCHOR-MOVED {tip}\nSELF-UPDATE-TAG 44.1.0\n",
        )
        self.assertEqual(self.fx.git(fresh, "symbolic-ref", "--short", "HEAD"), BRANCH)
        self.assertEqual(self.fx.git(fresh, "status", "--porcelain"), "")

    def test_a_clone_already_on_the_newest_tag_is_left_alone(self) -> None:
        tagged = self.fx.release("44.0.0")
        self.fx.git(self.fx.deploy, "pull", "-q", "--ff-only")
        self.fx.git(self.fx.deploy, "fetch", "-q", "origin", "refs/tags/44.0.0:refs/tags/44.0.0")
        code, out, _ = self._anchor(self.fx.deploy)
        self.assertEqual(code, update.EXIT_OK)
        self.assertEqual(out, f"SELF-UPDATE-ANCHORED {tagged}\nSELF-UPDATE-TAG 44.0.0\n")

    def test_no_release_tag_refuses_and_moves_nothing(self) -> None:
        self.fx.commit("a.txt", "a\n", "owner, signed, not released", sign="owner")
        self.fx.push()
        fresh = self._fresh_clone()
        before = self._head(fresh)
        code, out, err = self._anchor(fresh)
        self.assertEqual(code, update.EXIT_NO_RELEASE)
        self.assertEqual(out, "")
        self.assertIn("release", err)
        self.assertEqual(self._head(fresh), before)

    def test_a_bad_newest_tag_refuses_and_does_not_fall_back_to_an_older_one(self) -> None:
        self.fx.release("44.0.0")
        sha = self.fx.commit("b.txt", "b\n", "signed", sign="owner")
        self.fx.push()
        self.fx.tag("44.1.0", sha, annotated=False)
        self.fx.push_tag("44.1.0")
        fresh = self._fresh_clone()
        before = self._head(fresh)
        self.assertEqual(self._anchor(fresh)[0], update.EXIT_TAG_REFUSED)
        self.assertEqual(self._head(fresh), before)

    def test_the_tag_must_match_the_running_fedora(self) -> None:
        self.fx.release("44.0.0")
        self.fx.commit("b.txt", "b\n", "tip", sign="owner")
        self.fx.push()
        fresh = self._fresh_clone()
        before = self._head(fresh)
        self.fx.set_running_fedora(43)
        self.assertEqual(self._anchor(fresh)[0], update.EXIT_FEDORA_MISMATCH)
        self.assertEqual(self._head(fresh), before)

    def test_a_stray_file_is_refused_before_anything_moves(self) -> None:
        self.fx.release("44.0.0")
        self.fx.commit("b.txt", "b\n", "tip", sign="owner")
        self.fx.push()
        fresh = self._fresh_clone()
        before = self._head(fresh)
        with open(os.path.join(fresh, "planted.pyc"), "wb") as handle:
            handle.write(b"planted")
        self.assertEqual(self._anchor(fresh)[0], update.EXIT_DIRTY)
        self.assertEqual(self._head(fresh), before)

    def test_the_anchored_head_is_one_verify_head_accepts(self) -> None:
        self.fx.release("44.0.0")
        self.fx.commit("b.txt", "b\n", "tip, unsigned")
        self.fx.push()
        fresh = self._fresh_clone()
        self.assertEqual(self._anchor(fresh)[0], update.EXIT_OK)
        err = io.StringIO()
        code = update.verify_head(checkout=fresh, allowed_signers=self.fx.allowed, principal=PRINCIPAL,
                                  stderr=err, env=self.fx.env)
        self.assertEqual(code, update.EXIT_OK, err.getvalue())


class TestCli(UpdateCase):
    def _cli(self, *args: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, "-m", "helpers.self_update.update", *args],
            cwd=REPO_ROOT, capture_output=True, text=True, env=self.fx.env, check=False,
        )

    def test_the_module_runs_as_a_cli(self) -> None:
        new = self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        self.fx.push()
        result = self._cli(
            "--checkout", self.fx.deploy, "--remote", "origin", "--branch", BRANCH, "--channel", "branch",
            "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
            "--os-release", self.fx.os_release,
        )
        self.assertEqual(result.returncode, update.EXIT_OK, result.stderr)
        self.assertIn(f"SELF-UPDATE-NEW {new}", result.stdout)

    def test_the_channel_defaults_to_tags_on_the_command_line(self) -> None:
        self.fx.commit("a.txt", "a\n", "owner", sign="owner")
        self.fx.push()
        result = self._cli(
            "--checkout", self.fx.deploy, "--remote", "origin", "--branch", BRANCH,
            "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
            "--os-release", self.fx.os_release,
        )
        self.assertEqual(result.returncode, update.EXIT_NO_RELEASE, result.stderr)
        self.assertEqual(result.stdout, "")

    def test_a_tag_update_runs_as_a_cli(self) -> None:
        commit = self.fx.release("44.0.0")
        result = self._cli(
            "--checkout", self.fx.deploy, "--remote", "origin", "--branch", BRANCH, "--channel", "tags",
            "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
            "--os-release", self.fx.os_release,
        )
        self.assertEqual(result.returncode, update.EXIT_OK, result.stderr)
        self.assertIn(f"SELF-UPDATE-NEW {commit}", result.stdout)
        self.assertIn("SELF-UPDATE-TAG 44.0.0", result.stdout)

    def test_an_unknown_channel_is_a_usage_error(self) -> None:
        result = self._cli(
            "--checkout", self.fx.deploy, "--remote", "origin", "--branch", BRANCH, "--channel", "tip",
            "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
        )
        self.assertEqual(result.returncode, update.EXIT_USAGE)

    def test_anchor_runs_as_a_cli(self) -> None:
        signed = self.fx.deployed()
        self.fx.commit("a.txt", "a\n", "agent")
        self.fx.push()
        fresh = os.path.join(self.fx.root, "fresh")
        self.fx.git(self.fx.root, "clone", "-q", "-b", BRANCH, self.fx.origin, fresh)
        result = self._cli(
            "--anchor", "--checkout", fresh, "--branch", BRANCH, "--channel", "branch",
            "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
            "--os-release", self.fx.os_release,
        )
        self.assertEqual(result.returncode, update.EXIT_OK, result.stderr)
        self.assertIn(f"SELF-UPDATE-ANCHORED {signed}", result.stdout)

    def test_anchor_lands_on_the_newest_tag_as_a_cli(self) -> None:
        commit = self.fx.release("44.0.0")
        self.fx.commit("b.txt", "b\n", "owner, after the release", sign="owner")
        self.fx.push()
        fresh = os.path.join(self.fx.root, "fresh")
        self.fx.git(self.fx.root, "clone", "-q", "-b", BRANCH, self.fx.origin, fresh)
        result = self._cli(
            "--anchor", "--checkout", fresh, "--branch", BRANCH, "--channel", "tags",
            "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
            "--os-release", self.fx.os_release,
        )
        self.assertEqual(result.returncode, update.EXIT_OK, result.stderr)
        self.assertIn(f"SELF-UPDATE-ANCHORED {commit}", result.stdout)
        self.assertIn("SELF-UPDATE-TAG 44.0.0", result.stdout)

    def test_anchor_takes_the_allowed_file_on_the_command_line(self) -> None:
        self.fx.commit(".gitignore", "kept.yml\n", "ignore", sign="owner")
        self.fx.push()
        self.fx.git(self.fx.deploy, "pull", "-q", "--ff-only")
        with open(os.path.join(self.fx.deploy, "kept.yml"), "w", encoding="utf-8") as handle:
            handle.write("x: 1\n")
        common = ("--anchor", "--checkout", self.fx.deploy, "--branch", BRANCH, "--channel", "branch",
                  "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
                  "--os-release", self.fx.os_release)
        self.assertEqual(self._cli(*common).returncode, update.EXIT_DIRTY)
        result = self._cli(*common, "--allow-untracked", "kept.yml")
        self.assertEqual(result.returncode, update.EXIT_OK, result.stderr)

    def test_an_update_without_a_remote_is_a_usage_error(self) -> None:
        result = self._cli(
            "--checkout", self.fx.deploy, "--branch", BRANCH,
            "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
        )
        self.assertEqual(result.returncode, update.EXIT_USAGE)

    def test_missing_arguments_are_a_usage_error(self) -> None:
        result = self._cli("--checkout", self.fx.deploy)
        self.assertEqual(result.returncode, update.EXIT_USAGE)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
