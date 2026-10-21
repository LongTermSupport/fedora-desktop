"""Unit tests for the side rooms in helpers/pingbus/protocol.py (Plan 00211 SPEC §3-§8).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_protocol_rooms

The rooms index and back-links, the power levels and join rules of the reports and
questions rooms, the report, question and question-state shapes, the question index and its
attachment rules, and `validate_event` with `room="questions"`.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import protocol as p

SN = "team-a.agent-bus.internal"
TEAM = "team-a"
ADMIN = f"@admin:{SN}"
ALICE = f"@alice:{SN}"
BOB = f"@bob:{SN}"
ME = f"@myrepo.1+workstation.podman:{SN}"
WORKER = f"@myrepo.2+workstation.podman:{SN}"
MAIN = "!" + "M" * 43
REPORTS = "!" + "R" * 43
QUESTIONS = "!" + "Q" * 43


def eid(n: int) -> str:
    return "$" + f"{n:043d}"


def ctx(coordinator: str = ME) -> p.Context:
    roles = {ME: p.ROLE_WORKER, WORKER: p.ROLE_WORKER}
    roles[coordinator] = p.ROLE_COORDINATOR
    return p.Context(SN, frozenset({ALICE, BOB}), roles, {"example-org/myrepo": ()}, ())


def message(n: int, sender: str, content: dict, ts: int = 1000) -> dict:
    return {"type": p.EVENT_MESSAGE, "event_id": eid(n), "sender": sender,
            "origin_server_ts": ts, "content": content}


def human(body: str, relates: dict | None = None, **extra: object) -> dict:
    content: dict = {"msgtype": "m.text", "body": body}
    if relates is not None:
        content["m.relates_to"] = relates
    content.update(extra)
    return content


QUESTION_TEXT = "Pick the cost limit\nContext: the nightly run costs money.\nQuestion: what is the cap?\nOptions:\n1. 5\n2. 50"


class IndexTest(unittest.TestCase):
    def event(self, content: object, **over: object) -> dict:
        event = {"type": p.EVENT_ROOMS, "state_key": "", "sender": ADMIN, "content": content}
        event.update(over)
        return event

    def test_a_valid_index_names_its_rooms(self):
        self.assertEqual(p.parse_rooms_index(self.event({"v": 1, "reports": REPORTS}), ADMIN, MAIN),
                         {"reports": REPORTS})
        both = {"v": 1, "reports": REPORTS, "questions": QUESTIONS}
        self.assertEqual(p.parse_rooms_index(self.event(both), ADMIN, MAIN),
                         {"reports": REPORTS, "questions": QUESTIONS})

    def test_anything_else_is_untrusted(self):
        cases = {
            "not admin": self.event({"v": 1, "reports": REPORTS}, sender=ALICE),
            "state key": self.event({"v": 1, "reports": REPORTS}, state_key="x"),
            "type": self.event({"v": 1, "reports": REPORTS}, type=p.EVENT_TEAM),
            "version": self.event({"v": 2, "reports": REPORTS}),
            "bool version": self.event({"v": True, "reports": REPORTS}),
            "unknown key": self.event({"v": 1, "reports": REPORTS, "chat": QUESTIONS}),
            "not a room ID": self.event({"v": 1, "reports": "reports"}),
            "the main room": self.event({"v": 1, "reports": MAIN}),
            "one room twice": self.event({"v": 1, "reports": REPORTS, "questions": REPORTS}),
            "content not an object": self.event([]),
            "not an object": [],
        }
        for name, event in cases.items():
            with self.subTest(name), self.assertRaises(p.Untrusted):
                p.parse_rooms_index(event, ADMIN, MAIN)


class LinkTest(unittest.TestCase):
    def event(self, **content: object) -> dict:
        body = {"v": 1, "team": TEAM, "purpose": "reports", "main": MAIN}
        body.update(content)
        return {"type": p.EVENT_ROOM_LINK, "state_key": "", "sender": ADMIN, "content": body}

    def test_a_matching_link_passes(self):
        p.check_room_link(self.event(), ADMIN, TEAM, "reports", MAIN)

    def test_a_link_naming_anything_else_is_untrusted(self):
        cases = {
            "purpose": self.event(purpose="questions"),
            "team": self.event(team="team-b"),
            "main room": self.event(main=QUESTIONS),
            "version": self.event(v=2),
            "extra key": self.event(extra=1),
        }
        for name, event in cases.items():
            with self.subTest(name), self.assertRaises(p.Untrusted):
                p.check_room_link(event, ADMIN, TEAM, "reports", MAIN)
        foreign = {**self.event(), "sender": ALICE}
        with self.assertRaises(p.Untrusted):
            p.check_room_link(foreign, ADMIN, TEAM, "reports", MAIN)


class PowerLevelsTest(unittest.TestCase):
    def test_reports_are_read_only_for_humans(self):
        levels = p.side_power_levels("reports", [BOB, ALICE], open_=True)
        self.assertEqual(levels["users"], {ALICE: 0, BOB: 0})
        self.assertEqual((levels["users_default"], levels["events_default"]), (10, 10))
        self.assertEqual(levels["events"]["m.reaction"], 100)
        self.assertEqual(levels["notifications"], {"room": 100})
        self.assertEqual(levels["events"][p.EVENT_STATUS], 100)

    def test_questions_open_is_the_main_form_with_reactions_closed(self):
        expected = p.expected_power_levels([ALICE])
        expected["events"]["m.reaction"] = 100
        self.assertEqual(p.side_power_levels("questions", [ALICE], open_=True), expected)

    def test_questions_closed_lets_nobody_post(self):
        levels = p.side_power_levels("questions", [ALICE], open_=False)
        self.assertEqual(levels["users"], {ALICE: 0})
        self.assertEqual(levels["events_default"], 10)

    def test_the_check_is_exact(self):
        p.check_side_power_levels(p.side_power_levels("reports", [ALICE], open_=True), "reports", [ALICE], True)
        with self.assertRaises(p.Untrusted):
            p.check_side_power_levels(p.side_power_levels("questions", [ALICE], open_=True),
                                      "questions", [ALICE], False)
        drifted = p.side_power_levels("reports", [ALICE], open_=True)
        drifted["users_default"] = 10.0
        with self.assertRaises(p.Untrusted):
            p.check_side_power_levels(drifted, "reports", [ALICE], True)
        with self.assertRaises(ValueError):
            p.side_power_levels("chat", [ALICE], open_=True)


class JoinRulesTest(unittest.TestCase):
    def test_reports_are_restricted_to_the_main_rooms_members(self):
        rule = {"join_rule": "restricted", "allow": [{"type": "m.room_membership", "room_id": MAIN}]}
        self.assertEqual(p.expected_join_rules("reports", MAIN), rule)
        p.check_join_rules(rule, "reports", MAIN)

    def test_questions_are_invite_only(self):
        p.check_join_rules({"join_rule": "invite"}, "questions", MAIN)
        for rule in ({"join_rule": "public"}, {"join_rule": "restricted", "allow": []},
                     {"join_rule": "invite", "allow": []}):
            with self.subTest(rule), self.assertRaises(p.Untrusted):
                p.check_join_rules(rule, "questions", MAIN)
        with self.assertRaises(p.Untrusted):
            p.check_join_rules(p.expected_join_rules("reports", QUESTIONS), "reports", MAIN)


class ReportTest(unittest.TestCase):
    def test_a_report_is_a_quiet_notice(self):
        content = p.build_report("Merged the fix\nPR 12 is in.")
        self.assertEqual(content, {"msgtype": "m.notice", "body": "Merged the fix\nPR 12 is in.",
                                   "m.mentions": {}, p.REPORT_KEY: {"v": 1}})
        p.check_report_content(content)

    def test_caps_are_exact_to_the_byte(self):
        p.check_report_text("h\n" + "x" * (p.MAX_REPORT_BYTES - 2))
        with self.assertRaises(p.Refusal) as caught:
            p.check_report_text("h\n" + "x" * (p.MAX_REPORT_BYTES - 1))
        self.assertEqual(caught.exception.reason, "body")
        p.check_report_text("h" * p.HEADLINE_MAX)
        with self.assertRaises(p.Refusal):
            p.check_report_text("h" * (p.HEADLINE_MAX + 1))

    def test_empty_headline_or_secret_is_refused(self):
        for text, reason in (("", "body"), ("\nbody", "body"), ("  \nbody", "body"),
                             ("leak\nghp_" + "a" * 36, "secret")):
            with self.subTest(text), self.assertRaises(p.Refusal) as caught:
                p.check_report_text(text)
            self.assertEqual(caught.exception.reason, reason)

    def test_report_content_shape(self):
        good = p.build_report("headline")
        for name, bad in {"mentions": {**good, "m.mentions": {"user_ids": [ALICE]}},
                          "msgtype": {**good, "msgtype": "m.text"},
                          "version": {**good, p.REPORT_KEY: {"v": 2}},
                          "extra": {**good, "x": 1}}.items():
            with self.subTest(name), self.assertRaises(p.Refusal):
                p.check_report_content(bad)


class ThreadTextTest(unittest.TestCase):
    def test_cap_empty_and_secret(self):
        p.check_thread_text("x" * p.MAX_THREAD_BYTES, closing=False)
        p.check_thread_text("", closing=True)
        for text, closing, reason in (("x" * (p.MAX_THREAD_BYTES + 1), False, "body"), ("", False, "body"),
                                      ("ghp_" + "a" * 36, True, "secret")):
            with self.subTest(reason=reason, closing=closing), self.assertRaises(p.Refusal) as caught:
                p.check_thread_text(text, closing=closing)
            self.assertEqual(caught.exception.reason, reason)


class QuestionTextTest(unittest.TestCase):
    def test_a_full_question_parses(self):
        headline, question = p.check_question_text(QUESTION_TEXT)
        self.assertEqual(headline, "Pick the cost limit")
        self.assertEqual(question, "what is the cap?")

    def test_each_section_is_required(self):
        for missing in ("Context:", "Question:"):
            text = QUESTION_TEXT.replace(missing, "Note:")
            with self.subTest(missing), self.assertRaises(p.Refusal) as caught:
                p.check_question_text(text)
            self.assertEqual(caught.exception.reason, "body")
            self.assertIn(missing, caught.exception.detail)

    def test_options_are_numbered_from_one(self):
        for options in ("Options:\n2. a", "Options:\n1. a\n3. b", "Options:\nyes", "Options:",
                        "Options:\n" + "\n".join(f"{n}. x" for n in range(1, 11))):
            text = "head\nContext: c\nQuestion: q\n" + options
            with self.subTest(options), self.assertRaises(p.Refusal):
                p.check_question_text(text)

    def test_the_byte_cap_and_the_headline(self):
        base = "head\nContext: c\nQuestion: q\n"
        p.check_question_text(base + "x" * (p.MAX_QUESTION_BYTES - len(base)))
        with self.assertRaises(p.Refusal):
            p.check_question_text(base + "x" * (p.MAX_QUESTION_BYTES - len(base) + 1))
        with self.assertRaises(p.Refusal):
            p.check_question_text("Context: c\nQuestion: q")
        with self.assertRaises(p.Refusal):
            p.check_question_text("h" * (p.HEADLINE_MAX + 1) + "\nContext: c\nQuestion: q")

    def test_a_section_twice_is_refused(self):
        with self.assertRaises(p.Refusal):
            p.check_question_text("head\nContext: c\nQuestion: q\nQuestion: again")


class QuestionContentTest(unittest.TestCase):
    def test_the_wire_form_notifies_the_humans(self):
        content = p.build_question(3, QUESTION_TEXT, [BOB, ALICE], [WORKER], [eid(9)])
        self.assertEqual(content["msgtype"], "m.text")
        self.assertTrue(content["body"].startswith("Q3 Pick the cost limit\n"))
        self.assertEqual(content["m.mentions"], {"user_ids": [ALICE, BOB]})
        self.assertEqual(content[p.QUESTION_KEY], {"v": 1, "n": 3, "asked_by": [WORKER], "re": [eid(9)]})

    def test_a_question_event_parses_back(self):
        content = p.build_question(3, QUESTION_TEXT, [ALICE], [], [])
        q = p.parse_question(message(1, ME, content, ts=7), SN)
        self.assertEqual((q.n, q.event_id, q.sender, q.headline, q.question), (3, eid(1), ME, "Pick the cost limit",
                                                                                "what is the cap?"))

    def test_anything_else_is_not_a_question(self):
        good = p.build_question(3, QUESTION_TEXT, [ALICE], [], [])
        cases = {
            "human sender": message(1, ALICE, good),
            "number mismatch": message(1, ME, {**good, "body": "Q4" + good["body"][2:]}),
            "zero": message(1, ME, {**good, p.QUESTION_KEY: {**good[p.QUESTION_KEY], "n": 0}}),
            "extra key": message(1, ME, {**good, p.QUESTION_KEY: {**good[p.QUESTION_KEY], "x": 1}}),
            "no key": message(1, ME, {"msgtype": "m.text", "body": good["body"]}),
        }
        for name, event in cases.items():
            with self.subTest(name):
                self.assertIsNone(p.parse_question(event, SN))

    def test_a_thread_post_and_its_close(self):
        content = p.build_thread_post(eid(1), "Passed on; closing.", [ALICE], ask=False, close=3)
        self.assertEqual(content["msgtype"], "m.notice")
        self.assertEqual(content["m.mentions"], {})
        self.assertEqual(content["m.relates_to"]["rel_type"], "m.thread")
        self.assertEqual(content["m.relates_to"]["event_id"], eid(1))
        self.assertEqual(content[p.QUESTION_STATE_KEY], {"v": 1, "n": 3, "state": "closed"})
        asking = p.build_thread_post(eid(1), "Need a number", [ALICE], ask=True, re=eid(5))
        self.assertEqual(asking["msgtype"], "m.text")
        self.assertEqual(asking["m.mentions"], {"user_ids": [ALICE]})
        self.assertEqual(asking["m.relates_to"]["m.in_reply_to"], {"event_id": eid(5)})
        self.assertNotIn(p.QUESTION_STATE_KEY, asking)
        reply = p.build_thread_post(None, "seen", [ALICE], ask=False, re=eid(5))
        self.assertEqual(reply["m.relates_to"], {"m.in_reply_to": {"event_id": eid(5)}})
        with self.assertRaises(ValueError):
            p.build_thread_post(None, "x", [ALICE], ask=False)


class IndexAndAttachmentTest(unittest.TestCase):
    """SPEC §8.3: the first rule that holds attaches a human message to a question."""

    def setUp(self) -> None:
        self.events = [
            message(1, ME, p.build_question(1, QUESTION_TEXT, [ALICE], [WORKER], [eid(90)])),
            message(2, ME, p.build_question(2, QUESTION_TEXT.replace("cap", "date"), [ALICE], [], [])),
            message(3, ME, p.build_thread_post(eid(1), "a follow-up", [ALICE], ask=True)),
        ]

    def index(self, extra: list[dict] = ()) -> p.QuestionIndex:
        return p.build_question_index([*self.events, *extra], ctx())

    def test_rules_in_order(self):
        cases = {
            "thread": (human("50", {"rel_type": "m.thread", "event_id": eid(2), "is_falling_back": True,
                                    "m.in_reply_to": {"event_id": eid(2)}}), 2),
            "reply to a question": (human("> q\n\n50", {"m.in_reply_to": {"event_id": eid(2)}}), 2),
            "reply to a thread post": (human("50", {"m.in_reply_to": {"event_id": eid(3)}}), 1),
            "prefix": (human("q2: fifty"), 2),
            "prefix with a dot": (human("Q1. fifty"), 1),
            "unknown number": (human("Q9 fifty"), None),
            "prefix needs a separator": (human("Q1fifty"), None),
            "unattached": (human("by the way"), None),
        }
        index = self.index()
        for name, (content, n) in cases.items():
            with self.subTest(name):
                found = p.attach_question(message(10, ALICE, content), index)
                self.assertEqual(None if found is None else found.n, n)

    def test_an_edit_takes_its_originals_attachment(self):
        original = message(10, ALICE, human("Q1 five"))
        index = self.index([original])
        edit = message(11, ALICE, human("* fifty", {"rel_type": "m.replace", "event_id": eid(10)},
                                        **{"m.new_content": {"msgtype": "m.text", "body": "fifty"}}))
        self.assertEqual(p.attach_question(edit, index).n, 1)

    def test_states_and_unanswered_replies(self):
        replies = [message(10, ALICE, human("Q1 five")),
                   message(11, ME, p.build_thread_post(eid(1), "noted", [ALICE], ask=False)),
                   message(12, ALICE, human("Q1 no, fifty")),
                   message(13, ME, p.build_thread_post(eid(2), "", [ALICE], ask=False, close=2))]
        index = self.index(replies)
        views = {view.question.n: view for view in index.views()}
        self.assertEqual(views[1].state, "answered")
        self.assertEqual(views[1].unanswered, (eid(12),))
        self.assertEqual(views[2].state, "closed")
        self.assertEqual(index.next_number(), 3)
        fresh = p.build_question_index([self.events[0]], ctx())
        self.assertEqual(fresh.views()[0].state, "open")
        self.assertEqual(p.build_question_index([], ctx()).next_number(), 1)

    def test_an_open_duplicate_is_found_by_its_question_section(self):
        index = self.index()
        self.assertEqual(index.open_duplicate("what is the cap?").n, 1)
        self.assertIsNone(index.open_duplicate("something else"))


class QuestionsRoomValidateTest(unittest.TestCase):
    def validate(self, content: dict, sender: str = ALICE, *, coordinator: str = ME,
                 human_text: bool = True) -> p.Outcome:
        return p.validate_event(message(10, sender, content), ctx(coordinator), ME,
                                human_text=human_text, room=p.PURPOSE_QUESTIONS)

    def test_an_unaddressed_human_message_reaches_the_coordinator(self):
        outcome = self.validate(human("fifty"))
        self.assertEqual(outcome.kind, p.ACCEPT)
        self.assertEqual(outcome.human.text, "fifty")
        self.assertIsNone(outcome.human.revises)
        # The same message in the main room is not addressed to this member.
        main = p.validate_event(message(10, ALICE, human("fifty")), ctx(), ME)
        self.assertEqual((main.kind, main.reason), (p.IGNORE, p.IGNORE_NOT_ADDRESSED))

    def test_a_worker_never_takes_a_questions_room_item(self):
        outcome = self.validate(human("fifty"), coordinator=WORKER)
        self.assertEqual((outcome.kind, outcome.reason), (p.IGNORE, p.IGNORE_NOT_COORDINATOR))

    def test_an_edit_is_delivered_as_a_revision(self):
        edit = human("* fifty", {"rel_type": "m.replace", "event_id": eid(4)},
                     **{"m.new_content": {"msgtype": "m.text", "body": "fifty"}})
        outcome = self.validate(edit)
        self.assertEqual(outcome.kind, p.ACCEPT)
        self.assertEqual((outcome.human.text, outcome.human.revises), ("fifty", eid(4)))
        main = p.validate_event(message(10, ALICE, {**edit, "m.mentions": {"user_ids": [ME]}}), ctx(), ME)
        self.assertEqual((main.kind, main.reason), (p.DROP, "edit"))
        broken = human("* x", {"rel_type": "m.replace", "event_id": "nope"},
                       **{"m.new_content": {"msgtype": "m.text", "body": "x"}})
        self.assertEqual(self.validate(broken).reason, "schema")

    def test_the_reply_fallback_is_stripped(self):
        outcome = self.validate(human("> Q1 headline\n\nfifty", {"m.in_reply_to": {"event_id": eid(1)}}))
        self.assertEqual(outcome.human.text, "fifty")

    def test_an_agent_sender_and_human_text_off_are_dropped(self):
        self.assertEqual(self.validate(human("hi"), sender=WORKER).reason, "sender")
        self.assertEqual(self.validate(human("hi"), human_text=False).reason, "sender")

    def test_an_unknown_room_is_a_programming_error(self):
        with self.assertRaises(ValueError):
            p.validate_event(message(10, ALICE, human("x")), ctx(), ME, room="reports")


if __name__ == "__main__":
    unittest.main()
