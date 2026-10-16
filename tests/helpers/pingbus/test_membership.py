"""Unit tests for helpers/pingbus/membership.py and `config.active_teams` under
`PINGBUS_MEMBERSHIP`.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_membership

Spec: Plan 00203 SPEC-hot-control.md section 3.1 (the desired membership, host-written and
read-only in the session), section 3.2 (the applied membership the seat keeper writes), H4
(pingbus never acts on a team the desired file does not name) and H8 (`active_teams` reads
`PINGBUS_MEMBERSHIP` when it is set: applied ∩ desired, in desired order; `PINGBUS_TEAMS`
only when it is unset).
"""

from __future__ import annotations

import json
import os
import pathlib
import stat
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.pingbus import cli, config, membership
from tests.helpers.pingbus import test_cli as bus

CHECKOUT = "/srv/checkouts/myrepo"


def desired(**changes: object) -> dict:
    data: dict[str, object] = {
        "v": 1, "session": "ccy-myrepo-1", "kind": "ccy", "checkout": CHECKOUT,
        "generation": 7, "written_ms": 1_760_000_000_000,
        "seats": [{"team": "team-b", "seat": "2"}, {"team": "team-a", "seat": "1"}],
        "farewell": [{"team": "team-c", "seat": "3", "reason": "removed"}],
    }
    data.update(changes)
    return data


def applied(**changes: object) -> dict:
    data: dict[str, object] = {
        "v": 1, "generation": 7,
        "seats": [{"team": "team-a", "seat": "1", "handle": "myrepo.1+local.podman", "state": "held"},
                  {"team": "team-b", "seat": "2", "handle": "myrepo.2+local.podman", "state": "held"}],
        "refused": [{"team": "team-c", "seat": "4", "why": "held-elsewhere", "holder": "seat"}],
        "told": {"generation": 7, "roles": [{"team": "team-a", "handle": "myrepo.1+local.podman",
                                             "role": "worker"}]},
    }
    data.update(changes)
    return data


class DesiredParseTest(unittest.TestCase):
    """Section 3.1: every key, its grammar, and the refusals (unknown keys, `v` not 1)."""

    def test_a_full_file_parses(self) -> None:
        got = membership.parse_desired(desired())
        self.assertEqual(got.session, "ccy-myrepo-1")
        self.assertEqual(got.kind, "ccy")
        self.assertEqual(got.checkout, CHECKOUT)
        self.assertEqual(got.generation, 7)
        self.assertEqual(got.seats, (membership.SeatEntry("team-b", "2"), membership.SeatEntry("team-a", "1")))
        self.assertEqual(got.farewell, (membership.Farewell("team-c", "3", "removed"),))

    def test_every_session_name_the_host_writes_is_read(self) -> None:
        for name in ("ccy-proj-1", "_x", "a.b@c+d", "x" * 255):
            with self.subTest(name=name):
                self.assertEqual(membership.parse_desired(desired(session=name)).session, name)

    def test_an_empty_membership_is_valid(self) -> None:
        got = membership.parse_desired(desired(seats=[], farewell=[]))
        self.assertEqual((got.seats, got.farewell), ((), ()))

    def test_the_checkout_is_part_of_a_seat_identity(self) -> None:
        self.assertEqual(membership.parse_desired(desired()).identity(membership.SeatEntry("team-a", "1")),
                         (CHECKOUT, "team-a", "1"))

    def test_a_removal_reason_defaults_to_removed_by_the_owner(self) -> None:
        got = membership.parse_desired(desired())
        self.assertEqual(got.reason_for("team-c", "3"), "removed by the owner")
        self.assertEqual(got.reason_for("team-z", "9"), "removed by the owner")

    def refused(self, data: object, needle: str) -> None:
        with self.assertRaises(membership.MembershipError) as caught:
            membership.parse_desired(data)
        self.assertEqual(caught.exception.EXIT_CODE, 78)
        self.assertIn(needle, str(caught.exception))

    def test_unknown_or_missing_keys_are_refused(self) -> None:
        self.refused(desired(extra=1), "extra")
        data = desired()
        del data["checkout"]
        self.refused(data, "checkout")

    def test_a_version_other_than_1_is_refused(self) -> None:
        for v in (0, 2, "1", True):
            with self.subTest(v=v):
                self.refused(desired(v=v), "v")

    def test_each_key_keeps_its_grammar(self) -> None:
        cases = {
            "session": ("", "-lead", "a/b", "..", ".", "x" * 256, 5),
            "kind": ("podman", "", None),
            "checkout": ("relative/path", "", "/with\nnewline", 1),
            "generation": (0, -1, "7", True, 1.5),
            "written_ms": (-1, "0", None),
            "seats": ("team-a", None, [{"team": "team-a"}], [{"team": "Team", "seat": "1"}],
                      [{"team": "team-a", "seat": "0"}], [{"team": "team-a", "seat": "1", "x": 1}]),
            "farewell": (None, [{"team": "team-a", "seat": "1"}],
                         [{"team": "team-a", "seat": "1", "reason": "kicked"}]),
        }
        for key, values in cases.items():
            for value in values:
                with self.subTest(key=key, value=value):
                    self.refused(desired(**{key: value}), key)

    def test_one_seat_per_team(self) -> None:
        self.refused(desired(seats=[{"team": "team-a", "seat": "1"}, {"team": "team-a", "seat": "2"}]),
                     "one seat per team")

    def test_not_an_object_is_refused(self) -> None:
        for data in (None, [], "x", 1):
            with self.subTest(data=data):
                self.refused(data, "not an object")


class AppliedParseTest(unittest.TestCase):
    """Section 3.2: the keeper's record, private to the session."""

    def test_a_full_file_parses_and_round_trips(self) -> None:
        got = membership.parse_applied(applied())
        self.assertEqual(got.generation, 7)
        self.assertEqual([(s.team, s.seat, s.handle, s.state) for s in got.seats],
                         [("team-a", "1", "myrepo.1+local.podman", "held"),
                          ("team-b", "2", "myrepo.2+local.podman", "held")])
        self.assertEqual(got.refused, (membership.Refused("team-c", "4", "held-elsewhere", "seat"),))
        self.assertEqual(got.told, membership.Told(7, (("team-a", "myrepo.1+local.podman", "worker"),)))
        self.assertEqual(membership.parse_applied(got.to_json()), got)

    def test_told_is_optional_and_generation_0_is_the_launch_list(self) -> None:
        data = applied(generation=0)
        del data["told"]
        got = membership.parse_applied(data)
        self.assertIsNone(got.told)
        self.assertNotIn("told", got.to_json())

    def test_why_is_a_closed_set(self) -> None:
        for why in membership.WHY:
            membership.parse_applied(applied(refused=[{"team": "team-c", "seat": "4", "why": why,
                                                       "holder": "-"}]))
        with self.assertRaises(membership.MembershipError):
            membership.parse_applied(applied(refused=[{"team": "team-c", "seat": "4", "why": "other",
                                                       "holder": "-"}]))

    def test_refusals(self) -> None:
        bad = [
            applied(extra=1), applied(v=2), applied(generation=-1),
            applied(seats=[{"team": "team-a", "seat": "1", "handle": "not a handle", "state": "held"}]),
            applied(seats=[{"team": "team-a", "seat": "1", "handle": "myrepo.1+local.podman", "state": "gone"}]),
            applied(refused=[{"team": "team-c", "seat": "4", "why": "held-elsewhere", "holder": "a b"}]),
            applied(told={"generation": 7, "roles": [{"team": "team-a", "handle": "myrepo.1+local.podman",
                                                      "role": "admin"}]}),
        ]
        for data in bad:
            with self.subTest(data=data), self.assertRaises(membership.MembershipError):
                membership.parse_applied(data)


class FilesTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))

    def test_the_desired_file_is_read_from_its_directory(self) -> None:
        (self.tmp / membership.DESIRED_FILE).write_text(json.dumps(desired()), encoding="utf-8")
        self.assertEqual(membership.read_desired(self.tmp).generation, 7)

    def test_a_missing_desired_file_is_refused(self) -> None:
        with self.assertRaises(membership.MembershipError) as caught:
            membership.read_desired(self.tmp)
        self.assertIn(membership.DESIRED_FILE, str(caught.exception))

    def test_a_symlinked_or_malformed_or_duplicate_key_file_is_refused(self) -> None:
        path = self.tmp / membership.DESIRED_FILE
        other = self.tmp / "other.json"
        other.write_text(json.dumps(desired()), encoding="utf-8")
        path.symlink_to(other)
        with self.assertRaises(membership.MembershipError):
            membership.read_desired(self.tmp)
        path.unlink()
        for text in ("{", '{"v": 1, "v": 1}', "x" * (membership.MAX_FILE_BYTES + 1)):
            path.write_text(text, encoding="utf-8")
            with self.subTest(text=text[:20]), self.assertRaises(membership.MembershipError):
                membership.read_desired(self.tmp)

    def test_applied_is_written_atomically_private_and_read_back(self) -> None:
        record = membership.parse_applied(applied())
        membership.write_applied(self.tmp / membership.APPLIED_FILE, record)
        path = self.tmp / membership.APPLIED_FILE
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(membership.read_applied(self.tmp), record)
        self.assertEqual([p.name for p in self.tmp.iterdir()], [membership.APPLIED_FILE])

    def test_no_applied_file_is_none(self) -> None:
        self.assertIsNone(membership.read_applied(self.tmp))


class ActiveTest(unittest.TestCase):
    """H8: applied ∩ desired, in desired order; H4: a forged applied seat is never active."""

    def test_the_intersection_in_desired_order(self) -> None:
        self.assertEqual(membership.active(membership.parse_desired(desired()),
                                           membership.parse_applied(applied())),
                         ("team-b", "team-a"))

    def test_a_seat_applied_but_not_desired_is_not_active(self) -> None:
        forged = applied(seats=[*applied()["seats"],
                                {"team": "team-x", "seat": "1", "handle": "myrepo.1+local.podman",
                                 "state": "held"}])
        self.assertNotIn("team-x", membership.active(membership.parse_desired(desired()),
                                                     membership.parse_applied(forged)))

    def test_a_different_seat_of_a_desired_team_is_not_active(self) -> None:
        other = applied(seats=[{"team": "team-a", "seat": "9", "handle": "myrepo.9+local.podman",
                                "state": "held"}])
        self.assertEqual(membership.active(membership.parse_desired(desired()),
                                           membership.parse_applied(other)), ())

    def test_a_desired_seat_not_yet_applied_is_not_active(self) -> None:
        self.assertEqual(membership.active(membership.parse_desired(desired()), None), ())


class ConfigActiveTeamsTest(unittest.TestCase):
    """H8 through `config.active_teams`, the one reader every command goes through."""

    def setUp(self) -> None:
        tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.mount = tmp / "membership"
        self.mount.mkdir()
        self.home = tmp / "home"
        self.home.mkdir()
        self.env = {membership.MEMBERSHIP_VAR: str(self.mount), "PINGBUS_HOME": str(self.home)}

    def write(self, desired_data: dict, applied_data: dict | None) -> None:
        (self.mount / membership.DESIRED_FILE).write_text(json.dumps(desired_data), encoding="utf-8")
        if applied_data is not None:
            (self.home / membership.APPLIED_FILE).write_text(json.dumps(applied_data), encoding="utf-8")

    def test_membership_wins_over_pingbus_teams(self) -> None:
        self.write(desired(), applied())
        env = dict(self.env, PINGBUS_TEAMS="team-z")
        self.assertEqual(config.active_teams(env), ("team-b", "team-a"))

    def test_unset_reads_pingbus_teams_as_before(self) -> None:
        self.assertEqual(config.active_teams({"PINGBUS_TEAMS": "team-a"}), ("team-a",))

    def test_an_empty_membership_has_no_active_team(self) -> None:
        self.write(desired(seats=[], farewell=[]), applied(seats=[], refused=[]))
        with self.assertRaises(config.ConfigError) as caught:
            config.active_teams(self.env)
        self.assertIn("no team", str(caught.exception))

    def test_no_applied_file_yet_has_no_active_team(self) -> None:
        self.write(desired(), None)
        with self.assertRaises(config.ConfigError):
            config.active_teams(self.env)

    def test_off_relative_or_malformed_is_refused_78(self) -> None:
        for value, needle in (("off", "--no-bus"), ("relative", "absolute")):
            with self.subTest(value=value), self.assertRaises(config.ConfigError) as caught:
                config.active_teams(dict(self.env, **{membership.MEMBERSHIP_VAR: value}))
            self.assertIn(needle, str(caught.exception))
        self.write(desired(v=2), applied())
        with self.assertRaises(config.ConfigError):
            config.active_teams(self.env)
        self.write(desired(), applied(v=2))
        with self.assertRaises(config.ConfigError):
            config.active_teams(self.env)

    def test_a_team_not_desired_is_not_a_member_64(self) -> None:
        self.write(desired(), applied())
        with self.assertRaises(config.UsageError) as caught:
            config.select_teams(config.active_teams(self.env), "team-x")
        self.assertIn("not a member of `team-x`", str(caught.exception))


class ForgedAppliedCliTest(bus.BusCase):
    """H4 end to end: a forged `applied.json` naming an extra seat gives "not a member"
    (exit 64) on `send --team`, and that team is never loaded (so no watcher thread)."""

    def setUp(self) -> None:
        super().setUp()
        self.add_team()
        self.mount = self.tmp / "membership"
        self.mount.mkdir()
        (self.mount / membership.DESIRED_FILE).write_text(json.dumps(desired(
            seats=[{"team": bus.TEAM_A, "seat": "1"}], farewell=[])), encoding="utf-8")
        forged = applied(seats=[
            {"team": bus.TEAM_A, "seat": "1", "handle": bus.ME_LP, "state": "held"},
            {"team": bus.TEAM_B, "seat": "1", "handle": bus.ME_LP, "state": "held"}], refused=[])
        (self.home / membership.APPLIED_FILE).write_text(json.dumps(forged), encoding="utf-8")
        self.env = {k: v for k, v in self.env.items() if k != "PINGBUS_TEAMS"}
        self.env[membership.MEMBERSHIP_VAR] = str(self.mount)

    def test_send_to_the_forged_team_is_refused_64(self) -> None:
        code, out, err = self.run_cli("--team", bus.TEAM_B, "send", "ack", "--to", bus.ORCH_LP,
                                      "--re", bus.UNKNOWN_EVENT)
        self.assertEqual(code, cli.EXIT_USAGE, err)
        self.assertEqual(out, "")
        self.assertIn(f"not a member of `{bus.TEAM_B}`", err)
        self.assertEqual(self.b.messages_from_me(), [])

    def test_only_the_desired_team_is_loaded(self) -> None:
        members = config.load_active(self.env)
        self.assertEqual([member.team for member in members], [bus.TEAM_A])


class ConstantsTest(unittest.TestCase):
    def test_the_names(self) -> None:
        self.assertEqual(membership.MEMBERSHIP_VAR, "PINGBUS_MEMBERSHIP")
        self.assertEqual(membership.DESIRED_FILE, "membership.json")
        self.assertEqual(membership.APPLIED_FILE, "applied.json")
        self.assertEqual(membership.FLAG_FILE, "active")
        self.assertEqual(membership.WHY, ("held-elsewhere", "no-bundle", "bundle-invalid", "not-desired"))
        self.assertTrue(os.path.isabs(membership.CCY_MOUNT))


if __name__ == "__main__":
    unittest.main()
