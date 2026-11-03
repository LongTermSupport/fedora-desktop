"""Unit tests for where an agent's words to a human go (Plan 00211 Task 2.14).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_cli_human_routing

The defect class: an agent's message to a human sent where the human cannot answer it. In the
main room a human's threaded reply carries no mention, so it never reached the agent. Once the
team has its side rooms, `say --to HUMAN` and `send VERB --to HUMAN` in the main room are
refused, naming the way that works for the sender's role; the `REPLY` command of a main-room
`HUMAN` line names that way too; and a human's reply under an agent's message is delivered to
that agent. Real HTTP on loopback to the fake homeserver, as test_cli.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, protocol
from tests.helpers.pingbus import test_cli as bus
from tests.helpers.pingbus import test_cli_rooms as rooms


def thread_reply(root: str, body: str) -> dict:
    """A human's reply in the thread of `root`, as Element sends it: no mention."""
    return {"msgtype": "m.text", "body": body,
            "m.relates_to": {"rel_type": "m.thread", "event_id": root, "is_falling_back": True,
                             "m.in_reply_to": {"event_id": root}}}


class WorkerMainRoomTest(rooms.RoomsCase):
    coordinator = False

    def test_a_worker_is_refused_a_human_in_the_main_room_and_told_the_ways(self):
        self.rooms(questions=False)
        self.joined()
        code, _, err = self.run_cli("say", "--to", "alice", stdin="the fix is merged")
        self.assertEqual(code, cli.EXIT_REFUSED, err)
        self.assertIn("refused: target", err)
        self.assertIn("--to-coordinator", err)
        self.assertIn("pingbus report", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_a_worker_is_refused_a_verb_to_a_human_in_the_main_room(self):
        self.rooms(questions=False)
        self.joined()
        asked = self.a.human("please look")
        code, _, err = self.run_cli("send", "ack", "--to", "alice", "--re", asked)
        self.assertEqual(code, cli.EXIT_REFUSED, err)
        self.assertIn("--to-coordinator", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_a_human_beside_an_agent_is_refused_too(self):
        self.rooms(questions=False)
        self.joined()
        code, _, err = self.run_cli("say", "--to", "alice", "--to-coordinator", stdin="done")
        self.assertEqual(code, cli.EXIT_REFUSED, err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_agent_traffic_in_the_main_room_is_unchanged(self):
        self.rooms(questions=False)
        self.joined()
        self.assertEqual(self.run_cli("say", "--to", "peer.1", stdin="INFO x: y")[0], cli.EXIT_OK)
        self.assertEqual(self.run_cli("say", "--to-coordinator", stdin="DONE x: y")[0], cli.EXIT_OK)

    def test_before_the_side_rooms_exist_the_main_room_is_still_the_way_to_a_human(self):
        self.joined()
        self.assertEqual(self.run_cli("say", "--to", "alice", stdin="hello")[0], cli.EXIT_OK)

    def test_the_reply_to_a_humans_main_room_message_goes_to_the_coordinator(self):
        self.rooms(questions=False)
        self.joined()
        asked = self.a.human("why is it red?")
        code, out, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK, err)
        (reply,) = self.lines(out, "REPLY")
        self.assertEqual(reply[4], f"pingbus --team {bus.TEAM_A} say --to-coordinator --re '{asked}'")


class CoordinatorMainRoomTest(rooms.RoomsCase):
    def setUp(self) -> None:
        super().setUp()
        self.rooms()
        self.joined()

    def test_the_coordinator_is_refused_a_human_in_the_main_room_and_told_the_ways(self):
        code, _, err = self.run_cli("say", "--to", "alice", stdin="what cap?")
        self.assertEqual(code, cli.EXIT_REFUSED, err)
        self.assertIn("pingbus ask-human", err)
        self.assertIn("pingbus thread Q<n>", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_say_all_names_the_agents_but_no_human(self):
        code, _, err = self.run_cli("say", "--all", stdin="UPD all: rebase on F44")
        self.assertEqual(code, cli.EXIT_OK, err)
        (sent,) = self.a.messages_from_me()
        self.assertEqual(sorted(sent["m.mentions"]["user_ids"]), sorted([self.a.orch, self.a.peer]))

    def test_the_reply_to_a_humans_main_room_message_opens_a_question(self):
        asked = self.a.human("which branch ships?")
        code, out, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK, err)
        (reply,) = self.lines(out, "REPLY")
        self.assertEqual(reply[4], f"pingbus --team {bus.TEAM_A} ask-human --re '{asked}'")


class ThreadReplyInTheMainRoomTest(rooms.RoomsCase):
    """A human's thread reply under an agent's main-room message reaches that agent, also
    under one it sent before this rule (here posted straight to the room, as an earlier
    pingbus did), and only that agent."""

    coordinator = False

    def setUp(self) -> None:
        super().setUp()
        self.joined()

    def test_a_thread_reply_under_my_message_is_delivered_to_me(self):
        root = self.a.send(self.a.me, protocol.build_text([self.a.alice], "the fix is merged"))
        reply = self.a.send(self.a.alice, thread_reply(root, "thanks, now deploy it"))
        code, out, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK, err)
        (human,) = self.lines(out, "HUMAN")
        self.assertEqual((human[3], human[4]), (reply, "alice"))
        self.assertIn("thanks, now deploy it", human[6])
        code, out, err = self.run_cli("history")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertTrue(any(line[4] == "HUMAN" and line[6] == reply for line in self.lines(out, "HISTORY")), out)

    def test_a_thread_reply_under_another_agents_message_is_not_mine_and_not_unheard(self):
        root = self.a.send(self.a.peer, protocol.build_text([self.a.alice], "the docs are done"))
        self.a.send(self.a.alice, thread_reply(root, "great"))
        code, out, err = self.run_cli("recv")
        self.assertEqual((code, out), (cli.EXIT_NOTHING, ""), err)
        self.assertEqual(self.a.state.ignored_count(), 0)

    def test_a_thread_reply_under_a_humans_message_reached_no_agent(self):
        root = self.a.send(self.a.alice, {"msgtype": "m.text", "body": "note to self"})
        self.a.send(self.a.alice, thread_reply(root, "and another"))
        code, out, err = self.run_cli("recv")
        self.assertEqual((code, out), (cli.EXIT_NOTHING, ""), err)
        self.assertEqual(self.a.state.ignored_count(), 2)


if __name__ == "__main__":
    unittest.main()
