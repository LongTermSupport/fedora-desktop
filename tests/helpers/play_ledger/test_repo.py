"""Tests for helpers.play_ledger.repo — the git and filesystem facts a run record needs."""

from __future__ import annotations

import hashlib
import os
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import repo

FORTY_HEX = "a" * 40


def _completed(stdout: str, returncode: int = 0) -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess(args=["git"], returncode=returncode, stdout=stdout, stderr="")


class TestHeadCommit(unittest.TestCase):
    def test_returns_the_forty_hex_sha(self) -> None:
        run = mock.Mock(return_value=_completed(FORTY_HEX + "\n"))
        self.assertEqual(repo.head_commit("/repo", run=run), FORTY_HEX)

    def test_asks_git_about_the_repo_root_not_the_cwd(self) -> None:
        """An Ansible callback's cwd is whatever the operator was in, not the checkout."""
        run = mock.Mock(return_value=_completed(FORTY_HEX))
        repo.head_commit("/repo", run=run)
        argv = run.call_args.args[0]
        self.assertEqual(argv[:3], ["git", "-C", "/repo"])
        self.assertIn("rev-parse", argv)

    def test_passes_check_true_so_a_git_failure_raises(self) -> None:
        run = mock.Mock(return_value=_completed(FORTY_HEX))
        repo.head_commit("/repo", run=run)
        self.assertIs(run.call_args.kwargs["check"], True)

    def test_rejects_output_that_is_not_a_full_sha(self) -> None:
        """An abbreviated sha would silently break every later exact-match join."""
        run = mock.Mock(return_value=_completed("a1b2c3d\n"))
        with self.assertRaises(ValueError) as caught:
            repo.head_commit("/repo", run=run)
        self.assertIn("a1b2c3d", str(caught.exception))

    def test_rejects_empty_output(self) -> None:
        run = mock.Mock(return_value=_completed("\n"))
        with self.assertRaises(ValueError):
            repo.head_commit("/repo", run=run)

    def test_uppercase_hex_is_not_accepted(self) -> None:
        run = mock.Mock(return_value=_completed("A" * 40))
        with self.assertRaises(ValueError):
            repo.head_commit("/repo", run=run)


class TestDirtyState(unittest.TestCase):
    def test_empty_porcelain_output_is_clean(self) -> None:
        run = mock.Mock(return_value=_completed(""))
        state = repo.dirty_state("/repo", run=run)
        self.assertEqual(state, repo.DirtyState(paths=(), truncated=False))
        self.assertIs(state.dirty, False)

    def test_each_modified_staged_or_untracked_path_is_named_sorted(self) -> None:
        """Which paths, not whether: a play is stale only for a dirty path among its inputs."""
        run = mock.Mock(return_value=_completed(
            "?? CLAUDE/Plan/stray\0 M playbooks/imports/play-x.yml\0A  files/new.conf\0"))
        state = repo.dirty_state("/repo", run=run)
        self.assertEqual(state.paths, ("CLAUDE/Plan/stray", "files/new.conf", "playbooks/imports/play-x.yml"))
        self.assertIs(state.truncated, False)
        self.assertIs(state.dirty, True)

    def test_untracked_files_count_as_dirty(self) -> None:
        """An untracked play IS the case play_sha256 exists to catch."""
        run = mock.Mock(return_value=_completed("?? playbooks/imports/play-new.yml\0"))
        self.assertEqual(repo.dirty_state("/repo", run=run).paths, ("playbooks/imports/play-new.yml",))

    def test_a_path_with_spaces_is_kept_whole(self) -> None:
        run = mock.Mock(return_value=_completed(" M files/a b.conf\0"))
        self.assertEqual(repo.dirty_state("/repo", run=run).paths, ("files/a b.conf",))

    def test_more_paths_than_the_limit_are_cut_and_flagged(self) -> None:
        """A cut list cannot clear any play, so the record says it was cut."""
        run = mock.Mock(return_value=_completed("".join(f"?? f{n}\0" for n in range(5))))
        state = repo.dirty_state("/repo", run=run, limit=3)
        self.assertEqual(state.paths, ("f0", "f1", "f2"))
        self.assertIs(state.truncated, True)
        self.assertIs(state.dirty, True)

    def test_an_entry_it_cannot_read_raises(self) -> None:
        run = mock.Mock(return_value=_completed("garbage\0"))
        with self.assertRaises(ValueError):
            repo.dirty_state("/repo", run=run)

    def test_asks_git_about_the_repo_root_for_every_untracked_file_without_renames(self) -> None:
        """Without renames each entry is one path; with every untracked file, never a folder."""
        run = mock.Mock(return_value=_completed(""))
        repo.dirty_state("/repo", run=run)
        argv = run.call_args.args[0]
        self.assertEqual(argv[:3], ["git", "-C", "/repo"])
        for flag in ("--porcelain=v1", "-z", "--no-renames", "--untracked-files=all"):
            self.assertIn(flag, argv)
        self.assertIs(run.call_args.kwargs["check"], True)

    def test_against_real_git_each_file_is_named_never_its_folder(self) -> None:
        env = {key: value for key, value in os.environ.items()
               if key not in ("GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS")}
        env |= {"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}

        def git(root: str, *args: str) -> None:
            subprocess.run(["git", "-C", root, *args], check=True, capture_output=True, env=env)

        def run(argv, **kwargs):
            return subprocess.run(argv, env=env, **kwargs)

        with tempfile.TemporaryDirectory() as root:
            git(root, "init", "-q")
            for name in ("kept.yml", "moved.yml"):
                with open(os.path.join(root, name), "w", encoding="utf-8") as handle:
                    handle.write("a\n")
            git(root, "add", ".")
            git(root, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false",
                "commit", "-q", "-m", "first")
            self.assertEqual(repo.dirty_state(root, run=run), repo.DirtyState((), False))
            with open(os.path.join(root, "kept.yml"), "a", encoding="utf-8") as handle:
                handle.write("b\n")
            git(root, "mv", "moved.yml", "renamed.yml")
            os.makedirs(os.path.join(root, "CLAUDE", "Plan", "stray"))
            with open(os.path.join(root, "CLAUDE", "Plan", "stray", "a b"), "w", encoding="utf-8") as handle:
                handle.write("x")
            self.assertEqual(repo.dirty_state(root, run=run).paths,
                             ("CLAUDE/Plan/stray/a b", "kept.yml", "moved.yml", "renamed.yml"))


class TestSha256File(unittest.TestCase):
    def test_matches_hashlib_over_the_same_bytes(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "play.yml")
            payload = b"- name: A Play\n  hosts: localhost\n"
            with open(path, "wb") as handle:
                handle.write(payload)
            self.assertEqual(repo.sha256_file(path), hashlib.sha256(payload).hexdigest())

    def test_returns_sixty_four_lowercase_hex(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "play.yml")
            with open(path, "wb") as handle:
                handle.write(b"x")
            digest = repo.sha256_file(path)
            self.assertEqual(len(digest), 64)
            self.assertEqual(digest, digest.lower())

    def test_an_empty_file_hashes_rather_than_erroring(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "empty.yml")
            open(path, "wb").close()
            self.assertEqual(repo.sha256_file(path), hashlib.sha256(b"").hexdigest())

    def test_a_missing_file_raises(self) -> None:
        """Recording a hash for a play file that is not there would be a fabrication."""
        with tempfile.TemporaryDirectory() as base:
            with self.assertRaises(OSError):
                repo.sha256_file(os.path.join(base, "absent.yml"))

    def test_reads_in_chunks_so_a_large_play_does_not_load_whole(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "big.yml")
            payload = b"y" * (repo.CHUNK_BYTES * 3 + 7)
            with open(path, "wb") as handle:
                handle.write(payload)
            self.assertEqual(repo.sha256_file(path), hashlib.sha256(payload).hexdigest())


class TestUtcNow(unittest.TestCase):
    def test_formats_as_the_ledger_timestamp(self) -> None:
        stamp = repo.utc_now()
        self.assertRegex(stamp, r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")

    def test_is_utc_not_local(self) -> None:
        """A local-time stamp would sort wrongly against records from another zone."""
        import datetime

        before = datetime.datetime.now(datetime.timezone.utc)
        stamp = repo.utc_now()
        parsed = datetime.datetime.strptime(stamp, "%Y-%m-%dT%H:%M:%SZ").replace(
            tzinfo=datetime.timezone.utc
        )
        self.assertLessEqual(abs((parsed - before).total_seconds()), 5)

    def test_output_is_accepted_by_build_record(self) -> None:
        """The two must agree; a mismatch would fail every run at the last moment."""
        from helpers.play_ledger import ledger

        record = ledger.build_record(
            play="playbooks/imports/play-x.yml",
            name="X",
            commit=FORTY_HEX,
            dirty=False,
            play_sha256="b" * 64,
            outcome="ok",
            changed=0,
            started=repo.utc_now(),
            finished=repo.utc_now(),
        )
        self.assertEqual(record["schema"], ledger.SCHEMA)


if __name__ == "__main__":
    unittest.main()
