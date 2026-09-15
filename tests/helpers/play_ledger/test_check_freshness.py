"""Tests for helpers.play_ledger.check_freshness — the play-freshness executor.

The wiring between ledger, git and report. What is pinned here is the behaviour a
human sees: silence when clean, the sentinel refusing to answer, and an exit
status that distinguishes "nothing to say" from "I could not tell you".
"""

from __future__ import annotations

import io
import os
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import check_freshness, fetch_clock, ledger, repo, store

FORTY_HEX = "e" * 40
SIXTY_FOUR_HEX = "1" * 64
OTHER_SHA = "2" * 64
STAMP = "2026-09-14T09:00:00Z"


def _seed(base: str, plays: list[str], *, play_sha256: str = SIXTY_FOUR_HEX) -> None:
    store.ensure_ledger(base, commit=FORTY_HEX, at=STAMP)
    for play in plays:
        store.append_record(base, ledger.build_record(
            play=play, name=play, commit=FORTY_HEX, dirty=False,
            play_sha256=play_sha256, outcome="ok", changed=0,
            started=STAMP, finished=STAMP,
        ))


class TestCleanRun(unittest.TestCase):
    def test_a_fresh_ledger_says_nothing_on_stdout(self) -> None:
        """Silent when clean. A health check that always speaks gets muted, and then
        it is not a health check."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            out, err = io.StringIO(), io.StringIO()
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=out, stderr=err,
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(out.getvalue(), "")
            self.assertEqual(code, 0)

    def test_an_empty_ledger_is_clean_and_asks_git_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            store.ensure_ledger(base, commit=FORTY_HEX, at=STAMP)
            asked = []
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=io.StringIO(), stderr=io.StringIO(),
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: asked.append(play) or [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(asked, [])
            self.assertEqual(code, 0)

    def test_an_absent_ledger_is_clean_not_an_error(self) -> None:
        """Nothing has been run here since it would have been created. That is a state
        to handle, not a failure."""
        with tempfile.TemporaryDirectory() as outer:
            code = check_freshness.run(
                base=os.path.join(outer, "never-made"), repo_root="/repo",
                stdout=io.StringIO(), stderr=io.StringIO(),
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(code, 0)


class TestStaleRun(unittest.TestCase):
    def test_a_stale_play_is_named_on_stdout_with_its_commits(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            out = io.StringIO()
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=out, stderr=io.StringIO(),
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [("abc1234", "rework the play")],
                play_sha256_at_head=lambda root, play: OTHER_SHA,
            )
            printed = out.getvalue()
            self.assertIn("playbooks/a.yml", printed)
            self.assertIn("abc1234", printed)
            self.assertIn("rework the play", printed)
            self.assertEqual(code, check_freshness.EXIT_FINDINGS)

    def test_a_fresh_play_is_not_named_alongside_a_stale_one(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/fresh.yml", "playbooks/stale.yml"])
            out = io.StringIO()
            check_freshness.run(
                base=base, repo_root="/repo", stdout=out, stderr=io.StringIO(),
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: (
                    [("abc1234", "x")] if "stale" in play else []
                ),
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertNotIn("playbooks/fresh.yml", out.getvalue())


class TestBrokenLedger(unittest.TestCase):
    def test_the_sentinel_refuses_to_answer_and_exits_distinctly(self) -> None:
        """Not EXIT_FINDINGS: "I could not tell you" and "here is what is stale" are
        different outcomes and a caller must be able to tell them apart."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            store.mark_broken(base, error="disk full", at=STAMP)
            err = io.StringIO()
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=io.StringIO(), stderr=err,
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [("a", "b")],
                play_sha256_at_head=lambda root, play: OTHER_SHA,
            )
            self.assertEqual(code, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertIn("disk full", err.getvalue())

    def test_no_per_play_verdict_is_printed_while_broken(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            store.mark_broken(base, error="disk full", at=STAMP)
            out = io.StringIO()
            check_freshness.run(
                base=base, repo_root="/repo", stdout=out, stderr=io.StringIO(),
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [("a", "b")],
                play_sha256_at_head=lambda root, play: OTHER_SHA,
            )
            self.assertNotIn("playbooks/a.yml", out.getvalue())

    def test_git_is_never_consulted_while_broken(self) -> None:
        """Answering nothing means doing nothing — a fetch at login costs the user
        time for a result that will be discarded."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            store.mark_broken(base, error="disk full", at=STAMP)
            fetched = []
            check_freshness.run(
                base=base, repo_root="/repo", stdout=io.StringIO(), stderr=io.StringIO(),
                fetch=lambda root: fetched.append(root),
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(fetched, [])


class TestFailures(unittest.TestCase):
    def test_a_corrupt_ledger_line_fails_loudly(self) -> None:
        """fold_latest raises on a corrupt line rather than skipping it, and this must
        not soften that into a clean report."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            with open(ledger.runs_path(base), "a", encoding="utf-8") as handle:
                handle.write("{not json\n")
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=io.StringIO(), stderr=io.StringIO(),
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(code, check_freshness.EXIT_UNTRUSTWORTHY)

    def test_a_git_failure_is_untrustworthy_not_clean(self) -> None:
        """An unresolvable ledgered commit must never read as 'nothing changed'."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            def boom(root, commit, play):
                raise subprocess.CalledProcessError(128, ["git"])
            err = io.StringIO()
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=io.StringIO(), stderr=err,
                fetch=lambda root: None,
                changes_since=boom,
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(code, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertIn("playbooks/a.yml", err.getvalue())

    def test_a_fetch_failure_alone_is_NOT_untrustworthy(self) -> None:
        """Settled in DESIGN-host-health.md §8, and this test previously asserted the
        opposite. Offline at login is ordinary — a train, a hotel — and reporting on
        it every time is how the whole surface gets muted. What is reported instead
        is how long it has been, which is a fact about this host rather than about
        the network. Here the fetch has only just failed, so: silence."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            fetch_clock.record_success(base, at=repo.utc_now())

            def boom(root):
                raise subprocess.CalledProcessError(128, ["git", "fetch"])

            stdout = io.StringIO()
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=stdout, stderr=io.StringIO(),
                fetch=boom,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(code, check_freshness.EXIT_OK)
            self.assertEqual(stdout.getvalue(), "")

    def test_a_fetch_failure_after_a_LONG_gap_is_a_finding(self) -> None:
        """The other half. A host that has quietly stopped being checked at all is
        this plan's subject in its purest form."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            long_ago = fetch_clock.shift(
                repo.utc_now(), days=-(fetch_clock.STALE_AFTER_DAYS + 30))
            fetch_clock.record_success(base, at=long_ago)

            def boom(root):
                raise subprocess.CalledProcessError(128, ["git", "fetch"])

            stdout = io.StringIO()
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=stdout, stderr=io.StringIO(),
                fetch=boom,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(code, check_freshness.EXIT_FINDINGS)
            self.assertIn("has not reached the remote", stdout.getvalue())

    def test_a_fetch_failure_with_NO_record_at_all_is_a_finding(self) -> None:
        """Nothing has ever been checked here, which is not the same as offline
        today and must not read the same."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])

            def boom(root):
                raise subprocess.CalledProcessError(128, ["git", "fetch"])

            stdout = io.StringIO()
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=stdout, stderr=io.StringIO(),
                fetch=boom,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(code, check_freshness.EXIT_FINDINGS)
            self.assertIn("never", stdout.getvalue())

    def test_a_STAMP_WRITE_failure_is_not_blamed_on_the_fetch(self) -> None:
        """The stamp write used to live inside the fetch's own `try`, so a full disk
        produced two confident statements about a fetch that had just succeeded:
        "git fetch failed" on stderr, and "has never successfully reached the remote"
        on stdout. Neither was true.

        The failure is injected by putting a DIRECTORY where the stamp file belongs —
        a real `IsADirectoryError` from the real code path. Permissions would not do
        it here: these tests run as root, which bypasses the mode check.
        """
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            os.mkdir(os.path.join(base, fetch_clock.STAMP_NAME))

            stdout, stderr = io.StringIO(), io.StringIO()
            code = check_freshness.run(
                base=base, repo_root="/repo", stdout=stdout, stderr=stderr,
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(code, check_freshness.EXIT_OK)
            self.assertEqual(stdout.getvalue(), "")
            self.assertNotIn("git fetch failed", stderr.getvalue())
            self.assertNotIn("never", stderr.getvalue())
            self.assertIn("the fetch succeeded", stderr.getvalue())

    def test_a_SUCCESSFUL_fetch_stamps_the_clock(self) -> None:
        """Without this the bound never advances and every host eventually reports
        the long-gap finding for ever — a check that cries wolf, permanently."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            self.assertIsNone(fetch_clock.last_success(base))
            check_freshness.run(
                base=base, repo_root="/repo", stdout=io.StringIO(), stderr=io.StringIO(),
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertIsNotNone(fetch_clock.last_success(base))


class TestStreams(unittest.TestCase):
    def test_findings_go_to_stdout_and_diagnostics_to_stderr(self) -> None:
        """stdout is the payload a caller captures; the sentinel warning is not it."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, ["playbooks/a.yml"])
            store.mark_broken(base, error="disk full", at=STAMP)
            out, err = io.StringIO(), io.StringIO()
            check_freshness.run(
                base=base, repo_root="/repo", stdout=out, stderr=err,
                fetch=lambda root: None,
                changes_since=lambda root, commit, play: [],
                play_sha256_at_head=lambda root, play: SIXTY_FOUR_HEX,
            )
            self.assertEqual(out.getvalue(), "")
            self.assertNotEqual(err.getvalue(), "")


class TestDefaultWiring(unittest.TestCase):
    def test_run_defaults_to_the_real_git_functions(self) -> None:
        """The injected seams are for tests; the executor must work with none supplied."""
        with tempfile.TemporaryDirectory() as base, \
             mock.patch("helpers.play_ledger.git_history.fetch") as fetch, \
             mock.patch("helpers.play_ledger.git_history.changes_since", return_value=[]), \
             mock.patch("helpers.play_ledger.git_history.play_sha256_at_head",
                        return_value=SIXTY_FOUR_HEX):
            _seed(base, ["playbooks/a.yml"])
            code = check_freshness.run(base=base, repo_root="/repo",
                                       stdout=io.StringIO(), stderr=io.StringIO())
            self.assertEqual(code, 0)
            fetch.assert_called_once_with("/repo")


if __name__ == "__main__":
    unittest.main()


class TestClearBroken(unittest.TestCase):
    """The operator's route out of a recorded hole (issue #46).

    `store.clear_broken` existed from the start with NO caller — so a sentinel, once
    written, left every check downstream refusing for ever with nothing to do about
    it. A fail-safe with no reset is a fail-stop.
    """

    def test_clears_the_sentinel_so_recording_can_resume(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(base, error="ValueError: boom", at=STAMP)
            self.assertTrue(os.path.exists(ledger.sentinel_path(base)))
            out = io.StringIO()
            rc = check_freshness.clear_broken(base=base, stdout=out)
            self.assertEqual(rc, check_freshness.EXIT_OK)
            self.assertFalse(os.path.exists(ledger.sentinel_path(base)))

    def test_quotes_the_recorded_reason_back(self) -> None:
        """The reason is the only record of WHY, and clearing destroys it — so it is
        printed on the way out rather than silently unlinked."""
        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(base, error="ValueError: no source position", at=STAMP)
            out = io.StringIO()
            check_freshness.clear_broken(base=base, stdout=out)
            self.assertIn("no source position", out.getvalue())

    def test_says_the_missing_rows_are_not_recovered(self) -> None:
        """THE case that matters. Clearing makes the ledger stop reporting broken; it
        does not make it complete. An operator who reads this as 'fixed' would trust a
        history with a hole in it — which is the failure Plan 00109 exists to prevent."""
        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(base, error="ValueError: boom", at=STAMP)
            out = io.StringIO()
            check_freshness.clear_broken(base=base, stdout=out)
            self.assertIn("NOT recovered", out.getvalue())

    def test_no_sentinel_is_a_clean_no_op_not_an_error(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            out = io.StringIO()
            rc = check_freshness.clear_broken(base=base, stdout=out)
            self.assertEqual(rc, check_freshness.EXIT_OK)
            self.assertIn("no recorded hole", out.getvalue())

    def test_an_unreadable_reason_still_clears_the_sentinel(self) -> None:
        """The operator asked for the hole to be cleared. Failing to quote the reason
        back is no reason to leave the ledger refusing for ever."""
        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(base, error="ValueError: boom", at=STAMP)
            out = io.StringIO()
            with mock.patch("builtins.open", side_effect=OSError("denied")):
                rc = check_freshness.clear_broken(base=base, stdout=out)
            self.assertEqual(rc, check_freshness.EXIT_OK)
            self.assertFalse(os.path.exists(ledger.sentinel_path(base)))

    def test_the_run_path_is_untouched_by_the_flag_being_available(self) -> None:
        """Clearing is deliberate and explicit: a normal run must never do it, or the
        sentinel would stop meaning anything."""
        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(base, error="ValueError: boom", at=STAMP)
            out, err = io.StringIO(), io.StringIO()
            rc = check_freshness.run(base=base, repo_root=base, stdout=out, stderr=err)
            self.assertEqual(rc, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertTrue(os.path.exists(ledger.sentinel_path(base)))
