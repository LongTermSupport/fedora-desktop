"""The rule play-input-tree-untracked (Defence Before Fix; CLAUDE/QA.md; issue #191).

A play's input prefixes are directories whose every file counts as an input:
`affected_plays.affects` matches a changed path against them, and
`play_ledger.group_vars.play_text` walks each one and reads every file in it. A prefix that is
the checkout root, or a directory git tracks nothing in (the scratch tree `untracked/`, a
runtime directory such as `.claude/ccy/pingbus/seats/`), holds no checkout content: git never
reports a change there, so the prefix makes no play stale, and the walk reads scratch files,
other checkouts and run artefacts, among them named pipes that block it. `changed_plays` hung
on one of those under `untracked/plan-runs/`.

The rule reads git's own listing, not the mapper's: a finding is a prefix of a host play whose
normalised path is the root or climbs out of it, or under which `git ls-files` lists no tracked
file other than a `.gitignore`.

Fix a finding in the mapper, so it does not count such a directory as an input; never by
rewording the play to hide the reference.
"""

from __future__ import annotations

import os
import posixpath
import subprocess
import sys
import tempfile
import textwrap
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import group_vars as gv
from helpers.self_update import affected_plays as ap

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
RULE_ID = "play-input-tree-untracked"
SCRATCH_TREE = "untracked/"


def _isolated_git_env() -> dict[str, str]:
    env = {key: value for key, value in os.environ.items()
           if key not in ("GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS")}
    env["GIT_CONFIG_GLOBAL"] = os.devnull
    env["GIT_CONFIG_NOSYSTEM"] = "1"
    return env


def _tracked_under(repo_root: str, prefix: str) -> list[str]:
    """The tracked files under `prefix`, matched as a string (a prefix can end inside a name)."""
    listed = subprocess.run(["git", "-C", repo_root, "ls-files", "-z"], capture_output=True,
                            text=True, check=True, env=_isolated_git_env())
    return [path for path in listed.stdout.split("\0")
            if path.startswith(prefix) and posixpath.basename(path) != ".gitignore"]


def untracked_trees(repo_root: str, inputs: ap.Inputs) -> list[str]:
    """Every input prefix of `inputs` that is the root, outside it, or holds no tracked file."""
    findings = []
    for prefix in sorted(inputs.prefixes):
        normal = posixpath.normpath(prefix) if prefix else "."
        if normal == "." or normal == ".." or normal.startswith("../") or posixpath.isabs(normal):
            findings.append(f"{prefix!r} is the checkout root or outside it")
        elif not _tracked_under(repo_root, prefix):
            findings.append(f"{prefix!r} holds no tracked file")
    return findings


def scratch_reads(repo_root: str, inputs: ap.Inputs) -> list[str]:
    """Every file `play_text` would read through a walked input tree under the scratch tree
    `untracked/` or outside the checkout. It records what `play_text` hands its reader instead of
    reading it and judges the path by its place alone, not by the mapper's idea of a tree with
    checkout content, so it still goes red when the mapper's predicate is wrong."""
    handed: list[str] = []

    def record(path: str) -> str:
        handed.append(path)
        return ""

    with mock.patch.object(gv, "_read", side_effect=record):
        gv.play_text(repo_root, inputs)
    exact = {os.path.join(repo_root, path) for path in inputs.exact}
    walked = sorted({os.path.relpath(path, repo_root) for path in handed if path not in exact})
    return [path for path in walked if path == ".." or path.startswith(("../", SCRATCH_TREE))]


def _message(play: str, findings: list[str]) -> str:
    return (f"[{RULE_ID}] {play} walks input trees that hold no checkout content "
            f"(see CLAUDE/QA.md#{RULE_ID}):\n  " + "\n  ".join(findings))


class TestTheRule(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name
        for rel, text in (("files/a.conf", "a\n"), ("scratch/.gitignore", "*\n"), ("scratch/run.log", "x\n")):
            os.makedirs(os.path.join(self.root, os.path.dirname(rel)), exist_ok=True)
            with open(os.path.join(self.root, rel), "w", encoding="utf-8") as handle:
                handle.write(text)
        for args in (("init", "-q"), ("add", "-A")):
            subprocess.run(["git", "-C", self.root, *args], check=True, capture_output=True, env=_isolated_git_env())

    def test_a_tree_with_tracked_files_is_not_a_finding(self) -> None:
        self.assertEqual(untracked_trees(self.root, ap.Inputs(prefixes={"files/"})), [])

    def test_a_tree_tracking_only_its_gitignore_is_a_finding(self) -> None:
        self.assertEqual(len(untracked_trees(self.root, ap.Inputs(prefixes={"scratch/"}))), 1)

    def test_a_tree_git_has_never_seen_is_a_finding(self) -> None:
        self.assertEqual(len(untracked_trees(self.root, ap.Inputs(prefixes={"runtime/seats/"}))), 1)

    def test_the_root_and_a_climb_out_of_it_are_findings(self) -> None:
        found = untracked_trees(self.root, ap.Inputs(prefixes={"./", "", "files/../", "../x/"}))
        self.assertEqual(len(found), 4)

    def test_the_message_carries_the_rule_id(self) -> None:
        self.assertIn(f"[{RULE_ID}]", _message("p", ["x"]))
        self.assertIn(f"CLAUDE/QA.md#{RULE_ID}", _message("p", ["x"]))


class TestNoPlayWalksAnUntrackedTree(unittest.TestCase):
    """The rule over every host play of this checkout."""

    def test_no_host_play_has_an_input_tree_without_checkout_content(self) -> None:
        plays = ap.candidate_plays(REPO_ROOT)
        self.assertGreater(len(plays), 30)
        for play in plays:
            with self.subTest(play=play):
                findings = untracked_trees(REPO_ROOT, ap.play_inputs(REPO_ROOT, play))
                self.assertEqual(findings, [], _message(play, findings))


class TestPlayTextReadsNothingUnderTheScratchTree(unittest.TestCase):
    """The hazard itself, judged apart from the fix: `play_text` reads no file under `untracked/`
    (where the named pipes of old plan runs live) and none outside the checkout. The fix (the mapper moving such trees into `Inputs.local`) is reverted here, in the
    test, to prove the check goes red without it."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name
        play = """
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{{ root_dir }}/files/conf/"
                    dest: /etc/conf/
                - ansible.builtin.file:
                    path: "{{ root_dir }}/untracked/"
                    state: directory
        """
        for rel, text in (("playbooks/imports/play-a.yml", textwrap.dedent(play)), ("files/conf/a.conf", "a\n"),
                          ("untracked/.gitignore", "*\n"), ("untracked/run.log", "x\n")):
            os.makedirs(os.path.join(self.root, os.path.dirname(rel)), exist_ok=True)
            with open(os.path.join(self.root, rel), "w", encoding="utf-8") as handle:
                handle.write(text)
        for args in (("init", "-q"), ("add", "-A")):
            subprocess.run(["git", "-C", self.root, *args], check=True, capture_output=True, env=_isolated_git_env())

    def test_with_the_fix_reverted_the_scratch_tree_is_read_and_found(self) -> None:
        with mock.patch.object(ap, "untracked_trees", return_value=set()):
            inputs = ap.play_inputs(self.root, "playbooks/imports/play-a.yml")
        self.assertIn("untracked/", inputs.prefixes)
        self.assertEqual(scratch_reads(self.root, inputs), ["untracked/.gitignore", "untracked/run.log"])

    def test_with_the_fix_nothing_under_the_scratch_tree_is_read(self) -> None:
        inputs = ap.play_inputs(self.root, "playbooks/imports/play-a.yml")
        self.assertEqual(scratch_reads(self.root, inputs), [])

    def test_a_walk_that_climbs_out_of_the_checkout_is_found(self) -> None:
        inner = os.path.join(self.root, "inner")
        os.makedirs(inner)
        self.assertIn("../files/conf/a.conf", scratch_reads(inner, ap.Inputs(prefixes={"../files/"})))

    def test_no_host_play_reads_under_the_scratch_tree(self) -> None:
        for play in ap.candidate_plays(REPO_ROOT):
            with self.subTest(play=play):
                found = scratch_reads(REPO_ROOT, ap.play_inputs(REPO_ROOT, play))
                self.assertEqual(found, [], f"[{RULE_ID}] {play}: play_text reads scratch or outside files: {found[:20]}")


if __name__ == "__main__":
    unittest.main()
