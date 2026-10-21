"""Unit tests for the side rooms of helpers/agent_bus/admin.py (Plan 00211 SPEC §3, §4, §5.1,
§5.4, §5.5): the reports room for every team, the questions room once the team has a
coordinator, the index and back-links, adoption after a crash, kicks and invites.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_admin_side_rooms

Against the same fake homeserver and `Stubbed` kicks as test_admin.py.
"""

from __future__ import annotations

import io
import pathlib
import stat
import sys
import tarfile
import unittest
import urllib.parse

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import admin, registry
from helpers.pingbus import protocol
from tests.helpers.agent_bus.test_admin import ADMIN_ID, ALICE, BOB, SN, AdminTestCase, read_member, team_data

REPORTS = protocol.PURPOSE_REPORTS
QUESTIONS = protocol.PURPOSE_QUESTIONS
WORKER = "myrepo.1+workstation.podman"
SECOND = "myrepo.2+workstation.podman"


def uid(handle: str) -> str:
    return f"@{handle}:{SN}"


def q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


class SideRoomsCase(AdminTestCase):
    def side(self, purpose: str) -> str:
        return (self.team_dir / f"room_id.{purpose}").read_text(encoding="utf-8")

    def has_side(self, purpose: str) -> bool:
        return (self.team_dir / f"room_id.{purpose}").exists()

    def room_state(self, room: str, event_type: str, key: str = "", event: bool = False) -> tuple[int, dict]:
        return self.fake.request("GET", f"/_matrix/client/v3/rooms/{q(room)}/state/{q(event_type)}/{q(key)}",
                                 {"format": "event"} if event else None, token=self.admin_token())

    def room_membership(self, room: str, user_id: str) -> str | None:
        status, content = self.room_state(room, "m.room.member", user_id)
        return content.get("membership") if status == 200 else None

    def index(self) -> dict:
        status, content = self.room_state(self.room(), protocol.EVENT_ROOMS)
        return content if status == 200 else {}

    def as_admin(self, method: str, path: str, body: object = None) -> tuple[int, dict]:
        return self.fake.request(method, path, None, body, token=self.admin_token())

    def member(self, role: str = "worker") -> tuple[str, str]:
        """A new member; returns its handle and token."""
        bundle = admin.add_member(self.ctx(), self.transport, repo="myrepo", host="workstation", type_="podman",
                                  role=role, address="192.0.2.10", human_text=True)
        return bundle.handle, read_member(tarfile.open(fileobj=io.BytesIO(bundle.tar)), "token").decode()

    def join(self, room: str, token: str) -> int:
        return self.fake.request("POST", f"/_matrix/client/v3/rooms/{q(room)}/join", body={}, token=token)[0]

    def kick_reasons(self) -> list[tuple[str, str, str]]:
        return [(urllib.parse.unquote(c[1].split("/rooms/")[1].split("/")[0]), c[3]["user_id"], c[3]["reason"])
                for c in self.transport.calls if c[0] == "POST" and c[1].endswith("/kick")]

    def create_calls(self) -> int:
        return sum(1 for c in self.transport.calls if c[0] == "POST" and c[1].endswith("/createRoom"))


class ReportsRoomTest(SideRoomsCase):
    def test_bootstrap_creates_the_reports_room_exactly(self) -> None:
        changes = self.bootstrap()
        self.assertIn("reports room", changes)
        reports = self.side(REPORTS)
        self.assertTrue(protocol.is_room_id(reports))
        self.assertNotEqual(reports, self.room())
        self.assertEqual(stat.S_IMODE((self.team_dir / "room_id.reports").stat().st_mode), 0o600)
        _, create = self.room_state(reports, "m.room.create", event=True)
        self.assertEqual(create["sender"], ADMIN_ID)
        self.assertEqual(create["content"]["room_version"], protocol.ROOM_VERSION)
        self.assertNotIn("additional_creators", create["content"])
        _, link = self.room_state(reports, protocol.EVENT_ROOM_LINK, event=True)
        protocol.check_room_link(link, ADMIN_ID, "team-a", REPORTS, self.room())
        _, levels = self.room_state(reports, "m.room.power_levels")
        protocol.check_side_power_levels(levels, REPORTS, [ALICE], False)
        _, rules = self.room_state(reports, protocol.JOIN_RULES)
        protocol.check_join_rules(rules, REPORTS, self.room())
        self.assertEqual(self.room_state(reports, "m.room.history_visibility")[1], {"history_visibility": "shared"})
        self.assertEqual(self.room_state(reports, "m.room.name")[1], {"name": "team-a · reports"})
        self.assertEqual(self.room_state(reports, "m.room.topic")[1]["topic"], admin.REPORTS_TOPIC)
        self.assertEqual(self.room_membership(reports, ALICE), "invite")
        _, index_event = self.room_state(self.room(), protocol.EVENT_ROOMS, event=True)
        self.assertEqual(protocol.parse_rooms_index(index_event, ADMIN_ID, self.room()), {REPORTS: reports})
        self.assertFalse(self.has_side(QUESTIONS))

    def test_a_second_run_changes_nothing(self) -> None:
        self.bootstrap()
        before = len(self.transport.calls)
        self.assertEqual(self.bootstrap(), [])
        self.assertEqual([c for c in self.transport.calls[before:] if c[0] != "GET"], [])

    def test_a_main_room_member_joins_reports_by_the_restricted_rule(self) -> None:
        self.bootstrap()
        handle, token = self.member()
        self.assertEqual(self.join(self.room(), token), 200)
        self.assertEqual(self.join(self.side(REPORTS), token), 200)
        self.assertEqual(self.room_membership(self.side(REPORTS), uid(handle)), "join")

    def test_drift_in_a_side_room_is_repaired_and_named(self) -> None:
        self.bootstrap()
        reports = self.side(REPORTS)
        path = f"/_matrix/client/v3/rooms/{q(reports)}/state/"
        self.assertEqual(self.as_admin("PUT", path + "m.room.power_levels/", {"users_default": 100})[0], 200)
        self.assertEqual(self.as_admin("PUT", path + "m.room.join_rules/", {"join_rule": "invite"})[0], 200)
        self.assertEqual(self.as_admin("PUT", path + "m.room.topic/", {"topic": "x"})[0], 200)
        changes = self.bootstrap()
        self.assertEqual(sorted(changes), ["reports join rules", "reports power levels", "reports topic"])
        _, levels = self.room_state(reports, "m.room.power_levels")
        protocol.check_side_power_levels(levels, REPORTS, [ALICE], False)
        self.assertEqual(self.bootstrap(), [])

    def test_a_human_added_later_is_invited_to_reports_with_power_0(self) -> None:
        self.bootstrap()
        self.write_team(team_data(humans=["alice", "bob"]))
        changes = self.bootstrap()
        self.assertIn(f"invited {BOB} to reports", changes)
        _, levels = self.room_state(self.side(REPORTS), "m.room.power_levels")
        protocol.check_side_power_levels(levels, REPORTS, [ALICE, BOB], False)


class AdoptionTest(SideRoomsCase):
    """A crash between `createRoom` and the ID file's write (SPEC §5.1 step 1)."""

    def setUp(self) -> None:
        super().setUp()
        self.bootstrap()
        self.reports = self.side(REPORTS)
        (self.team_dir / "room_id.reports").unlink()

    def test_the_room_the_index_names_is_adopted(self) -> None:
        creates = self.create_calls()
        changes = self.bootstrap()
        self.assertEqual(changes, [f"adopted the reports room {self.reports}"])
        self.assertEqual(self.side(REPORTS), self.reports)
        self.assertEqual(self.create_calls(), creates)

    def test_a_room_the_index_does_not_name_is_found_through_joined_rooms(self) -> None:
        self.assertEqual(self.as_admin("PUT", f"/_matrix/client/v3/rooms/{q(self.room())}/state/"
                                       f"{protocol.EVENT_ROOMS}/", {"v": 1})[0], 200)
        creates = self.create_calls()
        changes = self.bootstrap()
        self.assertEqual(changes, [f"adopted the reports room {self.reports}", "rooms index"])
        self.assertEqual(self.side(REPORTS), self.reports)
        self.assertEqual(self.create_calls(), creates)

    def test_two_candidates_are_refused_naming_both(self) -> None:
        self.assertEqual(self.as_admin("PUT", f"/_matrix/client/v3/rooms/{q(self.room())}/state/"
                                       f"{protocol.EVENT_ROOMS}/", {"v": 1})[0], 200)
        link = {"v": 1, "team": "team-a", "purpose": REPORTS, "main": self.room()}
        status, made = self.as_admin("POST", "/_matrix/client/v3/createRoom", {
            "preset": "private_chat", "room_version": protocol.ROOM_VERSION,
            "initial_state": [{"type": protocol.EVENT_ROOM_LINK, "state_key": "", "content": link}]})
        self.assertEqual(status, 200)
        with self.assertRaises(admin.AdminError) as caught:
            self.bootstrap()
        self.assertIn(self.reports, str(caught.exception))
        self.assertIn(made["room_id"], str(caught.exception))
        self.assertFalse(self.has_side(REPORTS))

    def test_an_id_file_naming_a_room_without_the_back_link_is_refused(self) -> None:
        admin.write_private(self.team_dir / "room_id.reports", self.room())
        with self.assertRaisesRegex(admin.AdminError, "reports"):
            self.bootstrap()


class QuestionsRoomTest(SideRoomsCase):
    def setUp(self) -> None:
        super().setUp()
        self.bootstrap()

    def assert_open(self, coordinator: str) -> None:
        room = self.side(QUESTIONS)
        _, levels = self.room_state(room, "m.room.power_levels")
        protocol.check_side_power_levels(levels, QUESTIONS, [ALICE], True)
        self.assertEqual(self.room_state(room, "m.room.topic")[1]["topic"], admin.questions_topic([coordinator]))

    def assert_closed(self) -> None:
        room = self.side(QUESTIONS)
        _, levels = self.room_state(room, "m.room.power_levels")
        protocol.check_side_power_levels(levels, QUESTIONS, [ALICE], False)
        self.assertEqual(self.room_state(room, "m.room.topic")[1]["topic"], admin.QUESTIONS_CLOSED_TOPIC)

    def test_no_questions_room_without_a_coordinator(self) -> None:
        self.member()
        self.assertFalse(self.has_side(QUESTIONS))
        self.assertNotIn(QUESTIONS, self.index())

    def test_the_first_coordinator_creates_the_room_and_is_invited(self) -> None:
        handle, _ = self.member(role="coordinator")
        room = self.side(QUESTIONS)
        _, link = self.room_state(room, protocol.EVENT_ROOM_LINK, event=True)
        protocol.check_room_link(link, ADMIN_ID, "team-a", QUESTIONS, self.room())
        _, rules = self.room_state(room, protocol.JOIN_RULES)
        protocol.check_join_rules(rules, QUESTIONS, self.room())
        self.assertEqual(self.room_state(room, "m.room.history_visibility")[1], {"history_visibility": "shared"})
        self.assertEqual(self.room_state(room, "m.room.name")[1], {"name": "team-a · questions"})
        self.assert_open(handle)
        self.assertEqual(self.room_membership(room, uid(handle)), "invite")
        self.assertEqual(self.room_membership(room, ALICE), "invite")
        self.assertEqual(self.index(), {"v": 1, REPORTS: self.side(REPORTS), QUESTIONS: room})
        self.assertEqual(self.bootstrap(), [])

    def test_a_worker_is_never_invited(self) -> None:
        self.member(role="coordinator")
        worker, token = self.member()
        self.assertIsNone(self.room_membership(self.side(QUESTIONS), uid(worker)))
        self.assertEqual(self.join(self.side(QUESTIONS), token), 403)

    def test_demotion_kicks_then_closes_and_promotion_reopens(self) -> None:
        handle, token = self.member()
        changes = admin.set_role(self.ctx(), self.transport, handle, "coordinator")
        self.assertIn("questions room", changes)
        self.assertEqual(self.join(self.side(QUESTIONS), token), 200)
        changes = admin.set_role(self.ctx(), self.transport, handle, "worker")
        self.assertIn(f"kicked {uid(handle)} from questions", changes)
        self.assertEqual(self.kick_reasons(), [(self.side(QUESTIONS), uid(handle), admin.DEMOTED_REASON)])
        self.assertEqual(self.room_membership(self.side(QUESTIONS), uid(handle)), "leave")
        self.assert_closed()
        changes = admin.set_role(self.ctx(), self.transport, handle, "coordinator")
        self.assertIn(f"invited {uid(handle)} to questions", changes)
        self.assert_open(handle)
        self.assertEqual(self.bootstrap(), [])

    def test_kicks_come_before_invites_on_a_handover(self) -> None:
        first, token = self.member(role="coordinator")
        self.assertEqual(self.join(self.side(QUESTIONS), token), 200)
        second, _ = self.member()

        def swap(reg: registry.Registry) -> tuple[registry.Registry, None]:
            return reg.set_role(first, "worker").set_role(second, "coordinator"), None

        registry.update_registry(self.team_dir / "registry.json", "team-a", swap)
        start = len(self.transport.calls)
        self.bootstrap()
        calls = self.transport.calls[start:]
        kick = next(i for i, c in enumerate(calls) if c[1].endswith("/kick") and c[3]["user_id"] == uid(first))
        invite = next(i for i, c in enumerate(calls) if c[1].endswith("/invite") and c[3]["user_id"] == uid(second))
        self.assertLess(kick, invite)
        self.assert_open(second)

    def test_a_coordinator_that_rejected_its_invite_is_invited_again(self) -> None:
        handle, token = self.member(role="coordinator")
        room = self.side(QUESTIONS)
        self.assertEqual(self.fake.request("POST", f"/_matrix/client/v3/rooms/{q(room)}/leave",
                                           body={}, token=token)[0], 200)
        self.assertEqual(self.room_membership(room, uid(handle)), "leave")
        self.assertEqual(self.bootstrap(), [f"invited {uid(handle)} to questions"])
        self.assertEqual(self.room_membership(room, uid(handle)), "invite")

    def test_removing_the_coordinator_kicks_it_from_every_room_and_closes_questions(self) -> None:
        handle, token = self.member(role="coordinator")
        for room in (self.room(), self.side(REPORTS), self.side(QUESTIONS)):
            self.assertEqual(self.join(room, token), 200)
        admin.remove_member(self.ctx(), self.transport, handle)
        self.assertEqual(sorted(self.transport.kicked),
                         sorted([(self.room(), uid(handle)), (self.side(REPORTS), uid(handle)),
                                 (self.side(QUESTIONS), uid(handle))]))
        self.assert_closed()
        self.assertEqual(admin.remove_member(self.ctx(), self.transport, handle), [])

    def test_a_removed_human_is_kicked_from_every_room_before_power_levels_change(self) -> None:
        self.write_team(team_data(humans=["alice", "bob"]))
        self.bootstrap()
        self.member(role="coordinator")
        self.write_team(team_data(humans=["bob"]))
        start = len(self.transport.calls)
        self.bootstrap()
        rooms = (self.room(), self.side(REPORTS), self.side(QUESTIONS))
        self.assertEqual(sorted(r for r, u in self.transport.kicked if u == ALICE), sorted(rooms))
        calls = self.transport.calls[start:]
        last_kick = max(i for i, c in enumerate(calls) if c[1].endswith("/kick"))
        first_levels = min(i for i, c in enumerate(calls)
                           if c[0] == "PUT" and "m.room.power_levels" in urllib.parse.unquote(c[1]))
        self.assertLess(last_kick, first_levels)
        self.assertEqual(self.bootstrap(), [])

    def test_a_failed_call_names_the_step_and_a_rerun_converges(self) -> None:
        handle, _ = self.member()
        self.fake.inject("POST", r"/createRoom$", 500, {"errcode": "M_UNKNOWN", "error": "injected"})
        with self.assertRaisesRegex(admin.AdminError, "create the questions room"):
            admin.set_role(self.ctx(), self.transport, handle, "coordinator")
        self.assertFalse(self.has_side(QUESTIONS))
        self.assertIn("questions room", self.bootstrap())
        self.assert_open(handle)
        self.assertEqual(self.bootstrap(), [])


class ListAndCheckTest(SideRoomsCase):
    def setUp(self) -> None:
        super().setUp()
        self.bootstrap()

    def test_list_adds_room_lines_and_keeps_the_others(self) -> None:
        coordinator, token = self.member(role="coordinator")
        worker, _ = self.member()
        self.assertEqual(self.join(self.side(QUESTIONS), token), 200)
        self.assertEqual(admin.list_members(self.ctx(), self.transport), [
            "HUMAN\talice\tinvite",
            f"MEMBER\t{coordinator}\tcoordinator\tinvite\tactive",
            f"MEMBER\t{worker}\tworker\tinvite\tactive",
            f"ROOM\t{ALICE}\treports\tinvite",
            f"ROOM\t{uid(coordinator)}\treports\t-",
            f"ROOM\t{uid(worker)}\treports\t-",
            f"ROOM\t{ALICE}\tquestions\tinvite",
            f"ROOM\t{uid(coordinator)}\tquestions\tjoin",
            f"ROOM\t{uid(worker)}\tquestions\t-",
        ])

    def test_check_rooms_names_a_coordinator_not_joined_and_passes_once_it_is(self) -> None:
        coordinator, token = self.member(role="coordinator")
        problems = admin.check_rooms(self.ctx(), self.transport)
        self.assertTrue(any(coordinator in p and "questions" in p and "invite" in p for p in problems), problems)
        for room in (self.room(), self.side(REPORTS), self.side(QUESTIONS)):
            self.assertEqual(self.join(room, token), 200)
        self.assertEqual(admin.check_rooms(self.ctx(), self.transport), [])

    def test_check_rooms_reads_only(self) -> None:
        self.member(role="coordinator")
        start = len(self.transport.calls)
        admin.check_rooms(self.ctx(), self.transport)
        self.assertEqual([c for c in self.transport.calls[start:] if c[0] != "GET"], [])

    def test_check_rooms_names_drift(self) -> None:
        reports = self.side(REPORTS)
        self.assertEqual(self.as_admin("PUT", f"/_matrix/client/v3/rooms/{q(reports)}/state/m.room.join_rules/",
                                       {"join_rule": "invite"})[0], 200)
        problems = admin.check_rooms(self.ctx(), self.transport)
        self.assertTrue(any("reports" in p and "join rules" in p for p in problems), problems)

    def test_check_rooms_names_a_missing_room_and_a_worker_in_questions(self) -> None:
        coordinator, token = self.member(role="coordinator")
        worker, _ = self.member()
        self.assertEqual(self.join(self.side(QUESTIONS), token), 200)
        # The worker reached questions somehow (an invite by hand): check-rooms must say so.
        self.assertEqual(self.as_admin("POST", f"/_matrix/client/v3/rooms/{q(self.side(QUESTIONS))}/invite",
                                       {"user_id": uid(worker)})[0], 200)
        problems = admin.check_rooms(self.ctx(), self.transport)
        self.assertTrue(any(worker in p and "questions" in p for p in problems), problems)
        (self.team_dir / "room_id.reports").unlink()
        problems = admin.check_rooms(self.ctx(), self.transport)
        self.assertTrue(any("no reports room" in p for p in problems), problems)


if __name__ == "__main__":
    unittest.main()
