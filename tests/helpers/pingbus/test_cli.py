"""Unit tests for the network commands of helpers/pingbus/cli.py: send, say, recv, wait.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_cli

Spec: docs/agent-bus-protocol.md §7 (agent text to humans), §9 (on send; on receive;
reading the inbox: `recv` and `wait` re-fetch before printing), §10 (the shared send
bucket, TIMEOUT), §12 (multi-team, the lock), §13 (the commands), §14 (exit codes) and
§15 (output lines). Plan 00161's DESIGN.md section 12 row U11.

Every bundle is written to a temporary directory and points at a U08 fake homeserver on
loopback, so the commands run their real client, syncer and inbox; the forge is a stand-in
and the logical clock is injected. Every command's stdout and stderr are kept and checked
for every token at tear-down.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import socket
import sys
import tempfile
import threading
import time
import unittest
import urllib.parse

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, forge, inbox, limits, protocol
from tests.helpers.pingbus import fake_client_api

TEAM_A, SN_A = "team-a", "team-a.agent-bus.internal"
TEAM_B, SN_B = "team-b", "team-b.agent-bus.internal"
ME_LP = "myrepo.1+workstation.podman"
ORCH_LP = "orch.1+workstation.podman"
PEER_LP = "peer.1+workstation.podman"
SHA = "0123456789abcdef0123456789abcdef01234567"
PATH_REF = f"path:example-org/myrepo@{SHA}:docs/plan.md"
T0 = 1_791_000_000.0
STDOUT_KINDS = ("PING", "HUMAN", "TIMEOUT", "SENT")
#: Long enough that the fake's long-poll is really held, short enough to end with a test.
LONG_POLL_MS = 400


def q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class StubForge:
    """The forge client's interface (`api`, `check`): records each check, fails on demand."""

    api = "https://api.github.com"

    def __init__(self) -> None:
        self.checked: list[str] = []
        self.fail: dict[str, str] = {}

    def check(self, ref: protocol.Ref, branches: tuple[str, ...]) -> None:
        self.checked.append(ref.text)
        code = self.fail.get(ref.text)
        if code is not None:
            raise forge.ForgeError(code, "stub")


class Team:
    """One team: its fake homeserver on loopback, `admin`'s trusted room with a human and
    two other agents joined, and this member invited."""

    def __init__(self, case: BusCase, name: str, server_name: str) -> None:
        self.case, self.name, self.sn = case, name, server_name
        self.fake = fake_client_api.FakeHomeserver(server_name=server_name, clock=lambda: case.now)
        self.admin = self.fake.add_user("admin", admin=True)
        self.me = self.fake.add_user(ME_LP)
        self.orch = self.fake.add_user(ORCH_LP)
        self.peer = self.fake.add_user(PEER_LP)
        self.alice = self.fake.add_user("alice")
        self.tokens = {u: self.fake.mint_token(u) for u in
                       (self.admin, self.me, self.orch, self.peer, self.alice)}
        self.room = self.call(self.admin, "POST", "/_matrix/client/v3/createRoom", {
            "preset": "private_chat", "room_version": "12", "name": name,
            "power_level_content_override": protocol.expected_power_levels([self.alice]),
            "invite": [self.me, self.orch, self.peer, self.alice]})["room_id"]
        self.record = {
            "v": 1, "team": name, "humans": [self.alice],
            "roles": {self.me: "worker", self.orch: "orchestrator", self.peer: "worker"},
            "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
            "path_prefixes": ["docs/"], "forge_api": "https://api.github.com",
        }
        self.set_record(self.record)
        for uid in (self.orch, self.peer, self.alice):
            self.call(uid, "POST", f"/_matrix/client/v3/rooms/{q(self.room)}/join", {})
        self.base = case.enterContext(fake_client_api.serve(self.fake))
        self.bundle = case.home / name
        self.txn = 0

    def call(self, uid: str, method: str, path: str, body: object = None) -> dict:
        status, answer = self.fake.request(method, path, body=body, token=self.tokens[uid])
        self.case.assertEqual(status, 200, answer)
        return answer

    def set_record(self, record: dict) -> None:
        self.call(self.admin, "PUT", f"/_matrix/client/v3/rooms/{q(self.room)}/state/"
                  f"{protocol.EVENT_TEAM}/", record)

    def send(self, uid: str, content: dict) -> str:
        self.txn += 1
        return self.call(uid, "PUT", f"/_matrix/client/v3/rooms/{q(self.room)}/send/"
                         f"m.room.message/t{self.txn}", content)["event_id"]

    def ping(self, uid: str, verb: str, ref: str | None = None, re: str | None = None) -> str:
        return self.send(uid, protocol.build_ping(verb, [self.me], ref=ref, re=re))

    def human(self, body: str = "please halt") -> str:
        return self.send(self.alice, {"msgtype": "m.text", "body": body,
                                      "m.mentions": {"user_ids": [self.me]}})

    def write_bundle(self, *, member_limits: dict | None = None, human_text: bool | None = None,
                     base_url: str | None = None, token: str | None = None) -> None:
        self.bundle.mkdir(parents=True, exist_ok=True)
        member: dict[str, object] = {
            "protocol": 1, "team": self.name, "user_id": self.me, "server_name": self.sn,
            "base_url": base_url or self.base, "plain_http_hosts": ["127.0.0.1"],
            "token_file": "token", "admin": self.admin, "room": self.room,
        }
        if member_limits is not None:
            member["limits"] = member_limits
        if human_text is not None:
            member["human_text"] = human_text
        (self.bundle / "member.json").write_text(json.dumps(member), encoding="utf-8")
        token_path = self.bundle / "token"
        token_path.write_text(token or self.tokens[self.me], encoding="ascii")
        token_path.chmod(0o600)

    @property
    def state(self) -> inbox.TeamState:
        return inbox.TeamState(self.bundle / "state")

    def messages_from_me(self) -> list[dict]:
        room = self.fake.rooms[self.room]
        return [e.content for e in room.events if e.sender == self.me and e.type == "m.room.message"]

    def drop_log(self) -> list[list[str]]:
        path = self.state.path / inbox.DROPPED_LOG
        if not path.exists():
            return []
        return [line.split("\t") for line in path.read_text(encoding="ascii").splitlines()]


class BusCase(unittest.TestCase):
    """One active team by default; `add_team` makes a second one active too."""

    def setUp(self) -> None:
        self.now = T0
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.home = self.tmp / "pingbus"
        self.home.mkdir()
        (self.tmp / "home").mkdir()
        self.forge = StubForge()
        self.threads: list[threading.Thread] = []
        self.outputs: list[str] = []
        self.a = Team(self, TEAM_A, SN_A)
        self.a.write_bundle()
        self.teams = [self.a]
        self.env = {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "HOME": str(self.tmp / "home"),
            "PINGBUS_HOME": str(self.home),
            "PINGBUS_TEAMS": TEAM_A,
        }

    def tearDown(self) -> None:
        self.settle()
        for team in self.teams:
            for token in team.tokens.values():
                for text in self.outputs:
                    self.assertNotIn(token, text, "a token reached stdout or stderr")

    def add_team(self) -> Team:
        self.b = Team(self, TEAM_B, SN_B)
        self.b.write_bundle()
        self.teams.append(self.b)
        self.env["PINGBUS_TEAMS"] = f"{TEAM_A},{TEAM_B}"
        return self.b

    def runtime(self, long_poll_ms: int = LONG_POLL_MS) -> cli.Runtime:
        return cli.Runtime(
            forge_for=lambda member, environ: (lambda record: self.forge),
            clock_ms=lambda: int(self.now * 1000),
            sleep=lambda seconds: None,
            long_poll_ms=long_poll_ms,
            tick_s=0.05,
            threads=self.threads,
        )

    def run_cli(self, *argv: str, stdin: str | bytes = b"", long_poll_ms: int = LONG_POLL_MS,
                env: dict[str, str] | None = None) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        data = stdin.encode("utf-8") if isinstance(stdin, str) else stdin
        code = cli.main(list(argv), environ=env or self.env, stdout=out, stderr=err,
                        stdin=io.BytesIO(data), runtime=self.runtime(long_poll_ms))
        self.outputs += [out.getvalue(), err.getvalue()]
        return code, out.getvalue(), err.getvalue()

    def settle(self) -> None:
        """Let `wait`'s long-poll threads end (each releases its team's lock)."""
        for thread in self.threads:
            thread.join(10)
            self.assertFalse(thread.is_alive(), "a long-poll thread did not end")
        self.threads.clear()

    def joined(self) -> None:
        """Every active team past its first sync: invited, joined, the room verified."""
        code, out, err = self.run_cli("recv")
        self.assertEqual((code, out), (cli.EXIT_NOTHING, ""), err)

    def later(self, seconds: float, action) -> None:
        timer = threading.Timer(seconds, action)
        timer.daemon = True
        timer.start()
        self.addCleanup(timer.join, 10)

    def assert_stdout_kinds(self, out: str) -> None:
        for line in out.splitlines():
            self.assertIn(line.split("\t")[0], STDOUT_KINDS, line)

    def lines(self, out: str, kind: str) -> list[list[str]]:
        return [line.split("\t") for line in out.splitlines() if line.split("\t")[0] == kind]


# ── one test per exit code (spec §14) ──────────────────────────────────────────────────


class ExitCodeTest(BusCase):
    def test_0_recv_printed_a_line(self):
        self.joined()
        event = self.a.human("hello")
        code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(self.lines(out, "HUMAN")[0][3], event)

    def test_3_recv_found_nothing(self):
        self.joined()
        self.assertEqual(self.run_cli("recv")[:2], (cli.EXIT_NOTHING, ""))

    def test_3_wait_reached_its_timeout(self):
        self.joined()
        code, out, _ = self.run_cli("wait", "--timeout", "1")
        self.assertEqual((code, out), (cli.EXIT_NOTHING, ""))

    def test_4_send_refused_by_the_validator(self):
        self.joined()
        code, out, err = self.run_cli("send", "review", "commit:example-org/other@" + SHA, "--to", ORCH_LP)
        self.assertEqual((code, out), (cli.EXIT_REFUSED, ""))
        self.assertIn("allowlist", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_4_send_refused_by_role(self):
        self.joined()
        code, _, err = self.run_cli("send", "halt", "--to", PEER_LP)
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("role", err)

    def test_5_send_reference_did_not_resolve(self):
        self.joined()
        self.forge.fail[PATH_REF] = "not-found"
        code, out, err = self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)
        self.assertEqual((code, out), (cli.EXIT_FORGE, ""))
        self.assertIn("not-found", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_6_recv_only_dropped_items(self):
        self.joined()
        self.a.send(self.a.peer, {"msgtype": "m.text", "body": "agent free text"})
        code, out, err = self.run_cli("recv")
        self.assertEqual((code, out), (cli.EXIT_DROPPED, ""))
        self.assertIn("DROPPED\t1\t1\tschema=1", err)

    def test_7_homeserver_unreachable(self):
        self.a.write_bundle(base_url=f"http://127.0.0.1:{free_port()}")
        self.assertEqual(self.run_cli("recv")[0], cli.EXIT_UNREACHABLE)

    def test_8_token_rejected(self):
        self.a.write_bundle(token="syt_not_this_servers_token_0123")
        code, _, err = self.run_cli("send", "halt", "--to", PEER_LP)
        self.assertEqual(code, cli.EXIT_AUTH)
        self.assertNotIn("syt_not_this_servers_token_0123", err)

    def test_9_rate_limited_by_the_duplicate_window(self):
        self.joined()
        self.assertEqual(self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)[0], cli.EXIT_OK)
        code, out, err = self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)
        self.assertEqual((code, out), (cli.EXIT_RATE, ""))
        self.assertIn("duplicate", err)
        self.assertEqual(len(self.a.messages_from_me()), 1)

    def test_10_room_not_trusted(self):
        self.joined()
        self.a.set_record(dict(self.a.record, roles={self.a.orch: "orchestrator"}))
        code, out, _ = self.run_cli("send", "done", PATH_REF, "--to", ORCH_LP)
        self.assertEqual((code, out), (cli.EXIT_UNTRUSTED, ""))
        self.assertEqual(self.a.messages_from_me(), [])

    def test_64_send_with_two_teams_and_no_team_named(self):
        self.add_team()
        self.assertEqual(self.run_cli("send", "halt", "--to", PEER_LP)[0], cli.EXIT_USAGE)

    def test_75_wait_when_a_watcher_holds_every_lock(self):
        self.joined()
        with inbox.acquire_lock(self.a.state, "watch"):
            code, out, err = self.run_cli("wait", "--timeout", "1")
        self.assertEqual((code, out), (cli.EXIT_BUSY, ""))
        self.assertIn("watch", err)

    def test_78_no_active_team(self):
        env = dict(self.env)
        del env["PINGBUS_TEAMS"]
        for argv in (("send", "halt", "--to", PEER_LP), ("say", "--to", "alice"), ("recv",), ("wait",)):
            self.assertEqual(self.run_cli(*argv, env=env)[0], cli.EXIT_CONFIG, argv)


# ── send ───────────────────────────────────────────────────────────────────────────────


class SendTest(BusCase):
    def test_send_prints_one_sent_line_and_posts_the_ping(self):
        self.joined()
        code, out, _ = self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)
        self.assertEqual(code, cli.EXIT_OK)
        (sent,) = self.lines(out, "SENT")
        self.assertEqual(sent[:3], ["SENT", "1", TEAM_A])
        self.assertEqual(out, "\t".join(sent) + "\n")
        (content,) = self.a.messages_from_me()
        self.assertEqual(content, protocol.build_ping("review", [self.a.orch], ref=PATH_REF))
        self.assertEqual(self.forge.checked, [PATH_REF])

    def test_names_resolve_from_handles_localparts_and_user_ids(self):
        self.joined()
        code, _, err = self.run_cli("send", "review", PATH_REF, "--to", f"{ORCH_LP},{self.a.peer}")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.a.messages_from_me()[0][protocol.PING_KEY]["to"],
                         sorted([self.a.orch, self.a.peer]))

    def test_to_orchestrator_addresses_every_orchestrator(self):
        self.joined()
        code, _, err = self.run_cli("send", "review", PATH_REF, "--to-orchestrator")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.a.messages_from_me()[0][protocol.PING_KEY]["to"], [self.a.orch])

    def test_the_owner_repo_is_lowercased_before_validating(self):
        self.joined()
        code, _, err = self.run_cli("send", "review", PATH_REF.replace("example-org", "Example-Org"),
                                    "--to", ORCH_LP)
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.a.messages_from_me()[0][protocol.PING_KEY]["ref"], PATH_REF)

    def test_an_ack_expected_ping_is_tracked_and_times_out(self):
        self.joined()
        self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)
        sent_id = self.a.fake.rooms[self.a.room].events[-1].event_id
        self.now += limits.DEFAULTS["ack_timeout_s"] + 1
        code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(self.lines(out, "TIMEOUT"),
                         [["TIMEOUT", "1", TEAM_A, sent_id, ORCH_LP, "review", PATH_REF]])
        self.assertEqual(self.run_cli("recv")[0], cli.EXIT_NOTHING, "a TIMEOUT is reported once")

    def test_an_ack_settles_the_tracked_ping(self):
        self.joined()
        self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)
        sent_id = self.a.fake.rooms[self.a.room].events[-1].event_id
        self.a.ping(self.a.orch, "ack", re=sent_id)
        self.assertEqual(self.lines(self.run_cli("recv")[1], "PING")[0][5], "ack")
        self.now += limits.DEFAULTS["ack_timeout_s"] + 1
        self.assertEqual(self.run_cli("recv")[0], cli.EXIT_NOTHING)

    def test_a_refused_send_has_still_spent_its_token(self):
        self.joined()
        self.a.write_bundle(member_limits={"send_burst": 1})
        self.forge.fail[PATH_REF] = "provenance"
        self.assertEqual(self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)[0], cli.EXIT_FORGE)
        code, _, err = self.run_cli("send", "halt", "--to", PEER_LP, "--team", TEAM_A)
        self.assertEqual(code, cli.EXIT_REFUSED, err)
        code, _, err = self.run_cli("send", "done", PATH_REF, "--to", ORCH_LP)
        self.assertEqual(code, cli.EXIT_RATE, err)

    def test_to_and_to_orchestrator_are_exclusive_and_one_is_required(self):
        self.joined()
        self.assertEqual(self.run_cli("send", "halt")[0], cli.EXIT_USAGE)
        self.assertEqual(self.run_cli("send", "halt", "--to", PEER_LP, "--to-orchestrator")[0],
                         cli.EXIT_USAGE)


# ── say ────────────────────────────────────────────────────────────────────────────────


class SayTest(BusCase):
    def test_say_reads_the_text_from_stdin(self):
        self.joined()
        code, out, err = self.run_cli("say", "--to", "alice", stdin="the build is green\nsee the plan\n")
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(len(self.lines(out, "SENT")), 1)
        (content,) = self.a.messages_from_me()
        self.assertEqual(content, protocol.build_text([self.a.alice], "the build is green\nsee the plan"))

    def test_say_takes_humans_only(self):
        self.joined()
        for to in (ORCH_LP, self.a.orch, "nobody"):
            code, out, err = self.run_cli("say", "--to", to, stdin="hello")
            self.assertEqual((code, out), (cli.EXIT_REFUSED, ""), to)
            self.assertIn("target", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_say_refuses_secret_shaped_text(self):
        self.joined()
        secret = "ghp_" + "A" * 36
        code, out, err = self.run_cli("say", "--to", "alice", stdin=f"the token is {secret}")
        self.assertEqual((code, out), (cli.EXIT_REFUSED, ""))
        self.assertIn("secret", err)
        self.assertNotIn(secret, err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_say_refuses_the_members_own_token(self):
        self.joined()
        own = self.a.tokens[self.a.me]
        self.assertIsNone(protocol.secret_shaped(own), "the fake's token must not match a shape")
        code, out, err = self.run_cli("say", "--to", "alice", stdin=f"my token: {own} ok")
        self.assertEqual((code, out), (cli.EXIT_REFUSED, ""))
        self.assertIn("secret", err)
        self.assertEqual(self.a.messages_from_me(), [])

    def test_say_and_send_share_the_send_bucket(self):
        self.joined()
        self.a.write_bundle(member_limits={"send_burst": 1})
        self.assertEqual(self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)[0], cli.EXIT_OK)
        code, out, err = self.run_cli("say", "--to", "alice", stdin="hello")
        self.assertEqual((code, out), (cli.EXIT_RATE, ""), err)
        self.assertEqual(len(self.a.messages_from_me()), 1)

    def test_the_same_text_twice_is_a_duplicate_and_another_is_not(self):
        self.joined()
        self.assertEqual(self.run_cli("say", "--to", "alice", stdin="zebra quokka")[0], cli.EXIT_OK)
        self.assertEqual(self.run_cli("say", "--to", "alice", stdin="zebra quokka")[0], cli.EXIT_RATE)
        self.assertEqual(self.run_cli("say", "--to", "alice", stdin="another")[0], cli.EXIT_OK)
        outbox = (self.a.state.path / inbox.OUTBOX_FILE).read_text(encoding="ascii")
        self.assertNotIn("quokka", outbox, "the text is kept at rest only as a digest")

    def test_empty_and_oversized_text_are_refused(self):
        self.joined()
        self.assertEqual(self.run_cli("say", "--to", "alice", stdin="")[0], cli.EXIT_REFUSED)
        code, _, err = self.run_cli("say", "--to", "alice",
                                    stdin="x" * (protocol.MAX_AGENT_TEXT_BYTES + 1))
        self.assertEqual(code, cli.EXIT_REFUSED)
        self.assertIn("size", err)
        self.assertEqual(self.a.messages_from_me(), [])


# ── recv ───────────────────────────────────────────────────────────────────────────────


class RecvTest(BusCase):
    def stored_human(self, event_id: str, body: str) -> dict:
        """An inbox entry for a human message, as a syncer (or anything else) could write it."""
        return {"type": "m.room.message", "event_id": event_id, "sender": self.a.alice,
                "origin_server_ts": int(T0 * 1000),
                "content": {"msgtype": "m.text", "body": body, "m.mentions": {"user_ids": [self.a.me]}}}

    def test_recv_prints_pings_and_human_messages_and_consumes_them(self):
        self.joined()
        ping = self.a.ping(self.a.orch, "review", ref=PATH_REF)
        human = self.a.human("line one\nline two")
        code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.assert_stdout_kinds(out)
        self.assertEqual(self.lines(out, "PING"),
                         [["PING", "1", TEAM_A, ping, ORCH_LP, "review", PATH_REF, "-"]])
        self.assertEqual(self.lines(out, "HUMAN"),
                         [["HUMAN", "1", TEAM_A, human, "alice", str(int(T0 * 1000)),
                           json.dumps("line one\nline two")]])
        self.assertEqual(self.run_cli("recv")[0], cli.EXIT_NOTHING)

    def test_recv_refetches_and_prints_the_fetched_copy(self):
        self.joined()
        event = self.a.human("the server's words")
        with inbox.acquire_lock(self.a.state, "watch"):
            self.assertEqual(self.run_cli("recv")[0], cli.EXIT_NOTHING)
        stored = self.a.state.inbox_dir / f"{event}.json"
        self.assertFalse(stored.exists(), "the lock was held, so recv did not sync")
        self.a.state.commit_batch([self.stored_human(event, "planted words")], self.a.state.sync_token())
        self.a.fake.http_log.clear()
        code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(self.lines(out, "HUMAN")[0][-1], json.dumps("the server's words"))
        fetched = [r.path for r in self.a.fake.http_log if "/event/" in r.path]
        self.assertEqual(fetched, [f"/_matrix/client/v3/rooms/{q(self.a.room)}/event/{q(event)}"])

    def test_an_item_whose_fetched_copy_fails_is_dropped_not_printed(self):
        self.joined()
        event = self.a.human("to be redacted")
        self.a.state.commit_batch([self.stored_human(event, "to be redacted")], self.a.state.sync_token())
        self.a.call(self.a.admin, "PUT", f"/_matrix/client/v3/rooms/{q(self.a.room)}/redact/{q(event)}/r1", {})
        code, out, err = self.run_cli("recv")
        self.assertEqual((code, out), (cli.EXIT_DROPPED, ""))
        self.assertIn("schema=1", err)
        self.assertEqual(self.run_cli("recv")[0], cli.EXIT_NOTHING, "the item was consumed")

    def test_a_received_agent_text_is_ignored_not_dropped(self):
        self.joined()
        self.a.send(self.a.peer, protocol.build_text([self.a.alice], "status for the owner"))
        self.a.send(self.a.peer, protocol.build_text([self.a.alice], "addressed oddly") | {
            "m.mentions": {"user_ids": [self.a.me]}})
        code, out, err = self.run_cli("recv")
        self.assertEqual((code, out), (cli.EXIT_NOTHING, ""))
        self.assertNotIn("DROPPED", err)
        self.assertEqual(self.a.drop_log(), [])

    def test_drops_are_aggregated_on_stderr_beside_printed_lines(self):
        self.joined()
        self.a.send(self.a.peer, {"msgtype": "m.text", "body": "agent free text"})
        human = self.a.human()
        code, out, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(self.lines(out, "HUMAN")[0][3], human)
        self.assertIn("DROPPED\t1\t1\tschema=1", err)
        self.assertEqual([row[3] for row in self.a.drop_log()], ["schema"])

    def test_recv_reads_the_inbox_without_syncing_when_a_waker_holds_the_lock(self):
        self.joined()
        with inbox.acquire_lock(self.a.state, "watch"):
            self.a.fake.http_log.clear()
            code, _, err = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_NOTHING)
        self.assertEqual([r for r in self.a.fake.http_log if r.path.endswith("/sync")], [])
        self.assertIn("watch", err)

    def test_recv_covers_every_active_team_and_names_each(self):
        b = self.add_team()
        self.joined()
        self.a.human("for a")
        b.human("for b")
        code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(sorted(line[2] for line in self.lines(out, "HUMAN")), [TEAM_A, TEAM_B])

    def test_one_busy_team_does_not_stop_recv_for_the_others(self):
        b = self.add_team()
        self.joined()
        b.human("for b")
        with inbox.acquire_lock(self.a.state, "watch"):
            code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual([line[2] for line in self.lines(out, "HUMAN")], [TEAM_B])

    def test_human_text_false_drops_human_messages(self):
        self.a.write_bundle(human_text=False)
        self.joined()
        self.a.human()
        code, out, err = self.run_cli("recv")
        self.assertEqual((code, out), (cli.EXIT_DROPPED, ""))
        self.assertIn("sender=1", err)


# ── wait ───────────────────────────────────────────────────────────────────────────────


class WaitTest(BusCase):
    def test_wait_ends_on_the_first_valid_item(self):
        self.joined()
        self.later(0.2, lambda: self.a.ping(self.a.orch, "review", ref=PATH_REF))
        code, out, _ = self.run_cli("wait", "--timeout", "20")
        self.assertEqual(code, cli.EXIT_OK)
        self.assert_stdout_kinds(out)
        self.assertEqual([line[5] for line in self.lines(out, "PING")], ["review"])

    def test_wait_delivers_what_is_already_pending_at_once(self):
        self.joined()
        self.a.human()
        start = time.monotonic()
        code, out, _ = self.run_cli("wait", "--timeout", "20")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(len(self.lines(out, "HUMAN")), 1)
        self.assertLess(time.monotonic() - start, 5)

    def test_wait_is_not_ended_by_drops(self):
        self.joined()
        self.later(0.2, lambda: self.a.send(self.a.peer, {"msgtype": "m.text", "body": "free text"}))
        self.later(0.2, lambda: self.a.send(self.a.peer, protocol.build_text([self.a.alice], "for alice")))
        self.later(0.8, lambda: self.a.human("after the drop"))
        code, out, err = self.run_cli("wait", "--timeout", "20")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(self.lines(out, "HUMAN")[0][-1], json.dumps("after the drop"))
        self.assertIn("DROPPED\t1\t1\tschema=1", err)

    def test_wait_with_only_drops_reaches_its_timeout_not_exit_6(self):
        self.joined()
        self.later(0.2, lambda: self.a.send(self.a.peer, {"msgtype": "m.text", "body": "free text"}))
        code, out, err = self.run_cli("wait", "--timeout", "1")
        self.assertEqual((code, out), (cli.EXIT_NOTHING, ""))
        self.assertIn("schema=1", err)

    def test_wait_reports_a_due_timeout(self):
        self.joined()
        self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)
        self.now += limits.DEFAULTS["ack_timeout_s"] + 1
        code, out, _ = self.run_cli("wait", "--timeout", "20")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual(len(self.lines(out, "TIMEOUT")), 1)

    def test_wait_holds_the_lock_as_a_waiter_while_it_polls(self):
        self.joined()
        seen: list[str | None] = []
        self.later(0.3, lambda: seen.append(inbox.probe_lock(self.a.state)))
        self.later(0.5, lambda: self.a.human())
        self.assertEqual(self.run_cli("wait", "--timeout", "20")[0], cli.EXIT_OK)
        self.settle()
        self.assertEqual(seen, ["wait"])
        self.assertIsNone(inbox.probe_lock(self.a.state))

    def test_a_brief_recv_holder_does_not_make_wait_busy(self):
        self.joined()
        lock = inbox.acquire_lock(self.a.state, "recv")
        self.later(0.3, lock.release)
        code, _, err = self.run_cli("wait", "--timeout", "1")
        self.assertEqual(code, cli.EXIT_NOTHING, err)

    def test_one_busy_team_does_not_block_the_others(self):
        b = self.add_team()
        self.joined()
        with inbox.acquire_lock(self.a.state, "watch"):
            self.later(0.2, lambda: b.human("for b"))
            code, out, err = self.run_cli("wait", "--timeout", "20")
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual([line[2] for line in self.lines(out, "HUMAN")], [TEAM_B])
        self.assertIn(f"team {TEAM_A}", err)

    def test_every_team_is_long_polled_at_once(self):
        b = self.add_team()
        self.joined()
        self.later(0.3, lambda: b.human("for b"))
        start = time.monotonic()
        code, out, _ = self.run_cli("wait", "--timeout", "30", long_poll_ms=8000)
        elapsed = time.monotonic() - start
        self.assertEqual(code, cli.EXIT_OK)
        self.assertEqual([line[2] for line in self.lines(out, "HUMAN")], [TEAM_B])
        self.assertLess(elapsed, 4, "team B waited behind team A's long-poll")

    def test_the_timeout_is_held_to_its_bounds(self):
        self.joined()
        for value in ("0", "1791", "x"):
            self.assertEqual(self.run_cli("wait", "--timeout", value)[0], cli.EXIT_USAGE, value)


class StreamTest(BusCase):
    def test_a_rejected_token_reaches_neither_stream(self):
        bad = "syt_rejected_token_value_0123456789"
        self.a.write_bundle(token=bad)
        for argv in (("send", "halt", "--to", PEER_LP), ("recv",), ("wait", "--timeout", "1")):
            code, out, err = self.run_cli(*argv)
            self.assertEqual(code, cli.EXIT_AUTH, argv)
            self.assertNotIn(bad, out + err)
        code, out, err = self.run_cli("say", "--to", "alice", stdin="hi")
        self.assertEqual(code, cli.EXIT_AUTH)
        self.assertNotIn(bad, out + err)

    def test_only_the_stdout_kinds_reach_stdout(self):
        self.joined()
        self.a.send(self.a.peer, {"msgtype": "m.text", "body": "dropped"})
        self.a.ping(self.a.orch, "review", ref=PATH_REF)
        self.a.human()
        outs = [self.run_cli("send", "review", PATH_REF, "--to", ORCH_LP)[1],
                self.run_cli("say", "--to", "alice", stdin="hello")[1],
                self.run_cli("recv")[1]]
        self.now += limits.DEFAULTS["ack_timeout_s"] + 1
        outs.append(self.run_cli("wait", "--timeout", "5")[1])
        for out in outs:
            self.assertTrue(out)
            self.assert_stdout_kinds(out)


if __name__ == "__main__":
    unittest.main()
