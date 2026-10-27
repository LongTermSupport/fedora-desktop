"""Unit tests for the texts pingbus names for the plan checkers that read them (Plan 00201
Task 2.24): each named text keeps the bytes the product printed when it was inline.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_named_texts

A plan checker reads these through Plan 00201's `m7_check.py product-value`, never a copy
(QA.md checker-copies-cited-constant). The messages built from them are pinned whole by
test_seat, test_cli_infer, test_config and test_statusline; this file pins each name.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.pingbus import cli, config, inbox, seat, statusline


class NamedTextsKeepTheirBytes(unittest.TestCase):
    def test_cli(self) -> None:
        self.assertEqual(
            cli.CHOICES_HEAD,
            "several teams are active and --to/--re fit more than one (or none): name one with --team:")
        self.assertEqual(cli.WATCHER_EXITS, "the watcher exits")
        self.assertEqual(cli.WATCHER_ENDED, "the watcher ended")
        self.assertEqual(cli.WATCHER_REFUSED, "the watcher was refused")
        self.assertEqual(cli.WATCHER_RESTARTED, "starting it again")
        self.assertEqual(cli.TEAM_NOT_WATCHED, "no longer watched")
        self.assertEqual(cli.HOMESERVER_SILENT, "the homeserver does not answer")

    def test_inbox(self) -> None:
        self.assertEqual(inbox.NO_RECORD_YET, "no verified team record yet")

    def test_seat(self) -> None:
        self.assertEqual(seat.HELD_TEXT, "is held by another session")
        self.assertEqual(seat.ONE_PER_TEAM_TEXT, "one seat per team per session")
        self.assertEqual(seat.EMPTY_ITEM_TEXT, "a leading, doubled or trailing comma")

    def test_config(self) -> None:
        self.assertEqual(config.ELEMENT_REFUSAL, "a host member must not run as a user with an Element profile")
        self.assertEqual(config.DEFAULT_HOME_DIR, ".config/pingbus")

    def test_statusline(self) -> None:
        self.assertEqual(statusline.DOWN_MARK, "(down)")


class MessagesAreMadeFromThem(unittest.TestCase):
    def refusal(self, text: str) -> str:
        with self.assertRaises(seat.SeatListError) as caught:
            seat.parse_seat_list(text)
        return str(caught.exception)

    def test_seat_refusals(self) -> None:
        self.assertIn(seat.ONE_PER_TEAM_TEXT, self.refusal("dev1@t,dev2@t"))
        self.assertIn(f"({seat.EMPTY_ITEM_TEXT})", self.refusal("dev1@t,"))
        self.assertEqual(str(seat.SeatHeld(seat.SeatRef("qa2", "team-b"))),
                         f"seat `qa2@team-b` {seat.HELD_TEXT}")

    def test_default_home(self) -> None:
        self.assertEqual(config.resolve_home({"HOME": "/h"}), pathlib.Path("/h") / config.DEFAULT_HOME_DIR)
        self.assertEqual(config.member_env_file({"HOME": "/h"}), pathlib.Path("/h/.config/pingbus/env"))


if __name__ == "__main__":
    unittest.main()
