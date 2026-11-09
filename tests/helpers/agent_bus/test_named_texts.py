"""Unit tests for the texts agent-bus names for the plan checkers that read them (Plan 00201
Task 2.24): each named text keeps the bytes the product printed when it was inline.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_named_texts

Plan 00203's robustness acceptance reads these through Plan 00201's `m7_check.py
product-value`, never a copy (QA.md checker-copies-cited-constant). test_health and
test_checkout pin the whole lines; this file pins each name and that the line is made from it.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.agent_bus import checkout, health


class NamedTextsKeepTheirBytes(unittest.TestCase):
    def test_health(self) -> None:
        self.assertEqual(health.PROBLEM_MARK, "PROBLEM")
        self.assertEqual(health.RESTARTED_TEXT, "systemd restarted the homeserver")
        self.assertEqual(health.NEWEST_BACKUP_TEXT, "the newest backup is")
        self.assertEqual(health.BACKUP_TAR_PREFIX, "agent-bus-state-")

    def test_checkout(self) -> None:
        self.assertEqual(checkout.STARTING_TEXT, "is starting")
        self.assertEqual(checkout.UP_AFTER_TEXT, "is up after")


class LinesAreMadeFromThem(unittest.TestCase):
    def test_restart_event(self) -> None:
        hs = {"NRestarts": "2", "Result": "watchdog"}
        events, _ = health.restart_events(None, hs, 1_000_000.0)
        self.assertEqual(events[0]["what"], f"{health.RESTARTED_TEXT} 2 times since it was last started by hand "
                                            "(last cause: watchdog)")


if __name__ == "__main__":
    unittest.main()
