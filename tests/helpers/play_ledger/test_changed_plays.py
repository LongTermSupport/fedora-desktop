"""Tests for helpers.play_ledger.changed_plays — which plays `run.bash --changed` runs.

A play run here is to run again when any of its inputs (its own file, and everything it
deploys from the checkout) changed between the commit it last ran from and the working
tree, or when its last run did not succeed. Every other answer is a named line, never a
silent omission, and a question that cannot be answered prints nothing and exits 2.
"""

from __future__ import annotations

import contextlib
import io
import os
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

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
OPT_B = "playbooks/imports/optional/common/play-b-optional.yml"
OPT_A = "playbooks/imports/optional/common/play-a-optional.yml"
SCRATCH = "untracked/scratch/probe.yml"
DEV = "playbooks/dev/play-repo-tool.yml"

OLD = "playbooks/imports/play-claude-code.yml"

#: The order playbook-main.yml imports the core plays in, for the stubbed cases.
MAIN_ORDER = [BASIC, GIT, YOLO]


def _retired_history(root: str, commit: str, play: str) -> bool:
    """OLD was in the tree at COMMIT_A and removed by COMMIT_B; every other play is always there."""
    if play == OLD:
        return commit == COMMIT_A
    return play in INPUTS


def _seed(base: str, runs: list[tuple[str, str, str]], *, dirty: frozenset[str] = frozenset()) -> None:
    """runs: (play, commit, outcome). A play named in `dirty` ran from a dirty checkout."""
    store.ensure_ledger(base, commit=COMMIT_A, at=STAMP)
    for play, commit, outcome in runs:
        store.append_record(base, ledger.build_record(
            play=play, name=play, commit=commit, dirty=play in dirty, play_sha256=SHA,
            outcome=outcome, changed=0, started=STAMP, finished=STAMP,
        ))


INPUTS = {
    YOLO: Inputs(exact={YOLO, "files/home/.local/bin/ccy-sessions"},
                 prefixes={"files/var/local/claude-yolo/"}),
    GIT: Inputs(exact={GIT}, unresolved=[f"{GIT}:12: {{{{ root_dir }}}}/vars/git-signing.yml"]),
    BASIC: Inputs(exact={BASIC}),
    OPT_A: Inputs(exact={OPT_A}),
    OPT_B: Inputs(exact={OPT_B}),
}


def _isolated_git_env() -> dict[str, str]:
    """The environment helpers/CLAUDE.md requires of a test that runs git."""
    env = {key: value for key, value in os.environ.items()
           if key not in ("GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS")}
    env["GIT_CONFIG_GLOBAL"] = os.devnull
    env["GIT_CONFIG_NOSYSTEM"] = "1"
    return env


def _git(root: str, *args: str) -> str:
    return subprocess.run(["git", "-C", root, *args], check=True, capture_output=True,
                          text=True, env=_isolated_git_env()).stdout


def _init_repo(root: str) -> None:
    _git(root, "init", "-q")
    _git(root, "config", "user.email", "test@example.com")
    _git(root, "config", "user.name", "test")
    _git(root, "config", "commit.gpgsign", "false")


def _write(root: str, path: str, text: str) -> None:
    full = os.path.join(root, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w", encoding="utf-8") as handle:
        handle.write(text)


class _Case(unittest.TestCase):
    def _run(self, base: str, *, changed: dict[str, list[str]], present=None, tracked=None,
             diff_error: Exception | None = None, main_order: list[str] | None = None,
             tracked_at=None, retired: dict[str, str] | None = None):
        """changed: commit -> paths changed from it to the working tree."""
        present = set(INPUTS) if present is None else present
        tracked = set(INPUTS) if tracked is None else tracked
        order = MAIN_ORDER if main_order is None else main_order
        tracked_at = tracked_at or (lambda root, commit, play: play in tracked)
        retired_map = {} if retired is None else retired
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
            tracked_at=tracked_at,
            inputs_of=lambda root, play: INPUTS[play],
            main_order=lambda root: order,
            retired_plays=lambda root: retired_map,
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

    def test_plays_are_listed_in_the_order_the_main_playbook_runs_them(self) -> None:
        """Not by name: play-ZZ-repo-cleanup must come after the plays that add COPRs."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(BASIC, COMMIT_A, "ok"), (YOLO, COMMIT_A, "ok")])
            _, out, _ = self._run(base, changed={COMMIT_A: [YOLO, BASIC]}, main_order=[YOLO, BASIC])
            self.assertEqual(out, f"RUN {YOLO}\nRUN {BASIC}\n")

    def test_plays_outside_the_main_playbook_come_after_it_by_path(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(OPT_B, COMMIT_A, "ok"), (OPT_A, COMMIT_A, "ok"), (YOLO, COMMIT_A, "ok")])
            _, out, _ = self._run(base, changed={COMMIT_A: [OPT_B, OPT_A, YOLO]})
            self.assertEqual(out, f"RUN {YOLO}\nRUN {OPT_A}\nRUN {OPT_B}\n")

    def test_a_run_from_a_dirty_checkout_runs_again(self) -> None:
        """What it deployed may be an edit since reverted, which no diff can show."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(YOLO, COMMIT_A, "ok"), (BASIC, COMMIT_A, "ok")], dirty=frozenset({YOLO}))
            _, out, _ = self._run(base, changed={})
            self.assertEqual(out, f"RUN {YOLO}\n")


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
            _seed(base, [(SCRATCH, COMMIT_A, "ok")])
            code, out, _ = self._run(base, changed={}, present=set(), tracked=set())
            self.assertEqual(code, changed_plays.EXIT_OK)
            self.assertEqual(out, "")

    def test_a_failed_scratch_play_still_on_disk_is_not_offered(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(SCRATCH, COMMIT_A, "failed")])
            code, out, _ = self._run(base, changed={COMMIT_A: [SCRATCH]}, present={SCRATCH})
            self.assertEqual((code, out), (changed_plays.EXIT_OK, ""))

    def test_a_retired_play_its_successor_has_absorbed_is_not_mentioned(self) -> None:
        """The successor ran, successfully, at a commit that no longer has the old play."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(OLD, COMMIT_A, "ok"), (YOLO, COMMIT_B, "ok")])
            code, out, _ = self._run(base, changed={COMMIT_B: []}, present=set(INPUTS),
                                     tracked_at=_retired_history, retired={OLD: YOLO})
            self.assertEqual((code, out), (changed_plays.EXIT_OK, ""))

    def test_a_retired_play_runs_its_successor_until_absorbed(self) -> None:
        """Not GONE on every run for ever: the successor is what takes it over."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(OLD, COMMIT_A, "ok"), (YOLO, COMMIT_A, "ok")])
            _, out, _ = self._run(base, changed={COMMIT_A: []}, present=set(INPUTS),
                                  tracked_at=_retired_history, retired={OLD: YOLO})
            self.assertEqual(out, f"RUN {YOLO}\n")

    def test_a_retired_play_whose_successor_never_ran_here_runs_the_successor(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(OLD, COMMIT_A, "ok"), (BASIC, COMMIT_A, "ok")])
            _, out, _ = self._run(base, changed={COMMIT_A: []}, present=set(INPUTS),
                                  tracked_at=_retired_history, retired={OLD: YOLO})
            self.assertEqual(out, f"RUN {YOLO}\n")

    def test_a_retired_map_that_disagrees_with_head_answers_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(OLD, COMMIT_A, "ok")])
            code, out, err = self._run(base, changed={}, present=set(INPUTS),
                                       tracked_at=_retired_history, retired={OLD: "playbooks/imports/play-nope.yml"})
            self.assertEqual((code, out), (changed_plays.EXIT_NO_ANSWER, ""))
            self.assertIn("play-nope.yml", err)

    def test_a_dev_play_is_not_offered(self) -> None:
        """playbooks/dev/ works on the repo, not the host, so it is never a host catch-up."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(DEV, COMMIT_A, "failed")])
            code, out, _ = self._run(base, changed={COMMIT_A: [DEV]}, present={DEV}, tracked={DEV})
            self.assertEqual((code, out), (changed_plays.EXIT_OK, ""))


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

    def test_a_git_failure_names_the_play_and_how_to_run_it(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [(BASIC, COMMIT_A, "ok")])
            _, _, err = self._run(base, changed={}, diff_error=RuntimeError("bad object"))
            self.assertIn(BASIC, err)
            self.assertIn(f"./run.bash {BASIC}", err)


class TestMainOrder(unittest.TestCase):
    def test_reads_the_imports_in_order_as_repo_paths(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            _write(root, changed_plays.MAIN_PLAYBOOK,
                   "# a comment\n- import_playbook: imports/play-b.yml\n"
                   "# - import_playbook: imports/play-commented.yml\n"
                   "- import_playbook: imports/play-a.yml\n")
            self.assertEqual(changed_plays.main_order(root),
                             ["playbooks/imports/play-b.yml", "playbooks/imports/play-a.yml"])

    def test_a_trailing_comment_and_the_long_module_name_are_read(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            _write(root, changed_plays.MAIN_PLAYBOOK,
                   "- import_playbook: imports/play-b.yml  # why it is here\n"
                   "- ansible.builtin.import_playbook: imports/play-a.yml\n")
            self.assertEqual(changed_plays.main_order(root),
                             ["playbooks/imports/play-b.yml", "playbooks/imports/play-a.yml"])

    def test_an_import_line_it_cannot_read_raises_rather_than_reorder(self) -> None:
        """A play dropped from the order would run after play-ZZ-repo-cleanup, silently."""
        with tempfile.TemporaryDirectory() as root:
            _write(root, changed_plays.MAIN_PLAYBOOK,
                   "- import_playbook: imports/play-b.yml\n"
                   "- import_playbook: \"{{ somewhere }}/play-a.yml\" extra\n")
            with self.assertRaises(ValueError) as caught:
                changed_plays.main_order(root)
            self.assertIn("play-a.yml", str(caught.exception))

    def test_a_missing_main_playbook_raises(self) -> None:
        with tempfile.TemporaryDirectory() as root, self.assertRaises(OSError):
            changed_plays.main_order(root)

    def test_the_real_main_playbook_is_read_whole(self) -> None:
        """Every import line in this checkout's playbook-main.yml, each naming a real play."""
        repo_root = os.path.join(os.path.dirname(__file__), "..", "..", "..")
        with open(os.path.join(repo_root, changed_plays.MAIN_PLAYBOOK), encoding="utf-8") as handle:
            imports = [line for line in handle if "import_playbook" in line.split("#", 1)[0]]
        order = changed_plays.main_order(repo_root)
        self.assertEqual(len(order), len(imports))
        self.assertEqual(order[-1], "playbooks/imports/play-ZZ-repo-cleanup.yml")
        for play in order:
            self.assertTrue(os.path.isfile(os.path.join(repo_root, play)), play)


class TestRealGit(unittest.TestCase):
    """Against a real repository, with git's own config shut out."""

    def test_committed_and_working_tree_changes_are_both_seen(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            _init_repo(root)
            for name in ("a.txt", "b.txt", "c.txt"):
                _write(root, name, "one\n")
            _git(root, "add", ".")
            _git(root, "commit", "-q", "-m", "first")
            first = _git(root, "rev-parse", "HEAD").strip()
            _write(root, "a.txt", "two\n")
            _git(root, "commit", "-q", "-am", "second")
            _write(root, "b.txt", "uncommitted\n")
            with mock.patch.dict(os.environ, _isolated_git_env(), clear=True):
                self.assertEqual(sorted(changed_plays.changed_since(root, first)), ["a.txt", "b.txt"])

    def test_main_runs_the_play_whose_deployed_file_changed(self) -> None:
        """The success criterion end to end: the real mapper, the real ledger, the real git."""
        copy_task = (
            "- hosts: desktop\n  tasks:\n    - name: Copy\n      ansible.builtin.copy:\n"
            '        src: "{{{{ root_dir }}}}/files/home/{name}.conf"\n        dest: /tmp/{name}.conf\n'
        )
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as state:
            _init_repo(root)
            _write(root, changed_plays.MAIN_PLAYBOOK,
                   "- import_playbook: imports/play-two.yml\n- import_playbook: imports/play-one.yml\n")
            for name in ("one", "two"):
                _write(root, f"playbooks/imports/play-{name}.yml", copy_task.format(name=name))
                _write(root, f"files/home/{name}.conf", "v1\n")
            _git(root, "add", ".")
            _git(root, "commit", "-q", "-m", "first")
            first = _git(root, "rev-parse", "HEAD").strip()

            env = {**_isolated_git_env(), "XDG_STATE_HOME": state}
            base = ledger.ledger_dir(env, state)
            os.makedirs(base)
            store.ensure_ledger(base, commit=first, at=STAMP)
            for name in ("one", "two"):
                play = f"playbooks/imports/play-{name}.yml"
                store.append_record(base, ledger.build_record(
                    play=play, name=name, commit=first, dirty=False, play_sha256=SHA,
                    outcome="ok", changed=0, started=STAMP, finished=STAMP,
                ))
            _write(root, "files/home/one.conf", "v2\n")

            out = io.StringIO()
            with mock.patch.dict(os.environ, env, clear=True), contextlib.redirect_stdout(out):
                code = changed_plays.main(["--repo-root", root])
            self.assertEqual((code, out.getvalue()), (changed_plays.EXIT_OK, "RUN playbooks/imports/play-one.yml\n"))

            # Both changed: they come out in playbook-main.yml's order, not by name.
            _write(root, "files/home/two.conf", "v2\n")
            out = io.StringIO()
            with mock.patch.dict(os.environ, env, clear=True), contextlib.redirect_stdout(out):
                code = changed_plays.main(["--repo-root", root])
            self.assertEqual(
                (code, out.getvalue()),
                (changed_plays.EXIT_OK,
                 "RUN playbooks/imports/play-two.yml\nRUN playbooks/imports/play-one.yml\n"),
            )


if __name__ == "__main__":
    unittest.main()
