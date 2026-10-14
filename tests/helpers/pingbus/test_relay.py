"""Unit tests for helpers/pingbus/relay.py: the relay spool, the stall clock, the body cap.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_relay

Spec: Plan 00194 DESIGN.md sections 2.1, 2.2 and 2.7 and its "Spool line format" section;
docs/agent-bus-protocol.md §12 (the relay directory) and §15 (the spool line).
"""

from __future__ import annotations

import os
import pathlib
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import config, relay

TEAM = "team-a"
EID = "$" + "a" * 43
EID2 = "$" + "b" * 43


class ModeTest(unittest.TestCase):
    def test_unset_and_empty_are_off(self):
        self.assertFalse(relay.enabled({}))
        self.assertFalse(relay.enabled({relay.ENV: ""}))

    def test_relay_is_on(self):
        self.assertTrue(relay.enabled({relay.ENV: "relay"}))

    def test_any_other_value_is_refused_loudly(self):
        for value in ("socket", "1", "Relay", "relay "):
            with self.subTest(value=value), self.assertRaises(config.ConfigError) as caught:
                relay.enabled({relay.ENV: value})
            self.assertIn(relay.ENV, str(caught.exception))


class CapTest(unittest.TestCase):
    def test_a_short_body_is_whole_and_uncut(self):
        self.assertEqual(relay.cap_text("hello"), ("hello", "-"))

    def test_exactly_the_cap_is_whole(self):
        text = "x" * relay.BODY_MAX_BYTES
        self.assertEqual(relay.cap_text(text), (text, "-"))

    def test_a_long_body_is_cut_to_the_cap_and_says_how_much(self):
        shown, cut = relay.cap_text("x" * 9310)
        self.assertEqual(shown, "x" * 2048)
        self.assertEqual(cut, "2048/9310")

    def test_the_cut_never_splits_a_character(self):
        text = "é" * 2000  # two bytes each: 4000 bytes
        shown, cut = relay.cap_text(text)
        self.assertEqual(shown, "é" * 1024)
        self.assertEqual(cut, "2048/4000")
        # an odd boundary: one ASCII byte then two-byte characters
        shown, cut = relay.cap_text("a" + "é" * 2000)
        self.assertEqual(shown, "a" + "é" * 1023)
        self.assertEqual(cut, "2047/4001")

    def test_the_cut_never_splits_a_four_byte_character(self):
        shown, cut = relay.cap_text("😀" * 600)  # 2400 bytes
        self.assertEqual(shown, "😀" * 512)
        self.assertEqual(cut, "2048/2400")


class SpoolTest(unittest.TestCase):
    def setUp(self) -> None:
        self.home = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.spool = relay.Spool(self.home)

    def test_a_line_is_written_whole_under_the_team_and_event_id(self):
        self.spool.write(TEAM, EID, "HUMAN\t1\tx")
        path = self.home / "relay" / "spool" / TEAM / f"{EID}.line"
        self.assertEqual(path.read_text(encoding="utf-8"), "HUMAN\t1\tx\n")
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(path.parent.stat().st_mode), 0o700)
        self.assertTrue(self.spool.has(TEAM, EID))
        self.assertEqual(self.spool.ids(TEAM), frozenset({EID}))

    def test_a_write_leaves_no_temporary_file_for_the_plugin_to_read(self):
        self.spool.write(TEAM, EID, "x")
        names = os.listdir(self.home / "relay" / "spool" / TEAM)
        self.assertEqual(names, [f"{EID}.line"])

    def test_a_temporary_name_is_not_an_id(self):
        directory = self.home / "relay" / "spool" / TEAM
        directory.mkdir(parents=True)
        (directory / f".{EID}.line.0123456789abcdef.tmp").write_text("x", encoding="utf-8")
        (directory / "junk.line").write_text("x", encoding="utf-8")
        self.assertEqual(self.spool.ids(TEAM), frozenset())

    def test_a_bad_team_or_id_never_becomes_a_path(self):
        for team, event_id in (("../x", EID), (TEAM, "../" + EID), (TEAM, "$short"), ("", EID)):
            with self.subTest(team=team, event_id=event_id), self.assertRaises(ValueError):
                self.spool.write(team, event_id, "x")
            with self.subTest(team=team, event_id=event_id), self.assertRaises(ValueError):
                self.spool.withdraw(team, event_id)

    def test_a_timeout_line_is_keyed_by_event_and_target(self):
        key = relay.key(EID, "orch.1+host.podman")
        self.assertEqual(key, f"{EID}~orch.1+host.podman")
        self.spool.write(TEAM, key, "TIMEOUT")
        self.spool.write(TEAM, relay.key(EID, "peer.1+host.podman"), "TIMEOUT")
        self.assertEqual(len(self.spool.ids(TEAM)), 2)
        self.assertEqual(relay.split_key(key), (EID, "orch.1+host.podman"))
        self.assertEqual(relay.split_key(EID), (EID, None))

    def test_a_bad_target_in_a_key_never_becomes_a_path(self):
        for target in ("", "../x", "a/b", "A", "x" * 200):
            with self.subTest(target=target), self.assertRaises(ValueError):
                self.spool.write(TEAM, f"{EID}~{target}", "x")

    def test_the_exclusive_lock_is_held_by_one_user_at_a_time(self):
        with self.spool.exclusive():
            other = relay.Spool(self.home)
            with self.assertRaises(BlockingIOError), other.exclusive(block=False):
                pass
        with self.spool.exclusive(block=False):
            pass

    def test_a_line_may_not_carry_a_second_line(self):
        with self.assertRaises(ValueError):
            self.spool.write(TEAM, EID, "a\nb")

    def test_withdraw_removes_the_line_and_a_missing_one_is_fine(self):
        self.spool.write(TEAM, EID, "x")
        self.spool.withdraw(TEAM, EID)
        self.assertFalse(self.spool.has(TEAM, EID))
        self.spool.withdraw(TEAM, EID)
        self.spool.withdraw(TEAM, EID2)

    def test_withdraw_on_a_home_with_no_relay_directory_creates_nothing(self):
        self.spool.withdraw(TEAM, EID)
        self.assertEqual(os.listdir(self.home), [])

    def test_confirmed_lists_the_markers_the_plugin_wrote(self):
        directory = self.home / "relay" / "relayed" / TEAM
        directory.mkdir(parents=True)
        (directory / EID).write_text("", encoding="utf-8")
        (directory / "not-an-id").write_text("", encoding="utf-8")
        self.assertEqual(self.spool.confirmed(TEAM), (EID,))

    def test_finish_removes_the_line_and_the_marker(self):
        self.spool.write(TEAM, EID, "x")
        marker = self.home / "relay" / "relayed" / TEAM / EID
        marker.parent.mkdir(parents=True)
        marker.write_text("", encoding="utf-8")
        self.spool.finish(TEAM, EID)
        self.assertFalse(self.spool.has(TEAM, EID))
        self.assertFalse(marker.exists())
        self.spool.finish(TEAM, EID)

    def test_waiting_counts_lines_over_every_team(self):
        self.assertEqual(self.spool.waiting(), 0)
        self.spool.write(TEAM, EID, "x")
        self.spool.write("team-b", EID2, "y")
        self.assertEqual(self.spool.waiting(), 2)

    def test_stalled_is_a_marker_set_and_cleared(self):
        self.assertFalse(self.spool.stalled())
        self.spool.set_stalled(True)
        self.assertTrue(self.spool.stalled())
        self.assertTrue(relay.Spool(self.home).stalled(), "another process sees it")
        self.spool.set_stalled(True)
        self.spool.set_stalled(False)
        self.assertFalse(self.spool.stalled())
        self.spool.set_stalled(False)


class StallTest(unittest.TestCase):
    def setUp(self) -> None:
        self.now = 1000.0
        self.stall = relay.Stall(lambda: self.now, after_s=120)

    def at(self, seconds: float) -> None:
        self.now = 1000.0 + seconds

    def test_nothing_waiting_is_never_stalled(self):
        for t in (0, 500, 5000):
            self.at(t)
            self.assertFalse(self.stall.observe(waiting=0, confirmed=0))

    def test_items_waiting_for_the_window_with_no_confirmation_stall(self):
        self.assertFalse(self.stall.observe(waiting=1, confirmed=0))
        self.at(119)
        self.assertFalse(self.stall.observe(waiting=1, confirmed=0))
        self.at(120)
        self.assertTrue(self.stall.observe(waiting=1, confirmed=0))
        self.at(900)
        self.assertTrue(self.stall.observe(waiting=1, confirmed=0))

    def test_a_confirmation_restarts_the_window(self):
        self.stall.observe(waiting=2, confirmed=0)
        self.at(100)
        self.assertFalse(self.stall.observe(waiting=1, confirmed=1))
        self.at(219)
        self.assertFalse(self.stall.observe(waiting=1, confirmed=0))
        self.at(220)
        self.assertTrue(self.stall.observe(waiting=1, confirmed=0))

    def test_a_stall_clears_when_a_relay_is_confirmed(self):
        self.stall.observe(waiting=1, confirmed=0)
        self.at(200)
        self.assertTrue(self.stall.observe(waiting=1, confirmed=0))
        self.assertFalse(self.stall.observe(waiting=0, confirmed=1))

    def test_an_emptied_queue_forgets_the_wait(self):
        self.stall.observe(waiting=1, confirmed=0)
        self.at(100)
        self.stall.observe(waiting=0, confirmed=0)
        self.at(150)
        self.assertFalse(self.stall.observe(waiting=1, confirmed=0))
        self.at(269)
        self.assertFalse(self.stall.observe(waiting=1, confirmed=0))
        self.at(270)
        self.assertTrue(self.stall.observe(waiting=1, confirmed=0))


if __name__ == "__main__":
    unittest.main()
