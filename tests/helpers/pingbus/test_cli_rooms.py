"""Unit tests for the side-room commands of helpers/pingbus/cli.py (Plan 00211 SPEC §7, §8).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_cli_rooms

`report`, `ask-human`, `thread`, `questions`, `history --room`, the `HUMAN` line's appended
`ROOM` field and the `REPLY` command for a questions-room item. Real HTTP on loopback to the
fake homeserver, as test_cli.
"""

from __future__ import annotations

import io
import os
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, protocol
from tests.helpers.pingbus import test_cli as bus

QUESTION = "Pick the cost limit\nContext: the nightly run costs money.\nQuestion: what is the cap?"
OTHER = "Pick a branch\nContext: two branches are open.\nQuestion: which one ships?"


class _Unreadable(io.BytesIO):
    """A stdin that fails the test if anything reads it."""

    def read(self, *_args) -> bytes:
        raise AssertionError("stdin was read")

    readline = read


class _Terminal(io.BytesIO):
    def isatty(self) -> bool:
        return True


class RoomsCase(bus.BusCase):
    """Team a with this member as its coordinator; side rooms made on demand."""

    coordinator = True

    def setUp(self) -> None:
        super().setUp()
        a = self.a
        if self.coordinator:
            a.record = dict(a.record, roles={a.me: "coordinator", a.orch: "worker", a.peer: "worker"})
            a.set_record(a.record)
        self.side_txn = 0
        self.reports: str | None = None
        self.questions: str | None = None

    def make_side(self, purpose: str, *, invite: tuple[str, ...] = (), levels: dict | None = None) -> str:
        a = self.a
        initial = [
            {"type": protocol.JOIN_RULES, "state_key": "", "content": protocol.expected_join_rules(purpose, a.room)},
            {"type": protocol.EVENT_ROOM_LINK, "state_key": "",
             "content": {"v": 1, "team": a.name, "purpose": purpose, "main": a.room}},
        ]
        return a.call(a.admin, "POST", "/_matrix/client/v3/createRoom", {
            "preset": "private_chat", "room_version": "12", "name": f"{a.name} {purpose}",
            "power_level_content_override": levels or protocol.side_power_levels(purpose, [a.alice], open_=True),
            "initial_state": initial, "invite": list(invite)})["room_id"]

    def rooms(self, *, questions: bool = True, reports_levels: dict | None = None) -> None:
        a = self.a
        self.reports = self.make_side("reports", invite=(a.alice,), levels=reports_levels)
        index = {"v": 1, "reports": self.reports}
        if questions:
            self.questions = self.make_side("questions", invite=(a.alice, a.me))
            a.call(a.alice, "POST", f"/_matrix/client/v3/rooms/{bus.q(self.questions)}/join", {})
            index["questions"] = self.questions
        a.call(a.admin, "PUT", f"/_matrix/client/v3/rooms/{bus.q(a.room)}/state/{protocol.EVENT_ROOMS}/", index)

    def side_send(self, room: str, uid: str, content: dict) -> str:
        self.side_txn += 1
        return self.a.call(uid, "PUT", f"/_matrix/client/v3/rooms/{bus.q(room)}/send/m.room.message/"
                           f"r{self.side_txn}", content)["event_id"]

    def side_contents(self, room: str) -> list[dict]:
        return [e.content for e in self.a.fake.rooms[room].events if e.type == "m.room.message"]

    def ask(self, n: int = 1, text: str = QUESTION) -> str:
        return self.side_send(self.questions, self.a.me, protocol.build_question(n, text, [self.a.alice], [], []))

    def answer(self, body: str, relates: dict | None = None) -> str:
        content: dict = {"msgtype": "m.text", "body": body}
        if relates is not None:
            content["m.relates_to"] = relates
        return self.side_send(self.questions, self.a.alice, content)

    def lines(self, out: str, kind: str) -> list[list[str]]:
        return [line.split("\t") for line in out.splitlines() if line.split("\t")[0] == kind]


class ReportTest(RoomsCase):
    TEXT = "Merged the fix\nThe nightly job is green again."

    def test_a_report_lands_in_the_reports_room_as_a_quiet_notice(self):
        self.rooms()
        self.joined()
        code, out, err = self.run_cli("report", stdin=self.TEXT)
        self.assertEqual(code, cli.EXIT_OK, err)
        (sent,) = self.lines(out, "SENT")
        self.assertEqual(sent[2], bus.TEAM_A)
        self.assertEqual(self.side_contents(self.reports), [protocol.build_report(self.TEXT)])
        self.assertEqual(self.a.messages_from_me(), [])

    def test_a_worker_reports_too(self):
        a = self.a
        a.record = dict(a.record, roles={a.me: "worker", a.orch: "coordinator", a.peer: "worker"})
        a.set_record(a.record)
        self.rooms(questions=False)
        self.joined()
        self.assertEqual(self.run_cli("report", stdin=self.TEXT)[0], cli.EXIT_OK)

    def test_before_the_rooms_exist_it_is_no_room(self):
        self.joined()
        code, _, err = self.run_cli("report", stdin=self.TEXT)
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("no-room", err)
        self.assertIn("agent-bus play", err)

    def test_over_the_cap_is_refused_never_truncated(self):
        self.rooms()
        self.joined()
        text = "Headline\n" + "x" * (protocol.MAX_REPORT_BYTES - len("Headline\n") + 1)
        code, _, err = self.run_cli("report", stdin=text)
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("body", err)
        self.assertEqual(self.side_contents(self.reports), [])

    def test_the_same_report_twice_is_a_duplicate(self):
        self.rooms()
        self.joined()
        self.assertEqual(self.run_cli("report", stdin=self.TEXT)[0], cli.EXIT_OK)
        code, _, err = self.run_cli("report", stdin=self.TEXT)
        self.assertEqual(code, cli.EXIT_RATE)
        self.assertIn("duplicate", err)

    def test_the_hourly_cap(self):
        self.a.write_bundle(member_limits={"report_per_hour": 2})
        self.rooms()
        self.joined()
        for n in range(2):
            self.assertEqual(self.run_cli("report", stdin=f"Report {n}")[0], cli.EXIT_OK)
        self.assertEqual(self.run_cli("report", stdin="Report 3")[0], cli.EXIT_RATE)

    def test_an_untrusted_reports_room_is_exit_10_naming_it(self):
        levels = protocol.side_power_levels("reports", [self.a.alice], open_=True)
        levels["events_default"] = 0
        self.rooms(reports_levels=levels)
        self.joined()
        code, _, err = self.run_cli("report", stdin=self.TEXT)
        self.assertEqual(code, cli.EXIT_UNTRUSTED)
        self.assertIn("reports room", err)


class AskHumanTest(RoomsCase):
    def setUp(self) -> None:
        super().setUp()
        self.rooms()
        self.joined()

    def test_a_question_is_numbered_after_the_highest_and_mentions_the_humans(self):
        self.ask(1)
        self.ask(2, OTHER)
        code, out, err = self.run_cli("ask-human", stdin="Pick a name\nContext: c.\nQuestion: which name?")
        self.assertEqual(code, cli.EXIT_OK, err)
        (sent,) = self.lines(out, "SENT")
        (question,) = self.lines(out, "QUESTION")
        self.assertEqual(question, ["QUESTION", "1", bus.TEAM_A, "Q3", "open", sent[3], "-", "0"])
        content = self.side_contents(self.questions)[-1]
        self.assertEqual(content["msgtype"], "m.text")
        self.assertEqual(content["m.mentions"], {"user_ids": [self.a.alice]})
        self.assertTrue(content["body"].startswith("Q3 ❓ Pick a name"))

    def test_asked_by_and_re_are_recorded(self):
        worker_event = self.a.send(self.a.peer, protocol.build_text([self.a.me], "what is the cap?"))
        code, out, err = self.run_cli("ask-human", "--asked-by", "peer.1", "--re", worker_event, stdin=QUESTION)
        self.assertEqual(code, cli.EXIT_OK, err)
        content = self.side_contents(self.questions)[-1]
        self.assertEqual(content[protocol.QUESTION_KEY]["asked_by"], [self.a.peer])
        self.assertEqual(content[protocol.QUESTION_KEY]["re"], [worker_event])
        self.assertEqual(self.lines(out, "QUESTION")[0][6], bus.PEER_LP)

    def test_a_re_that_is_not_a_main_room_event_is_refused(self):
        code, _, _ = self.run_cli("ask-human", "--re", bus.UNKNOWN_EVENT, stdin=QUESTION)
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertEqual(self.side_contents(self.questions), [])

    def test_a_missing_section_is_refused_naming_it(self):
        code, _, err = self.run_cli("ask-human", stdin="Pick\nContext: only context.")
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("Question:", err)

    def test_an_open_duplicate_is_refused_naming_it(self):
        self.ask(1)
        code, _, err = self.run_cli("ask-human", stdin=QUESTION.replace("Pick the cost limit", "Again"))
        self.assertEqual(code, cli.EXIT_RATE)
        line = f"{cli.PROG}: {cli.QUESTION_DUPLICATE.format(team=self.a.name, question='Q1')}\n"
        self.assertIn(line, err.splitlines(keepends=True))


class QuestionTypeTest(RoomsCase):
    """A question's type is its root's icon, after `Q<n>`; a decision by default."""

    def setUp(self) -> None:
        super().setUp()
        self.rooms()
        self.joined()

    def test_the_default_type_is_a_decision(self):
        code, _, err = self.run_cli("ask-human", stdin=QUESTION)
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertTrue(self.side_contents(self.questions)[-1]["body"].startswith("Q1 ❓ Pick the cost limit\n"))

    def test_each_type_has_its_icon(self):
        icons = {"decision": "❓", "action": "📨", "approval": "🔐", "suggestion": "💡", "confirm": "ℹ️"}
        for n, (kind, icon) in enumerate(icons.items(), start=1):
            text = f"Pick {kind}\nContext: c.\nQuestion: which {kind}?"
            code, _, err = self.run_cli("ask-human", "--type", kind, stdin=text)
            self.assertEqual(code, cli.EXIT_OK, err)
            self.assertTrue(self.side_contents(self.questions)[-1]["body"].startswith(f"Q{n} {icon} Pick {kind}\n"))

    def test_an_unknown_type_is_a_usage_error(self):
        self.assertEqual(self.run_cli("ask-human", "--type", "urgent", stdin=QUESTION)[0], cli.EXIT_USAGE)
        self.assertEqual(self.side_contents(self.questions), [])

    def test_a_headline_that_already_carries_an_icon_is_refused(self):
        for icon in ("❓", "📨", "🔐", "💡", "ℹ️", "✅"):
            code, _, err = self.run_cli("ask-human", stdin=f"{icon} {QUESTION}")
            self.assertEqual(code, cli.EXIT_REFUSED)
            self.assertIn("--type", err)
        self.assertEqual(self.side_contents(self.questions), [])

    def test_a_typed_question_is_read_with_its_plain_headline(self):
        self.assertEqual(self.run_cli("ask-human", "--type", "approval", stdin=QUESTION)[0], cli.EXIT_OK)
        event = self.a.fake.rooms[self.questions].events[-1]
        found = protocol.parse_question({"type": event.type, "event_id": bus.UNKNOWN_EVENT, "sender": self.a.me,
                                         "content": event.content}, bus.SN_A)
        self.assertEqual((found.n, found.headline), (1, "Pick the cost limit"))


class WorkerAskTest(RoomsCase):
    coordinator = False

    def test_a_worker_cannot_ask_a_human(self):
        self.rooms()
        self.joined()
        code, _, err = self.run_cli("ask-human", stdin=QUESTION)
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("role", err)


class ThreadTest(RoomsCase):
    def setUp(self) -> None:
        super().setUp()
        self.rooms()
        self.joined()
        self.root = self.ask(1)

    def test_a_note_goes_into_the_questions_thread_without_notifying(self):
        code, out, err = self.run_cli("thread", "Q1", stdin="I need it as a number")
        self.assertEqual(code, cli.EXIT_OK, err)
        content = self.side_contents(self.questions)[-1]
        self.assertEqual(content, protocol.build_thread_post(self.root, "I need it as a number", [self.a.alice],
                                                             ask=False))
        self.assertEqual(len(self.lines(out, "SENT")), 1)

    def test_ask_notifies_and_close_closes(self):
        self.assertEqual(self.run_cli("thread", "Q1", "--ask", stdin="A number please")[0], cli.EXIT_OK)
        self.assertEqual(self.side_contents(self.questions)[-1]["m.mentions"], {"user_ids": [self.a.alice]})
        self.assertEqual(self.run_cli("thread", "Q1", "--close")[0], cli.EXIT_OK)
        self.assertEqual(self.side_contents(self.questions)[-2][protocol.QUESTION_STATE_KEY]["state"], "closed")
        code, out, _ = self.run_cli("questions")
        self.assertEqual((code, self.lines(out, "QUESTION")), (cli.EXIT_OK, []))
        self.assertEqual(self.lines(self.run_cli("questions", "--all")[1], "QUESTION")[0][4], "closed")

    def test_close_says_closed_and_retitles_the_root_it_asked(self):
        code, _, err = self.run_cli("thread", "Q1", "--close")
        self.assertEqual(code, cli.EXIT_OK, err)
        post, edit = self.side_contents(self.questions)[-2:]
        self.assertEqual(post["body"], "✅ CLOSED — Q1")
        self.assertEqual(post[protocol.QUESTION_STATE_KEY]["state"], "closed")
        self.assertEqual(edit["m.relates_to"], {"rel_type": "m.replace", "event_id": self.root})
        new = edit["m.new_content"]
        self.assertTrue(new["body"].startswith("Q1 ✅ CLOSED — Pick the cost limit\nContext: "))
        self.assertEqual(new[protocol.QUESTION_KEY]["n"], 1)
        self.assertEqual(edit["body"], "* " + new["body"])
        self.assertNotIn("m.mentions", new)
        self.assertEqual(edit["m.mentions"], {})

    def test_a_retitled_root_still_reads_as_its_question(self):
        self.assertEqual(self.run_cli("thread", "Q1", "--close")[0], cli.EXIT_OK)
        new = self.side_contents(self.questions)[-1]["m.new_content"]
        found = protocol.parse_question({"type": "m.room.message", "event_id": self.root, "sender": self.a.me,
                                         "content": new}, bus.SN_A)
        self.assertEqual((found.n, found.headline, found.question), (1, "Pick the cost limit", "what is the cap?"))
        self.assertEqual(self.lines(self.run_cli("questions", "--all")[1], "QUESTION")[0][4], "closed")

    def test_a_typed_root_keeps_no_type_icon_once_closed(self):
        self.assertEqual(self.run_cli("ask-human", "--type", "action", stdin=OTHER)[0], cli.EXIT_OK)
        self.assertEqual(self.run_cli("thread", "Q2", "--close")[0], cli.EXIT_OK)
        new = self.side_contents(self.questions)[-1]["m.new_content"]
        self.assertTrue(new["body"].startswith("Q2 ✅ CLOSED — Pick a branch\n"))

    def test_a_root_another_seat_asked_is_left_as_it_is_and_said(self):
        a = self.a
        a.call(a.admin, "POST", f"/_matrix/client/v3/rooms/{bus.q(self.questions)}/invite", {"user_id": a.peer})
        a.call(a.peer, "POST", f"/_matrix/client/v3/rooms/{bus.q(self.questions)}/join", {})
        self.side_send(self.questions, a.peer, protocol.build_question(2, OTHER, [a.alice], [], []))
        code, _, err = self.run_cli("thread", "Q2", "--close")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertNotIn("m.new_content", self.side_contents(self.questions)[-1])
        self.assertIn("Q2's root was posted by another seat", err)

    def test_close_reads_no_stdin(self):
        """Issue #260: run with no input in an agent's shell, stdin is an open pipe that never
        ends, so a close that read it waited for ever. A close with no note never touches it."""
        code, _, err = self.run_cli("thread", "Q1", "--close", stdin=_Unreadable())
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.side_contents(self.questions)[-2][protocol.QUESTION_STATE_KEY]["state"], "closed")

    def test_close_with_note_reads_the_note_from_stdin(self):
        code, _, err = self.run_cli("thread", "Q1", "--close", "--note", "-", stdin="Settled: 40")
        self.assertEqual(code, cli.EXIT_OK, err)
        content = self.side_contents(self.questions)[-2]
        self.assertEqual(content[protocol.QUESTION_STATE_KEY]["state"], "closed")
        self.assertIn("Settled: 40", content["body"])

    def test_a_note_is_given_only_as_stdin_and_only_with_close(self):
        self.assertEqual(self.run_cli("thread", "Q1", "--note", "-", stdin="x")[0], cli.EXIT_USAGE)
        self.assertEqual(self.run_cli("thread", "Q1", "--close", "--note", "file.txt")[0], cli.EXIT_USAGE)

    def test_a_post_refuses_a_terminal_at_once(self):
        code, _, err = self.run_cli("thread", "Q1", stdin=_Terminal(b"typed"))
        self.assertEqual(code, cli.EXIT_USAGE)
        self.assertIn("terminal", err)

    def test_a_post_with_no_input_says_so_then_gives_up(self):
        read_fd, write_fd = os.pipe()
        self.addCleanup(os.close, write_fd)
        with os.fdopen(read_fd, "rb") as stream:
            code, _, err = self.run_cli("thread", "Q1", stdin=stream, stdin_wait=lambda fd, timeout: False)
        self.assertEqual(code, cli.EXIT_USAGE)
        self.assertIn("waiting for input on stdin", err)
        self.assertIn("no input arrived on stdin within 60 s", err)
        self.assertEqual(self.side_contents(self.questions)[-1][protocol.QUESTION_KEY]["n"], 1)

    def test_re_answers_an_unattached_message(self):
        loose = self.answer("by the way, the budget moved")
        self.assertEqual(self.run_cli("thread", "--re", loose, stdin="Noted")[0], cli.EXIT_OK)
        relates = self.side_contents(self.questions)[-1]["m.relates_to"]
        self.assertEqual(relates, {"m.in_reply_to": {"event_id": loose}})

    def test_an_unknown_question_is_a_usage_error(self):
        self.assertEqual(self.run_cli("thread", "Q9", stdin="hello")[0], cli.EXIT_USAGE)
        self.assertEqual(self.run_cli("thread", stdin="hello")[0], cli.EXIT_USAGE)


class QuestionsTest(RoomsCase):
    def close(self, n: int, root: str) -> str:
        return self.side_send(self.questions, self.a.me,
                              protocol.build_thread_post(root, "", [self.a.alice], ask=False, close=n))

    def test_retitle_closed_edits_each_closed_root_of_this_seat_once(self):
        a = self.a
        self.rooms()
        self.joined()
        a.call(a.admin, "POST", f"/_matrix/client/v3/rooms/{bus.q(self.questions)}/invite", {"user_id": a.peer})
        a.call(a.peer, "POST", f"/_matrix/client/v3/rooms/{bus.q(self.questions)}/join", {})
        q1 = self.ask(1)
        q2 = self.side_send(self.questions, a.peer, protocol.build_question(2, OTHER, [a.alice], [], []))
        self.ask(3, "Pick a day\nContext: c.\nQuestion: which day?")
        self.close(1, q1)
        self.close(2, q2)
        code, out, err = self.run_cli("questions", "--retitle-closed")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual([r[:4] for r in self.lines(out, "RETITLED")], [["RETITLED", "1", bus.TEAM_A, "Q1"]])
        self.assertIn("Q2's root was posted by another seat", err)
        edits = [c for c in self.side_contents(self.questions) if "m.new_content" in c]
        self.assertEqual([e["m.relates_to"]["event_id"] for e in edits], [q1])
        self.assertTrue(edits[0]["m.new_content"]["body"].startswith("Q1 ✅ CLOSED — Pick the cost limit\n"))
        code, out, _ = self.run_cli("questions", "--retitle-closed")
        self.assertEqual((code, self.lines(out, "RETITLED")), (cli.EXIT_OK, []))
        self.assertEqual(len([c for c in self.side_contents(self.questions) if "m.new_content" in c]), 1)

    def test_states_unanswered_and_reply_to(self):
        self.rooms()
        self.joined()
        self.ask(1)
        q2 = self.ask(2, OTHER)
        reply = self.answer("ship main", {"rel_type": "m.thread", "event_id": q2})
        code, out, err = self.run_cli("questions")
        self.assertEqual(code, cli.EXIT_OK, err)
        rows = self.lines(out, "QUESTION")
        self.assertEqual([(r[3], r[4], r[7]) for r in rows], [("Q1", "open", "0"), ("Q2", "answered", "1")])
        self.assertEqual(self.lines(out, "REPLY_TO"), [["REPLY_TO", "1", bus.TEAM_A, "Q2", reply]])


class QuestionsExitCodesTest(RoomsCase):
    """`history --room questions` exits as `questions` does for the same situation."""

    def test_before_the_rooms_exist_both_are_no_room(self):
        self.joined()
        for args in (("questions",), ("history", "--room", "questions")):
            with self.subTest(args=args):
                code, _, err = self.run_cli(*args)
                self.assertEqual(code, cli.EXIT_REFUSED, err)
                self.assertIn("no-room", err)


class WorkerQuestionsExitCodesTest(RoomsCase):
    coordinator = False

    def test_for_a_worker_both_are_refused_by_role(self):
        self.rooms()
        self.joined()
        for args in (("questions",), ("history", "--room", "questions")):
            with self.subTest(args=args):
                code, _, err = self.run_cli(*args)
                self.assertEqual(code, cli.EXIT_REFUSED, err)
                self.assertIn("role", err)


class HumanLineTest(RoomsCase):
    def setUp(self) -> None:
        super().setUp()
        self.rooms()
        self.joined()

    def test_a_main_room_human_line_ends_with_no_room(self):
        self.a.human("please halt")
        code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        (human,) = self.lines(out, "HUMAN")
        self.assertEqual(len(human), 8)
        self.assertEqual(human[7], "-")

    def test_an_answer_in_a_thread_names_its_question_and_replies_by_thread(self):
        root = self.ask(1)
        reply = self.answer("cap it at 10", {"rel_type": "m.thread", "event_id": root})
        code, out, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK, err)
        (human,) = self.lines(out, "HUMAN")
        self.assertEqual((human[3], human[7]), (reply, "questions:Q1"))
        (command,) = self.lines(out, "REPLY")
        self.assertEqual(command[4], f"pingbus --team {bus.TEAM_A} thread Q1")

    def test_an_unattached_message_replies_by_its_event(self):
        loose = self.answer("unprompted: the budget moved")
        code, out, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.lines(out, "HUMAN")[0][7], "questions:-")
        self.assertEqual(self.lines(out, "REPLY")[0][4], f"pingbus --team {bus.TEAM_A} thread --re '{loose}'")

    def test_history_room_questions_shows_the_room(self):
        root = self.ask(1)
        self.answer("cap it at 10", {"rel_type": "m.thread", "event_id": root})
        code, out, err = self.run_cli("history", "--room", "questions")
        self.assertEqual(code, cli.EXIT_OK, err)
        rows = self.lines(out, "HISTORY")
        self.assertIn(("in", "HUMAN"), [(r[2], r[4]) for r in rows])
        self.assertIn("questions:Q1", [r[-1] for r in rows if r[4] == "HUMAN"])


if __name__ == "__main__":
    unittest.main()
