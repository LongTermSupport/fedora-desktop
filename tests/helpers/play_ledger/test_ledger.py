"""Unit tests for helpers/play_ledger/ledger.py — the pure ledger record logic.

Run from the repo root:

    python3 -m unittest tests.helpers.play_ledger.test_ledger
"""

from __future__ import annotations

import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.play_ledger import ledger

PLAY = "playbooks/imports/play-gnome-shell-extensions.yml"
COMMIT = "b07092eb1f4d8c3a9e2f07a1b5c6d7e8f9012345"


class TestLedgerDir(unittest.TestCase):
    def test_xdg_state_home_is_honoured(self):
        path = ledger.ledger_dir({"XDG_STATE_HOME": "/somewhere/state"}, "/home/u")
        self.assertEqual(path, "/somewhere/state/fedora-desktop/play-ledger")

    def test_falls_back_to_the_specified_default(self):
        # Host state, not repo state: it must not be committable and must survive
        # a re-clone, which is the whole reason it is not in the working tree.
        path = ledger.ledger_dir({}, "/home/u")
        self.assertEqual(path, "/home/u/.local/state/fedora-desktop/play-ledger")

    def test_an_empty_xdg_state_home_is_not_a_path(self):
        path = ledger.ledger_dir({"XDG_STATE_HOME": ""}, "/home/u")
        self.assertEqual(path, "/home/u/.local/state/fedora-desktop/play-ledger")

    def test_a_relative_xdg_state_home_is_refused(self):
        # The spec says it must be absolute; a relative one would put the ledger
        # somewhere that depends on the cwd of whoever ran the play.
        with self.assertRaises(ValueError):
            ledger.ledger_dir({"XDG_STATE_HOME": "relative/state"}, "/home/u")

    def test_the_file_names_are_derived_from_the_directory(self):
        base = ledger.ledger_dir({}, "/home/u")
        self.assertEqual(ledger.runs_path(base), f"{base}/runs.jsonl")
        self.assertEqual(ledger.sentinel_path(base), f"{base}/BROKEN")


class TestBuildRecord(unittest.TestCase):
    def _record(self, **overrides):
        fields = {
            "play": PLAY,
            "name": "Gnome Shell Extensions",
            "commit": COMMIT,
            "dirty": False,
            "play_sha256": "9f2c" + "0" * 60,
            "outcome": "ok",
            "changed": 3,
            "started": "2026-09-14T10:00:00Z",
            "finished": "2026-09-14T10:04:00Z",
        }
        fields.update(overrides)
        return ledger.build_record(**fields)

    def test_every_declared_field_is_present(self):
        record = self._record()
        self.assertEqual(
            set(record),
            {
                "schema",
                "play",
                "name",
                "commit",
                "dirty",
                "dirty_paths",
                "dirty_paths_truncated",
                "narrowed",
                "play_sha256",
                "outcome",
                "changed",
                "started",
                "finished",
            },
        )

    def test_a_full_run_records_no_narrowing(self):
        self.assertEqual(self._record()["narrowed"], {})

    def test_a_narrowed_run_records_its_options_with_tags_sorted(self):
        record = self._record(narrowed={"tags": ["member", "inbox"], "limit": "localhost"})
        self.assertEqual(record["narrowed"], {"tags": ["inbox", "member"], "limit": "localhost"})

    def test_an_unknown_narrowing_option_is_refused(self):
        with self.assertRaises(ValueError):
            self._record(narrowed={"forks": "5"})

    def test_a_narrowing_option_must_carry_a_value(self):
        for narrowed in ({"tags": []}, {"tags": [""]}, {"limit": ""}, {"start_at_task": None},
                         {"skip_tags": "member"}):
            with self.subTest(narrowed=narrowed), self.assertRaises(ValueError):
                self._record(narrowed=narrowed)

    def test_the_schema_version_is_stamped(self):
        # So a later reader can refuse a shape it does not understand rather than
        # misread it.
        self.assertEqual(self._record()["schema"], ledger.SCHEMA)

    def test_an_unknown_outcome_is_refused(self):
        with self.assertRaises(ValueError):
            self._record(outcome="probably-fine")

    def test_each_known_outcome_is_accepted(self):
        for outcome in ("ok", "failed", "unreachable"):
            self.assertEqual(self._record(outcome=outcome)["outcome"], outcome)

    def test_a_play_path_must_be_repo_relative(self):
        # An absolute path embeds the checkout location, which differs per clone —
        # the ledger would then never join against a play path again.
        with self.assertRaises(ValueError):
            self._record(play="/home/u/repo/playbooks/imports/play-x.yml")

    def test_a_malformed_commit_is_refused(self):
        with self.assertRaises(ValueError):
            self._record(commit="HEAD")

    def test_a_malformed_timestamp_is_refused(self):
        with self.assertRaises(ValueError):
            self._record(started="14th September")

    def test_dirty_must_be_a_real_boolean(self):
        # "false" is truthy; a string here would silently record every run as
        # dirty, which makes play_sha256's whole reason for existing unreadable.
        with self.assertRaises(ValueError):
            self._record(dirty="false")

    def test_a_clean_run_records_no_dirty_paths(self):
        record = self._record()
        self.assertEqual((record["dirty_paths"], record["dirty_paths_truncated"]), ([], False))

    def test_a_dirty_run_records_which_paths_sorted(self):
        record = self._record(dirty=True, dirty_paths=("b", "a"))
        self.assertEqual((record["dirty"], record["dirty_paths"]), (True, ["a", "b"]))

    def test_dirty_must_agree_with_the_paths(self):
        # A dirty run naming no path would clear every play; a clean one naming paths is a lie.
        for fields in ({"dirty": True}, {"dirty_paths": ("a",)}, {"dirty_paths_truncated": True}):
            with self.subTest(fields=fields), self.assertRaises(ValueError):
                self._record(**fields)

    def test_a_cut_list_is_dirty_even_with_paths(self):
        record = self._record(dirty=True, dirty_paths=("a",), dirty_paths_truncated=True)
        self.assertIs(record["dirty_paths_truncated"], True)

    def test_a_dirty_path_must_be_a_repo_relative_string(self):
        for bad in ("", "/abs", 3):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self._record(dirty=True, dirty_paths=(bad,))


class TestRecordedDirtyPaths(unittest.TestCase):
    """What a record says was dirty: the paths, or None when it cannot say all of them."""

    def test_a_clean_run_names_none(self):
        self.assertEqual(ledger.recorded_dirty_paths({"dirty": False}), ())

    def test_a_dirty_run_names_its_paths(self):
        record = {"dirty": True, "dirty_paths": ["a", "b"], "dirty_paths_truncated": False}
        self.assertEqual(ledger.recorded_dirty_paths(record), ("a", "b"))

    def test_a_record_from_before_the_paths_were_kept_cannot_say(self):
        self.assertIsNone(ledger.recorded_dirty_paths({"dirty": True}))

    def test_a_cut_list_cannot_say(self):
        record = {"dirty": True, "dirty_paths": ["a"], "dirty_paths_truncated": True}
        self.assertIsNone(ledger.recorded_dirty_paths(record))

    def test_a_list_it_cannot_read_cannot_say(self):
        for paths in ("a", [3], [], None):
            record = {"dirty": True, "dirty_paths": paths, "dirty_paths_truncated": False}
            with self.subTest(paths=paths):
                self.assertIsNone(ledger.recorded_dirty_paths(record))

    def test_an_unreadable_flag_cannot_say(self):
        self.assertIsNone(ledger.recorded_dirty_paths({"dirty": "yes"}))


class TestSerialise(unittest.TestCase):
    def test_one_record_is_one_line(self):
        record = ledger.build_record(
            play=PLAY,
            name="X",
            commit=COMMIT,
            dirty=False,
            play_sha256="a" * 64,
            outcome="ok",
            changed=0,
            started="2026-09-14T10:00:00Z",
            finished="2026-09-14T10:00:01Z",
        )
        line = ledger.serialise(record)
        self.assertNotIn("\n", line.rstrip("\n"))
        self.assertTrue(line.endswith("\n"))
        self.assertEqual(json.loads(line), record)

    def test_keys_are_sorted_so_two_runs_diff_cleanly(self):
        record = ledger.genesis_record(commit=COMMIT, at="2026-09-14T10:00:00Z")
        keys = list(json.loads(ledger.serialise(record)))
        self.assertEqual(keys, sorted(keys))


class TestGenesisRecord(unittest.TestCase):
    def test_it_records_when_the_ledger_started_knowing_things(self):
        # Without it, "no record for this play" is ambiguous between "never run
        # here" and "run before the ledger existed", and every silence is a guess.
        record = ledger.genesis_record(commit=COMMIT, at="2026-09-14T10:00:00Z")
        self.assertEqual(record["kind"], "genesis")
        self.assertEqual(record["commit"], COMMIT)
        self.assertEqual(record["at"], "2026-09-14T10:00:00Z")
        self.assertEqual(record["schema"], ledger.SCHEMA)

    def test_genesis_is_distinguishable_from_a_run(self):
        genesis = ledger.genesis_record(commit=COMMIT, at="2026-09-14T10:00:00Z")
        self.assertNotIn("play", genesis)


def _run_line(play, finished, outcome="ok", narrowed=None):
    return ledger.serialise(
        ledger.build_record(
            play=play,
            name="X",
            commit=COMMIT,
            dirty=False,
            play_sha256="a" * 64,
            outcome=outcome,
            changed=0,
            started=finished,
            finished=finished,
            narrowed=narrowed,
        )
    )


class TestIsFullRun(unittest.TestCase):
    def test_a_run_with_no_narrowing_is_full(self):
        self.assertIs(ledger.is_full_run({"narrowed": {}}), True)

    def test_a_narrowed_run_is_not_full(self):
        self.assertIs(ledger.is_full_run({"narrowed": {"tags": ["member"]}}), False)

    def test_a_record_from_before_narrowing_was_kept_reads_as_full(self):
        """Nothing can say what such a run covered, and every run before was judged full."""
        self.assertIs(ledger.is_full_run({}), True)


class TestDescribeNarrowing(unittest.TestCase):
    def test_it_spells_the_options_as_the_command_line_did(self):
        text = ledger.describe_narrowing(
            {"tags": ["a", "b"], "skip_tags": ["c"], "limit": "h", "start_at_task": "Install it",
             "step": True}
        )
        self.assertEqual(
            text, "--tags a,b --skip-tags c --limit h --start-at-task 'Install it' --step"
        )

    def test_step_is_recorded_only_as_true(self):
        with self.assertRaises(ValueError):
            ledger.build_record(
                play=PLAY, name="X", commit=COMMIT, dirty=False, play_sha256="a" * 64,
                outcome="ok", changed=0, started="2026-09-14T10:00:00Z",
                finished="2026-09-14T10:00:00Z", narrowed={"step": "yes"},
            )


class TestForgetRecord(unittest.TestCase):
    def test_it_names_the_play_and_when(self):
        record = ledger.forget_record(play=PLAY, at="2026-09-14T10:00:00Z")
        self.assertEqual(
            record, {"schema": ledger.SCHEMA, "kind": "forget", "play": PLAY, "at": "2026-09-14T10:00:00Z"}
        )

    def test_the_play_must_be_repo_relative_and_the_time_utc(self):
        with self.assertRaises(ValueError):
            ledger.forget_record(play="/abs/play.yml", at="2026-09-14T10:00:00Z")
        with self.assertRaises(ValueError):
            ledger.forget_record(play=PLAY, at="yesterday")


class TestFoldLatestFull(unittest.TestCase):
    """Issue #278: a tagged run that failed made the whole play due for ever."""

    def test_a_narrowed_run_does_not_replace_the_last_full_run(self):
        lines = [
            _run_line(PLAY, "2026-09-01T00:00:00Z", outcome="ok"),
            _run_line(PLAY, "2026-09-14T00:00:00Z", outcome="failed", narrowed={"tags": ["member"]}),
        ]
        self.assertEqual(ledger.fold_latest_full(lines)[PLAY]["outcome"], "ok")
        self.assertEqual(ledger.fold_latest(lines)[PLAY]["outcome"], "failed")

    def test_a_play_only_ever_run_narrowed_has_no_full_run(self):
        lines = [_run_line(PLAY, "2026-09-14T00:00:00Z", narrowed={"limit": "localhost"})]
        self.assertEqual(ledger.fold_latest_full(lines), {})
        self.assertEqual(list(ledger.fold_latest(lines)), [PLAY])

    def test_a_later_full_run_still_wins(self):
        lines = [
            _run_line(PLAY, "2026-09-01T00:00:00Z", outcome="failed", narrowed={"tags": ["x"]}),
            _run_line(PLAY, "2026-09-14T00:00:00Z", outcome="failed"),
        ]
        self.assertEqual(ledger.fold_latest_full(lines)[PLAY]["finished"], "2026-09-14T00:00:00Z")


class TestForget(unittest.TestCase):
    """A play cleared on a host where it does not apply: its runs until then are dropped."""

    def _forget(self, at):
        return ledger.serialise(ledger.forget_record(play=PLAY, at=at))

    def test_runs_up_to_the_forget_are_dropped_from_both_folds(self):
        lines = [_run_line(PLAY, "2026-09-01T00:00:00Z", outcome="failed"), self._forget("2026-09-02T00:00:00Z")]
        self.assertEqual(ledger.fold_latest(lines), {})
        self.assertEqual(ledger.fold_latest_full(lines), {})

    def test_a_run_after_the_forget_counts_again(self):
        lines = [
            self._forget("2026-09-02T00:00:00Z"),
            _run_line(PLAY, "2026-09-01T00:00:00Z", outcome="failed"),
            _run_line(PLAY, "2026-09-03T00:00:00Z", outcome="ok"),
        ]
        self.assertEqual(ledger.fold_latest(lines)[PLAY]["outcome"], "ok")

    def test_a_forget_touches_no_other_play(self):
        other = "playbooks/imports/play-python.yml"
        lines = [_run_line(other, "2026-09-01T00:00:00Z"), self._forget("2026-09-02T00:00:00Z")]
        self.assertEqual(list(ledger.fold_latest(lines)), [other])

    def test_a_forget_with_no_play_is_an_error(self):
        line = json.dumps({"schema": ledger.SCHEMA, "kind": "forget", "at": "2026-09-02T00:00:00Z"})
        with self.assertRaises(ValueError):
            ledger.fold_latest([line])


class TestRecordedPlays(unittest.TestCase):
    """Issue #323: every play the ledger holds any record of, which no fold drops."""

    def test_narrowed_runs_and_forgets_count_and_genesis_does_not(self):
        other = "playbooks/imports/play-python.yml"
        forgotten = "playbooks/imports/play-fonts.yml"
        lines = [
            ledger.serialise(ledger.genesis_record(commit="a" * 40, at="2026-09-01T00:00:00Z")),
            _run_line(PLAY, "2026-09-14T00:00:00Z", narrowed={"tags": ["x"]}),
            _run_line(other, "2026-09-14T00:00:00Z"),
            ledger.serialise(ledger.forget_record(play=forgotten, at="2026-09-02T00:00:00Z")),
        ]
        self.assertEqual(ledger.recorded_plays(lines), frozenset({PLAY, other, forgotten}))

    def test_a_corrupt_line_raises(self):
        with self.assertRaises(ValueError):
            ledger.recorded_plays(["{not json"])


class TestFoldLatest(unittest.TestCase):
    def _line(self, play, finished, outcome="ok"):
        return _run_line(play, finished, outcome)

    def test_the_latest_record_per_play_wins(self):
        lines = [
            self._line(PLAY, "2026-09-01T00:00:00Z", outcome="failed"),
            self._line(PLAY, "2026-09-14T00:00:00Z", outcome="ok"),
        ]
        latest = ledger.fold_latest(lines)
        self.assertEqual(latest[PLAY]["outcome"], "ok")

    def test_order_in_the_file_does_not_decide_it_the_timestamp_does(self):
        # Two concurrent runs interleave appends, so file order is not run order.
        lines = [
            self._line(PLAY, "2026-09-14T00:00:00Z", outcome="ok"),
            self._line(PLAY, "2026-09-01T00:00:00Z", outcome="failed"),
        ]
        latest = ledger.fold_latest(lines)
        self.assertEqual(latest[PLAY]["outcome"], "ok")

    def test_plays_are_kept_apart(self):
        other = "playbooks/imports/play-python.yml"
        latest = ledger.fold_latest(
            [self._line(PLAY, "2026-09-14T00:00:00Z"), self._line(other, "2026-09-13T00:00:00Z")]
        )
        self.assertEqual(sorted(latest), sorted([PLAY, other]))

    def test_genesis_records_are_not_plays(self):
        lines = [
            ledger.serialise(ledger.genesis_record(commit=COMMIT, at="2026-09-01T00:00:00Z")),
            self._line(PLAY, "2026-09-14T00:00:00Z"),
        ]
        latest = ledger.fold_latest(lines)
        self.assertEqual(list(latest), [PLAY])

    def test_blank_lines_are_skipped(self):
        latest = ledger.fold_latest(["", "\n", self._line(PLAY, "2026-09-14T00:00:00Z")])
        self.assertEqual(list(latest), [PLAY])

    def test_a_corrupt_line_is_an_error_not_a_skip(self):
        # A half-written line means the ledger has a hole. Skipping it would make
        # the fold quietly answer from an incomplete history, which is the exact
        # failure this plan exists to catch on the host.
        with self.assertRaises(ValueError):
            ledger.fold_latest(["{not json", self._line(PLAY, "2026-09-14T00:00:00Z")])

    def test_a_future_schema_is_refused_rather_than_misread(self):
        record = ledger.build_record(
            play=PLAY,
            name="X",
            commit=COMMIT,
            dirty=False,
            play_sha256="a" * 64,
            outcome="ok",
            changed=0,
            started="2026-09-14T10:00:00Z",
            finished="2026-09-14T10:00:00Z",
        )
        record["schema"] = ledger.SCHEMA + 1
        with self.assertRaises(ValueError):
            ledger.fold_latest([json.dumps(record) + "\n"])


if __name__ == "__main__":
    unittest.main()
