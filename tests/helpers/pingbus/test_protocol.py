"""Tests for the slice of helpers/pingbus/protocol.py that helpers/pingbus/config.py uses.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_protocol

Unit U02 owns protocol.py and its full test suite; these cases pin only the three names
config.py imports (`is_team_name`, `parse_handle`, `is_room_id`), with the grammars of
PROTOCOL.md section 3, so U02's module must keep them.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import protocol


class TestTeamName(unittest.TestCase):
    def test_valid(self) -> None:
        for name in ("a", "team-a", "t1", "a" * 24):
            with self.subTest(name=name):
                self.assertTrue(protocol.is_team_name(name))

    def test_invalid(self) -> None:
        for name in ("", "A", "1a", "-a", "a_b", "a" * 25, "a.b", "../a", None, 7):
            with self.subTest(name=name):
                self.assertFalse(protocol.is_team_name(name))


class TestHandle(unittest.TestCase):
    def test_parts(self) -> None:
        handle = protocol.parse_handle("myrepo.12+workstation.podman")
        self.assertIsNotNone(handle)
        assert handle is not None
        self.assertEqual(
            (handle.repo, handle.n, handle.host, handle.type),
            ("myrepo", 12, "workstation", "podman"),
        )

    def test_every_type(self) -> None:
        for kind in ("podman", "lxc", "docker", "vm", "host"):
            with self.subTest(kind=kind):
                handle = protocol.parse_handle(f"r.1+h.{kind}")
                self.assertIsNotNone(handle)

    def test_invalid(self) -> None:
        for value in (
            "admin",
            "alice",
            "r.0+h.host",
            "r.1+h.bare",
            "R.1+h.host",
            "r.1+-h.host",
            "r.1=h.host",
            None,
        ):
            with self.subTest(value=value):
                self.assertIsNone(protocol.parse_handle(value))


class TestRoomId(unittest.TestCase):
    def test_valid(self) -> None:
        self.assertTrue(protocol.is_room_id("!" + "aZ0_-" * 8 + "abc"))

    def test_invalid(self) -> None:
        for value in ("!" + "a" * 42, "!" + "a" * 44, "#" + "a" * 43, "!" + "a" * 42 + ":", None):
            with self.subTest(value=value):
                self.assertFalse(protocol.is_room_id(value))


if __name__ == "__main__":
    unittest.main()
