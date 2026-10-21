"""Unit tests for the side rooms in helpers/pingbus/inbox.py (Plan 00211 SPEC §6, §7).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_inbox_rooms

Every stored item records its room: a questions-room item carries a top-level `room` key
and its question, a main-room item is byte-identical to what older code writes. The side
rooms' state file (`rooms.json`) and the report send times (`reports.json`, kept apart from
the outbox's gate, whose keys an older reader checks exactly).
"""

from __future__ import annotations

import json
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import inbox, limits, protocol

SN = "team-a.agent-bus.internal"
ME = f"@myrepo.1+workstation.podman:{SN}"
WORKER = f"@myrepo.2+workstation.podman:{SN}"
ALICE = f"@alice:{SN}"
ROOM_Q = "!" + "Q" * 43
ROOM_R = "!" + "R" * 43
T0 = 1_791_234_567_890
HOUR_MS = 3_600_000


def ctx(me_role: str = protocol.ROLE_COORDINATOR) -> protocol.Context:
    return protocol.Context(SN, frozenset({ALICE}), {ME: me_role, WORKER: protocol.ROLE_WORKER},
                            {"example-org/myrepo": ()}, ())


def eid(n: int) -> str:
    return "$" + f"{n:043d}"


def questions_event(n: int, body: str = "the cap is 50", question: object = None, **over: object) -> dict:
    event = {"type": "m.room.message", "event_id": eid(n), "sender": ALICE, "origin_server_ts": T0,
             "content": {"msgtype": "m.text", "body": body}, "room": "questions", "question": question}
    event.update(over)
    return event


def main_event(n: int) -> dict:
    return {"type": "m.room.message", "event_id": eid(n), "sender": ALICE, "origin_server_ts": T0,
            "content": {"msgtype": "m.text", "body": "hi", "m.mentions": {"user_ids": [ME]}}}


class Case(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        (pathlib.Path(tmp.name) / "team-a").mkdir()
        self.state = inbox.TeamState(pathlib.Path(tmp.name) / "team-a" / "state")

    def stored(self, n: int) -> dict:
        return json.loads((self.state.inbox_dir / f"{eid(n)}.json").read_text())


class StoredRoomTest(Case):
    def test_an_unaddressed_questions_item_survives_revalidation(self):
        self.state.commit_batch([questions_event(1, question={"n": 3, "headline": "Pick the cap"})], "s1")
        items = self.state.pending(ctx(), ME, human_text=True).items
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0].room, "questions")
        self.assertEqual(items[0].question, (3, "Pick the cap"))
        self.assertEqual(items[0].outcome.human.text, "the cap is 50")

    def test_an_unattached_questions_item_has_no_question(self):
        self.state.commit_batch([questions_event(1)], "s1")
        self.assertEqual(self.stored(1)["question"], None)
        item = self.state.pending(ctx(), ME, human_text=True).items[0]
        self.assertEqual((item.room, item.question), ("questions", None))

    def test_a_worker_reads_no_questions_item(self):
        self.state.commit_batch([questions_event(1)], "s1")
        self.assertEqual(self.state.pending(ctx(protocol.ROLE_WORKER), ME, human_text=True).items, ())

    def test_a_main_room_item_is_byte_identical_to_the_old_format(self):
        event = main_event(1)
        self.state.commit_batch([event], "s1")
        raw = (self.state.inbox_dir / f"{eid(1)}.json").read_bytes()
        old = json.dumps({k: event[k] for k in inbox.STORED_KEYS}, ensure_ascii=True,
                         separators=(",", ":")).encode("ascii")
        self.assertEqual(raw, old)
        item = self.state.pending(ctx(), ME, human_text=True).items[0]
        self.assertEqual((item.room, item.question), ("main", None))

    def test_an_item_without_the_room_key_reads_as_main(self):
        # An unaddressed human message in the main room is not this member's.
        event = questions_event(1)
        del event["room"], event["question"]
        self.state.commit_batch([event], "s1")
        self.assertEqual(self.state.pending(ctx(), ME, human_text=True).rejected, 1)

    def test_a_bad_room_or_question_is_refused_on_write(self):
        for bad in (questions_event(1, room="reports"), questions_event(1, room="main"),
                    questions_event(1, question={"n": 0, "headline": "x"}),
                    questions_event(1, question={"n": 1, "headline": ""}),
                    questions_event(1, question={"n": 1, "headline": "x" * 121}),
                    questions_event(1, question={"n": 1, "headline": "x", "y": 1}),
                    {**main_event(1), "question": None}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.state.commit_batch([bad], "s1")

    def test_a_planted_room_or_question_is_rejected_on_read(self):
        self.state.commit_batch([], "s1")
        for n, (room, question) in enumerate((("reports", None), ("main", None),
                                              ("questions", {"n": True, "headline": "x"}),
                                              ("questions", "Q1")), start=1):
            data = questions_event(n, room=room, question=question)
            (self.state.inbox_dir / f"{eid(n)}.json").write_text(json.dumps(data))
        missing = questions_event(9)
        del missing["question"]
        (self.state.inbox_dir / f"{eid(9)}.json").write_text(json.dumps(missing))
        result = self.state.pending(ctx(), ME, human_text=True)
        self.assertEqual((result.items, result.rejected), ((), 5))


class SideRoomsFileTest(Case):
    def test_saved_and_read_back(self):
        rooms = {"reports": inbox.SideRoom(ROOM_R, "join", None),
                 "questions": inbox.SideRoom(ROOM_Q, "pending", "not invited yet")}
        self.state.save_side_rooms(rooms)
        self.assertEqual(self.state.side_rooms(), rooms)
        self.assertEqual(json.loads((self.state.path / "rooms.json").read_text()),
                         {"v": 1, "rooms": {"reports": {"room": ROOM_R, "state": "join", "reason": None},
                                            "questions": {"room": ROOM_Q, "state": "pending",
                                                          "reason": "not invited yet"}}})

    def test_absent_is_no_rooms(self):
        self.assertEqual(self.state.side_rooms(), {})

    def test_states_and_reasons_are_checked(self):
        for bad in ({"main": inbox.SideRoom(ROOM_R, "join", None)},
                    {"reports": inbox.SideRoom("!short", "join", None)},
                    {"reports": inbox.SideRoom(ROOM_R, "held", None)},
                    {"reports": inbox.SideRoom(ROOM_R, "refused", "x" * 201)},
                    {"reports": inbox.SideRoom(ROOM_R, "refused", "two\nlines")}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.state.save_side_rooms(bad)
        self.state.ensure_dirs()
        (self.state.path / "rooms.json").write_text('{"v": 1, "rooms": {"reports": {"room": 1}}}')
        with self.assertRaises(inbox.StateError):
            self.state.side_rooms()

    def test_losing_main_room_trust_forgets_the_side_rooms(self):
        self.state.save_side_rooms({"reports": inbox.SideRoom(ROOM_R, "join", None)})
        self.state.mark_untrusted("power levels")
        self.assertEqual(self.state.side_rooms(), {})


class ReportGateTest(Case):
    LIM = limits.Limits(report_per_hour=2)

    def test_the_hourly_cap_then_the_window_slides(self):
        self.state.admit_report(self.LIM, "a", T0)
        self.state.admit_report(self.LIM, "b", T0 + 1)
        with self.assertRaises(limits.RateLimited) as caught:
            self.state.admit_report(self.LIM, "c", T0 + 2)
        self.assertEqual(caught.exception.reason, "rate")
        self.state.admit_report(self.LIM, "c", T0 + HOUR_MS)

    def test_a_duplicate_within_the_window_is_refused(self):
        self.state.admit_report(self.LIM, "a", T0)
        with self.assertRaises(limits.RateLimited) as caught:
            self.state.admit_report(self.LIM, "a", T0 + 1000)
        self.assertEqual(caught.exception.reason, "duplicate")
        self.state.admit_report(self.LIM, "a", T0 + limits.DUPLICATE_WINDOW_S * 1000)

    def test_a_refused_report_records_no_send_time(self):
        self.state.admit_report(self.LIM, "a", T0)
        with self.assertRaises(limits.RateLimited):
            self.state.admit_report(self.LIM, "a", T0 + 1)
        self.state.admit_report(self.LIM, "b", T0 + 2)

    def test_send_times_live_apart_from_the_outbox_gate(self):
        self.state.admit_report(self.LIM, "a", T0)
        gate = json.loads((self.state.path / "outbox.json").read_text())["gate"]
        self.assertEqual(set(gate), {"bucket", "recent"})
        self.assertEqual(json.loads((self.state.path / "reports.json").read_text()), {"v": 1, "sent": [T0]})

    def test_the_bucket_is_shared_with_say(self):
        tight = limits.Limits(send_burst=1, send_per_minute=1, report_per_hour=60)
        self.state.admit_report(tight, "a", T0)
        with self.assertRaises(limits.RateLimited) as caught:
            self.state.admit_send(tight, "text", None, None, [WORKER], T0 + 1)
        self.assertEqual(caught.exception.reason, "rate")

    def test_a_planted_reports_file_is_refused(self):
        self.state.ensure_dirs()
        (self.state.path / "reports.json").write_text('{"v": 1, "sent": ["x"]}')
        with self.assertRaises(inbox.StateError):
            self.state.admit_report(self.LIM, "a", T0)


if __name__ == "__main__":
    unittest.main()
