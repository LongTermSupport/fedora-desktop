"""Plan 00201 M5 (S-C8, S-C9): `send` and `say` of a session on several teams infer the team when
that is unambiguous (the event `--re` belongs to one team's room, or every `--to` name resolves
in exactly one active team) and otherwise fail with the choices; `--team` always wins."""

from __future__ import annotations

import os
import sys
import unittest
from urllib.parse import quote

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.pingbus import cli, protocol
from tests.helpers.pingbus.test_cli import TEAM_A, TEAM_B, BusCase

SOLO_LP = "solo.1+workstation.podman"
LONE_LP = "lone.1+workstation.podman"
NOT_INFERABLE = "several teams are active and --to/--re fit more than one (or none): name one with --team:"


class InferTeamTest(BusCase):
    def add_agent(self, team, localpart: str) -> str:
        """A worker only this team has: made, invited, joined and put in the team record."""
        user = team.fake.add_user(localpart)
        team.tokens[user] = team.fake.mint_token(user)
        room = quote(team.room, safe="")
        team.call(team.admin, "POST", f"/_matrix/client/v3/rooms/{room}/invite", {"user_id": user})
        team.call(user, "POST", f"/_matrix/client/v3/rooms/{room}/join", {})
        team.set_record(dict(team.record, roles=dict(team.record["roles"], **{user: "worker"})))
        team.record = dict(team.record, roles=dict(team.record["roles"], **{user: "worker"}))
        return user

    def two_teams(self) -> None:
        """Teams A and B; B alone has the agent `solo.1`; A has a description."""
        b = self.add_team()
        self.solo = self.add_agent(b, SOLO_LP)
        self.lone = self.add_agent(self.a, LONE_LP)
        self.joined()
        self.a.state.save_about("Builds the example site")

    def test_say_to_a_name_only_one_team_has_goes_to_that_team(self):
        self.two_teams()
        code, out, err = self.run_cli("say", "--to", "solo.1", stdin="hello")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertTrue(out.startswith(f"SENT\t1\t{TEAM_B}\t"), out)
        self.assertEqual(self.a.messages_from_me(), [])
        self.assertEqual(self.b.messages_from_me(), [protocol.build_text([self.solo], "hello")])

    def test_a_full_user_id_names_its_teams_server(self):
        self.two_teams()
        code, out, err = self.run_cli("say", "--to", self.solo, stdin="hello")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertTrue(out.startswith(f"SENT\t1\t{TEAM_B}\t"), out)

    def test_send_re_an_event_of_one_teams_room_goes_to_that_team(self):
        self.two_teams()
        event = self.b.ping(self.b.orch, "halt")
        self.run_cli("recv")
        code, out, err = self.run_cli("send", "ack", "--to", "orch.1", "--re", event)
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertTrue(out.startswith(f"SENT\t1\t{TEAM_B}\t"), out)
        self.assertEqual(self.a.messages_from_me(), [])
        self.assertEqual(len(self.b.messages_from_me()), 1)

    def test_a_name_both_teams_have_fits_more_than_one_and_lists_the_choices(self):
        self.two_teams()
        code, out, err = self.run_cli("say", "--to", "peer.1", stdin="hello")
        self.assertEqual((code, out), (cli.EXIT_USAGE, ""))
        self.assertIn(NOT_INFERABLE, err)
        self.assertIn(f"  {TEAM_A}  Builds the example site", err)
        self.assertIn(f"  {TEAM_B}  (no description)", err)
        self.assertEqual((self.a.messages_from_me(), self.b.messages_from_me()), ([], []))

    def test_a_name_no_team_has_fits_none(self):
        self.two_teams()
        code, _, err = self.run_cli("say", "--to", "nobody.9", stdin="hello")
        self.assertEqual(code, cli.EXIT_USAGE)
        self.assertIn(NOT_INFERABLE, err)

    def test_names_split_across_teams_fit_none(self):
        self.two_teams()
        code, _, err = self.run_cli("say", "--to", "solo.1,lone.1", stdin="hello")
        self.assertEqual(code, cli.EXIT_USAGE)
        self.assertIn(NOT_INFERABLE, err)

    def test_the_coordinator_alone_is_not_enough_to_infer(self):
        self.two_teams()
        code, _, err = self.run_cli("say", "--to-coordinator", stdin="hello")
        self.assertEqual(code, cli.EXIT_USAGE)
        self.assertIn(NOT_INFERABLE, err)

    def test_an_event_no_team_holds_falls_back_to_the_names(self):
        self.two_teams()
        code, _, err = self.run_cli("send", "ack", "--to", "orch.1", "--re", "$" + "A" * 43)
        self.assertEqual(code, cli.EXIT_USAGE)
        self.assertIn(NOT_INFERABLE, err)

    def test_a_team_with_no_verified_record_is_unknown_so_nothing_is_inferred(self):
        self.two_teams()
        self.b.state.forget_team_record()
        code, out, err = self.run_cli("say", "--to", "peer.1", stdin="hello")
        self.assertEqual((code, out), (cli.EXIT_USAGE, ""))
        self.assertIn(NOT_INFERABLE, err)
        self.assertIn(f"  {TEAM_B}  (no verified team record yet: run pingbus recv or name it with --team)", err)
        self.assertIn(f"  {TEAM_A}  Builds the example site", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_an_unknown_team_does_not_stop_re_inference(self):
        self.two_teams()
        event = self.a.ping(self.a.orch, "halt")
        self.run_cli("recv")
        self.b.state.forget_team_record()
        code, out, err = self.run_cli("send", "ack", "--to", "orch.1", "--re", event)
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertTrue(out.startswith(f"SENT\t1\t{TEAM_A}\t"), out)

    def test_team_always_wins(self):
        self.two_teams()
        code, out, err = self.run_cli("--team", TEAM_A, "say", "--to", "peer.1", stdin="hello")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertTrue(out.startswith(f"SENT\t1\t{TEAM_A}\t"), out)
        code, _, err = self.run_cli("--team", TEAM_A, "say", "--to", "solo.1", stdin="hello")
        self.assertEqual(code, cli.EXIT_USAGE)
        self.assertIn(f"no member of team {TEAM_A} is named that", err)

    def test_one_active_team_needs_no_inference(self):
        self.joined()
        code, out, err = self.run_cli("say", "--to", "peer.1", stdin="hello")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertTrue(out.startswith(f"SENT\t1\t{TEAM_A}\t"), out)


if __name__ == "__main__":
    unittest.main()
