"""Tests for helpers.play_ledger.check_freshness — the play-freshness executor.

The wiring between ledger, the shared judgement (`changed_plays.judge`) and the report.
What is pinned here is the behaviour a human sees: silence when clean, the sentinel
refusing to answer, an exit status that distinguishes "nothing to say" from "I could not
tell you", and (issue #110) the same plays `run.bash --changed` runs. Which plays have to
run again is tested where it is decided, in test_changed_plays.py.
"""

from __future__ import annotations

import contextlib
import hashlib
import io
import os
import subprocess
import sys
import tempfile
import unittest
from typing import Any
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import changed_plays, check_freshness, fetch_clock, ledger, repo, store

FORTY_HEX = "e" * 40
SIXTY_FOUR_HEX = "1" * 64
STAMP = "2026-09-14T09:00:00Z"

A = "playbooks/imports/play-a.yml"
B = "playbooks/imports/play-b.yml"
OLD = "playbooks/imports/play-old.yml"

#: The folded ledgers the fake judge of the last `_run` was handed.
ASKED: list[dict] = []


def _seed(base: str, plays: list[str]) -> None:
    store.ensure_ledger(base, commit=FORTY_HEX, at=STAMP)
    for play in plays:
        store.append_record(base, ledger.build_record(
            play=play, name=play, commit=FORTY_HEX, dirty=False,
            play_sha256=SIXTY_FOUR_HEX, outcome="ok", changed=0,
            started=STAMP, finished=STAMP,
        ))


def _judgement(states: dict[str, str] | None = None, *, gone: tuple[str, ...] = (),
               unresolved: dict[str, list[str]] | None = None,
               changed: dict[str, list[str]] | None = None, dirty: frozenset[str] = frozenset(),
               took_over: dict[str, list[str]] | None = None,
               not_judged: tuple[str, ...] = ()) -> changed_plays.Judgement:
    states = states or {}
    return changed_plays.Judgement(
        order=tuple(states) + gone, states=states, gone=gone, unresolved=unresolved or {},
        changed_inputs=changed or {}, dirty=dirty, took_over=took_over or {},
        not_judged=not_judged,
    )


def _run(base: str, judgement: changed_plays.Judgement | Exception | None = None,
         **overrides: Any) -> tuple[int, str, str]:
    """Run the check with no fetch and a judge that answers `judgement` (or raises it)."""
    ASKED.clear()

    def judge(latest: dict, root: str) -> changed_plays.Judgement:
        ASKED.append(latest)
        if isinstance(judgement, Exception):
            raise judgement
        return judgement if judgement is not None else _judgement()

    arguments: dict[str, Any] = {"fetch": lambda root: None, "judge": judge}
    arguments.update(overrides)
    out, err = io.StringIO(), io.StringIO()
    code = check_freshness.run(base=base, repo_root="/repo", stdout=out, stderr=err, **arguments)
    return code, out.getvalue(), err.getvalue()


class TestCleanRun(unittest.TestCase):
    def test_a_current_play_says_nothing_on_stdout(self) -> None:
        """Silent when clean. A health check that always speaks gets muted, and then
        it is not a health check."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            code, out, _ = _run(base, _judgement({A: changed_plays.STATE_CURRENT}))
            self.assertEqual((code, out), (check_freshness.EXIT_OK, ""))

    def test_an_unresolved_play_is_not_a_fault(self) -> None:
        """run.bash --changed does not run it either; the coverage sentence names it."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            code, out, _ = _run(base, _judgement({A: changed_plays.STATE_UNRESOLVED},
                                                 unresolved={A: ["somewhere"]}))
            self.assertEqual((code, out), (check_freshness.EXIT_OK, ""))

    def test_an_empty_ledger_is_clean_and_judges_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            store.ensure_ledger(base, commit=FORTY_HEX, at=STAMP)
            code, _, _ = _run(base)
            self.assertEqual(code, check_freshness.EXIT_OK)
            self.assertEqual(ASKED, [])

    def test_an_absent_ledger_is_clean_not_an_error(self) -> None:
        """Nothing has been run here since it would have been created. That is a state
        to handle, not a failure."""
        with tempfile.TemporaryDirectory() as outer:
            code, _, _ = _run(os.path.join(outer, "never-made"))
            self.assertEqual(code, check_freshness.EXIT_OK)


class TestFindings(unittest.TestCase):
    def test_a_stale_play_is_named_with_the_paths_that_changed(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            code, out, _ = _run(base, _judgement({A: changed_plays.STATE_STALE},
                                                 changed={A: ["files/home/a.conf"]}))
            self.assertEqual(code, check_freshness.EXIT_FINDINGS)
            self.assertEqual(out, f"{A} — changed since it was run here\n    changed: files/home/a.conf\n")

    def test_many_changed_paths_are_counted_past_the_first_few(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            paths = [f"files/{n}.conf" for n in range(5)]
            _, out, _ = _run(base, _judgement({A: changed_plays.STATE_STALE}, changed={A: paths}))
            self.assertIn("changed: files/0.conf, files/1.conf, files/2.conf and 2 more\n", out)

    def test_a_dirty_run_says_so(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            _, out, _ = _run(base, _judgement({A: changed_plays.STATE_STALE}, dirty=frozenset({A})))
            self.assertEqual(out, f"{A} — changed since it was run here\n"
                                  "    it last ran from a checkout with uncommitted changes\n")

    def test_a_failed_last_run_is_a_finding_of_its_own_kind(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            code, out, _ = _run(base, _judgement({A: changed_plays.STATE_FAILED}))
            self.assertEqual(code, check_freshness.EXIT_FINDINGS)
            self.assertEqual(out, f"{A} — its last run here did not succeed\n")

    def test_a_successor_says_which_removed_play_it_takes_over(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [OLD])
            _, out, _ = _run(base, _judgement({A: changed_plays.STATE_STALE}, took_over={A: [OLD]}))
            self.assertEqual(out, f"{A} — changed since it was run here\n"
                                  f"    it takes over {OLD}, which the checkout removed\n")

    def test_a_gone_play_is_named(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [OLD])
            code, out, _ = _run(base, _judgement(gone=(OLD,)))
            self.assertEqual((code, out), (check_freshness.EXIT_FINDINGS, f"{OLD} — no longer in the checkout\n"))

    def test_a_current_play_is_not_named_alongside_a_stale_one(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A, B])
            _, out, _ = _run(base, _judgement({A: changed_plays.STATE_CURRENT, B: changed_plays.STATE_STALE},
                                              changed={B: [B]}))
            self.assertNotIn(A, out)
            self.assertIn(B, out)

    def test_the_judge_is_handed_the_folded_ledger(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A, B])
            _run(base)
            self.assertEqual(sorted(ASKED[0]), [A, B])


class TestBrokenLedger(unittest.TestCase):
    def test_the_sentinel_refuses_to_answer_and_exits_distinctly(self) -> None:
        """Not EXIT_FINDINGS: "I could not tell you" and "here is what is stale" are
        different outcomes and a caller must be able to tell them apart."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            store.mark_broken(base, error="disk full", at=STAMP)
            code, out, err = _run(base, _judgement({A: changed_plays.STATE_STALE}))
            self.assertEqual(code, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertIn("disk full", err)
            self.assertNotIn(A, out)

    def test_nothing_is_fetched_or_judged_while_broken(self) -> None:
        """Answering nothing means doing nothing — a fetch at login costs the user
        time for a result that will be discarded."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            store.mark_broken(base, error="disk full", at=STAMP)
            fetched: list[str] = []
            _run(base, fetch=fetched.append)
            self.assertEqual(fetched, [])
            self.assertEqual(ASKED, [])


class TestFailures(unittest.TestCase):
    def test_a_corrupt_ledger_line_fails_loudly(self) -> None:
        """fold_latest raises on a corrupt line rather than skipping it, and this must
        not soften that into a clean report."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            with open(ledger.runs_path(base), "a", encoding="utf-8") as handle:
                handle.write("{not json\n")
            code, _, _ = _run(base)
            self.assertEqual(code, check_freshness.EXIT_UNTRUSTWORTHY)

    def test_no_answer_from_the_judgement_is_untrustworthy_not_clean(self) -> None:
        """An unjudgeable play must never read as 'nothing changed'."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            code, out, err = _run(base, changed_plays.NoAnswer(
                f"cannot judge {A}, which last ran from x: bad object",
                f"run it by name instead: ./run.bash {A}"))
            self.assertEqual((code, out), (check_freshness.EXIT_UNTRUSTWORTHY, ""))
            self.assertEqual(err, f"play-freshness: cannot judge {A}, which last ran from x: bad object\n"
                                  f"play-freshness: run it by name instead: ./run.bash {A}\n")

    def test_a_fetch_failure_alone_is_NOT_untrustworthy(self) -> None:
        """Settled in DESIGN-host-health.md §8. Offline at login is ordinary — a train, a
        hotel — and reporting on it every time is how the whole surface gets muted. What
        is reported instead is how long it has been, which is a fact about this host
        rather than about the network. Here the fetch has only just failed, so: silence."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            fetch_clock.record_success(base, at=repo.utc_now())

            def boom(root: str) -> None:
                raise subprocess.CalledProcessError(128, ["git", "fetch"])

            code, out, _ = _run(base, fetch=boom)
            self.assertEqual((code, out), (check_freshness.EXIT_OK, ""))

    def test_a_fetch_failure_after_a_LONG_gap_is_a_finding(self) -> None:
        """The other half. A host that has quietly stopped being checked at all is
        this plan's subject in its purest form."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            long_ago = fetch_clock.shift(
                repo.utc_now(), days=-(fetch_clock.STALE_AFTER_DAYS + 30))
            fetch_clock.record_success(base, at=long_ago)

            def boom(root: str) -> None:
                raise subprocess.CalledProcessError(128, ["git", "fetch"])

            code, out, _ = _run(base, fetch=boom)
            self.assertEqual(code, check_freshness.EXIT_FINDINGS)
            self.assertIn("has not reached the remote", out)

    def test_a_fetch_failure_with_NO_record_at_all_is_a_finding(self) -> None:
        """Nothing has ever been checked here, which is not the same as offline
        today and must not read the same."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])

            def boom(root: str) -> None:
                raise subprocess.CalledProcessError(128, ["git", "fetch"])

            code, out, _ = _run(base, fetch=boom)
            self.assertEqual(code, check_freshness.EXIT_FINDINGS)
            self.assertIn("never", out)

    def test_a_STAMP_WRITE_failure_is_not_blamed_on_the_fetch(self) -> None:
        """A full disk must not produce "git fetch failed" on stderr and "has never
        successfully reached the remote" on stdout about a fetch that had just succeeded.

        The failure is injected by putting a DIRECTORY where the stamp file belongs —
        a real `IsADirectoryError` from the real code path. Permissions would not do
        it here: these tests run as root, which bypasses the mode check.
        """
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            os.mkdir(os.path.join(base, fetch_clock.STAMP_NAME))
            code, out, err = _run(base)
            self.assertEqual((code, out), (check_freshness.EXIT_OK, ""))
            self.assertNotIn("git fetch failed", err)
            self.assertNotIn("never", err)
            self.assertIn("the fetch succeeded", err)

    def test_a_SUCCESSFUL_fetch_stamps_the_clock(self) -> None:
        """Without this the bound never advances and every host eventually reports
        the long-gap finding for ever — a check that cries wolf, permanently."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            self.assertIsNone(fetch_clock.last_success(base))
            _run(base)
            self.assertIsNotNone(fetch_clock.last_success(base))


class TestStreams(unittest.TestCase):
    def test_findings_go_to_stdout_and_diagnostics_to_stderr(self) -> None:
        """stdout is the payload a caller captures; the sentinel warning is not it."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            store.mark_broken(base, error="disk full", at=STAMP)
            _, out, err = _run(base)
            self.assertEqual(out, "")
            self.assertNotEqual(err, "")


class TestJudgedSink(unittest.TestCase):
    """`judged` receives EVERY verdict, current included — the panel's play runner lists
    plays with their state, and a current play is the commonest row in it."""

    def test_every_play_arrives_with_its_state(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A, B])
            judged: list = []
            _run(base, _judgement({A: changed_plays.STATE_CURRENT, B: changed_plays.STATE_FAILED,
                                   OLD: changed_plays.STATE_UNRESOLVED}, gone=("playbooks/imports/x.yml",),
                                  unresolved={OLD: ["y"]}), judged=judged)
            self.assertEqual(
                [(verdict.play, verdict.state) for verdict in judged],
                [(A, "fresh"), (B, "failed"), (OLD, "unresolved"), ("playbooks/imports/x.yml", "gone")],
            )

    def test_a_broken_ledger_judges_nothing_into_the_sink(self) -> None:
        """No per-play state is known while the sentinel stands, so none is offered."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            store.mark_broken(base, error="a hole", at=STAMP)
            judged: list = []
            _run(base, judged=judged)
            self.assertEqual(judged, [])

    def test_no_answer_judges_nothing_into_the_sink(self) -> None:
        """A partial list is not offered as the whole: the run answered untrustworthy."""
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            judged: list = []
            code, _, _ = _run(base, changed_plays.NoAnswer("bad object"), judged=judged)
            self.assertEqual(code, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertEqual(judged, [])


class TestCoverage(unittest.TestCase):
    """What the section judged, and what it leaves out (issue #110)."""

    def _sentence(self, judgement: changed_plays.Judgement | Exception) -> list[str]:
        with tempfile.TemporaryDirectory() as base:
            _seed(base, [A])
            coverage: list[str] = []
            _run(base, judgement, coverage=coverage)
            return coverage

    def test_it_states_the_count_and_the_rule_it_judged_by(self) -> None:
        coverage = self._sentence(_judgement({A: changed_plays.STATE_CURRENT}, gone=(OLD,)))
        self.assertEqual(len(coverage), 1)
        self.assertTrue(coverage[0].startswith("2 plays run here under playbooks/imports/, judged as "
                                               "run.bash --changed judges them"), coverage[0])
        self.assertNotIn("not judged", coverage[0])

    def test_it_names_what_it_cannot_see_and_what_it_does_not_judge(self) -> None:
        dev = "playbooks/dev/play-tool.yml"
        coverage = self._sentence(_judgement({A: changed_plays.STATE_UNRESOLVED},
                                             unresolved={A: ["x"]}, not_judged=(dev,)))
        self.assertIn(f"1 of them hold a reference that cannot be followed, so a change there is not seen: {A}",
                      coverage[0])
        self.assertIn(f"1 other ledgered plays are not host plays and are not judged: {dev}", coverage[0])

    def test_no_answer_states_no_coverage(self) -> None:
        self.assertEqual(self._sentence(changed_plays.NoAnswer("x")), [])


class TestDefaultWiring(unittest.TestCase):
    def test_run_defaults_to_the_real_fetch_and_the_shared_judgement(self) -> None:
        """The injected seams are for tests; the executor must work with none supplied."""
        with tempfile.TemporaryDirectory() as base, \
             mock.patch("helpers.play_ledger.git_history.fetch") as fetch, \
             mock.patch("helpers.play_ledger.changed_plays.judge", return_value=_judgement()) as judge:
            _seed(base, [A])
            code = check_freshness.run(base=base, repo_root="/repo",
                                       stdout=io.StringIO(), stderr=io.StringIO())
            self.assertEqual(code, 0)
            fetch.assert_called_once_with("/repo")
            self.assertEqual(judge.call_args.kwargs, {"repo_root": "/repo"})


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
            # DATED. `store.clear_broken` takes `at` and this caller passed nothing, so
            # the marker was a bare newline: it said a hole had been cleared and not
            # when, and "the records are a lower bound from some unknown point" is not
            # a fact anyone can act on. `at` having no production caller is the same
            # shape as `clear_broken` itself having had none.
            with open(ledger.cleared_path(base), encoding="utf-8") as handle:
                marker = handle.read().strip()
            self.assertTrue(marker, "the CLEARED marker carries no timestamp")
            self.assertRegex(marker, r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}")

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

    def test_only_if_reason_clears_a_sentinel_with_that_reason(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(
                base, error="ValueError: play '/x.yml' is outside the repo root '/r'", at=STAMP)
            rc = check_freshness.clear_broken(
                base=base, stdout=io.StringIO(), stderr=io.StringIO(),
                only_if_reason="is outside the repo root")
            self.assertEqual(rc, check_freshness.EXIT_OK)
            self.assertFalse(os.path.exists(ledger.sentinel_path(base)))

    def test_only_if_reason_refuses_a_sentinel_with_any_other_reason(self) -> None:
        """An unattended clear (meta-deploy) is safe only for the cause it was written
        for. A hole with another reason has a cause nobody has fixed, so it stays."""
        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(
                base, error="OSError: cannot write runs.jsonl: Permission denied", at=STAMP)
            err = io.StringIO()
            rc = check_freshness.clear_broken(
                base=base, stdout=io.StringIO(), stderr=err,
                only_if_reason="is outside the repo root")
            self.assertEqual(rc, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertTrue(os.path.exists(ledger.sentinel_path(base)))
            self.assertFalse(os.path.exists(ledger.cleared_path(base)))
            self.assertIn("[FATAL]", err.getvalue())
            self.assertIn("Permission denied", err.getvalue())

    def test_only_if_reason_refuses_when_the_reason_cannot_be_read(self) -> None:
        """Unlike a plain clear, an unreadable reason cannot be shown to match."""
        real_open = open
        sentinel_name = os.path.basename(ledger.sentinel_path(""))

        def only_the_sentinel_fails(path: Any, *args: Any, **kwargs: Any) -> Any:
            if str(path).endswith(sentinel_name):
                raise OSError("denied")
            return real_open(path, *args, **kwargs)

        with tempfile.TemporaryDirectory() as base:
            store.mark_broken(base, error="ValueError: is outside the repo root", at=STAMP)
            err = io.StringIO()
            with mock.patch("builtins.open", side_effect=only_the_sentinel_fails):
                rc = check_freshness.clear_broken(
                    base=base, stdout=io.StringIO(), stderr=err,
                    only_if_reason="is outside the repo root")
            self.assertEqual(rc, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertTrue(os.path.exists(ledger.sentinel_path(base)))
            self.assertIn("[FATAL]", err.getvalue())

    def test_only_if_reason_with_no_sentinel_is_a_clean_no_op(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            out = io.StringIO()
            rc = check_freshness.clear_broken(
                base=base, stdout=out, stderr=io.StringIO(),
                only_if_reason="is outside the repo root")
            self.assertEqual(rc, check_freshness.EXIT_OK)
            self.assertIn("no recorded hole", out.getvalue())

    def test_main_wires_only_if_reason_to_the_guard(self) -> None:
        with tempfile.TemporaryDirectory() as home:
            environment = {"XDG_STATE_HOME": os.path.join(home, "state")}
            with mock.patch.dict(os.environ, environment, clear=False):
                base = ledger.ledger_dir(os.environ, home)
                os.makedirs(base, exist_ok=True)
                store.mark_broken(base, error="ValueError: boom", at=STAMP)
                with contextlib.redirect_stdout(io.StringIO()), \
                        contextlib.redirect_stderr(io.StringIO()):
                    rc = check_freshness.main(
                        ["--clear-broken", "--only-if-reason", "is outside the repo root"])
            self.assertEqual(rc, check_freshness.EXIT_UNTRUSTWORTHY)
            self.assertTrue(os.path.exists(ledger.sentinel_path(base)))

    def test_only_if_reason_without_clear_broken_is_a_usage_error(self) -> None:
        with contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as raised:
                check_freshness.main(["--only-if-reason", "x"])
        self.assertNotEqual(raised.exception.code, 0)

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


def _isolated_git_env() -> dict[str, str]:
    """The environment helpers/CLAUDE.md requires of a test that runs git."""
    env = {key: value for key, value in os.environ.items()
           if key not in ("GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS")}
    env["GIT_CONFIG_GLOBAL"] = os.devnull
    env["GIT_CONFIG_NOSYSTEM"] = "1"
    return env


class TestAgreesWithRunBashChanged(unittest.TestCase):
    """Issue #110: on one host the report listed 10 changed plays and `run.bash --changed` 25.

    The report and the runner must name the same plays, on a real repository and the real
    ledger, for each way a play has to run again: a file it deploys changed (its own file
    untouched), its last run failed, it ran from a dirty checkout, its own file changed in the
    working tree and not yet committed. One play is current and must be in neither.
    """

    COPY_TASK = (
        "- hosts: desktop\n  tasks:\n    - name: Copy\n      ansible.builtin.copy:\n"
        '        src: "{{{{ root_dir }}}}/files/home/{name}.conf"\n        dest: /tmp/{name}.conf\n'
    )
    NAMES = ("deployed", "failed", "dirty", "uncommitted", "current")

    def _git(self, root: str, *args: str) -> str:
        return subprocess.run(["git", "-C", root, *args], check=True, capture_output=True,
                              text=True, env=_isolated_git_env()).stdout

    def _write(self, root: str, path: str, text: str) -> None:
        full = os.path.join(root, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w", encoding="utf-8") as handle:
            handle.write(text)

    def test_the_report_names_exactly_the_plays_run_bash_changed_runs(self) -> None:
        with tempfile.TemporaryDirectory() as root, tempfile.TemporaryDirectory() as state:
            self._git(root, "init", "-q")
            self._git(root, "config", "user.email", "test@example.com")
            self._git(root, "config", "user.name", "test")
            self._git(root, "config", "commit.gpgsign", "false")
            self._write(root, changed_plays.MAIN_PLAYBOOK, "".join(
                f"- import_playbook: imports/play-{name}.yml\n" for name in self.NAMES))
            for name in self.NAMES:
                self._write(root, f"playbooks/imports/play-{name}.yml", self.COPY_TASK.format(name=name))
                self._write(root, f"files/home/{name}.conf", "v1\n")
            self._git(root, "add", ".")
            self._git(root, "commit", "-q", "-m", "first")
            first = self._git(root, "rev-parse", "HEAD").strip()

            base = os.path.join(state, "ledger")
            store.ensure_ledger(base, commit=first, at=STAMP)
            for name in self.NAMES:
                play = f"playbooks/imports/play-{name}.yml"
                with open(os.path.join(root, play), "rb") as handle:
                    digest = hashlib.sha256(handle.read()).hexdigest()
                store.append_record(base, ledger.build_record(
                    play=play, name=name, commit=first, dirty=name == "dirty",
                    play_sha256=digest, outcome="failed" if name == "failed" else "ok",
                    changed=0, started=STAMP, finished=STAMP,
                ))
            self._write(root, "files/home/deployed.conf", "v2\n")
            self._git(root, "commit", "-q", "-am", "the deployed file changes")
            with open(os.path.join(root, "playbooks/imports/play-uncommitted.yml"), "a",
                      encoding="utf-8") as handle:
                handle.write("# an edit not yet committed\n")

            with mock.patch.dict(os.environ, _isolated_git_env(), clear=True):
                runner_out = io.StringIO()
                self.assertEqual(changed_plays.run(base=base, repo_root=root, stdout=runner_out,
                                                   stderr=io.StringIO()), changed_plays.EXIT_OK)
                report_out = io.StringIO()
                self.assertEqual(
                    check_freshness.run(base=base, repo_root=root, stdout=report_out,
                                        stderr=io.StringIO(), fetch=lambda r: None),
                    check_freshness.EXIT_FINDINGS)

            runner = [line.removeprefix("RUN ") for line in runner_out.getvalue().splitlines()]
            report = [line.split(" — ")[0] for line in report_out.getvalue().splitlines()
                      if not line.startswith(" ")]
            want = [f"playbooks/imports/play-{name}.yml" for name in self.NAMES if name != "current"]
            self.assertEqual(runner, want)
            self.assertEqual(report, runner)


# Must stay LAST in the file. `unittest.main()` here collects only what is defined
# ABOVE it, so a class appended after this block is silently dropped on direct
# execution — `python3 tests/.../test_check_freshness.py` reported 17 tests and `OK`
# while the module path reported 23. A test suite that under-collects and says OK is
# the exact failure shape this plan keeps finding elsewhere.
if __name__ == "__main__":
    unittest.main()
