"""Unit tests for helpers/pyenv/install_pyenv_versions.py.

The executor is the thin side-effecting wrapper around the `pyenv` binary; the
version-selection brains live in resolver.py (see test_resolver.py). Here we mock
subprocess so no real pyenv/network/compilation happens, and assert the executor
drives pyenv correctly: refresh, resolve, skip already-present, install missing,
and emit the PYENV-* markers the playbook keys its changed_when on.
"""

from __future__ import annotations

import contextlib
import io
import os
import pathlib
import subprocess
import sys
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pyenv import install_pyenv_versions as executor

SAMPLE_LISTING = """\
Available versions:
  3.9.18
  3.9.19
  3.11.9
  3.12.3
  3.13.0
  3.13.2
  3.14.0
  3.14.1
"""


def _make_fake_run(installed_bare, install_calls, git_calls=None):
    """Return a subprocess.run stand-in answering git + pyenv subcommands."""

    def fake_run(cmd, **_kwargs):
        if os.path.basename(cmd[0]) == "git":
            # cmd is ["git", "-C", <root>, <subcommand>, ...]; record the rest.
            if git_calls is not None:
                git_calls.append(cmd[3:])
            return subprocess.CompletedProcess(cmd, 0, "")
        sub = cmd[1:]  # pyenv subcommand
        if sub == ["install", "--list"]:
            return subprocess.CompletedProcess(cmd, 0, SAMPLE_LISTING)
        if sub == ["versions", "--bare"]:
            return subprocess.CompletedProcess(cmd, 0, installed_bare)
        if sub[:2] == ["install", "-s"]:
            install_calls.append(sub[2])
            return subprocess.CompletedProcess(cmd, 0, "")
        raise AssertionError(f"unexpected call: {cmd}")

    return fake_run


class TestExecutorMain(unittest.TestCase):
    def _run(self, *, minor_count, installed_bare, present_versions):
        install_calls = []
        with mock.patch.object(
            executor.subprocess, "run", _make_fake_run(installed_bare, install_calls)
        ), mock.patch.object(executor.os.path, "exists", return_value=True), \
            mock.patch.object(
                executor.os.path,
                "isdir",
                # ".git" → repo is a valid checkout; otherwise simulate which
                # versions/<X> dirs already exist on disk.
                side_effect=lambda p: pathlib.Path(p).name == ".git"
                or pathlib.Path(p).name in present_versions,
            ):
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                rc = executor.main(
                    ["--minor-count", str(minor_count), "--pyenv-root", "/fake/.pyenv"]
                )
        return rc, install_calls, out.getvalue()

    def test_installs_missing_and_skips_already_present(self):
        # Top 2 = 3.14, 3.13. 3.14.1 already on disk → only 3.13.2 is built.
        rc, calls, output = self._run(
            minor_count=2, installed_bare="3.13.0\n", present_versions={"3.14.1"}
        )
        self.assertEqual(rc, 0)
        self.assertEqual(calls, ["3.13.2"])
        self.assertIn("PYENV-CHANGED: installed 3.13.2", output)
        self.assertNotIn("3.14.1", "".join(calls))
        self.assertIn("PYENV-DONE changed=1", output)

    def test_no_changes_when_everything_present(self):
        rc, calls, output = self._run(
            minor_count=2,
            installed_bare="",
            present_versions={"3.14.1", "3.13.2"},
        )
        self.assertEqual(rc, 0)
        self.assertEqual(calls, [])
        self.assertIn("PYENV-DONE changed=0", output)
        self.assertNotIn("PYENV-CHANGED", output)

    def test_warns_about_stale_installed_minor_but_keeps_it(self):
        # Top 2 = 3.14, 3.13. Installed 3.9 is outside → warn + still upgrade it.
        rc, calls, output = self._run(
            minor_count=2, installed_bare="3.9.5\n", present_versions=set()
        )
        self.assertEqual(rc, 0)
        self.assertIn("3.9.19", calls)  # latest 3.9 patch still installed
        self.assertIn("PYENV-WARN: keeping installed Python 3.9", output)

    def test_aborts_when_pyenv_binary_missing(self):
        with mock.patch.object(executor.os.path, "exists", return_value=False):
            with self.assertRaises(SystemExit):
                executor.main(["--pyenv-root", "/nope"])

    def test_refreshes_definitions_via_git_fetch_and_hard_reset(self):
        # Must NOT use `pyenv update` (its `git merge origin` breaks on modern
        # git / detached HEAD). Force-align the repo to upstream: fetch, then
        # reset --hard — which cannot hit the "unmergeable" wall and leaves
        # untracked built versions/shims/plugins alone.
        git_calls = []
        with mock.patch.object(
            executor.subprocess, "run", _make_fake_run("", [], git_calls)
        ), mock.patch.object(executor.os.path, "exists", return_value=True), \
            mock.patch.object(executor.os.path, "isdir", return_value=True):
            with contextlib.redirect_stdout(io.StringIO()):
                rc = executor.main(
                    ["--minor-count", "1", "--pyenv-root", "/fake/.pyenv"]
                )
        self.assertEqual(rc, 0)
        self.assertEqual(git_calls[0][0], "fetch")
        self.assertEqual(git_calls[1][:2], ["reset", "--hard"])
        self.assertIn("FETCH_HEAD", git_calls[1])

    def test_aborts_when_pyenv_root_is_not_a_git_checkout(self):
        # ~/.pyenv exists (pyenv binary present) but is not a git repo → fail
        # loudly and tell the user to re-run the installer; do NOT auto-nuke.
        def isdir(path):
            return not pathlib.Path(path).name == ".git"

        with mock.patch.object(executor.os.path, "exists", return_value=True), \
            mock.patch.object(executor.os.path, "isdir", side_effect=isdir):
            with self.assertRaises(SystemExit):
                executor.main(["--pyenv-root", "/fake/.pyenv"])


if __name__ == "__main__":
    unittest.main()
