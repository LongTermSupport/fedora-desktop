"""Unit tests for `pingbus seat retire` and `pingbus reach` (Plan 00203 H22 step 5, H23
step 5), against the fake homeserver of test_cli.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_seat_retire
"""

from __future__ import annotations

import os
import pathlib
import shutil
import sys
import time
import urllib.parse

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, inbox, protocol, seat
from tests.helpers.pingbus import test_cli as bus


class SeatBusCase(bus.BusCase):
    """Team A's bundle is a checkout's seat `1@team-a`, linked into the home as a session's is."""

    def setUp(self) -> None:
        super().setUp()
        self.root = self.tmp / "checkout" / "seats"
        self.seat_dir = self.root / bus.TEAM_A / "1"
        self.seat_dir.mkdir(parents=True)
        for directory in (self.root, self.root / bus.TEAM_A, self.seat_dir):
            directory.chmod(0o700)
        shutil.rmtree(self.a.bundle)
        self.a.bundle = self.seat_dir
        self.a.write_bundle()
        (self.home / bus.TEAM_A).symlink_to(self.seat_dir)

    def status_until(self) -> int | None:
        room = urllib.parse.quote(self.a.room, safe="")
        user = urllib.parse.quote(self.a.me, safe="")
        status, answer = self.a.fake.request(
            "GET", f"/_matrix/client/v3/rooms/{room}/state/{protocol.EVENT_STATUS}/{user}",
            token=self.a.tokens[self.a.me])
        return answer.get("until") if status == 200 else None


class RetireTest(SeatBusCase):
    def retire(self, *extra: str) -> tuple[int, str, str]:
        return self.run_cli("seat", "retire", "--root", str(self.root), "1@team-a", *extra)

    def test_the_sync_position_goes_and_the_status_is_expired(self):
        self.joined()
        self.assertIsNotNone(self.a.state.sync_token())
        code, out, err = self.retire()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(out, "")
        self.assertIsNone(self.a.state.sync_token())
        self.assertLessEqual(self.status_until(), int(self.now * 1000))
        self.assertIn("sync position cleared", err)
        self.assertIn("listening status expired", err)
        self.assertIsNone(inbox.probe_lock_at(seat.lock_path(self.root, seat.SeatRef("1", bus.TEAM_A))))

    def test_a_rerun_is_harmless(self):
        self.joined()
        self.assertEqual(self.retire()[0], cli.EXIT_OK)
        code, _, err = self.retire()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertIn("already clear", err)

    def test_a_held_seat_is_refused_75_and_kept(self):
        self.joined()
        lock = inbox.acquire_lock_at(seat.lock_path(self.root, seat.SeatRef("1", bus.TEAM_A)),
                                     inbox.SEAT_KIND, claimed_ms=1)
        self.addCleanup(lock.release)
        code, _, err = self.retire()
        self.assertEqual(code, cli.EXIT_BUSY, err)
        self.assertIsNotNone(self.a.state.sync_token())

    def test_a_homeserver_down_still_clears_the_position_and_fails(self):
        self.joined()
        self.a.write_bundle(base_url=f"http://127.0.0.1:{bus.free_port()}")
        code, _, err = self.retire()
        self.assertNotEqual(code, cli.EXIT_OK)
        self.assertIsNone(self.a.state.sync_token())
        self.assertIn("sync position cleared", err)
        self.assertNotIn("listening status expired", err)

    def test_one_seat_must_be_named(self):
        for item in ("team-a", "1@team-a,2@team-a"):
            with self.subTest(item=item):
                code, _, _ = self.run_cli("seat", "retire", "--root", str(self.root), item)
                self.assertEqual(code, cli.EXIT_USAGE)

    def test_a_seat_with_no_directory_is_refused_78(self):
        code, _, err = self.run_cli("seat", "retire", "--root", str(self.root), "9@team-a")
        self.assertEqual(code, cli.EXIT_CONFIG)
        self.assertIn("9@team-a", err)


class RetireWaitTest(SeatBusCase):
    """H23 step 5 after H11: the removed team's watcher thread lets its sync lock go only
    after the long poll in flight ends, so retire waits for it, bounded, instead of failing."""

    def runtime(self, *args, **kwargs) -> cli.Runtime:
        rt = super().runtime(*args, **kwargs)
        rt.sleep = time.sleep
        rt.sync_wait_s = 1.0
        return rt

    def test_a_sync_lock_let_go_soon_is_waited_for(self):
        self.joined()
        lock = inbox.acquire_lock(self.a.state, "watch")
        self.later(0.3, lock.release)
        code, _, err = self.run_cli("seat", "retire", "--root", str(self.root), "1@team-a")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertIsNone(self.a.state.sync_token())

    def test_a_sync_lock_kept_past_the_wait_is_75_and_changes_nothing(self):
        self.joined()
        lock = inbox.acquire_lock(self.a.state, "watch")
        self.addCleanup(lock.release)
        code, _, err = self.run_cli("seat", "retire", "--root", str(self.root), "1@team-a")
        self.assertEqual(code, cli.EXIT_BUSY, err)
        self.assertIsNotNone(self.a.state.sync_token())
        self.assertIsNone(inbox.probe_lock_at(seat.lock_path(self.root, seat.SeatRef("1", bus.TEAM_A))))

    def test_the_default_wait_outlasts_a_long_poll(self):
        self.assertGreater(cli.Runtime().sync_wait_s, cli.limits.SYNC_LONG_POLL_S)


class FarewellTest(SeatBusCase):
    """H23 step 6: the goodbye of a removed seat, posted from the host with the seat's own
    bundle, to the team's humans and coordinator(s): a fixed template, the seat held meanwhile."""

    def farewell(self) -> tuple[int, str, str]:
        return self.run_cli("seat", "farewell", "--root", str(self.root), "1@team-a")

    def test_the_goodbye_is_posted_as_the_seat(self):
        self.joined()
        code, out, err = self.farewell()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(out, "")
        (content,) = self.a.messages_from_me()
        self.assertIn(f"{bus.ME_LP} left the team: removed by the owner", content["body"])
        self.assertEqual(set(content["m.mentions"]["user_ids"]), {self.a.alice, self.a.orch})
        self.assertIsNone(inbox.probe_lock_at(seat.lock_path(self.root, seat.SeatRef("1", bus.TEAM_A))))

    def test_a_seat_removed_twice_inside_the_duplicate_window_says_goodbye_twice(self):
        """Plan 00194 Task 2.0i: removed, added back, removed again within 60 s."""
        self.joined()
        self.assertEqual(self.farewell()[0], cli.EXIT_OK)
        self.now += 19
        code, _, err = self.farewell()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(len(self.a.messages_from_me()), 2)

    def test_a_held_seat_is_refused_75(self):
        lock = inbox.acquire_lock_at(seat.lock_path(self.root, seat.SeatRef("1", bus.TEAM_A)),
                                     inbox.SEAT_KIND, claimed_ms=1)
        self.addCleanup(lock.release)
        self.assertEqual(self.farewell()[0], cli.EXIT_BUSY)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_a_homeserver_down_fails_and_says_so(self):
        self.a.write_bundle(base_url=f"http://127.0.0.1:{bus.free_port()}")
        code, _, err = self.farewell()
        self.assertNotEqual(code, cli.EXIT_OK)
        self.assertIn("homeserver", err)


class ReachTest(SeatBusCase):
    def test_the_homeserver_answers(self):
        code, out, err = self.run_cli("reach", "--team", bus.TEAM_A, "--root", str(self.root))
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(out, "")
        self.assertIn(self.a.base, err)

    def test_no_answer_names_the_base_url(self):
        url = f"http://127.0.0.1:{bus.free_port()}"
        self.a.write_bundle(base_url=url)
        code, _, err = self.run_cli("reach", "--team", bus.TEAM_A, "--root", str(self.root))
        self.assertNotEqual(code, cli.EXIT_OK)
        self.assertIn(url, err)

    def test_a_team_with_no_seat_is_refused_78(self):
        code, _, err = self.run_cli("reach", "--team", "team-z", "--root", str(self.root))
        self.assertEqual(code, cli.EXIT_CONFIG, err)

    def test_the_team_must_be_named(self):
        code, _, _ = self.run_cli("reach", "--root", str(self.root))
        self.assertEqual(code, cli.EXIT_USAGE)

    def test_no_token_is_sent(self):
        self.run_cli("reach", "--team", bus.TEAM_A, "--root", str(self.root))
        token = self.a.tokens[self.a.me]
        self.assertFalse(any(token in str(record) for record in getattr(self.a.fake, "http_log", [])))
        self.assertTrue(os.path.isdir(self.seat_dir))
