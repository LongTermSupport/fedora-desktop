"""Tests for helpers.host_health.play_runner — the panel's play list and the one gate a
requested play passes before it runs (Plan 00109, Task 4.3).

The list is derived from `check_freshness`'s own verdicts; the gate is strict because
the name it judges comes from a state file on disk, not from a person typing it.
"""

from __future__ import annotations

import io
import os
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import play_runner
from helpers.play_ledger import changed_plays, check_freshness, freshness, ledger, store

FORTY_HEX = "e" * 40
SIXTY_FOUR_HEX = "1" * 64
STAMP = "2026-09-14T09:00:00Z"
PLAY = "playbooks/imports/play-example.yml"


def _verdict(play: str, state: str) -> freshness.Verdict:
    return freshness.Verdict(play, state, ())


class TestRunnable(unittest.TestCase):
    def test_each_play_carries_its_own_state_sorted_by_path(self) -> None:
        verdicts = [
            _verdict("playbooks/imports/play-b.yml", freshness.STALE),
            _verdict("playbooks/imports/play-a.yml", freshness.FRESH),
            _verdict("playbooks/imports/play-c.yml", freshness.FAILED),
        ]
        self.assertEqual(
            play_runner.runnable(verdicts),
            [
                {"play": "playbooks/imports/play-a.yml", "state": "fresh"},
                {"play": "playbooks/imports/play-b.yml", "state": "stale"},
                {"play": "playbooks/imports/play-c.yml", "state": "failed"},
            ],
        )

    def test_a_gone_play_is_not_offered(self) -> None:
        """Nothing to run; the health section already reports it."""
        verdicts = [_verdict(PLAY, freshness.GONE), _verdict("playbooks/x.yml", freshness.FRESH)]
        self.assertEqual(
            [entry["play"] for entry in play_runner.runnable(verdicts)], ["playbooks/x.yml"]
        )

    def test_a_ledgered_play_outside_playbooks_is_not_offered(self) -> None:
        """The runner only launches plays under playbooks/, so it lists no other."""
        verdicts = [_verdict("tests/fixtures/play.yml", freshness.FRESH)]
        self.assertEqual(play_runner.runnable(verdicts), [])


class _Checkout:
    """A throwaway checkout with one executable play and one that is not."""

    def __init__(self, root: str) -> None:
        self.root = root
        self.play = self.add(PLAY)
        self.add("playbooks/imports/not-executable.yml", executable=False)

    def add(self, relative: str, *, executable: bool = True) -> str:
        path = os.path.join(self.root, relative)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write("---\n")
        if executable:
            os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR)
        return path


class TestValidate(unittest.TestCase):
    def setUp(self) -> None:
        self._dir = tempfile.TemporaryDirectory()
        self.checkout = _Checkout(self._dir.name)
        self.offered = {PLAY, "playbooks/imports/not-executable.yml",
                        "playbooks/imports/absent.yml"}

    def tearDown(self) -> None:
        self._dir.cleanup()

    def validate(self, play: str) -> str:
        return play_runner.validate(play, repo_root=self.checkout.root, offered=self.offered)

    def test_an_offered_executable_play_resolves_to_its_absolute_path(self) -> None:
        self.assertEqual(self.validate(PLAY), os.path.realpath(self.checkout.play))

    def test_every_malformed_name_is_refused(self) -> None:
        for play in (
            "",
            "/etc/passwd",
            f"/{PLAY}",
            "playbooks/../run.bash",
            "playbooks//imports/play-example.yml",
            "./playbooks/imports/play-example.yml",
            "playbooks/imports/play-example.yml/",
            "scripts/qa-all.bash",
            "playbooks/imports/play-example.yaml",
            "playbooks/imports/play example.yml\n",
            "-e playbooks/imports/play-example.yml",
        ):
            with self.subTest(play=play), self.assertRaises(play_runner.PlayRefused):
                self.validate(play)

    def test_a_play_the_panel_does_not_offer_is_refused(self) -> None:
        self.checkout.add("playbooks/imports/never-run-here.yml")
        with self.assertRaises(play_runner.PlayRefused) as caught:
            self.validate("playbooks/imports/never-run-here.yml")
        self.assertIn("does not offer", str(caught.exception))

    def test_an_offered_play_missing_from_the_checkout_is_refused(self) -> None:
        with self.assertRaises(play_runner.PlayRefused):
            self.validate("playbooks/imports/absent.yml")

    def test_a_play_without_its_execute_bit_is_refused(self) -> None:
        """It runs through its own shebang, which a non-executable file cannot."""
        with self.assertRaises(play_runner.PlayRefused) as caught:
            self.validate("playbooks/imports/not-executable.yml")
        self.assertIn("executable", str(caught.exception))

    def test_a_symlink_out_of_the_playbooks_tree_is_refused(self) -> None:
        outside = os.path.join(self.checkout.root, "elsewhere.yml")
        with open(outside, "w", encoding="utf-8") as handle:
            handle.write("---\n")
        os.chmod(outside, 0o755)
        link = os.path.join(self.checkout.root, "playbooks", "imports", "linked.yml")
        os.symlink(outside, link)
        self.offered.add("playbooks/imports/linked.yml")
        with self.assertRaises(play_runner.PlayRefused):
            self.validate("playbooks/imports/linked.yml")


UNRECORDED = "playbooks/imports/play-unrecorded.yml"
NEVER = "playbooks/imports/play-never.yml"
UNJUDGED = "playbooks/imports/play-unjudged.yml"
GONE = "playbooks/imports/play-gone.yml"


def _judgement() -> changed_plays.Judgement:
    """PLAY ran here and is current; UNRECORDED has no record but its files show it ran;
    NEVER and UNJUDGED have no record and are not run; GONE ran here and is gone."""
    states = {PLAY: changed_plays.STATE_CURRENT, UNRECORDED: changed_plays.STATE_UNRECORDED}
    return changed_plays.Judgement(
        order=(PLAY, UNRECORDED, GONE), states=states, gone=(GONE,), unresolved={},
        changed_inputs={}, dirty=frozenset(), took_over={}, not_judged=(),
        deployed={UNRECORDED: ["/etc/unrecorded.conf"]}, never=(NEVER,), unjudged=(UNJUDGED,),
    )


class _Ledger(unittest.TestCase):
    """A ledger with one run of PLAY, and a judge that answers `_judgement()`."""

    def setUp(self) -> None:
        self._dir = tempfile.TemporaryDirectory()
        self.state_home = os.path.join(self._dir.name, "state")
        self.base = ledger.ledger_dir({"XDG_STATE_HOME": self.state_home}, self._dir.name)
        store.ensure_ledger(self.base, commit=FORTY_HEX, at=STAMP)
        store.append_record(self.base, ledger.build_record(
            play=PLAY, name=PLAY, commit=FORTY_HEX, dirty=False,
            play_sha256=SIXTY_FOUR_HEX, outcome="ok", changed=0,
            started=STAMP, finished=STAMP,
        ))
        self.asked: list[tuple[dict, dict]] = []

    def tearDown(self) -> None:
        self._dir.cleanup()

    def judge(self, latest: dict, **kwargs) -> changed_plays.Judgement:
        self.asked.append((latest, kwargs))
        return _judgement()


class TestOffered(_Ledger):
    """Review of #323: the gate accepts exactly the plays the list offers, no more."""

    def test_it_is_exactly_what_runnable_offers_for_the_same_judgement(self) -> None:
        offered = play_runner.offered_plays(self.base, "/repo", judge=self.judge)
        rows = play_runner.runnable(check_freshness.verdicts(_judgement()))
        self.assertEqual(offered, {row["play"] for row in rows})
        self.assertEqual(offered, {PLAY, UNRECORDED})

    def test_the_judge_gets_the_full_runs_and_every_recorded_play(self) -> None:
        play_runner.offered_plays(self.base, "/repo", judge=self.judge)
        latest, kwargs = self.asked[0]
        self.assertEqual(list(latest), [PLAY])
        self.assertEqual(kwargs, {"repo_root": "/repo", "recorded": frozenset({PLAY})})

    def test_a_broken_ledger_offers_nothing(self) -> None:
        with open(ledger.sentinel_path(self.base), "w", encoding="utf-8") as handle:
            handle.write("a hole\n")
        with self.assertRaises(ValueError):
            play_runner.offered_plays(self.base, "/repo", judge=self.judge)
        self.assertEqual(self.asked, [])


class TestMain(_Ledger):
    """The executor the on-demand command calls: stdout is the path, and only the path."""

    def setUp(self) -> None:
        super().setUp()
        self.checkout = _Checkout(os.path.join(self._dir.name, "checkout"))
        for play in (UNRECORDED, NEVER, UNJUDGED):
            self.checkout.add(play)

    def main(self, play: str, judge=None) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = play_runner.main(
            ["--repo-root", self.checkout.root, "--validate", play],
            environ={"XDG_STATE_HOME": self.state_home}, home=self._dir.name,
            stdout=out, stderr=err, judge=judge or self.judge,
        )
        return code, out.getvalue(), err.getvalue()

    def test_a_play_with_no_record_whose_files_show_it_ran_is_accepted(self) -> None:
        """runnable() offers it, so the gate must pass it (review of #323)."""
        code, out, _ = self.main(UNRECORDED)
        self.assertEqual((code, out), (0, f"{os.path.realpath(os.path.join(self.checkout.root, UNRECORDED))}\n"))

    def test_a_play_not_offered_is_refused_though_it_is_in_the_checkout(self) -> None:
        for play in (NEVER, UNJUDGED):
            with self.subTest(play=play):
                code, out, err = self.main(play)
                self.assertEqual((code, out), (play_runner.EXIT_REFUSED, ""))
                self.assertIn("does not offer", err)

    def test_a_judgement_that_cannot_be_made_refuses_every_play(self) -> None:
        def no_answer(latest: dict, **kwargs) -> changed_plays.Judgement:
            raise changed_plays.NoAnswer("cannot judge x", "run it by name instead")

        code, out, err = self.main(PLAY, judge=no_answer)
        self.assertEqual((code, out), (play_runner.EXIT_LEDGER_UNREADABLE, ""))
        self.assertIn("cannot judge x", err)

    def test_an_accepted_play_prints_its_path_and_nothing_else(self) -> None:
        code, out, err = self.main(PLAY)
        self.assertEqual(code, 0)
        self.assertEqual(out, f"{os.path.realpath(self.checkout.play)}\n")
        self.assertEqual(err, "")

    def test_a_refused_play_says_why_on_stderr_and_prints_no_path(self) -> None:
        code, out, err = self.main("playbooks/../run.bash")
        self.assertEqual(code, play_runner.EXIT_REFUSED)
        self.assertEqual(out, "")
        self.assertIn("playbooks/../run.bash", err)

    def test_an_unreadable_ledger_refuses_rather_than_trusting_the_name(self) -> None:
        with open(ledger.runs_path(self.base), "a", encoding="utf-8") as handle:
            handle.write("{not json\n")
        code, out, err = self.main(PLAY)
        self.assertEqual(code, play_runner.EXIT_LEDGER_UNREADABLE)
        self.assertEqual(out, "")
        self.assertIn("ledger", err)


if __name__ == "__main__":
    unittest.main()
