"""Unit tests for `pingbus history` (helpers/pingbus/cli.py) and the backward read it rests
on (`Syncer.history`, helpers/pingbus/syncer.py).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_cli_history

Spec: docs/agent-bus-protocol.md §13 (`history`), §15 (the `HISTORY` line), §9 (the
receive checks every item goes through). Plan 00161's DESIGN.md section 5.5 ("History"),
D43 and section 12 row U33: the room, not the local cache, is the source, so a seat that
comes back with fresh local state still reads its earlier items; nothing is consumed,
acked or moved.

Every command runs against the U08 fake homeserver on loopback, through `test_cli`'s
`BusCase` (a real client, syncer and inbox; a stand-in forge; tokens checked absent from
every output at tear-down).
"""

from __future__ import annotations

import hashlib
import io
import pathlib
import re
import shutil
import sys
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, protocol, syncer
from tests.helpers.pingbus import test_cli as bus

SHA = bus.SHA
PATH_REF = bus.PATH_REF
DAY_S = 86400
HISTORY_RE = re.compile(
    rf"HISTORY\t1\t(?:in|out)\t\d+\t(?:PING|HUMAN|SENT)\t[a-z][a-z0-9-]{{0,23}}"
    rf"\t{protocol.EVENT_ID_PATTERN}(?:\t[^\t\n]+)*")


class HistoryCase(bus.BusCase):
    def history(self, *argv: str) -> tuple[int, list[list[str]], str]:
        code, out, err = self.run_cli("history", *argv)
        for line in out.splitlines():
            self.assertTrue(HISTORY_RE.fullmatch(line), line)
        return code, [line.split("\t") for line in out.splitlines()], err

    @staticmethod
    def ids(rows: list[list[str]]) -> list[str]:
        return [row[6] for row in rows]

    def human_to(self, users: list[str] | None, body: str = "a note", room: bool = False) -> str:
        mentions: dict[str, object] = {"user_ids": users or []}
        if room:
            mentions["room"] = True
        return self.a.send(self.a.alice, {"msgtype": "m.text", "body": body, "m.mentions": mentions})

    def ping_to(self, sender: str, to: list[str], verb: str = "review", ref: str | None = PATH_REF) -> str:
        return self.a.send(sender, protocol.build_ping(verb, to, ref=ref))

    def tree(self) -> dict[str, str]:
        """Every file under the bundle's state directory, by path, with a digest of its bytes."""
        root = self.a.state.path
        return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in sorted(root.rglob("*")) if p.is_file()}


class HistoryLineTest(unittest.TestCase):
    TEAM, SN = "team-a", "team-a.agent-bus.internal"
    EVENT = "$" + "A" * 43
    ME = f"@myrepo.1+workstation.podman:{SN}"
    ORCH = f"@orch.1+workstation.podman:{SN}"

    def test_a_ping_received(self) -> None:
        ping = protocol.Ping("review", (self.ME,), protocol.parse_ref(PATH_REF), None,
                             sender=self.ORCH, event_id=self.EVENT, origin_server_ts=7)
        line = cli.history_line(self.TEAM, self.SN, syncer.HistoryItem("in", 7, ping=ping))
        self.assertEqual(line, f"HISTORY\t1\tin\t7\tPING\t{self.TEAM}\t{self.EVENT}"
                               f"\torch.1+workstation.podman\treview\t{PATH_REF}\t-")

    def test_a_human_message_is_escaped_as_its_human_line(self) -> None:
        human = protocol.HumanMessage(self.EVENT, f"@alice:{self.SN}", 9, "two\tfields\nno")
        line = cli.history_line(self.TEAM, self.SN, syncer.HistoryItem("in", 9, human=human))
        self.assertEqual(line, f'HISTORY\t1\tin\t9\tHUMAN\t{self.TEAM}\t{self.EVENT}\talice\t9'
                               '\t"two\\tfields\\nno"')

    def test_an_agent_text_sent_is_its_sent_line_without_the_text(self) -> None:
        line = cli.history_line(self.TEAM, self.SN, syncer.HistoryItem("out", 3, sent_event_id=self.EVENT))
        self.assertEqual(line, f"HISTORY\t1\tout\t3\tSENT\t{self.TEAM}\t{self.EVENT}")

    def test_a_bad_direction_or_time_is_refused(self) -> None:
        for item in (syncer.HistoryItem("sideways", 3, sent_event_id=self.EVENT),
                     syncer.HistoryItem("out", -1, sent_event_id=self.EVENT),
                     syncer.HistoryItem("out", True, sent_event_id=self.EVENT),
                     syncer.HistoryItem("out", 3)):
            with self.subTest(item=item), self.assertRaises(ValueError):
                cli.history_line(self.TEAM, self.SN, item)

    def test_the_line_goes_to_stdout(self) -> None:
        out, err = io.StringIO(), io.StringIO()
        cli.emit(cli.history_line(self.TEAM, self.SN, syncer.HistoryItem("out", 3, sent_event_id=self.EVENT)),
                 out, err)
        self.assertEqual(err.getvalue(), "")
        self.assertTrue(out.getvalue().startswith("HISTORY\t"))


class HistoryTest(HistoryCase):
    def test_sent_and_received_items_newest_first(self) -> None:
        self.joined()
        ping = self.ping_to(self.a.orch, [self.a.me])
        self.now += 1
        human = self.a.human("please look")
        self.now += 1
        code, out, err = self.run_cli("send", "ack", "--to", bus.ORCH_LP, "--re", ping)
        self.assertEqual(code, cli.EXIT_OK, err)
        ack = out.split("\t")[3].strip()
        self.now += 1
        code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.ids(rows), [ack, human, ping])
        self.assertEqual([(r[2], r[4]) for r in rows], [("out", "PING"), ("in", "HUMAN"), ("in", "PING")])
        self.assertEqual(rows[0][7:], ["myrepo.1+workstation.podman", "ack", "-", ping])
        self.assertEqual(rows[1][7:], ["alice", str(int((bus.T0 + 1) * 1000)), '"please look"'])
        self.assertEqual(rows[2][7:], ["orch.1+workstation.podman", "review", PATH_REF, "-"])
        self.assertEqual([int(r[3]) for r in rows], sorted((int(r[3]) for r in rows), reverse=True))

    def test_only_items_naming_or_sent_by_this_seat(self) -> None:
        self.joined()
        mine = self.ping_to(self.a.orch, [self.a.me, self.a.peer])
        self.ping_to(self.a.orch, [self.a.peer])
        self.human_to([self.a.orch], "to the orchestrator only")
        everyone = self.human_to(None, "to the room", room=True)
        self.ping_to(self.a.peer, [self.a.orch], verb="done")
        code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.ids(rows), [everyone, mine])
        self.assertNotIn("DROPPED", err)

    def test_an_agent_text_this_seat_sent_is_listed_without_its_text(self) -> None:
        self.joined()
        code, out, err = self.run_cli("say", "--to", "alice", stdin="the review is done\n")
        self.assertEqual(code, cli.EXIT_OK, err)
        said = out.split("\t")[3].strip()
        code, rows, _ = self.history()
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(rows, [["HISTORY", "1", "out", str(int(bus.T0 * 1000)), "SENT", bus.TEAM_A, said]])

    def test_an_item_older_than_any_receive_limit_is_still_listed(self) -> None:
        self.joined()
        ping = self.ping_to(self.a.orch, [self.a.me])
        human = self.a.human("old news")
        self.now += 30 * DAY_S
        self.assertEqual(self.ids(self.history()[1]), [human, ping])

    def test_the_ping_path_runs_the_forge_check(self) -> None:
        self.joined()
        ref = f"commit:example-org/myrepo@{SHA}"
        self.forge.fail[ref] = "not-found"
        self.ping_to(self.a.orch, [self.a.me], ref=ref)
        good = self.ping_to(self.a.orch, [self.a.me])
        code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(self.ids(rows), [good])
        self.assertIn(ref, self.forge.checked)
        self.assertIn("DROPPED\t1\t1\tunresolved=1", err)

    def test_a_forged_or_role_less_event_is_never_printed(self) -> None:
        self.joined()
        stranger = self.a.fake.add_user("stranger.1+workstation.podman")
        self.a.tokens[stranger] = self.a.fake.mint_token(stranger)
        self.a.call(self.a.admin, "POST", f"/_matrix/client/v3/rooms/{bus.q(self.a.room)}/invite",
                    {"user_id": stranger})
        self.a.call(stranger, "POST", f"/_matrix/client/v3/rooms/{bus.q(self.a.room)}/join", {})
        self.ping_to(stranger, [self.a.me])
        forged = protocol.build_ping("review", [self.a.me], ref=PATH_REF)
        forged["body"] = "[agent-bus] something else"
        self.a.send(self.a.orch, forged)
        self.a.send(self.a.peer, {"msgtype": "m.text", "body": "agent free text",
                                  "m.mentions": {"user_ids": [self.a.me]}})
        self.a.send(self.a.peer, protocol.build_ping("halt", [self.a.me]))
        code, rows, err = self.history()
        self.assertEqual((code, rows), (cli.EXIT_OK, []))
        self.assertIn("DROPPED\t1\t4\tschema=1,sender=1,role=1,body=1", err)

    def test_a_ping_from_a_member_whose_role_was_removed_is_not_printed(self) -> None:
        self.joined()
        self.ping_to(self.a.peer, [self.a.me], verb="done")
        roles = {self.a.me: "worker", self.a.orch: "orchestrator"}
        self.a.set_record(dict(self.a.record, roles=roles))
        code, rows, err = self.history()
        self.assertEqual((code, rows), (cli.EXIT_OK, []))
        self.assertIn("sender=1", err)

    def test_human_text_false_lists_no_human_message(self) -> None:
        self.a.write_bundle(human_text=False)
        self.joined()
        self.a.human("ignored by this member")
        ping = self.ping_to(self.a.orch, [self.a.me])
        self.assertEqual(self.ids(self.history()[1]), [ping])

    def test_a_redacted_own_ping_is_not_printed(self) -> None:
        self.joined()
        code, out, err = self.run_cli("send", "done", PATH_REF, "--to", bus.ORCH_LP)
        self.assertEqual(code, cli.EXIT_OK, err)
        sent = out.split("\t")[3].strip()
        self.a.call(self.a.admin, "PUT", f"/_matrix/client/v3/rooms/{bus.q(self.a.room)}/redact/"
                    f"{bus.q(sent)}/r1", {})
        code, rows, err = self.history()
        self.assertEqual((code, rows), (cli.EXIT_OK, []))
        self.assertIn("schema=1", err)


class HistoryLimitTest(HistoryCase):
    def test_the_default_is_50_newest(self) -> None:
        self.joined()
        sent = [self.human_to([self.a.me], f"note {i}") for i in range(55)]
        code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.ids(rows), sent[::-1][:50])

    def test_limit_n(self) -> None:
        self.joined()
        sent = [self.human_to([self.a.me], f"note {i}") for i in range(5)]
        self.assertEqual(self.ids(self.history("--limit", "2")[1]), sent[::-1][:2])

    def test_the_limit_is_held_to_its_bounds(self) -> None:
        self.joined()
        for value in ("0", "-1", "501", "many"):
            with self.subTest(value=value):
                code, rows, err = self.history("--limit", value)
                self.assertEqual((code, rows), (cli.EXIT_USAGE, []))
                self.assertIn("--limit", err)
        self.assertEqual(self.history("--limit", "500")[0], cli.EXIT_OK)

    def test_pages_until_the_limit_is_met(self) -> None:
        self.joined()
        old = self.human_to([self.a.me], "the oldest")
        for i in range(12):
            self.ping_to(self.a.orch, [self.a.peer])
        new = self.human_to([self.a.me], "the newest")
        with mock.patch.object(syncer, "HISTORY_PAGE_LIMIT", 4):
            code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.ids(rows), [new, old])

    def test_a_scan_that_reaches_its_page_cap_says_so(self) -> None:
        self.joined()
        old = self.human_to([self.a.me], "beyond the cap")
        for i in range(10):
            self.ping_to(self.a.orch, [self.a.peer])
        new = self.human_to([self.a.me], "within the cap")
        with mock.patch.object(syncer, "HISTORY_PAGE_LIMIT", 3), \
                mock.patch.object(syncer, "HISTORY_MAX_PAGES", 2):
            code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.ids(rows), [new])
        self.assertNotIn(old, self.ids(rows))
        self.assertIn("older messages were not read", err)


class HistoryStateTest(HistoryCase):
    def test_nothing_is_consumed_acked_or_moved(self) -> None:
        self.joined()
        first = self.ping_to(self.a.orch, [self.a.me])
        self.assertEqual(self.run_cli("recv")[0], cli.EXIT_OK)
        second = self.ping_to(self.a.orch, [self.a.me])
        before = self.tree()
        sent_before = len(self.a.messages_from_me())
        code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.ids(rows), [second, first])
        self.assertEqual(self.tree(), before)
        self.assertEqual(len(self.a.messages_from_me()), sent_before)
        code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual([line.split("\t")[3] for line in out.splitlines()], [second])

    def test_a_seat_returned_with_fresh_local_state_still_sees_its_items(self) -> None:
        self.joined()
        ping = self.ping_to(self.a.orch, [self.a.me])
        self.assertEqual(self.run_cli("recv")[0], cli.EXIT_OK)
        code, out, _ = self.run_cli("send", "ack", "--to", bus.ORCH_LP, "--re", ping)
        ack = out.split("\t")[3].strip()
        shutil.rmtree(self.a.state.path)
        code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.ids(rows), [ack, ping])
        self.assertIsNone(self.a.state.sync_token())

    def test_an_untrusted_room_prints_nothing_and_is_exit_10(self) -> None:
        self.joined()
        self.ping_to(self.a.orch, [self.a.me])
        self.a.set_record(dict(self.a.record, roles={self.a.orch: "orchestrator"}))
        code, rows, _ = self.history()
        self.assertEqual((code, rows), (cli.EXIT_UNTRUSTED, []))

    def test_a_room_never_joined_is_exit_10(self) -> None:
        code, rows, _ = self.history()
        self.assertEqual((code, rows), (cli.EXIT_UNTRUSTED, []))

    def test_no_active_team_is_78(self) -> None:
        env = dict(self.env)
        del env["PINGBUS_TEAMS"]
        self.assertEqual(self.run_cli("history", env=env)[0], cli.EXIT_CONFIG)


class HistoryTeamsTest(HistoryCase):
    def test_every_active_team_in_order_or_the_one_named(self) -> None:
        b = self.add_team()
        self.joined()
        in_a = self.ping_to(self.a.orch, [self.a.me])
        in_b = b.ping(b.orch, "review", PATH_REF)
        code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual([(r[5], r[6]) for r in rows], [(bus.TEAM_A, in_a), (bus.TEAM_B, in_b)])
        code, out, err = self.run_cli("--team", bus.TEAM_B, "history")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual([line.split("\t")[6] for line in out.splitlines()], [in_b])

    def test_a_failing_team_is_reported_and_the_others_still_print(self) -> None:
        b = self.add_team()
        self.joined()
        in_b = b.ping(b.orch, "review", PATH_REF)
        self.a.set_record(dict(self.a.record, roles={self.a.orch: "orchestrator"}))
        code, rows, err = self.history()
        self.assertEqual(code, cli.EXIT_UNTRUSTED)
        self.assertEqual(self.ids(rows), [in_b])
        self.assertIn(f"team {bus.TEAM_A}", err)


if __name__ == "__main__":
    unittest.main()
