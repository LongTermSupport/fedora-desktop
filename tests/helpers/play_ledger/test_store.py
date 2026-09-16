"""Unit tests for helpers/play_ledger/store.py — the ledger's filesystem half.

Run from the repo root:

    python3 -m unittest tests.helpers.play_ledger.test_store
"""

from __future__ import annotations

import json
import os
import pathlib
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.play_ledger import ledger, store

PLAY = "playbooks/imports/play-gnome-shell-extensions.yml"
COMMIT = "b07092eb1f4d8c3a9e2f07a1b5c6d7e8f9012345"
NOW = "2026-09-14T10:00:00Z"


def _record(play: str = PLAY, finished: str = NOW) -> dict:
    return ledger.build_record(
        play=play,
        name="X",
        commit=COMMIT,
        dirty=False,
        play_sha256="a" * 64,
        outcome="ok",
        changed=0,
        started=NOW,
        finished=finished,
    )


class StoreTestCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = os.path.join(self.tmp.name, "play-ledger")


class TestEnsureLedger(StoreTestCase):
    def test_it_creates_the_directory_owner_only(self):
        # A record of what this machine has had done to it is nobody else's
        # business on a shared box.
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        mode = stat.S_IMODE(os.stat(self.base).st_mode)
        self.assertEqual(mode, 0o700)

    def test_the_first_call_writes_a_genesis_record(self):
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        lines = pathlib.Path(ledger.runs_path(self.base)).read_text(encoding="utf-8").splitlines()
        self.assertEqual(len(lines), 1)
        self.assertEqual(json.loads(lines[0])["kind"], "genesis")

    def test_the_runs_file_is_owner_only(self):
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        mode = stat.S_IMODE(os.stat(ledger.runs_path(self.base)).st_mode)
        self.assertEqual(mode, 0o600)

    def test_a_second_call_does_not_write_a_second_genesis(self):
        # Otherwise the ledger's recorded age resets on every run and its
        # silences stop meaning anything.
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        store.ensure_ledger(self.base, commit=COMMIT, at="2026-09-15T10:00:00Z")
        lines = pathlib.Path(ledger.runs_path(self.base)).read_text(encoding="utf-8").splitlines()
        self.assertEqual(len(lines), 1)

    def test_it_is_idempotent_over_an_existing_directory(self):
        os.makedirs(self.base, mode=0o700)
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        self.assertTrue(os.path.isfile(ledger.runs_path(self.base)))


class TestAppendRecord(StoreTestCase):
    def test_a_record_is_appended_as_one_line(self):
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        store.append_record(self.base, _record())
        lines = pathlib.Path(ledger.runs_path(self.base)).read_text(encoding="utf-8").splitlines()
        self.assertEqual(len(lines), 2)
        self.assertEqual(json.loads(lines[1])["play"], PLAY)

    def test_appends_accumulate_rather_than_replace(self):
        # History is what makes "what changed since you last ran this" answerable;
        # a latest-only file throws it away.
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        store.append_record(self.base, _record(finished="2026-09-14T10:00:00Z"))
        store.append_record(self.base, _record(finished="2026-09-14T11:00:00Z"))
        records = ledger.fold_latest(store.read_lines(self.base))
        self.assertEqual(records[PLAY]["finished"], "2026-09-14T11:00:00Z")
        lines = pathlib.Path(ledger.runs_path(self.base)).read_text(encoding="utf-8").splitlines()
        self.assertEqual(len(lines), 3)

    def test_appending_without_a_ledger_raises(self):
        # Silently creating one here would lose the genesis record and with it the
        # ability to tell "never run" from "run before the ledger existed".
        with self.assertRaises(OSError):
            store.append_record(self.base, _record())

    def test_an_unserialisable_record_raises_before_touching_the_file(self):
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        before = pathlib.Path(ledger.runs_path(self.base)).read_text(encoding="utf-8")
        with self.assertRaises(TypeError):
            store.append_record(self.base, {"play": object()})
        after = pathlib.Path(ledger.runs_path(self.base)).read_text(encoding="utf-8")
        self.assertEqual(before, after)


class TestSentinel(StoreTestCase):
    def test_a_fresh_ledger_is_not_broken(self):
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        self.assertIsNone(store.broken_reason(self.base))

    def test_marking_broken_records_the_reason_and_the_time(self):
        # A callback cannot fail an Ansible run, so the failure is recorded here
        # and Phase 2 refuses to answer while it exists.
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        store.mark_broken(self.base, error="disk full", at=NOW)
        reason = store.broken_reason(self.base)
        self.assertIsNotNone(reason)
        self.assertIn("disk full", reason)
        self.assertIn(NOW, reason)

    def test_it_can_mark_broken_even_when_the_ledger_was_never_created(self):
        # "Could not create the ledger at all" is precisely a case that must be
        # recorded, so this path cannot depend on the ledger existing.
        store.mark_broken(self.base, error="permission denied", at=NOW)
        self.assertIn("permission denied", store.broken_reason(self.base))

    def test_marking_broken_twice_keeps_the_first_reason(self):
        # The first failure is the one that explains the hole; later ones are
        # consequences of it.
        store.mark_broken(self.base, error="first", at=NOW)
        store.mark_broken(self.base, error="second", at="2026-09-14T11:00:00Z")
        reason = store.broken_reason(self.base)
        self.assertIn("first", reason)
        self.assertNotIn("second", reason)

    def test_clearing_is_explicit(self):
        store.mark_broken(self.base, error="x", at=NOW)
        store.clear_broken(self.base, at=NOW)
        self.assertIsNone(store.broken_reason(self.base))

    def test_clearing_an_unbroken_ledger_is_not_an_error(self):
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        store.clear_broken(self.base, at=NOW)
        self.assertIsNone(store.broken_reason(self.base))

    def test_an_undated_CLEARED_marker_cannot_be_written(self):
        """The marker's job is to say the records are a lower bound FROM SOME POINT ON.
        `at` used to default to "" and the sole caller duly wrote a bare newline, so the
        wrong marker was representable and therefore written. Refused, not defaulted."""
        store.mark_broken(self.base, error="x", at=NOW)
        with self.assertRaises(ValueError):
            store.clear_broken(self.base, at="")
        self.assertIsNotNone(
            store.broken_reason(self.base),
            "the sentinel was removed even though the marker could not be written")


class TestReadLines(StoreTestCase):
    def test_an_absent_ledger_reads_as_nothing(self):
        # "No ledger yet" is a state a reader must handle; it is not an error.
        self.assertEqual(store.read_lines(self.base), [])

    def test_it_returns_every_line_including_genesis(self):
        store.ensure_ledger(self.base, commit=COMMIT, at=NOW)
        store.append_record(self.base, _record())
        self.assertEqual(len(store.read_lines(self.base)), 2)


if __name__ == "__main__":
    unittest.main()
