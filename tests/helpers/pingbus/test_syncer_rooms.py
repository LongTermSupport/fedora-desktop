"""Unit tests for the side rooms in helpers/pingbus/syncer.py (Plan 00211 SPEC §4, §6, §8.3).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_syncer_rooms

The index in the main room, the reports join on reading it, side-room trust (each failure
leaving the main room trusted), the invite rule, the coordinator's filter, the questions-room
receive path with its attachments, its gap fill, and `history(room="questions")`. Real HTTP on
loopback to the fake homeserver, as test_syncer.
"""

from __future__ import annotations

import json
import pathlib
import re
import sys
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import inbox, matrix, protocol, syncer
from tests.helpers.pingbus.test_syncer import TEAM, TeamCase, q

QUESTION = "Pick the cost limit\nContext: the nightly run costs money.\nQuestion: what is the cap?"


class SideRoomCase(TeamCase):
    """The main room of TeamCase, with this member as the team's coordinator."""

    coordinator = True

    def setUp(self) -> None:
        super().setUp()
        if self.coordinator:
            self.record["roles"] = {self.me: "coordinator", self.orch: "worker", self.peer: "worker"}
            self.put_state(self.admin, protocol.EVENT_TEAM, "", self.record)
        self.side_txn = 0

    def make_side(self, purpose: str, *, invite: tuple[str, ...] = (), levels: dict | None = None,
                  join_rules: dict | None = None, link: dict | None = None, creator: str | None = None) -> str:
        initial = [
            {"type": protocol.JOIN_RULES, "state_key": "",
             "content": join_rules or protocol.expected_join_rules(purpose, self.room)},
            {"type": protocol.EVENT_ROOM_LINK, "state_key": "",
             "content": link or {"v": 1, "team": TEAM, "purpose": purpose, "main": self.room}},
        ]
        return self.call(creator or self.admin, "POST", "/_matrix/client/v3/createRoom", {
            "preset": "private_chat", "room_version": "12", "name": f"{TEAM} · {purpose}",
            "power_level_content_override": levels or protocol.side_power_levels(purpose, [self.alice], open_=True),
            "initial_state": initial, "invite": list(invite)})["room_id"]

    def index(self, **rooms: str) -> None:
        self.put_state(self.admin, protocol.EVENT_ROOMS, "", {"v": 1, **rooms})

    def both(self, *, invite_me: bool = True) -> tuple[str, str]:
        reports = self.make_side("reports", invite=(self.alice,))
        questions = self.make_side("questions", invite=(self.alice, self.me) if invite_me else (self.alice,))
        self.call(self.alice, "POST", f"/_matrix/client/v3/rooms/{q(questions)}/join", {})
        self.index(reports=reports, questions=questions)
        return reports, questions

    def side_send(self, room: str, uid: str, content: dict) -> str:
        self.side_txn += 1
        return self.call(uid, "PUT", f"/_matrix/client/v3/rooms/{q(room)}/send/m.room.message/q{self.side_txn}",
                         content)["event_id"]

    def ask(self, room: str, n: int = 1, text: str = QUESTION) -> str:
        return self.side_send(room, self.me, protocol.build_question(n, text, [self.alice], [], []))

    def answer(self, room: str, body: str, relates: dict | None = None) -> str:
        content: dict = {"msgtype": "m.text", "body": body}
        if relates is not None:
            content["m.relates_to"] = relates
        return self.side_send(room, self.alice, content)

    def filter_rooms(self) -> list[list[str]]:
        return [json.loads(query["filter"])["room"]["rooms"] for query in self.sync_queries()]

    def membership(self, room: str) -> str | None:
        return self.fake.rooms[room].membership(self.me)

    def items(self) -> list[inbox.Item]:
        return list(self.pending().items)


class FilterShapeTest(unittest.TestCase):
    def test_the_index_is_read_and_side_rooms_are_named_only_when_asked(self):
        main, other = "!" + "M" * 43, "!" + "Q" * 43
        flt = syncer.sync_filter(main, 5)
        self.assertEqual(flt["room"]["rooms"], [main])
        self.assertIn(protocol.EVENT_ROOMS, flt["room"]["state"]["types"])
        self.assertIn(protocol.EVENT_ROOMS, flt["room"]["timeline"]["types"])
        self.assertEqual(syncer.sync_filter(main, 5, also=(other,))["room"]["rooms"], [main, other])
        self.assertIn(protocol.EVENT_ROOMS, syncer.TRUST_TYPES)
        self.assertEqual(set(syncer.SIDE_TRUST_TYPES) - set(syncer.TRUST_TYPES),
                         {protocol.JOIN_RULES, protocol.EVENT_ROOM_LINK})


class ReportsJoinTest(SideRoomCase):
    coordinator = False

    def test_the_reports_room_is_joined_on_reading_the_index(self):
        reports, questions = self.both(invite_me=False)
        s = self.ready()
        self.assertEqual(self.membership(reports), "join")
        self.assertEqual(s.side["reports"], inbox.SideRoom(reports, "join", None))
        self.assertEqual(self.state.side_rooms()["reports"].state, "join")
        # A worker never syncs a side room, and is never in questions.
        self.assertEqual(s.side["questions"].state, "pending")
        self.assertIsNone(self.membership(questions))
        s.sync_once()
        self.assertTrue(all(rooms == [self.room] for rooms in self.filter_rooms()))

    def test_an_index_written_later_is_taken_up_mid_stream(self):
        s = self.ready()
        self.assertEqual(s.side, {})
        reports = self.make_side("reports", invite=(self.alice,))
        self.index(reports=reports)
        s.sync_once()
        self.assertEqual(self.membership(reports), "join")

    def test_a_refused_join_is_recorded_not_fatal(self):
        reports = self.make_side("reports", join_rules={"join_rule": "invite"})
        self.index(reports=reports)
        s = self.ready()
        self.assertEqual(s.side["reports"].state, "refused")
        self.assertIsNotNone(s.record)

    def test_no_index_means_no_side_rooms(self):
        s = self.ready()
        self.assertEqual((s.side, self.state.side_rooms()), ({}, {}))

    def test_an_index_not_in_its_exact_form_is_not_read(self):
        reports = self.make_side("reports")
        self.index(reports=reports, main=self.room)
        s = self.ready()
        self.assertEqual(s.side, {})
        self.assertIsNone(self.membership(reports))
        self.assertTrue(any("rooms index" in line for line in self.logged), self.logged)


class QuestionsJoinTest(SideRoomCase):
    def test_a_coordinators_invite_is_accepted_and_the_room_verified(self):
        reports, questions = self.both()
        s = self.ready()
        self.assertEqual(self.membership(questions), "join")
        self.assertEqual(s.side["questions"], inbox.SideRoom(questions, "join", None))
        s.sync_once()
        self.assertEqual(self.filter_rooms()[-1], [self.room, questions])

    def test_not_invited_yet_is_pending_then_the_invite_is_taken(self):
        reports, questions = self.both(invite_me=False)
        s = self.ready()
        self.assertEqual(s.side["questions"].state, "pending")
        self.assertEqual(s.side["questions"].reason, syncer.REASON_NOT_INVITED)
        # The filter names it while pending, so the invite is seen.
        s.sync_once()
        self.assertEqual(self.filter_rooms()[-1], [self.room, questions])
        self.call(self.admin, "POST", f"/_matrix/client/v3/rooms/{q(questions)}/invite", {"user_id": self.me})
        s.sync_once()
        self.assertEqual(self.membership(questions), "join")
        self.assertEqual(s.side["questions"].state, "join")

    def test_the_rooms_history_before_the_join_is_skipped(self):
        reports, questions = self.both()
        self.answer(questions, "said before the coordinator joined")
        s = self.ready()
        s.sync_once()
        after = self.answer(questions, "said after")
        s.sync_once()
        self.assertEqual([i.event_id for i in self.items()], [after])

    def invite(self, *, inviter: str, creator: str) -> dict:
        return {"invite_state": {"events": [
            {"type": "m.room.create", "state_key": "", "sender": creator, "content": {"room_version": "12"}},
            {"type": "m.room.member", "state_key": self.me, "sender": inviter,
             "content": {"membership": "invite"}},
        ]}}

    def test_the_invite_rule(self):
        member, questions, stray = self.member(), "!" + "Q" * 43, "!" + "S" * 43
        ok = self.invite(inviter=self.admin, creator=self.admin)
        foreign = self.invite(inviter=self.outsider, creator=self.outsider)
        cases = [
            ((self.room, ok, questions, True), syncer.INVITE_MAIN),
            ((questions, ok, questions, True), syncer.INVITE_QUESTIONS),
            # The index names it, but this member may not take it: left pending, never rejected.
            ((questions, ok, questions, False), syncer.INVITE_PENDING),
            # An admin invite the index does not (yet) name.
            ((stray, ok, questions, True), syncer.INVITE_PENDING),
            ((stray, ok, None, True), syncer.INVITE_PENDING),
            ((questions, foreign, questions, True), syncer.INVITE_REJECT),
            ((stray, self.invite(inviter=self.admin, creator=self.outsider), questions, True), syncer.INVITE_REJECT),
            ((stray, [], questions, True), syncer.INVITE_REJECT),
        ]
        for (room_id, invite, indexed, may), expected in cases:
            with self.subTest(room=room_id, may=may, expected=expected):
                self.assertEqual(syncer.invite_action(room_id, invite, member, indexed, may), expected)


class WorkerQuestionsTest(SideRoomCase):
    coordinator = False

    def test_a_workers_questions_invite_is_left_pending(self):
        reports, questions = self.both()
        s = self.ready()
        s.sync_once()
        self.assertEqual(self.membership(questions), "invite")
        self.assertEqual(s.side["questions"], inbox.SideRoom(questions, "pending", syncer.REASON_NOT_COORDINATOR))


class NoHumanTextTest(SideRoomCase):
    human_text = False

    def test_the_invite_stays_pending_with_the_reason_and_syncs_still_run(self):
        reports, questions = self.both()
        s = self.ready()
        s.sync_once()
        self.assertEqual(self.membership(questions), "invite")
        self.assertEqual(s.side["questions"], inbox.SideRoom(questions, "refused", syncer.REASON_NO_HUMAN_TEXT))
        self.assertEqual(syncer.REASON_NO_HUMAN_TEXT,
                         "human_text is false; re-issue the bundle without --no-human-text")
        self.assertIsNotNone(s.record)
        self.assertEqual(self.filter_rooms()[-1], [self.room])


class SideTrustTest(SideRoomCase):
    def joined(self, **make: object) -> tuple[syncer.Syncer, str]:
        reports = self.make_side("reports", invite=(self.alice,))
        questions = self.make_side("questions", invite=(self.alice, self.me), **make)
        self.call(self.alice, "POST", f"/_matrix/client/v3/rooms/{q(questions)}/join", {})
        self.index(reports=reports, questions=questions)
        return self.ready(), questions

    def assert_only_questions_untrusted(self, s: syncer.Syncer) -> None:
        self.assertEqual(s.side["questions"].state, "untrusted")
        self.assertIsNotNone(s.side["questions"].reason)
        self.assertIsNotNone(s.record)
        self.assertEqual(s.side["reports"].state, "join")
        main = self.human("still heard")
        s.sync_once()
        self.assertIn(main, [i.event_id for i in self.items()])
        self.assertEqual(self.filter_rooms()[-1], [self.room])

    def test_each_failure_leaves_the_main_room_trusted(self):
        cases = {
            "link": {"link": {"v": 1, "team": TEAM, "purpose": "reports", "main": self.room}},
            "power levels": {"levels": protocol.side_power_levels("questions", [self.alice], open_=False)},
            "join rules": {"join_rules": {"join_rule": "public"}},
        }
        for name, make in cases.items():
            with self.subTest(name):
                self.state.forget_sync_token()
                s, _ = self.joined(**make)
                self.assert_only_questions_untrusted(s)

    def test_a_room_another_account_created_is_untrusted(self):
        reports = self.make_side("reports", invite=(self.alice,))
        questions = self.make_side("questions", creator=self.outsider, invite=(self.me,))
        self.index(reports=reports, questions=questions)
        s = self.ready()
        self.assertNotEqual(s.side["questions"].state, "join")
        self.assertIsNotNone(s.record)

    def test_a_change_to_a_trust_type_reverifies(self):
        s, questions = self.joined()
        s.sync_once()
        self.call(self.admin, "PUT", f"/_matrix/client/v3/rooms/{q(questions)}/state/{protocol.JOIN_RULES}/",
                  {"join_rule": "public"})
        self.answer(questions, "after the change")
        s.sync_once()
        self.assertEqual(s.side["questions"].state, "untrusted")
        self.assertEqual(self.items(), [])

    def test_the_closed_form_is_required_once_no_coordinator_is_left(self):
        s, questions = self.joined()
        self.record["roles"] = {self.me: "worker", self.orch: "worker", self.peer: "worker"}
        self.put_state(self.admin, protocol.EVENT_TEAM, "", self.record)
        s.sync_once()
        # A record that names this member worker takes the room out of its filter.
        self.assertNotEqual(s.side["questions"].state, "join")
        self.answer(questions, "nobody reads this")
        s.sync_once()
        self.assertEqual(self.filter_rooms()[-1], [self.room])


#: The non-transient homeserver failures a side room can meet, as `inject` arguments: a refused
#: restricted join (Synapse's 400) and an answer that is not JSON (`Unreachable`, not transient).
SIDE_FAILURES = {
    "400 refused join": (400, {"errcode": "M_UNABLE_TO_AUTHORISE_JOIN", "error": "no"}),
    "no usable answer": (200, []),
}
#: The transient ones, what a homeserver restart looks like: a server error, and a rate limit
#: past its retries. A refused connection is `test_a_transport_failure_is_raised`.
TRANSIENT_FAILURES = {
    "500": (500, {"errcode": "M_UNKNOWN", "error": "boom"}),
    "429": (429, {"errcode": "M_LIMIT_EXCEEDED", "error": "slow", "retry_after_ms": 1}),
}


class SideRoomFailureTest(SideRoomCase):
    """A non-transient homeserver failure in a side room is that room's alone (SPEC §4.3): it
    is recorded with a reason and logged, and every main-room step goes on. A transient one is
    raised, so the watcher's retry runs, and nothing is marked or committed for it."""

    def fail(self, method: str, pattern: str, failure: str) -> None:
        status, body = {**SIDE_FAILURES, **TRANSIENT_FAILURES}[failure]
        self.fake.inject(method, pattern, status, body, times=1000)

    def restart_over(self) -> None:
        self.fake._faults.clear()

    def test_a_transient_side_failure_is_raised_and_recovers(self):
        cases = {
            "reports join": ("POST", "reports", "/join$"),
            "questions join": ("POST", "questions", "/join$"),
            "questions state": ("GET", "questions", "/state/"),
        }
        for failure in TRANSIENT_FAILURES:
            for name, (method, purpose, tail) in cases.items():
                with self.subTest(failure=failure, case=name):
                    self.state.forget_sync_token()
                    self.restart_over()
                    rooms = dict(zip(("reports", "questions"), self.both(), strict=True))
                    self.fail(method, rf"/rooms/{re.escape(q(rooms[purpose]))}{tail}", failure)
                    with self.assertRaises(matrix.MatrixError) as raised:
                        self.ready()
                    self.assertTrue(raised.exception.transient)
                    self.assertIsNone(self.state.sync_token())
                    self.restart_over()
                    s = self.ready()
                    self.assertEqual((s.side["reports"].state, s.side["questions"].state), ("join", "join"))

    def test_a_transport_failure_is_raised(self):
        reports, questions = self.both()
        s = self.syncer()
        join = s.client.join

        def refuse(room_id: str) -> dict:
            if room_id == questions:
                raise matrix.Unreachable("POST join", detail="ConnectionRefusedError", transient=True)
            return join(room_id)

        with mock.patch.object(s.client, "join", side_effect=refuse), self.assertRaises(matrix.Unreachable):
            s.sync_once()
        self.assertNotEqual(self.state.side_rooms().get("questions", inbox.SideRoom(questions, "join", None)).state,
                            "untrusted")
        s.sync_once()
        self.assertEqual(s.side["questions"].state, "join")

    def test_after_a_restart_mid_gap_fill_the_missed_answers_are_delivered(self):
        reports, questions = self.both()
        s = self.ready()
        s.sync_once()
        token = self.state.sync_token()
        sent = [self.answer(questions, f"answer {n}") for n in range(syncer.TIMELINE_LIMIT + 5)]
        self.fail("GET", rf"/rooms/{re.escape(q(questions))}/messages$", "500")
        with self.assertRaises(matrix.MatrixError):
            s.sync_once()
        self.assertEqual(self.state.sync_token(), token, "the token never passes a failed gap fill")
        self.restart_over()
        self.assertEqual(s.sync_once().accepted, tuple(sent))
        self.assertEqual(s.side["questions"].state, "join")

    def test_after_a_restart_mid_reverify_the_room_stays_trusted_and_answers_arrive(self):
        reports, questions = self.both()
        s = self.ready()
        s.sync_once()
        answer = self.answer(questions, "cap it at 10")
        # A trust-type event makes the next sync reverify the questions room.
        self.call(self.admin, "PUT", f"/_matrix/client/v3/rooms/{q(questions)}/state/{protocol.JOIN_RULES}/",
                  protocol.expected_join_rules("questions", self.room))
        self.fail("GET", rf"/rooms/{re.escape(q(questions))}/state/", "500")
        with self.assertRaises(matrix.MatrixError):
            s.sync_once()
        self.assertEqual(s.side["questions"].state, "join")
        self.restart_over()
        self.assertIn(answer, s.sync_once().accepted)
        self.assertEqual(s.side["questions"].state, "join")

    def assert_main_room_works(self, s: syncer.Syncer) -> None:
        self.assertIsNotNone(s.record)
        main = self.human("still heard")
        s.sync_once()
        self.assertIn(main, [i.event_id for i in self.items()])

    def test_a_reports_join_failure_is_recorded_and_the_main_room_goes_on(self):
        for failure in SIDE_FAILURES:
            with self.subTest(failure):
                self.state.forget_sync_token()
                self.fake._faults.clear()
                reports = self.make_side("reports", invite=(self.alice,))
                self.index(reports=reports)
                self.fail("POST", rf"/rooms/{re.escape(q(reports))}/join$", failure)
                self.logged.clear()
                s = self.ready()
                self.assertEqual(s.side["reports"].state, "refused")
                self.assertIn("homeserver", s.side["reports"].reason)
                self.assertEqual(self.state.side_rooms()["reports"], s.side["reports"])
                self.assertTrue(any("reports room" in line for line in self.logged), self.logged)
                self.assert_main_room_works(s)

    def test_a_questions_join_failure_is_recorded_and_the_main_room_goes_on(self):
        for failure in SIDE_FAILURES:
            with self.subTest(failure):
                self.state.forget_sync_token()
                self.fake._faults.clear()
                reports, questions = self.both()
                self.fail("POST", rf"/rooms/{re.escape(q(questions))}/join$", failure)
                self.logged.clear()
                s = self.ready()
                self.assertEqual(s.side["questions"].state, "pending")
                self.assertIn("homeserver", s.side["questions"].reason)
                self.assertTrue(any("questions room" in line for line in self.logged), self.logged)
                self.assert_main_room_works(s)

    def test_a_side_state_read_failure_is_untrusted_and_the_main_room_goes_on(self):
        for failure in SIDE_FAILURES:
            with self.subTest(failure):
                self.state.forget_sync_token()
                self.fake._faults.clear()
                reports, questions = self.both()
                self.fail("GET", rf"/rooms/{re.escape(q(questions))}/state/", failure)
                self.logged.clear()
                s = self.ready()
                self.assertEqual(s.side["questions"].state, "untrusted")
                self.assertIn("homeserver", s.side["questions"].reason)
                self.assertTrue(any("questions room" in line for line in self.logged), self.logged)
                self.assert_main_room_works(s)

    def test_the_main_rooms_own_failure_still_fails(self):
        s = self.ready()
        self.fail("GET", rf"/rooms/{re.escape(q(self.room))}/state/", "500")
        with self.assertRaises(matrix.MatrixError):
            s.verify_room()



class QuestionsReceiveTest(SideRoomCase):
    def setUp(self) -> None:
        super().setUp()
        self.reports, self.questions = self.both()
        self.s = self.ready()
        self.s.sync_once()

    def test_every_human_message_reaches_the_coordinator_with_its_question(self):
        root = self.ask(self.questions)
        threaded = self.answer(self.questions, "50", {"rel_type": "m.thread", "event_id": root})
        prefixed = self.answer(self.questions, "Q1: or 60")
        loose = self.answer(self.questions, "unrelated thought")
        batch = self.s.sync_once()
        self.assertEqual(batch.accepted, (threaded, prefixed, loose))
        by_id = {item.event_id: item for item in self.items()}
        self.assertEqual(by_id[threaded].room, "questions")
        self.assertEqual(by_id[threaded].question, (1, "Pick the cost limit"))
        self.assertEqual(by_id[prefixed].question, (1, "Pick the cost limit"))
        self.assertIsNone(by_id[loose].question)
        stored = json.loads((self.state.inbox_dir / f"{threaded}.json").read_text())
        self.assertEqual((stored["room"], stored["question"]), ("questions", {"n": 1, "headline": "Pick the cost limit"}))

    def test_an_edit_is_delivered_as_a_revision_with_its_originals_question(self):
        root = self.ask(self.questions)
        first = self.answer(self.questions, "50", {"rel_type": "m.thread", "event_id": root})
        edit = self.side_send(self.questions, self.alice, {
            "msgtype": "m.text", "body": "* 60", "m.new_content": {"msgtype": "m.text", "body": "60"},
            "m.relates_to": {"rel_type": "m.replace", "event_id": first}})
        self.s.sync_once()
        item = {i.event_id: i for i in self.items()}[edit]
        self.assertEqual((item.outcome.human.text, item.outcome.human.revises), ("60", first))
        self.assertEqual(item.question, (1, "Pick the cost limit"))

    def test_a_demoted_coordinator_stops_at_once(self):
        self.record["roles"] = {self.me: "worker", self.orch: "coordinator", self.peer: "worker"}
        self.put_state(self.admin, protocol.EVENT_TEAM, "", self.record)
        self.answer(self.questions, "for the old coordinator")
        self.s.sync_once()
        self.assertEqual(self.s.sync_once().accepted, ())
        self.assertEqual(self.filter_rooms()[-1], [self.room])

    def test_a_limited_questions_timeline_is_gap_filled(self):
        sent = [self.answer(self.questions, f"answer {n}") for n in range(syncer.TIMELINE_LIMIT + 5)]
        batch = self.s.sync_once()
        self.assertEqual(batch.accepted, tuple(sent))
        gap = [r for r in self.fake.http_log if r.path.endswith("/messages") and q(self.questions) in r.path]
        self.assertTrue(gap)

    def test_history_of_the_questions_room(self):
        root = self.ask(self.questions)
        reply = self.answer(self.questions, "50", {"rel_type": "m.thread", "event_id": root})
        found = self.s.history(10, room="questions")
        self.assertEqual([(i.direction, i.room) for i in found.items], [("in", "questions"), ("out", "questions")])
        self.assertEqual(found.items[0].human.event_id, reply)
        self.assertEqual(found.items[0].question, (1, "Pick the cost limit"))
        self.assertEqual(found.items[1].sent_event_id, root)
        self.assertEqual(self.s.history(10).items, ())

    def test_history_needs_a_joined_questions_room(self):
        self.call(self.admin, "PUT", f"/_matrix/client/v3/rooms/{q(self.questions)}/state/{protocol.JOIN_RULES}/",
                  {"join_rule": "public"})
        with self.assertRaises(syncer.RoomUntrusted):
            self.s.history(10, room="questions")


if __name__ == "__main__":
    unittest.main()
