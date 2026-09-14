"""Tests for helpers.play_ledger.plugin_support — every decision the callback plugin defers.

`ansible` is not importable by this interpreter, so anything left inside the
callback plugin itself is untested by construction. These are the tests that
make the plugin safe to keep dumb.
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import ledger, plugin_support, store

FORTY_HEX = "c" * 40
SIXTY_FOUR_HEX = "d" * 64
STAMP = "2026-09-14T12:00:00Z"


def _record(play: str = "playbooks/imports/play-x.yml") -> dict:
    return ledger.build_record(
        play=play,
        name="X",
        commit=FORTY_HEX,
        dirty=False,
        play_sha256=SIXTY_FOUR_HEX,
        outcome="ok",
        changed=1,
        started=STAMP,
        finished=STAMP,
    )


class TestShouldRecord(unittest.TestCase):
    def test_a_normal_run_records(self) -> None:
        self.assertIs(plugin_support.should_record({}), True)

    def test_check_mode_does_not_record(self) -> None:
        """A --check run applied nothing; ledgering it would report the play fresh
        when the host never received it — the exact lie Phase 2 exists to catch."""
        self.assertIs(plugin_support.should_record({"check": True}), False)

    def test_check_false_records(self) -> None:
        self.assertIs(plugin_support.should_record({"check": False}), True)

    def test_list_tasks_does_not_record(self) -> None:
        self.assertIs(plugin_support.should_record({"listtasks": True}), False)

    def test_list_hosts_does_not_record(self) -> None:
        self.assertIs(plugin_support.should_record({"listhosts": True}), False)

    def test_list_tags_does_not_record(self) -> None:
        self.assertIs(plugin_support.should_record({"listtags": True}), False)

    def test_syntax_check_does_not_record(self) -> None:
        self.assertIs(plugin_support.should_record({"syntax": True}), False)

    def test_any_one_suppressing_flag_is_enough(self) -> None:
        self.assertIs(plugin_support.should_record({"check": False, "listtags": True}), False)

    def test_returns_a_real_bool(self) -> None:
        self.assertIsInstance(plugin_support.should_record({"check": True}), bool)

    def test_an_unknown_flag_does_not_suppress(self) -> None:
        """Only the flags that mean 'nothing was applied' suppress; a new unrelated
        CLI flag must not silently stop the ledger recording."""
        self.assertIs(plugin_support.should_record({"diff": True, "forks": 5}), True)


class TestPlaySource(unittest.TestCase):
    def test_extracts_the_file_from_an_ansible_position_triple(self) -> None:
        self.assertEqual(
            plugin_support.play_source(("/repo/playbooks/imports/play-x.yml", 3, 5)),
            "/repo/playbooks/imports/play-x.yml",
        )

    def test_accepts_a_two_element_position(self) -> None:
        self.assertEqual(plugin_support.play_source(("/repo/p.yml", 3)), "/repo/p.yml")

    def test_none_raises_rather_than_yielding_a_blank_path(self) -> None:
        """No position means no play path, and a record with no play is unjoinable —
        it must become a recorded hole, not a row nobody can use."""
        with self.assertRaises(ValueError):
            plugin_support.play_source(None)

    def test_empty_tuple_raises(self) -> None:
        with self.assertRaises(ValueError):
            plugin_support.play_source(())

    def test_a_blank_filename_raises(self) -> None:
        with self.assertRaises(ValueError):
            plugin_support.play_source(("", 1, 1))

    def test_a_non_string_filename_raises(self) -> None:
        with self.assertRaises(ValueError):
            plugin_support.play_source((None, 1, 1))


class TestRepoRootFrom(unittest.TestCase):
    def test_is_the_parent_of_the_plugin_directory(self) -> None:
        self.assertEqual(
            plugin_support.repo_root_from("/repo/callback_plugins/play_ledger.py"), "/repo"
        )

    def test_is_derived_from_the_plugin_file_not_the_cwd(self) -> None:
        """A callback's cwd is wherever the operator was; only the plugin's own
        location reliably names the checkout the plays came from."""
        original = os.getcwd()
        with tempfile.TemporaryDirectory() as elsewhere:
            os.chdir(elsewhere)
            try:
                self.assertEqual(
                    plugin_support.repo_root_from("/repo/callback_plugins/play_ledger.py"), "/repo"
                )
            finally:
                os.chdir(original)

    def test_resolves_symlinks(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            real = os.path.join(base, "real")
            os.makedirs(os.path.join(real, "callback_plugins"))
            link = os.path.join(base, "link")
            os.symlink(real, link)
            plugin = os.path.join(link, "callback_plugins", "play_ledger.py")
            self.assertEqual(plugin_support.repo_root_from(plugin), os.path.realpath(real))


class TestWriteRecords(unittest.TestCase):
    def test_creates_the_ledger_and_appends_every_record(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            plugin_support.write_records(
                base, [_record("playbooks/a.yml"), _record("playbooks/b.yml")],
                commit=FORTY_HEX, at=STAMP,
            )
            lines = store.read_lines(base)
            self.assertEqual(len(lines), 3, "genesis plus two runs")
            self.assertEqual(json.loads(lines[0])["kind"], "genesis")
            self.assertEqual(sorted(ledger.fold_latest(lines)), ["playbooks/a.yml", "playbooks/b.yml"])

    def test_a_second_run_does_not_rewrite_genesis(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            plugin_support.write_records(base, [_record("playbooks/a.yml")], commit=FORTY_HEX, at=STAMP)
            plugin_support.write_records(base, [_record("playbooks/b.yml")], commit=FORTY_HEX, at=STAMP)
            genesis = [line for line in store.read_lines(base) if '"genesis"' in line]
            self.assertEqual(len(genesis), 1)

    def test_no_records_still_creates_the_ledger(self) -> None:
        """A run that matched no play must still date the ledger, or a later silence
        cannot be told from 'the ledger did not exist yet'."""
        with tempfile.TemporaryDirectory() as base:
            plugin_support.write_records(base, [], commit=FORTY_HEX, at=STAMP)
            self.assertTrue(os.path.exists(ledger.runs_path(base)))

    def test_the_directory_is_owner_only(self) -> None:
        with tempfile.TemporaryDirectory() as outer:
            base = os.path.join(outer, "play-ledger")
            plugin_support.write_records(base, [], commit=FORTY_HEX, at=STAMP)
            self.assertEqual(os.stat(base).st_mode & 0o777, 0o700)

    def test_a_failure_propagates_so_the_plugin_can_record_it(self) -> None:
        """write_records must NOT swallow: the plugin's whole fail-fast story is that
        it catches, writes the sentinel and prints the marker."""
        with tempfile.TemporaryDirectory() as base:
            unserialisable = dict(_record("playbooks/a.yml"), name={"a", "set"})
            with self.assertRaises(TypeError):
                plugin_support.write_records(base, [unserialisable], commit=FORTY_HEX, at=STAMP)

    def test_records_written_before_a_failure_survive_it(self) -> None:
        """A partial run recorded plus a sentinel beats nothing recorded: the rows that
        did land are true, and the sentinel is what makes the hole visible."""
        with tempfile.TemporaryDirectory() as base:
            good = _record("playbooks/a.yml")
            unserialisable = dict(_record("playbooks/b.yml"), name={"a", "set"})
            with self.assertRaises(TypeError):
                plugin_support.write_records(
                    base, [good, unserialisable], commit=FORTY_HEX, at=STAMP
                )
            self.assertEqual(list(ledger.fold_latest(store.read_lines(base))), ["playbooks/a.yml"])


class TestRecordFailure(unittest.TestCase):
    def test_writes_the_sentinel_phase_two_reads(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            plugin_support.record_failure(base, error="disk full", at=STAMP)
            self.assertEqual(store.broken_reason(base), f"{STAMP} disk full")

    def test_works_when_the_ledger_was_never_created(self) -> None:
        """'Could not create the ledger at all' is exactly a failure that must be
        recordable."""
        with tempfile.TemporaryDirectory() as outer:
            base = os.path.join(outer, "never-made")
            plugin_support.record_failure(base, error="permission denied", at=STAMP)
            self.assertIn("permission denied", store.broken_reason(base) or "")

    def test_keeps_the_first_reason(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            plugin_support.record_failure(base, error="first", at=STAMP)
            plugin_support.record_failure(base, error="second", at=STAMP)
            self.assertIn("first", store.broken_reason(base) or "")
            self.assertNotIn("second", store.broken_reason(base) or "")

    def test_returns_the_marker_line_for_stderr(self) -> None:
        """Ansible swallows an exception raised in a callback, so the operator's only
        live signal is this line."""
        with tempfile.TemporaryDirectory() as base:
            line = plugin_support.record_failure(base, error="disk full", at=STAMP)
            self.assertTrue(line.startswith(plugin_support.FAILURE_MARKER))
            self.assertIn("disk full", line)

    def test_a_sentinel_that_cannot_be_written_still_yields_a_marker(self) -> None:
        """The last resort must not itself raise into a callback Ansible will swallow."""
        with tempfile.TemporaryDirectory() as outer:
            blocker = os.path.join(outer, "blocked")
            open(blocker, "w").close()  # a FILE where the ledger dir must be
            line = plugin_support.record_failure(blocker, error="original cause", at=STAMP)
            self.assertTrue(line.startswith(plugin_support.FAILURE_MARKER))
            self.assertIn("original cause", line)


if __name__ == "__main__":
    unittest.main()
