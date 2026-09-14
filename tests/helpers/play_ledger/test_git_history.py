"""Tests for helpers.play_ledger.git_history — the git side of the freshness check.

`git fetch` only, never a merge or a checkout: this runs at login on a machine
someone is using, and moving their working tree under them is a Non-Goal of
Plan 00109, not merely impolite.
"""

from __future__ import annotations

import os
import subprocess
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import git_history


def _completed(stdout: str = "", returncode: int = 0) -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess(args=["git"], returncode=returncode, stdout=stdout, stderr="")


class TestFetch(unittest.TestCase):
    def test_fetches_without_touching_the_working_tree(self) -> None:
        run = mock.Mock(return_value=_completed())
        git_history.fetch("/repo", run=run)
        argv = run.call_args.args[0]
        self.assertIn("fetch", argv)
        for forbidden in ("merge", "pull", "checkout", "reset", "rebase"):
            self.assertNotIn(forbidden, argv)

    def test_names_the_repo_root_not_the_cwd(self) -> None:
        run = mock.Mock(return_value=_completed())
        git_history.fetch("/repo", run=run)
        self.assertEqual(run.call_args.args[0][:3], ["git", "-C", "/repo"])

    def test_it_is_bounded_by_a_timeout(self) -> None:
        """The one genuinely network-bound call on the login path, and nothing underneath
        it: systemd disables the start timeout for `Type=oneshot` by default, so an
        unbounded hung fetch would hold `graphical-session.target` in `activating` with
        nothing ever ending it. A check that stalls the session gets removed from it."""
        run = mock.Mock(return_value=_completed())
        git_history.fetch("/repo", run=run)
        timeout = run.call_args.kwargs.get("timeout")
        self.assertTrue(
            isinstance(timeout, (int, float)) and timeout > 0,
            f"fetch must pass a positive timeout, got {timeout!r}",
        )

    def test_it_never_waits_for_credentials_nobody_can_type(self) -> None:
        """A login path has no terminal for a prompt to appear on, so git would wait
        for an answer that cannot come and the timeout above would be the only thing
        that ended the last step of the login."""
        run = mock.Mock(return_value=_completed())
        git_history.fetch("/repo", run=run)
        environment = run.call_args.kwargs.get("env") or {}
        self.assertEqual(environment.get("GIT_TERMINAL_PROMPT"), "0")

    def test_it_passes_the_rest_of_the_environment_through(self) -> None:
        """Replacing the environment rather than extending it would drop HOME, and
        git reads its own configuration from there."""
        run = mock.Mock(return_value=_completed())
        git_history.fetch("/repo", run=run)
        environment = run.call_args.kwargs.get("env") or {}
        for name in os.environ:
            self.assertIn(name, environment)


class TestChangesSince(unittest.TestCase):
    def test_parses_short_sha_and_subject(self) -> None:
        run = mock.Mock(return_value=_completed("abc1234\x1ffirst subject\ndef5678\x1fsecond\n"))
        self.assertEqual(
            git_history.changes_since("/repo", "a" * 40, "playbooks/a.yml", run=run),
            [("abc1234", "first subject"), ("def5678", "second")],
        )

    def test_no_commits_is_an_empty_list_not_an_error(self) -> None:
        run = mock.Mock(return_value=_completed(""))
        self.assertEqual(git_history.changes_since("/repo", "a" * 40, "playbooks/a.yml", run=run), [])

    def test_a_subject_containing_the_separator_is_not_split_twice(self) -> None:
        """A commit subject is free text; splitting on every separator would truncate it."""
        run = mock.Mock(return_value=_completed("abc1234\x1ffix: a\x1fb thing\n"))
        self.assertEqual(
            git_history.changes_since("/repo", "a" * 40, "playbooks/a.yml", run=run),
            [("abc1234", "fix: a\x1fb thing")],
        )

    def test_scopes_the_log_to_the_one_play_path(self) -> None:
        run = mock.Mock(return_value=_completed(""))
        git_history.changes_since("/repo", "a" * 40, "playbooks/a.yml", run=run)
        argv = run.call_args.args[0]
        self.assertIn("--", argv)
        self.assertEqual(argv[-1], "playbooks/a.yml")

    def test_the_range_starts_at_the_ledgered_commit(self) -> None:
        commit = "a" * 40
        run = mock.Mock(return_value=_completed(""))
        git_history.changes_since("/repo", commit, "playbooks/a.yml", run=run)
        self.assertIn(f"{commit}..HEAD", run.call_args.args[0])

    def test_a_blank_line_is_skipped_rather_than_yielding_an_empty_commit(self) -> None:
        run = mock.Mock(return_value=_completed("abc1234\x1fone\n\n"))
        self.assertEqual(
            git_history.changes_since("/repo", "a" * 40, "playbooks/a.yml", run=run),
            [("abc1234", "one")],
        )

    def test_a_line_without_a_separator_raises(self) -> None:
        """Silently dropping it would under-report churn and call a stale play fresh."""
        run = mock.Mock(return_value=_completed("no-separator-here\n"))
        with self.assertRaises(ValueError):
            git_history.changes_since("/repo", "a" * 40, "playbooks/a.yml", run=run)

    def test_an_unknown_ledgered_commit_raises_rather_than_reporting_no_changes(self) -> None:
        """A commit that is not in this clone (a dropped branch, a fresh shallow clone)
        makes git exit non-zero. Treating that as 'no commits touched it' would report
        every such play fresh for ever."""
        run = mock.Mock(side_effect=subprocess.CalledProcessError(128, ["git"]))
        with self.assertRaises(subprocess.CalledProcessError):
            git_history.changes_since("/repo", "a" * 40, "playbooks/a.yml", run=run)


class TestPlaySha256AtHead(unittest.TestCase):
    def test_returns_the_blob_hash_of_the_file_at_head(self) -> None:
        import hashlib

        payload = b"- name: A Play\n"
        run = mock.Mock(return_value=subprocess.CompletedProcess(
            args=["git"], returncode=0, stdout=payload, stderr=b""))
        self.assertEqual(
            git_history.play_sha256_at_head("/repo", "playbooks/a.yml", run=run),
            hashlib.sha256(payload).hexdigest(),
        )

    def test_reads_bytes_not_text_so_the_hash_matches_the_file_on_disk(self) -> None:
        run = mock.Mock(return_value=subprocess.CompletedProcess(
            args=["git"], returncode=0, stdout=b"x", stderr=b""))
        git_history.play_sha256_at_head("/repo", "playbooks/a.yml", run=run)
        self.assertNotIn("text", run.call_args.kwargs)

    def test_a_play_absent_from_head_returns_None(self) -> None:
        """None is the GONE signal freshness.classify expects — distinct from a hash
        that merely differs."""
        run = mock.Mock(side_effect=subprocess.CalledProcessError(128, ["git"]))
        self.assertIsNone(git_history.play_sha256_at_head("/repo", "playbooks/a.yml", run=run))

    def test_asks_for_the_path_at_HEAD(self) -> None:
        run = mock.Mock(return_value=subprocess.CompletedProcess(
            args=["git"], returncode=0, stdout=b"x", stderr=b""))
        git_history.play_sha256_at_head("/repo", "playbooks/a.yml", run=run)
        self.assertIn("HEAD:playbooks/a.yml", run.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
