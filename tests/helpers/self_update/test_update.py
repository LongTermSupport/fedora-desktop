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

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import update

PRINCIPAL = "owner@example.com"
BRANCH = "F44"
REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))


class Fixture:
    """origin (bare), author (pushes), deploy (the clone the updater runs on)."""

    def __init__(self, root: str) -> None:
        self.root = root
        self.home = os.path.join(root, "home")
        os.makedirs(self.home)
        self.env = {
            **os.environ,
            "HOME": self.home,
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_CONFIG_SYSTEM": os.devnull,
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_TERMINAL_PROMPT": "0",
        }
        self.owner_key = self._key("owner")
        self.other_key = self._key("other")
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
            key = self.owner_key if sign == "owner" else self.other_key
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
            "--checkout", self.fx.deploy, "--remote", "origin", "--branch", BRANCH,
            "--allowed-signers", self.fx.allowed, "--principal", PRINCIPAL,
            "--os-release", self.fx.os_release,
        )
        self.assertEqual(result.returncode, update.EXIT_OK, result.stderr)
        self.assertIn(f"SELF-UPDATE-NEW {new}", result.stdout)

    def test_missing_arguments_are_a_usage_error(self) -> None:
        result = self._cli("--checkout", self.fx.deploy)
        self.assertEqual(result.returncode, update.EXIT_USAGE)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
