"""Tests for bus_human.py, the scripted test human (Plan 00194 Tasks 2.0c, 2.0d and 2.0f; Plan
00211's acceptance).

Every command runs in process against the fake Tuwunel client API of
tests/helpers/pingbus/fake_client_api.py, served on loopback over real HTTP, so the sender's
requests go through pingbus's own `matrix.Client` exactly as on the host. No real homeserver,
no secret of anyone's: the passwords and tokens are made up here.

Run from anywhere:
    python3 CLAUDE/Plan/00194-agent-bus-live-delivery-supervisor-plugin/test_bus_human.py
"""

from __future__ import annotations

import importlib
import importlib.util
import io
import json
import os
import pathlib
import stat
import sys
import tempfile
import unittest

_HERE = pathlib.Path(__file__).resolve().parent
_ROOT = next(p for p in (_HERE, *_HERE.parents) if (p / "ansible.cfg").exists())
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))
protocol = importlib.import_module("helpers.pingbus.protocol")
fake_api = importlib.import_module("tests.helpers.pingbus.fake_client_api")

_SPEC = importlib.util.spec_from_file_location("bus_human", _HERE / "bus_human.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'bus_human.py'}")
bh = importlib.util.module_from_spec(_SPEC)
sys.modules["bus_human"] = bh
_SPEC.loader.exec_module(bh)

SN = "accept194c.bus"
PASSWORD = "a-made-up-password-for-the-fake-0123"
SEAT = "myrepo.1+workstation.podman"
PEER = "myrepo.2+workstation.podman"
HOME = "/home/u"


class ContentTest(unittest.TestCase):
    """The content Element sends, built without a homeserver."""

    def test_plain_text_has_empty_mentions(self) -> None:
        content = bh.element_content("@room look at this", mentions=[], at_room=False)
        self.assertEqual(content, {"msgtype": "m.text", "body": "@room look at this", "m.mentions": {}})

    def test_a_pill_names_the_user_in_mentions_body_and_html(self) -> None:
        user = f"@{SEAT}:{SN}"
        content = bh.element_content("please <stop> & commit", mentions=[user], at_room=False)
        self.assertEqual(content["m.mentions"], {"user_ids": [user]})
        self.assertEqual(content["body"], f"{user}: please <stop> & commit")
        self.assertEqual(content["format"], "org.matrix.custom.html")
        self.assertEqual(content["formatted_body"],
                         f'<a href="https://matrix.to/#/{user}">{user}</a>: please &lt;stop&gt; &amp; commit')

    def test_an_at_room_pill_sets_mentions_room(self) -> None:
        content = bh.element_content("@room hello", mentions=[], at_room=True)
        self.assertEqual(content["m.mentions"], {"room": True})
        self.assertEqual(content["body"], "@room hello")

    def test_a_thread_reply_relates_to_its_root(self) -> None:
        content = bh.element_content("an answer", mentions=[], at_room=False, thread="$root")
        self.assertEqual(content["m.relates_to"], {
            "rel_type": "m.thread", "event_id": "$root", "is_falling_back": True,
            "m.in_reply_to": {"event_id": "$root"}})

    def test_a_reply_in_a_thread_names_both(self) -> None:
        content = bh.element_content("x", mentions=[], at_room=False, thread="$root", reply_to="$other")
        self.assertEqual(content["m.relates_to"], {
            "rel_type": "m.thread", "event_id": "$root", "is_falling_back": False,
            "m.in_reply_to": {"event_id": "$other"}})

    def test_a_plain_reply(self) -> None:
        content = bh.element_content("x", mentions=[], at_room=False, reply_to="$other")
        self.assertEqual(content["m.relates_to"], {"m.in_reply_to": {"event_id": "$other"}})

    def test_an_edit_replaces_its_event(self) -> None:
        content = bh.element_content("fixed text", mentions=[], at_room=False, edit="$mine")
        self.assertEqual(content, {
            "msgtype": "m.text", "body": "* fixed text", "m.mentions": {},
            "m.new_content": {"msgtype": "m.text", "body": "fixed text", "m.mentions": {}},
            "m.relates_to": {"rel_type": "m.replace", "event_id": "$mine"}})

    def test_an_edit_is_neither_a_reply_nor_a_thread(self) -> None:
        with self.assertRaises(ValueError):
            bh.element_content("x", mentions=[], at_room=False, edit="$mine", thread="$root")
        with self.assertRaises(ValueError):
            bh.element_content("x", mentions=[], at_room=False, edit="$mine", reply_to="$r")

    def test_an_empty_body_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            bh.element_content("", mentions=[], at_room=False)


class LiveCase(unittest.TestCase):
    """A fake homeserver with a team room the human and two agents are invited to."""

    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = pathlib.Path(tmp.name)
        self.fake = fake_api.FakeHomeserver(server_name=SN)
        self.human = self.fake.add_user("tester", password=PASSWORD)
        self.seat = self.fake.add_user(SEAT)
        self.peer = self.fake.add_user(PEER)
        admin = self.fake.add_user("admin", admin=True)
        self.admin_token = self.fake.mint_token(admin)
        self.room = self.create_room("accept194c")
        served = fake_api.serve(self.fake)
        self.base = served.__enter__()
        self.addCleanup(served.__exit__, None, None, None)
        self.session = self.dir / "tester.session"

    def create_room(self, name: str, invite: tuple[str, ...] | None = None) -> str:
        status, answer = self.fake.request("POST", "/_matrix/client/v3/createRoom",
                                           body={"name": name}, token=self.admin_token)
        self.assertEqual(status, 200, answer)
        room = answer["room_id"]
        for user in (self.human, self.seat, self.peer) if invite is None else invite:
            status, answer = self.fake.request("POST", f"/_matrix/client/v3/rooms/{room}/invite",
                                               body={"user_id": user}, token=self.admin_token)
            self.assertEqual(status, 200, answer)
        return room

    def run_cli(self, *argv: str, stdin: str = "") -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = bh.main(list(argv), stdin=io.StringIO(stdin), out=out, err=err)
        return code, out.getvalue(), err.getvalue()

    def login(self) -> None:
        code, out, err = self.run_cli("login", "--base", self.base, "--plain-http-host", "127.0.0.1",
                                      "--user", "tester", "--session", str(self.session), stdin=PASSWORD + "\n")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"{self.human}\n")

    def events(self, room: str) -> list:
        return [e for e in self.fake.rooms[room].events if e.type == "m.room.message"]


class LoginTest(LiveCase):
    def test_login_writes_a_private_session_and_prints_only_the_user_id(self) -> None:
        self.login()
        info = os.stat(self.session)
        self.assertEqual(stat.S_IMODE(info.st_mode), 0o600)
        saved = json.loads(self.session.read_text(encoding="utf-8"))
        self.assertEqual(saved["user_id"], self.human)
        self.assertEqual(saved["base_url"], self.base)
        self.assertNotIn(PASSWORD, self.session.read_text(encoding="utf-8"))

    def test_a_wrong_password_fails_and_says_neither_password_nor_token(self) -> None:
        code, out, err = self.run_cli("login", "--base", self.base, "--plain-http-host", "127.0.0.1",
                                      "--user", "tester", "--session", str(self.session), stdin="wrong-one\n")
        self.assertEqual(code, 1)
        self.assertEqual(out, "")
        self.assertNotIn("wrong-one", err)
        self.assertIn("403", err)
        self.assertFalse(self.session.exists())

    def test_an_existing_session_file_is_never_overwritten(self) -> None:
        self.session.write_text("someone else's", encoding="utf-8")
        code, _, err = self.run_cli("login", "--base", self.base, "--plain-http-host", "127.0.0.1",
                                    "--user", "tester", "--session", str(self.session), stdin=PASSWORD)
        self.assertEqual(code, 1)
        self.assertIn("exists", err)
        self.assertEqual(self.session.read_text(encoding="utf-8"), "someone else's")

    def test_no_password_on_stdin_is_refused_before_any_request(self) -> None:
        code, _, err = self.run_cli("login", "--base", self.base, "--plain-http-host", "127.0.0.1",
                                    "--user", "tester", "--session", str(self.session), stdin="")
        self.assertEqual(code, 1)
        self.assertIn("no password", err)
        self.assertEqual(self.fake.http_log, [])

    def test_the_password_is_never_an_argument(self) -> None:
        code, _, _ = self.run_cli("login", "--base", self.base, "--user", "tester",
                                  "--session", str(self.session), "--password", PASSWORD)
        self.assertEqual(code, 64)

    def test_plain_http_needs_the_host_listed(self) -> None:
        code, _, err = self.run_cli("login", "--base", self.base, "--user", "tester",
                                    "--session", str(self.session), stdin=PASSWORD)
        self.assertEqual(code, 1)
        self.assertIn("plain_http_hosts", err)

    def test_a_loose_session_file_is_refused(self) -> None:
        self.login()
        self.session.chmod(0o644)
        code, _, err = self.run_cli("join", "--session", str(self.session), "--room", self.room)
        self.assertEqual(code, 1)
        self.assertIn("looser than 0600", err)

    def test_logout_ends_the_session_and_deletes_the_file(self) -> None:
        self.login()
        self.fake.inject("POST", r"/_matrix/client/v3/logout$", 200, {})
        code, _, err = self.run_cli("logout", "--session", str(self.session))
        self.assertEqual(code, 0, err)
        self.assertFalse(self.session.exists())
        self.assertTrue(any(r.path.endswith("/logout") for r in self.fake.http_log))


class PostTest(LiveCase):
    def setUp(self) -> None:
        super().setUp()
        self.login()
        code, _, err = self.run_cli("join", "--session", str(self.session), "--room", self.room)
        self.assertEqual(code, 0, err)

    def test_a_mention_reaches_the_room_as_element_sends_it(self) -> None:
        code, out, err = self.run_cli("post", "--session", str(self.session), "--room", self.room,
                                      "--mention", self.seat, stdin="H1 abc: reply ACK\n")
        self.assertEqual(code, 0, err)
        [event] = self.events(self.room)
        self.assertEqual(out, f"{event.event_id}\n")
        self.assertEqual(event.sender, self.human)
        self.assertEqual(event.content, bh.element_content("H1 abc: reply ACK", mentions=[self.seat], at_room=False))

    def test_a_typed_at_room_carries_no_room_mention(self) -> None:
        code, _, err = self.run_cli("post", "--session", str(self.session), "--room", self.room,
                                    stdin="@room is anyone there")
        self.assertEqual(code, 0, err)
        [event] = self.events(self.room)
        self.assertEqual(event.content["m.mentions"], {})
        self.assertEqual(event.content["body"], "@room is anyone there")

    def test_a_body_is_sent_whole_with_its_newlines(self) -> None:
        body = "line one\nline two\n\x1b[201~ ‮ end"
        code, _, err = self.run_cli("post", "--session", str(self.session), "--room", self.room,
                                    "--mention", self.seat, stdin=body + "\n")
        self.assertEqual(code, 0, err)
        [event] = self.events(self.room)
        self.assertEqual(event.content["body"], f"{self.seat}: {body}")

    def test_a_thread_reply_and_its_edit(self) -> None:
        _, root, _ = self.run_cli("post", "--session", str(self.session), "--room", self.room, stdin="Q1 a question")
        code, reply, err = self.run_cli("post", "--session", str(self.session), "--room", self.room,
                                        "--thread", root.strip(), stdin="an answer")
        self.assertEqual(code, 0, err)
        code, edit, err = self.run_cli("post", "--session", str(self.session), "--room", self.room,
                                       "--edit", reply.strip(), stdin="a better answer")
        self.assertEqual(code, 0, err)
        events = {e.event_id: e for e in self.events(self.room)}
        self.assertEqual(events[reply.strip()].content["m.relates_to"]["rel_type"], "m.thread")
        self.assertEqual(events[edit.strip()].content["m.relates_to"],
                         {"rel_type": "m.replace", "event_id": reply.strip()})

    def test_an_empty_body_posts_nothing(self) -> None:
        code, _, err = self.run_cli("post", "--session", str(self.session), "--room", self.room, stdin="\n")
        self.assertEqual(code, 1)
        self.assertIn("empty", err)
        self.assertEqual(self.events(self.room), [])

    def test_find_sees_the_seats_reply_and_not_an_older_one(self) -> None:
        token = self.fake.mint_token(self.seat)
        self.fake.request("POST", f"/_matrix/client/v3/rooms/{self.room}/join", body={}, token=token)
        content = protocol.build_text([self.human], "ACK-old")
        self.fake.request("PUT", f"/_matrix/client/v3/rooms/{self.room}/send/m.room.message/t1",
                          body=content, token=token)
        since = self.fake.now_ms() + 1
        args = ("find", "--session", str(self.session), "--room", self.room, "--sender", self.seat,
                "--since-ms", str(since))
        code, out, _ = self.run_cli(*args, "--contains", "ACK-old")
        self.assertEqual((code, out), (3, ""))
        self.fake._clock = lambda: (since + 5) / 1000
        status, sent = self.fake.request("PUT", f"/_matrix/client/v3/rooms/{self.room}/send/m.room.message/t2",
                                         body=protocol.build_text([self.human], "ACK-new"), token=token)
        self.assertEqual(status, 200, sent)
        code, out, err = self.run_cli(*args, "--contains", "ACK-new")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"{sent['event_id']}\n")


class RoomsTest(LiveCase):
    def test_rooms_accepts_every_invite_and_lists_each_room_by_name(self) -> None:
        questions = self.create_room("accept194c · questions", invite=(self.human,))
        self.login()
        code, out, err = self.run_cli("rooms", "--session", str(self.session))
        self.assertEqual(code, 0, err)
        self.assertEqual(sorted(out.splitlines()), sorted([
            f"ROOM\t{self.room}\taccept194c", f"ROOM\t{questions}\taccept194c · questions"]))
        self.assertEqual(self.fake.rooms[questions].membership(self.human), "join")


class AgentSayTest(LiveCase):
    """The peer of H3: an agent's text, built by pingbus's own protocol.build_text, from a seat's
    bundle as `agent-bus seat take` leaves it."""

    def bundle(self) -> pathlib.Path:
        bundle = self.dir / "seats" / "accept194c" / "2"
        bundle.mkdir(parents=True, mode=0o700)
        data = {"protocol": 1, "team": "accept194c", "user_id": self.peer, "server_name": SN,
                "base_url": self.base, "plain_http_hosts": ["127.0.0.1"], "token_file": "token",
                "admin": f"@admin:{SN}", "room": self.room}
        (bundle / "member.json").write_text(json.dumps(data), encoding="utf-8")
        token = bundle / "token"
        token.write_text(self.fake.mint_token(self.peer), encoding="utf-8")
        token.chmod(0o600)
        return bundle

    def test_the_peer_joins_and_posts_pingbus_text(self) -> None:
        code, out, err = self.run_cli("agent-say", "--bundle", str(self.bundle()), "--team", "accept194c",
                                      "--to", self.seat, stdin=f"create the file {HOME}/x.txt\n")
        self.assertEqual(code, 0, err)
        [event] = self.events(self.room)
        self.assertEqual(out, f"{event.event_id}\n")
        self.assertEqual(event.sender, self.peer)
        self.assertEqual(event.content, protocol.build_text([self.seat], f"create the file {HOME}/x.txt"))

    def test_the_human_logs_in_on_the_homeserver_a_seat_bundle_names(self) -> None:
        code, out, err = self.run_cli("login", "--bundle", str(self.bundle()), "--team", "accept194c",
                                      "--user", "tester", "--session", str(self.session), stdin=PASSWORD)
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"{self.human}\n")
        saved = json.loads(self.session.read_text(encoding="utf-8"))
        self.assertEqual((saved["base_url"], saved["plain_http_hosts"]), (self.base, ["127.0.0.1"]))

    def test_login_names_one_homeserver(self) -> None:
        code, _, err = self.run_cli("login", "--bundle", str(self.bundle()), "--team", "accept194c",
                                    "--base", self.base, "--user", "tester", "--session", str(self.session),
                                    stdin=PASSWORD)
        self.assertEqual(code, 1)
        self.assertIn("--bundle", err)

    def test_a_bundle_of_another_team_is_refused(self) -> None:
        code, _, err = self.run_cli("agent-say", "--bundle", str(self.bundle()), "--team", "accept194d",
                                    "--to", self.seat, stdin="x")
        self.assertEqual(code, 1)
        self.assertIn("team", err)
        self.assertEqual(self.events(self.room), [])


if __name__ == "__main__":
    unittest.main()
