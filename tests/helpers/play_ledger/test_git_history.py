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


class TestPathExistsAt(unittest.TestCase):
    def test_a_path_git_lists_at_the_commit_exists(self) -> None:
        run = mock.Mock(return_value=_completed("playbooks/a.yml\n"))
        self.assertTrue(git_history.path_exists_at("/repo", "abc", "playbooks/a.yml", run=run))

    def test_a_path_git_does_not_list_is_absent(self) -> None:
        run = mock.Mock(return_value=_completed(""))
        self.assertFalse(git_history.path_exists_at("/repo", "abc", "playbooks/a.yml", run=run))

    def test_an_unresolvable_commit_raises_rather_than_reading_as_absent(self) -> None:
        """"Absent" would retire a finding on the strength of a commit git cannot even
        find — a dead clone must not read as a removed play."""
        run = mock.Mock(side_effect=subprocess.CalledProcessError(128, ["git"]))
        with self.assertRaises(subprocess.CalledProcessError):
            git_history.path_exists_at("/repo", "abc", "playbooks/a.yml", run=run)

    def test_asks_the_tree_of_that_commit_from_the_repo_root(self) -> None:
        run = mock.Mock(return_value=_completed(""))
        git_history.path_exists_at("/repo", "abc", "playbooks/a.yml", run=run)
        argv = run.call_args.args[0]
        self.assertEqual(argv[:4], ["git", "-C", "/repo", "ls-tree"])
        self.assertIn("--full-tree", argv)
        self.assertEqual(argv[-3:], ["abc", "--", "playbooks/a.yml"])
        self.assertTrue(run.call_args.kwargs.get("check"))


if __name__ == "__main__":
    unittest.main()
