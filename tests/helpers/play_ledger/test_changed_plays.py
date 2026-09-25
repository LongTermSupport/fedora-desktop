"""Tests for helpers.play_ledger.changed_plays — which plays `run.bash --changed` runs.

A play run here is to run again when any of its inputs (its own file, and everything it
deploys from the checkout) changed between the commit it last ran from and the working
tree, or when its last run did not succeed. Every other answer is a named line, never a
silent omission, and a question that cannot be answered prints nothing and exits 2.
"""

from __future__ import annotations

import io
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import changed_plays, ledger, store
from helpers.self_update.affected_plays import Inputs

COMMIT_A = "a" * 40
COMMIT_B = "b" * 40
SHA = "1" * 64
STAMP = "2026-09-25T09:00:00Z"

YOLO = "playbooks/imports/play-claude-yolo.yml"
GIT = "playbooks/imports/play-git-configure-and-tools.yml"
BASIC = "playbooks/imports/play-basic-configs.yml"


def _seed(base: str, runs: list[tuple[str, str, str]]) -> None:
    """runs: (play, commit, outcome)."""
    store.ensure_ledger(base, commit=COMMIT_A, at=STAMP)
    for play, commit, outcome in runs:
        store.append_record(base, ledger.build_record(
            play=play, name=play, commit=commit, dirty=False, play_sha256=SHA,
            outcome=outcome, changed=0, started=STAMP, finished=STAMP,
        ))


INPUTS = {
    YOLO: Inputs(exact={YOLO, "files/home/.local/bin/ccy-sessions"},
                 prefixes={"files/var/local/claude-yolo/"}),
    GIT: Inputs(exact={GIT}, unresolved=[f"{GIT}:12: {{{{ root_dir }}}}/vars/git-signing.yml"]),
    BASIC: Inputs(exact={BASIC}),
}


class _Case(unittest.TestCase):
    def _run(self, base: str, *, changed: dict[str, list[str]], present=None, tracked=None,
             diff_error: Exception | None = None):
        """changed: commit -> paths changed from it to the working tree."""
        present = set(INPUTS) if present is None else present
        tracked = set(INPUTS) if tracked is None else tracked
        self.diffs: list[str] = []

        def changed_since(root: str, commit: str) -> list[str]:
            self.diffs.append(commit)
            if diff_error is not None:
                raise diff_error
            return changed.get(commit, [])

        out, err = io.StringIO(), io.StringIO()
        code = changed_plays.run(
            base=base, repo_root="/repo", stdout=out, stderr=err,
            changed_since=changed_since,
            exists_now=lambda root, play: play in present,
            tracked_at=lambda root, commit, play: play in tracked,
            inputs_of=lambda root, play: INPUTS[play],
        )
        return code, out.getvalue(), err.getvalue()


class TestWhatRuns(_Case):
    def test_a_play_whose_own_file_changed_runs(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(BASIC, COMMIT_A, "ok")])
            code, out, _ = self._run(base, changed={COMMIT_A: [BASIC]})
            self.assertEqual(code, changed_plays.EXIT_OK)
            self.assertEqual(out, f"RUN {BASIC}\n")

    def test_a_play_whose_deployed_file_changed_runs(self) -> None:
        """The case the ledger alone misses: the play's own file is untouched."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(YOLO, COMMIT_A, "ok")])
            code, out, _ = self._run(base, changed={COMMIT_A: ["files/home/.local/bin/ccy-sessions"]})
            self.assertEqual(out, f"RUN {YOLO}\n")

    def test_a_change_under_a_deployed_directory_runs_the_play(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(YOLO, COMMIT_A, "ok")])
            _, out, _ = self._run(base, changed={COMMIT_A: ["files/var/local/claude-yolo/lib/common.bash"]})
            self.assertEqual(out, f"RUN {YOLO}\n")

    def test_an_unrelated_change_runs_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(YOLO, COMMIT_A, "ok"), (BASIC, COMMIT_A, "ok")])
            code, out, _ = self._run(base, changed={COMMIT_A: ["docs/ccy.md"]})
            self.assertEqual(code, changed_plays.EXIT_OK)
            self.assertEqual(out, "")

    def test_a_last_run_that_did_not_succeed_runs_again_unchanged(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(BASIC, COMMIT_A, "failed"), (YOLO, COMMIT_A, "unreachable")])
            _, out, _ = self._run(base, changed={})
            self.assertEqual(out, f"RUN {BASIC}\nRUN {YOLO}\n")

    def test_each_play_is_judged_from_its_own_last_commit(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(YOLO, COMMIT_A, "ok"), (BASIC, COMMIT_B, "ok")])
            _, out, _ = self._run(base, changed={COMMIT_A: [BASIC], COMMIT_B: []})
            self.assertEqual(out, "")

    def test_one_diff_per_distinct_commit(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(YOLO, COMMIT_A, "ok"), (BASIC, COMMIT_A, "ok"), (GIT, COMMIT_B, "ok")])
            self._run(base, changed={})
            self.assertEqual(sorted(self.diffs), [COMMIT_A, COMMIT_B])

    def test_plays_are_listed_in_a_stable_order(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(YOLO, COMMIT_A, "ok"), (BASIC, COMMIT_A, "ok")])
            _, out, _ = self._run(base, changed={COMMIT_A: [YOLO, BASIC]})
            self.assertEqual(out, f"RUN {BASIC}\nRUN {YOLO}\n")


class TestNamedNotRun(_Case):
    def test_a_play_that_cannot_be_followed_is_named_when_it_would_not_run(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(GIT, COMMIT_A, "ok")])
            code, out, _ = self._run(base, changed={})
            self.assertEqual(code, changed_plays.EXIT_OK)
            self.assertEqual(out, f"UNRESOLVED {GIT} {INPUTS[GIT].unresolved[0]}\n")

    def test_a_play_that_runs_anyway_is_not_also_named_unresolved(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(GIT, COMMIT_A, "ok")])
            _, out, _ = self._run(base, changed={COMMIT_A: [GIT]})
            self.assertEqual(out, f"RUN {GIT}\n")

    def test_a_deleted_play_the_repo_tracked_is_gone(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(BASIC, COMMIT_A, "ok")])
            _, out, _ = self._run(base, changed={}, present=set())
            self.assertEqual(out, f"GONE {BASIC}\n")

    def test_a_play_the_repo_never_tracked_is_not_mentioned(self) -> None:
        """A scratch probe: it was never the repo's to run again."""
        with tempfile.TemporaryDirectory() as base:
            store.ensure_ledger(base, commit=COMMIT_A, at=STAMP)
            store.append_record(base, ledger.build_record(
                play="untracked/scratch/probe.yml", name="probe", commit=COMMIT_A, dirty=False,
                play_sha256=SHA, outcome="ok", changed=0, started=STAMP, finished=STAMP,
            ))
            code, out, _ = self._run(base, changed={}, present=set(), tracked=set())
            self.assertEqual(code, changed_plays.EXIT_OK)
            self.assertEqual(out, "")


class TestNoAnswer(_Case):
    def test_nothing_ever_run_is_a_clean_empty_answer(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            store.ensure_ledger(base, commit=COMMIT_A, at=STAMP)
            code, out, _ = self._run(base, changed={})
            self.assertEqual((code, out), (changed_plays.EXIT_OK, ""))

    def test_a_broken_ledger_answers_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(BASIC, COMMIT_A, "ok")])
            store.mark_broken(base, error="a run could not be recorded", at=STAMP)
            code, out, err = self._run(base, changed={COMMIT_A: [BASIC]})
            self.assertEqual(code, changed_plays.EXIT_NO_ANSWER)
            self.assertEqual(out, "")
            self.assertIn("could not be recorded", err)

    def test_a_corrupt_ledger_answers_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(BASIC, COMMIT_A, "ok")])
            with open(ledger.runs_path(base), "a", encoding="utf-8") as handle:
                handle.write("{not json\n")
            code, out, _ = self._run(base, changed={COMMIT_A: [BASIC]})
            self.assertEqual((code, out), (changed_plays.EXIT_NO_ANSWER, ""))

    def test_a_git_failure_answers_nothing_not_a_partial_list(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(BASIC, COMMIT_A, "failed"), (YOLO, COMMIT_A, "ok")])
            code, out, err = self._run(base, changed={}, diff_error=RuntimeError("bad object"))
            self.assertEqual((code, out), (changed_plays.EXIT_NO_ANSWER, ""))
            self.assertIn("bad object", err)


class TestRealGit(unittest.TestCase):
    """changed_since against a real repository: committed and uncommitted changes both count."""

    def test_committed_and_working_tree_changes_are_both_seen(self) -> None:
        import subprocess

        with tempfile.TemporaryDirectory() as root:
            def git(*args: str) -> str:
                return subprocess.run(["git", "-C", root, *args], check=True,
                                      capture_output=True, text=True).stdout

            git("init", "-q")
            git("config", "user.email", "test@example.com")
            git("config", "user.name", "test")
            git("config", "commit.gpgsign", "false")
            for name in ("a.txt", "b.txt", "c.txt"):
                with open(os.path.join(root, name), "w", encoding="utf-8") as handle:
                    handle.write("one\n")
            git("add", ".")
            git("commit", "-q", "-m", "first")
            first = git("rev-parse", "HEAD").strip()
            with open(os.path.join(root, "a.txt"), "w", encoding="utf-8") as handle:
                handle.write("two\n")
            git("commit", "-q", "-am", "second")
            with open(os.path.join(root, "b.txt"), "w", encoding="utf-8") as handle:
                handle.write("uncommitted\n")
            self.assertEqual(sorted(changed_plays.changed_since(root, first)), ["a.txt", "b.txt"])


if __name__ == "__main__":
    unittest.main()
