"""Tests for helpers.play_ledger.check_freshness — the play-freshness executor.

The wiring between ledger, git and report. What is pinned here is the behaviour a
human sees: silence when clean, the sentinel refusing to answer, and an exit
status that distinguishes "nothing to say" from "I could not tell you".
"""

from __future__ import annotations

import contextlib
import io
import os
import subprocess
import sys
import tempfile
import unittest
from typing import Any
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

    def test_it_clears_ONLY_the_sentinel_and_leaves_the_records_alone(self) -> None:
        """The one operation in this plan that deletes host state on an operator's
        instruction, so what it must NOT touch is worth pinning.

        A mutation that also unlinked runs.jsonl passed the whole suite — the records
        are the thing clearing is supposed to preserve, and nothing noticed.
        """
        with tempfile.TemporaryDirectory() as base:
            os.makedirs(base, exist_ok=True)
            with open(ledger.runs_path(base), "w", encoding="utf-8") as handle:
                handle.write('{"schema": 1, "kind": "genesis"}\n{"schema": 1}\n')
            store.mark_broken(base, error="ValueError: boom", at=STAMP)
            check_freshness.clear_broken(base=base, stdout=io.StringIO())
            with open(ledger.runs_path(base), encoding="utf-8") as handle:
                self.assertEqual(len(handle.read().splitlines()), 2)

    def test_the_cleared_hole_outlives_the_sentinel(self) -> None:
        """Clearing stops the ledger being KNOWN-broken; it does not make it complete.

        Without the CLEARED marker, `login_report.plays_run_here` went from None
        straight to a PARTIAL set the moment the sentinel was removed — so every pin
        whose row was in the hole stopped being reported, which is the precise
        suppression that function's own docstring calls unacceptable.
        """
        from helpers.host_health import login_report

        with tempfile.TemporaryDirectory() as base:
            os.makedirs(base, exist_ok=True)
            with open(ledger.runs_path(base), "w", encoding="utf-8") as handle:
                handle.write(
                    '{"schema": 1, "play": "playbooks/imports/play-x.yml",'
                    ' "outcome": "ok", "at": "2026-09-15T00:00:00Z"}\n')
            store.mark_broken(base, error="ValueError: boom", at=STAMP)
            self.assertIsNone(login_report.plays_run_here(base))
            check_freshness.clear_broken(base=base, stdout=io.StringIO())
            self.assertTrue(os.path.exists(ledger.cleared_path(base)))
            self.assertIsNone(
                login_report.plays_run_here(base),
                "a cleared hole still leaves the record set a lower bound")

    def test_a_ledger_that_never_had_a_hole_still_gives_a_real_answer(self) -> None:
        """The discrimination control for the case above. If `plays_run_here` answered
        None unconditionally it would pass that test while making the ledger useless,
        so prove an untroubled ledger is still read."""
        from helpers.host_health import login_report

        with tempfile.TemporaryDirectory() as base:
            os.makedirs(base, exist_ok=True)
            with open(ledger.runs_path(base), "w", encoding="utf-8") as handle:
                handle.write(
                    '{"schema": 1, "play": "playbooks/imports/play-x.yml",'
                    ' "outcome": "ok", "at": "2026-09-15T00:00:00Z"}\n')
            self.assertEqual(
                login_report.plays_run_here(base), {"playbooks/imports/play-x.yml"})

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
        back is no reason to leave the ledger refusing for ever.

        The mock fails ONLY the sentinel read. It used to replace `builtins.open`
        wholesale, which also broke the CLEARED marker write — and that write must NOT
        be swallowed: if the clearing cannot be recorded, the sentinel has to stay. An
        over-broad mock was asserting the opposite of the intended contract.
        """
        real_open = open
        sentinel_name = os.path.basename(ledger.sentinel_path(""))

        def only_the_sentinel_fails(path: Any, *args: Any, **kwargs: Any) -> Any:
            if str(path).endswith(sentinel_name):
                raise OSError("denied")
            return real_open(path, *args, **kwargs)

        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(base, error="ValueError: boom", at=STAMP)
            out = io.StringIO()
            with mock.patch("builtins.open", side_effect=only_the_sentinel_fails):
                rc = check_freshness.clear_broken(base=base, stdout=out)
            self.assertEqual(rc, check_freshness.EXIT_OK)
            self.assertFalse(os.path.exists(ledger.sentinel_path(base)))
            self.assertIn("could not be read", out.getvalue())

    def test_main_actually_wires_the_flag_to_the_clearing(self) -> None:
        """Drives `main`, not `clear_broken`.

        Every other case here calls `clear_broken` directly, so a flag-name typo, an
        inverted branch or a dropped `if` in `main` left them all green — which is the
        same tested-function-with-an-untested-caller shape as the defect this whole
        class exists for: `store.clear_broken` was tested and had no caller at all.
        """
        with tempfile.TemporaryDirectory() as home:
            environment = {"XDG_STATE_HOME": os.path.join(home, "state")}
            with mock.patch.dict(os.environ, environment, clear=False):
                base = ledger.ledger_dir(os.environ, home)
                os.makedirs(base, exist_ok=True)
                store.mark_broken(base, error="ValueError: boom", at=STAMP)
                out = io.StringIO()
                with contextlib.redirect_stdout(out):
                    rc = check_freshness.main(["--clear-broken"])
            self.assertEqual(rc, check_freshness.EXIT_OK)
            self.assertFalse(os.path.exists(ledger.sentinel_path(base)))
            self.assertIn("cleared the recorded hole", out.getvalue())

    def test_main_without_the_flag_does_not_clear(self) -> None:
        """The discrimination control: proves the case above passes because the FLAG
        was honoured, not because `main` clears unconditionally."""
        with tempfile.TemporaryDirectory() as home:
            environment = {"XDG_STATE_HOME": os.path.join(home, "state")}
            with mock.patch.dict(os.environ, environment, clear=False):
                base = ledger.ledger_dir(os.environ, home)
                os.makedirs(base, exist_ok=True)
                store.mark_broken(base, error="ValueError: boom", at=STAMP)
                with contextlib.redirect_stdout(io.StringIO()), \
                        contextlib.redirect_stderr(io.StringIO()):
                    check_freshness.main(["--repo-root", base])
            self.assertTrue(os.path.exists(ledger.sentinel_path(base)))

    def test_the_run_path_is_untouched_by_the_flag_being_available(self) -> None:
        """Clearing is deliberate and explicit: a normal run must never do it, or the
        sentinel would stop meaning anything."""
        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(base, error="ValueError: boom", at=STAMP)
            out, err = io.StringIO(), io.StringIO()
            rc = check_freshness.run(base=base, repo_root=base, stdout=out, stderr=err)
            self.assertEqual(rc, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertTrue(os.path.exists(ledger.sentinel_path(base)))


# Must stay LAST in the file. `unittest.main()` here collects only what is defined
# ABOVE it, so a class appended after this block is silently dropped on direct
# execution — `python3 tests/.../test_check_freshness.py` reported 17 tests and `OK`
# while the module path reported 23. A test suite that under-collects and says OK is
# the exact failure shape this plan keeps finding elsewhere.
if __name__ == "__main__":
    unittest.main()
