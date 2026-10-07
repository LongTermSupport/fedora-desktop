"""Unit tests for `pingbus status` and `pingbus inbox` (helpers/pingbus/cli.py), the room
view they read (`state/room.json`, written by the syncer), and the wakers' `listening`
status.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_cli_status

Spec: docs/agent-bus-protocol.md §8 (status: own state key, role holders only, stale),
§12 (the state directory, the lock, a waker), §13 (`status`, `inbox`: offline), §15 (every
field printed only after it passed its grammar). Plan 00161's DESIGN.md section 4 (joined
members who are neither humans, role holders nor `admin` are reported), section 6
(liveness is a lock, never a PID; `status` says which wake path is live) and section 12
row U12.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import re
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, inbox, protocol
from tests.helpers.pingbus import test_cli as bus

MARKER = "MARKER-never-printed"
TEAM_RE = r"[a-z][a-z0-9-]{0,23}"
LOCALPART_RE = rf"(?:{protocol.HANDLE_PATTERN}|{protocol.HUMAN_LOCALPART_PATTERN})"
USER_RE = r"@[\x21-\x7e]{1,254}"
COUNT_RE = r"(?:\d+|-)"
LINE_RES = {
    "TEAM": re.compile(
        rf"TEAM\t{TEAM_RE}\t{protocol.HANDLE_PATTERN}\ttrust=(?:trusted|untrusted)"
        rf"\twake=(?:watcher|waiter|none)\tpending={COUNT_RE}\thumans={COUNT_RE}\tpings={COUNT_RE}"
        rf"\toverdue=\d+\tdropped=\d+\tunexpected={COUNT_RE}"),
    "UNTRUSTED": re.compile(rf"UNTRUSTED\t{TEAM_RE}\t[\x20-\x7e]{{1,200}}"),
    "MEMBER": re.compile(
        rf"MEMBER\t{TEAM_RE}\t{LOCALPART_RE}\t(?:human|orchestrator|worker)\t(?:listening|stale|-)"),
    "UNEXPECTED": re.compile(rf"UNEXPECTED\t{TEAM_RE}\t{USER_RE}\t(?:join|invite)"),
    "PENDING": re.compile(
        rf"PENDING\t{TEAM_RE}\t{protocol.EVENT_ID_PATTERN}\t{LOCALPART_RE}\t\d+"
        rf"\t(?:human|{'|'.join(protocol.VERBS)})"),
    "SEAT": re.compile(
        rf"SEAT\t{TEAM_RE}\t(?:{protocol.HANDLE_SEAT_PATTERN})\t(?:held|free)\t(?:self|-)"
        rf"\t{protocol.HANDLE_PATTERN}"),
}


class StatusCase(bus.BusCase):
    def report(self, command: str = "status", **kwargs) -> tuple[int, list[list[str]], str]:
        code, out, err = self.run_cli(command, **kwargs)
        self.assertNotIn(MARKER, out + err)
        for line in out.splitlines():
            kind = line.split("\t")[0]
            self.assertIn(kind, LINE_RES, line)
            self.assertRegex(line, LINE_RES[kind])
            self.assertTrue(LINE_RES[kind].fullmatch(line), line)
        return code, [line.split("\t") for line in out.splitlines()], err

    def team_line(self, rows: list[list[str]], team: str = bus.TEAM_A) -> dict[str, str]:
        (row,) = [r for r in rows if r[0] == "TEAM" and r[1] == team]
        return dict(field.split("=", 1) for field in row[3:]) | {"handle": row[2]}

    def members(self, rows: list[list[str]]) -> dict[str, tuple[str, str]]:
        return {r[2]: (r[3], r[4]) for r in rows if r[0] == "MEMBER"}

    def put_status(self, uid: str, until_ms: int, key: str | None = None) -> None:
        a = self.a
        a.call(uid, "PUT", f"/_matrix/client/v3/rooms/{bus.q(a.room)}/state/"
               f"{protocol.EVENT_STATUS}/{bus.q(key if key is not None else uid)}",
               {"v": 1, "state": "listening", "until": until_ms})

    def outsider_joins(self) -> str:
        a = self.a
        mallory = a.fake.add_user("mallory")
        a.tokens[mallory] = a.fake.mint_token(mallory)
        a.call(a.admin, "POST", f"/_matrix/client/v3/rooms/{bus.q(a.room)}/invite", {"user_id": mallory})
        a.call(mallory, "POST", f"/_matrix/client/v3/rooms/{bus.q(a.room)}/join", {})
        return mallory


class StatusTest(StatusCase):
    def test_members_and_roles_from_the_cached_record(self):
        self.joined()
        code, rows, _ = self.report()
        self.assertEqual(code, cli.EXIT_OK)
        team = self.team_line(rows)
        self.assertEqual((team["handle"], team["trust"], team["wake"]), (bus.ME_LP, "trusted", "none"))
        self.assertEqual(self.members(rows), {
            "alice": ("human", "-"),
            bus.ME_LP: ("worker", "-"),
            bus.ORCH_LP: ("orchestrator", "-"),
            bus.PEER_LP: ("worker", "-"),
        })

    def test_status_is_offline(self):
        self.joined()
        self.a.fake.http_log.clear()
        self.report()
        self.assertEqual(self.a.fake.http_log, [])

    def test_wake_path_from_the_lock(self):
        self.joined()
        for kind, shown in (("watch", "watcher"), ("wait", "waiter"), ("recv", "none")):
            with inbox.acquire_lock(self.a.state, kind):
                self.assertEqual(self.team_line(self.report()[1])["wake"], shown, kind)
        self.assertEqual(self.team_line(self.report()[1])["wake"], "none")

    def test_a_stale_lock_file_and_pid_file_mean_nothing(self):
        self.joined()
        (self.a.state.path / inbox.LOCK_FILE).write_text("watch", encoding="ascii")
        (self.a.state.path / "watch.pid").write_text(str(os.getpid()), encoding="ascii")
        self.assertEqual(self.team_line(self.report()[1])["wake"], "none")

    def test_pending_counts_without_text(self):
        self.joined()
        self.a.state.commit_batch([self.stored_human(bus.UNKNOWN_EVENT, MARKER)], self.a.state.sync_token())
        team = self.team_line(self.report()[1])
        self.assertEqual((team["pending"], team["humans"], team["pings"]), ("1", "1", "0"))

    def test_member_status_listening_then_stale(self):
        self.joined()
        until = int(self.now * 1000) + 60_000
        self.put_status(self.a.orch, until)
        self.put_status(self.a.peer, until, key="")
        self.run_cli("recv")
        members = self.members(self.report()[1])
        self.assertEqual(members[bus.ORCH_LP], ("orchestrator", "listening"))
        self.assertEqual(members[bus.PEER_LP], ("worker", "-"), "a status under a foreign key is ignored")
        self.now += 61
        self.assertEqual(self.members(self.report()[1])[bus.ORCH_LP], ("orchestrator", "stale"))

    def test_unexpected_members_are_reported(self):
        self.joined()
        mallory = self.outsider_joins()
        self.run_cli("recv")
        _, rows, _ = self.report()
        self.assertEqual([r[2:] for r in rows if r[0] == "UNEXPECTED"], [[mallory, "join"]])
        self.assertEqual(self.team_line(rows)["unexpected"], "1")

    def test_members_joined_before_this_one_are_known(self):
        mallory = self.outsider_joins()
        self.joined()
        _, rows, _ = self.report()
        self.assertEqual([r[2] for r in rows if r[0] == "UNEXPECTED"], [mallory])

    def test_untrusted_room_says_why(self):
        self.joined()
        self.a.set_record(dict(self.a.record, roles={self.a.orch: "orchestrator"}))
        self.assertEqual(self.run_cli("recv")[0], cli.EXIT_UNTRUSTED)
        code, rows, _ = self.report()
        self.assertEqual(code, cli.EXIT_OK)
        team = self.team_line(rows)
        self.assertEqual((team["trust"], team["pending"], team["unexpected"]), ("untrusted", "-", "-"))
        (reason,) = [r for r in rows if r[0] == "UNTRUSTED"]
        self.assertIn("role", reason[2])
        self.assertEqual(self.members(rows), {})

    def test_before_the_first_sync_the_room_is_not_yet_trusted(self):
        code, rows, _ = self.report()
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(self.team_line(rows)["trust"], "untrusted")

    def test_overdue_acks_and_drops_are_counted(self):
        self.joined()
        self.run_cli("send", "review", bus.PATH_REF, "--to", bus.ORCH_LP)
        self.a.send(self.a.peer, {"msgtype": "m.text", "body": "agent free text"})
        self.run_cli("recv")
        self.now += bus.limits.DEFAULTS["ack_timeout_s"] + 1
        team = self.team_line(self.report()[1])
        self.assertEqual((team["overdue"], team["dropped"]), ("1", "1"))

    def test_every_active_team_is_reported(self):
        self.add_team()
        self.joined()
        _, rows, _ = self.report()
        self.assertEqual(sorted(r[1] for r in rows if r[0] == "TEAM"), [bus.TEAM_A, bus.TEAM_B])

    def test_a_planted_room_view_with_a_bad_user_id_is_refused(self):
        self.joined()
        path = self.a.state.path / inbox.ROOM_FILE
        view = json.loads(path.read_text(encoding="ascii"))
        view["members"]["@evil\tline:" + bus.SN_A] = "join"
        path.write_text(json.dumps(view), encoding="ascii")
        code, out, err = self.run_cli("status")
        self.assertEqual((code, out), (cli.EXIT_CONFIG, ""))
        self.assertIn(inbox.ROOM_FILE, err)

    def test_one_bad_team_does_not_hide_the_others(self):
        """Team A's state is unreadable: it is reported on stderr, team B is still printed,
        and the exit is the first failure's code."""
        self.add_team()
        self.joined()
        (self.a.state.path / inbox.ROOM_FILE).write_text("not json", encoding="ascii")
        code, rows, err = self.report()
        self.assertEqual(code, cli.EXIT_CONFIG)
        self.assertEqual([r[1] for r in rows if r[0] == "TEAM"], [bus.TEAM_B])
        self.assertIn(f"team {bus.TEAM_A}", err)

    def test_a_planted_status_for_a_non_member_is_not_printed(self):
        self.joined()
        path = self.a.state.path / inbox.ROOM_FILE
        view = json.loads(path.read_text(encoding="ascii"))
        view["statuses"][f"@mallory:{bus.SN_A}"] = int(self.now * 1000) + 60_000
        path.write_text(json.dumps(view), encoding="ascii")
        _, rows, _ = self.report()
        self.assertNotIn("mallory", self.members(rows))


class RoomViewTest(StatusCase):
    def test_the_first_sync_records_every_joined_member(self):
        self.joined()
        members, statuses = self.a.state.room_view()
        for uid in (self.a.admin, self.a.me, self.a.orch, self.a.peer, self.a.alice):
            self.assertEqual(members.get(uid), "join", uid)
        self.assertEqual(statuses, {})

    def test_a_member_who_leaves_is_forgotten(self):
        self.joined()
        self.a.call(self.a.peer, "POST", f"/_matrix/client/v3/rooms/{bus.q(self.a.room)}/leave", {})
        self.run_cli("recv")
        members, _ = self.a.state.room_view()
        self.assertNotIn(self.a.peer, members)

    def test_statuses_survive_into_a_new_process(self):
        self.joined()
        until = int(self.now * 1000) + 60_000
        self.put_status(self.a.orch, until)
        self.run_cli("recv")
        self.run_cli("recv")
        self.assertEqual(self.a.state.room_view()[1], {self.a.orch: until})


class InboxTest(StatusCase):
    def test_inbox_lists_without_consuming_and_never_shows_text(self):
        self.joined()
        ping = self.a.ping(self.a.orch, "review", ref=bus.PATH_REF)
        human = self.a.human(MARKER)
        (member,) = cli.config.load_active(self.env)
        cli._seat(member, self.env, self.runtime(), io.StringIO()).syncer.sync_once(0)
        self.a.fake.http_log.clear()
        code, rows, _ = self.report("inbox")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(self.a.fake.http_log, [], "inbox is offline")
        self.assertEqual(sorted((r[2], r[3], r[5]) for r in rows),
                         sorted([(ping, bus.ORCH_LP, "review"), (human, "alice", "human")]))
        code, out, _ = self.run_cli("recv")
        self.assertEqual(len(out.splitlines()), 2, "inbox consumed nothing")

    def test_an_empty_inbox_prints_nothing(self):
        self.joined()
        code, rows, _ = self.report("inbox")
        self.assertEqual((code, rows), (cli.EXIT_OK, []))


class SeatStatusTest(StatusCase):
    """DESIGN.md section 5.5 "Status": one `SEAT` line per seat of the checkout (team, seat,
    `held`/`free`, `self`/`-`, handle), offline, after the teams."""

    def setUp(self) -> None:
        super().setUp()
        self.root = self.tmp / "checkout" / "seats"
        own = self.root / bus.TEAM_A / "dev1"
        own.parent.mkdir(parents=True)
        os.rename(self.home / bus.TEAM_A, own)
        own.chmod(0o700)
        (self.home / bus.TEAM_A).symlink_to(own)
        self.sibling = self.root / bus.TEAM_A / "dev2"
        self.sibling.mkdir(mode=0o700)
        member = json.loads((own / "member.json").read_text(encoding="utf-8"))
        member["user_id"] = f"@myrepo.dev2+local.podman:{bus.SN_A}"
        (self.sibling / "member.json").write_text(json.dumps(member), encoding="utf-8")

    def seat_rows(self, rows: list[list[str]]) -> list[list[str]]:
        return [r[1:] for r in rows if r[0] == "SEAT"]

    def test_every_seat_with_its_lock_and_self(self):
        self.joined()
        code, rows, err = self.report()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.seat_rows(rows), [
            [bus.TEAM_A, "dev1", "free", "self", bus.ME_LP],
            [bus.TEAM_A, "dev2", "free", "-", "myrepo.dev2+local.podman"],
        ])
        self.assertEqual(rows[-1][0], "SEAT", "the SEAT lines follow the teams")
        with inbox.acquire_lock_at(self.sibling / "seat.lock", inbox.SEAT_KIND, claimed_ms=1):
            self.assertEqual(self.seat_rows(self.report()[1])[1][2], "held")

    def test_a_seat_with_a_broken_bundle_is_reported_and_the_rest_still_print(self):
        self.joined()
        (self.sibling / "member.json").write_text("{", encoding="utf-8")
        code, rows, err = self.report()
        self.assertEqual(code, cli.EXIT_CONFIG)
        self.assertEqual([r[1] for r in self.seat_rows(rows)], ["dev1"])
        self.assertIn("seat dev2@team-a", err)

    def test_no_seats_directory_prints_no_seat_line(self):
        (self.home / bus.TEAM_A).unlink()
        os.rename(self.root / bus.TEAM_A / "dev1", self.home / bus.TEAM_A)
        for path in (self.sibling / "member.json",):
            path.unlink()
        self.sibling.rmdir()
        (self.root / bus.TEAM_A).rmdir()
        self.root.rmdir()
        self.joined()
        self.assertEqual(self.seat_rows(self.report()[1]), [])


class WakerStatusTest(StatusCase):
    def test_wait_publishes_listening_until_its_deadline(self):
        self.joined()
        code, _, _ = self.run_cli("wait", "--timeout", "1")
        self.assertEqual(code, cli.EXIT_NOTHING)
        event = self.a.fake.rooms[self.a.room].state[(protocol.EVENT_STATUS, self.a.me)]
        self.assertEqual(event.content, {"v": 1, "state": "listening", "until": int(self.now * 1000) + 1000})

    def test_wait_that_delivers_at_once_publishes_nothing(self):
        self.joined()
        self.a.human()
        self.assertEqual(self.run_cli("wait", "--timeout", "5")[0], cli.EXIT_OK)
        self.assertNotIn((protocol.EVENT_STATUS, self.a.me), self.a.fake.rooms[self.a.room].state)


if __name__ == "__main__":
    unittest.main()
