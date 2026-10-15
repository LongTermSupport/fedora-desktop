"""Plan 00201 M3: `recv` prints a ready REPLY command for HUMAN and PEER items, and `--to`
takes a seat's short name (`<repo>.<seat>`) besides a handle, a human localpart or a user ID."""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.pingbus import cli, inbox, protocol
from tests.helpers.pingbus.test_cli import ORCH_LP, PEER_LP, TEAM_A, BusCase

TWIN_LP = "peer.1+lab.host"


class ReplyLineTest(BusCase):
    def test_a_peer_text_is_followed_by_a_reply_command_with_the_short_name(self):
        self.joined()
        event = self.a.send(self.a.peer, protocol.build_text([self.a.me], "the docs are merged"))
        code, out, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.lines(out, "REPLY"),
                         [["REPLY", "1", TEAM_A, event, f"pingbus --team {TEAM_A} say --to peer.1"]])
        self.assertEqual([line.split("\t")[0] for line in out.splitlines()], ["PEER", "REPLY"])

    def test_a_human_message_is_followed_by_a_reply_command_with_the_localpart(self):
        self.joined()
        event = self.a.human()
        out = self.run_cli("recv")[1]
        self.assertEqual(self.lines(out, "REPLY"),
                         [["REPLY", "1", TEAM_A, event, f"pingbus --team {TEAM_A} say --to alice"]])

    def test_a_ping_has_no_reply_line(self):
        self.joined()
        self.a.ping(self.a.orch, "halt")
        out = self.run_cli("recv")[1]
        self.assertEqual(len(self.lines(out, "PING")), 1)
        self.assertEqual(self.lines(out, "REPLY"), [])

    def test_a_short_name_two_members_share_gives_the_full_handle(self):
        self.joined()
        twin = self.a.fake.add_user(TWIN_LP)
        self.a.set_record(dict(self.a.record, roles=dict(self.a.record["roles"], **{twin: "worker"})))
        self.a.send(self.a.peer, protocol.build_text([self.a.me], "which peer am I"))
        out = self.run_cli("recv")[1]
        (reply,) = self.lines(out, "REPLY")
        self.assertEqual(reply[4], f"pingbus --team {TEAM_A} say --to {PEER_LP}")

    def test_the_reply_command_is_accepted_by_say(self):
        self.joined()
        self.a.send(self.a.peer, protocol.build_text([self.a.me], "ping me back"))
        (reply,) = self.lines(self.run_cli("recv")[1], "REPLY")
        argv = reply[4].split()
        self.assertEqual(argv[0], "pingbus")
        code, _, err = self.run_cli(*argv[1:], stdin="done")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.a.messages_from_me(), [protocol.build_text([self.a.peer], "done")])

    def test_recv_while_a_watcher_holds_the_lock_still_prints_the_reply(self):
        """The inbox-read path (no sync, the cached record) prints REPLY after PEER too."""
        self.joined()
        text = protocol.build_text([self.a.me], "synced by the watcher")
        event = self.a.send(self.a.peer, text)
        self.a.state.commit_batch([{"type": "m.room.message", "event_id": event, "sender": self.a.peer,
                                    "origin_server_ts": 0, "content": text}], self.a.state.sync_token())
        with inbox.acquire_lock(self.a.state, "watch"):
            code, out, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertIn("not syncing: a watch process holds the sync lock; reading the inbox", err)
        self.assertEqual([line.split("\t")[0] for line in out.splitlines()], ["PEER", "REPLY"])
        self.assertEqual(self.lines(out, "REPLY"),
                         [["REPLY", "1", TEAM_A, event, f"pingbus --team {TEAM_A} say --to peer.1"]])

    def test_history_prints_no_reply_line(self):
        self.joined()
        self.a.human()
        self.run_cli("recv")
        self.assertEqual(self.lines(self.run_cli("history")[1], "REPLY"), [])


class ShortNameTest(BusCase):
    def test_send_and_say_take_a_short_name(self):
        self.joined()
        code, _, err = self.run_cli("say", "--to", "peer.1", stdin="hello")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.a.messages_from_me(), [protocol.build_text([self.a.peer], "hello")])
        code, _, err = self.run_cli("send", "ack", "--to", "orch.1", "--re", "$" + "A" * 43)
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(len(self.a.messages_from_me()), 2)

    def test_a_handle_a_localpart_and_a_user_id_still_resolve(self):
        self.joined()
        for number, to in enumerate((PEER_LP, "alice", self.a.peer)):
            code, _, err = self.run_cli("say", "--to", to, stdin=f"hello {number}")
            self.assertEqual(code, cli.EXIT_OK, (to, err))

    def test_an_ambiguous_short_name_is_refused_naming_the_candidates(self):
        self.joined()
        twin = self.a.fake.add_user(TWIN_LP)
        self.a.set_record(dict(self.a.record, roles=dict(self.a.record["roles"], **{twin: "worker"})))
        code, out, err = self.run_cli("say", "--to", "peer.1", stdin="hello")
        self.assertEqual((code, out), (cli.EXIT_USAGE, ""))
        self.assertIn(f"--to peer.1 is ambiguous in team {TEAM_A}: {PEER_LP}, {TWIN_LP}", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_an_unknown_name_is_refused_naming_the_members(self):
        self.joined()
        code, out, err = self.run_cli("say", "--to", "proj.9", stdin="hello")
        self.assertEqual((code, out), (cli.EXIT_USAGE, ""))
        self.assertIn(f"--to proj.9: no member of team {TEAM_A} is named that.", err)
        self.assertIn(PEER_LP, err)
        self.assertIn("Humans: alice", err)

    def test_a_reserved_name_is_still_refused_by_the_validator(self):
        self.joined()
        code, _, err = self.run_cli("say", "--to", "admin", stdin="hello")
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("target", err)

    def test_a_short_name_with_team_named_and_two_teams_active(self):
        """The live case: `say --team <team> --to <repo>.<seat>` on a session with two teams."""
        self.add_team()
        self.joined()
        code, _, err = self.run_cli("say", "--team", TEAM_A, "--to", "peer.1", stdin="hello")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.a.messages_from_me(), [protocol.build_text([self.a.peer], "hello")])

    def test_a_target_refusal_names_what_it_got_and_who_may_be_addressed(self):
        self.joined()
        for to in ("admin", "orch.1,admin"):
            code, out, err = self.run_cli("say", "--to", to, stdin="hello")
            self.assertEqual((code, out), (cli.EXIT_REFUSED, ""), to)
            self.assertIn("refused: target: --to admin cannot be addressed by say", err)
            self.assertIn(f"Agents: {ORCH_LP}, {PEER_LP}", err)
            self.assertIn("Humans: alice", err)
        code, _, err = self.run_cli("send", "halt", "--to", "admin")
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("refused: target: --to admin cannot be addressed by send halt", err)
        self.assertIn("Humans: -", err, "halt does not go to humans")
        code, _, err = self.run_cli("say", "--to", "@room", stdin="hello")
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("refused: target: --to @room cannot be addressed by say", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_the_help_shows_the_forms(self):
        for command in ("say", "send"):
            code, out, _ = self.run_cli(command, "--help")
            self.assertEqual(code, 0)
            for form in ("--to proj.1 ", "--to proj.1+local.podman", "--to alice", "--to @proj.1+local.podman:",
                         "pingbus recv"):
                self.assertIn(form, out, (command, form))


if __name__ == "__main__":
    unittest.main()
