"""Unit tests for helpers/play_ledger/collector.py — the callback's logic.

The Ansible callback plugin is a thin adapter over this. Everything that decides
anything lives here, where it can be tested without Ansible importable: helpers
are stdlib-only, and `ansible` is not on the test interpreter's path.

Run from the repo root:

    python3 -m unittest tests.helpers.play_ledger.test_collector
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.play_ledger import collector

COMMIT = "b07092eb1f4d8c3a9e2f07a1b5c6d7e8f9012345"
PLAY = "playbooks/imports/play-gnome-shell-extensions.yml"
OTHER = "playbooks/imports/play-python.yml"


def _collector(**overrides):
    fields = {
        "commit": COMMIT,
        "dirty": False,
        "hash_play": lambda path: "a" * 64,
        "repo_root": "/repo",
    }
    fields.update(overrides)
    return collector.RunCollector(**fields)


class TestPlayPaths(unittest.TestCase):
    def test_an_absolute_play_path_is_made_repo_relative(self):
        # The ledger joins on the play path, and an absolute one embeds the
        # checkout location, which differs per clone.
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="X", at="2026-09-14T10:00:00Z")
        records = rc.on_end(at="2026-09-14T10:01:00Z")
        self.assertEqual(records[0]["play"], PLAY)

    def test_a_line_suffix_from_ansible_is_stripped(self):
        # Ansible's Play.get_path() returns "<file>:<line>".
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}:4", name="X", at="2026-09-14T10:00:00Z")
        records = rc.on_end(at="2026-09-14T10:01:00Z")
        self.assertEqual(records[0]["play"], PLAY)

    def test_a_play_outside_the_repo_is_refused(self):
        # A play from somewhere else cannot be compared against this repo's HEAD,
        # so recording it would put an unanswerable row in the ledger.
        rc = _collector()
        with self.assertRaises(ValueError):
            rc.on_play_start(
                play_path="/elsewhere/play-x.yml", name="X", at="2026-09-14T10:00:00Z"
            )


class TestOutcome(unittest.TestCase):
    def _run(self, results):
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="X", at="2026-09-14T10:00:00Z")
        for outcome, changed in results:
            rc.on_result(outcome=outcome, changed=changed)
        return rc.on_end(at="2026-09-14T10:01:00Z")[0]

    def test_all_ok_is_ok(self):
        self.assertEqual(self._run([("ok", False), ("ok", True)])["outcome"], "ok")

    def test_one_failure_makes_the_play_failed(self):
        self.assertEqual(self._run([("ok", False), ("failed", False)])["outcome"], "failed")

    def test_unreachable_outranks_failed(self):
        # A host that could not be reached did not run the play at all; reporting
        # "failed" would claim it ran and did not work.
        record = self._run([("failed", False), ("unreachable", False)])
        self.assertEqual(record["outcome"], "unreachable")

    def test_unreachable_outranks_failed_whatever_the_order(self):
        record = self._run([("unreachable", False), ("failed", False)])
        self.assertEqual(record["outcome"], "unreachable")

    def test_a_play_with_no_results_is_still_recorded_as_ok(self):
        # Every task skipped is a real, legitimate run of that play — and the
        # freshness axis cares that it ran, not that it did anything.
        self.assertEqual(self._run([])["outcome"], "ok")

    def test_changed_counts_only_changed_results(self):
        record = self._run([("ok", True), ("ok", False), ("ok", True)])
        self.assertEqual(record["changed"], 2)

    def test_an_unknown_result_state_is_refused(self):
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="X", at="2026-09-14T10:00:00Z")
        with self.assertRaises(ValueError):
            rc.on_result(outcome="probably-fine", changed=False)


class TestMultiplePlays(unittest.TestCase):
    def test_each_play_gets_its_own_record(self):
        # playbook-main.yml imports many plays; one record saying "everything ran"
        # would lose the axis Phase 2 needs.
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="A", at="2026-09-14T10:00:00Z")
        rc.on_result(outcome="ok", changed=True)
        rc.on_play_start(play_path=f"/repo/{OTHER}", name="B", at="2026-09-14T10:05:00Z")
        rc.on_result(outcome="failed", changed=False)
        records = rc.on_end(at="2026-09-14T10:10:00Z")

        self.assertEqual([r["play"] for r in records], [PLAY, OTHER])
        self.assertEqual(records[0]["outcome"], "ok")
        self.assertEqual(records[1]["outcome"], "failed")

    def test_results_do_not_leak_from_one_play_into_the_next(self):
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="A", at="2026-09-14T10:00:00Z")
        rc.on_result(outcome="failed", changed=False)
        rc.on_play_start(play_path=f"/repo/{OTHER}", name="B", at="2026-09-14T10:05:00Z")
        records = rc.on_end(at="2026-09-14T10:10:00Z")
        self.assertEqual(records[1]["outcome"], "ok")
        self.assertEqual(records[1]["changed"], 0)

    def test_a_play_is_timed_from_its_own_start_to_the_next_boundary(self):
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="A", at="2026-09-14T10:00:00Z")
        rc.on_play_start(play_path=f"/repo/{OTHER}", name="B", at="2026-09-14T10:05:00Z")
        records = rc.on_end(at="2026-09-14T10:10:00Z")
        self.assertEqual(records[0]["started"], "2026-09-14T10:00:00Z")
        self.assertEqual(records[0]["finished"], "2026-09-14T10:05:00Z")
        self.assertEqual(records[1]["started"], "2026-09-14T10:05:00Z")
        self.assertEqual(records[1]["finished"], "2026-09-14T10:10:00Z")

    def test_the_same_play_twice_yields_two_records(self):
        # Imported twice in one run is a real thing; collapsing them would hide it.
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="A", at="2026-09-14T10:00:00Z")
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="A", at="2026-09-14T10:05:00Z")
        self.assertEqual(len(rc.on_end(at="2026-09-14T10:10:00Z")), 2)

    def test_ending_with_no_plays_yields_nothing(self):
        self.assertEqual(_collector().on_end(at="2026-09-14T10:00:00Z"), [])

    def test_on_end_is_idempotent(self):
        # The plugin may see both a stats event and an explicit close; emitting the
        # last play twice would double-count it in every freshness report.
        rc = _collector()
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="A", at="2026-09-14T10:00:00Z")
        self.assertEqual(len(rc.on_end(at="2026-09-14T10:01:00Z")), 1)
        self.assertEqual(rc.on_end(at="2026-09-14T10:02:00Z"), [])


class TestRecordContents(unittest.TestCase):
    def test_the_commit_and_dirty_flag_are_carried_through(self):
        rc = _collector(dirty=True)
        rc.on_play_start(play_path=f"/repo/{PLAY}", name="X", at="2026-09-14T10:00:00Z")
        record = rc.on_end(at="2026-09-14T10:01:00Z")[0]
        self.assertEqual(record["commit"], COMMIT)
        self.assertTrue(record["dirty"])

    def test_the_play_is_hashed_at_its_absolute_path(self):
        seen = []

        def hash_play(path):
            seen.append(path)
            return "b" * 64

        rc = _collector(hash_play=hash_play)
        rc.on_play_start(play_path=f"/repo/{PLAY}:4", name="X", at="2026-09-14T10:00:00Z")
        record = rc.on_end(at="2026-09-14T10:01:00Z")[0]

        self.assertEqual(seen, [f"/repo/{PLAY}"])
        self.assertEqual(record["play_sha256"], "b" * 64)


if __name__ == "__main__":
    unittest.main()
