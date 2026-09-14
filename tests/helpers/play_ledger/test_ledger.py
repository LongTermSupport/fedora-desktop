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
                "play_sha256",
                "outcome",
                "changed",
                "started",
                "finished",
            },
        )

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


class TestFoldLatest(unittest.TestCase):
    def _line(self, play, finished, outcome="ok"):
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
            )
        )

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
