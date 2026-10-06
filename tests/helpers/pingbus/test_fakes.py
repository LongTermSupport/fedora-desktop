"""Unit tests for the fake Tuwunel homeserver: fake_client_api.py and fake_admin_api.py.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_fakes

The fakes stand in for Tuwunel 1.9.3 in every container test of Plan 00161 (DESIGN.md
section 11). They are trusted only as far as this file proves them against what the real
server answered: probe H4 (U00) recorded 73 exchanges with a throwaway Tuwunel, scrubbed,
in fixtures/tuwunel/. `ReplayTest` sends every recorded request, in order, to a fresh
fake and requires the same status, the same error text, and the same response once the
values a server makes up (tokens, IDs, timestamps, batch tokens) are set aside. The other
tests pin the rules the design leans on: `@` state keys, power levels per event type, and
that a power-0 member may write `agent_bus.status` under a key that is not a user ID.
"""

from __future__ import annotations

import copy
import hashlib
import hmac
import json
import pathlib
import re
import sys
import threading
import time
import unittest
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import protocol
from tests.helpers.pingbus import fake_admin_api, fake_client_api

FIXTURES = pathlib.Path(__file__).resolve().parent / "fixtures" / "tuwunel"
PROBE_SN = "probe.agent-bus.internal"
SN = "team-a.agent-bus.internal"

# Response keys the replay does not compare, and why. Nothing in pingbus or the admin tool
# reads them; a fake that copied Tuwunel's bookkeeping for them would test nothing.
IGNORED_KEYS = frozenset({
    "device_lists",          # E2EE device tracking; the team room is unencrypted
    "unsigned",              # age, prev_content, transaction_id: Tuwunel fills them unevenly
    "unread_notifications",  # push-rule counts; Tuwunel's differ between equal syncs
})
# Values a server invents: compared by presence and null-ness, not by value.
VOLATILE_KEYS = frozenset({
    "access_token", "device_id", "nonce", "event_id", "room_id", "origin_server_ts",
    "next_batch", "prev_batch", "start", "end", "last_seen_ts",
})
BATCH_KEYS = ("next_batch", "prev_batch", "start", "end")
# Where Tuwunel 1.9.3 answered something the fake deliberately does not copy. Each entry
# names the fixture, the recorded event dropped before comparing, and why.
KNOWN_DIVERGENCES = {
    "058-sync-held.json": (
        (protocol.EVENT_STATUS, "probe"),
        "Tuwunel repeats an unchanged agent_bus.status (key `probe`, delivered by 055) in an "
        "incremental sync's state; the fake sends state only when it changed or is a "
        "lazy-loaded member, as the spec says. Receivers must tolerate both.",
    ),
}
_PLACEHOLDER_RE = re.compile(r"<(token|password|mac):[a-z0-9-]+>")
_ID_BODY_RE = re.compile(r"(?<![A-Za-z0-9_-])(?:[!$]|%21|%24)([A-Za-z0-9_-]{43})(?![A-Za-z0-9_-])")


def load_fixtures() -> list[tuple[str, dict]]:
    names = json.loads((FIXTURES / "index.json").read_text(encoding="utf-8"))
    return [(name, json.loads((FIXTURES / name).read_text(encoding="utf-8"))) for name in names]


def synapse_mac(secret: str, nonce: str, user: str, password: str, admin: bool) -> str:
    """Synapse's shared-secret registration MAC, written here independently of the fake."""
    parts = [nonce, user, password, "admin" if admin else "notadmin"]
    return hmac.new(secret.encode(), "\x00".join(parts).encode(), hashlib.sha1).hexdigest()


def _volatile(value: object) -> str:
    return "<null>" if value is None else "<set>"


def normalise(obj: object) -> object:
    """The comparable form of a response: ignored keys gone, volatile values reduced to
    presence, room-ID dict keys replaced, and state lists made order-free."""
    if isinstance(obj, list):
        return [normalise(item) for item in obj]
    if not isinstance(obj, dict):
        return obj
    out: dict[str, object] = {}
    for key, value in obj.items():
        if key in IGNORED_KEYS:
            continue
        name = "<room>" if protocol.is_room_id(key) else key
        if key in VOLATILE_KEYS:
            out[name] = _volatile(value)
        elif key in ("state", "invite_state") and isinstance(value, dict):
            events = [normalise(event) for event in value.get("events", [])]
            out[name] = {**normalise({k: v for k, v in value.items() if k != "events"}),
                         "events": sorted(events, key=lambda e: json.dumps(e, sort_keys=True))}
        else:
            out[name] = normalise(value)
    return out


def drop_divergence(name: str, response: dict) -> dict:
    if name not in KNOWN_DIVERGENCES:
        return response
    (event_type, state_key), _reason = KNOWN_DIVERGENCES[name]
    response = copy.deepcopy(response)
    for room in response["rooms"]["join"].values():
        events = room["state"]["events"]
        kept = [e for e in events if (e["type"], e.get("state_key")) != (event_type, state_key)]
        if len(kept) == len(events):
            raise AssertionError(f"{name}: the documented divergence is no longer in the fixture")
        room["state"]["events"] = kept
    return response


class Replay:
    """Maps the fixtures' scrubbed values onto the values a fresh fake hands out."""

    def __init__(self, fake: fake_admin_api.FakeAdminHomeserver) -> None:
        self.fake = fake
        self.tokens: dict[str, str] = {}
        self.ids: dict[str, str] = {}
        self.batches: dict[str, str] = {}
        self.nonces: dict[str, str] = {}

    def _sub_ids(self, text: str) -> str:
        for recorded, real in self.ids.items():
            text = text.replace(recorded, real)
        return text

    def request(self, record: dict) -> tuple[str, str, dict[str, str], object, str | None]:
        query = {}
        for key, value in record["query"].items():
            query[key] = self.batches[value] if key in ("since", "from", "to") else self._sub_ids(value)
        body = record["body"]
        if isinstance(body, dict):
            body = json.loads(self._sub_ids(json.dumps(body)))
            if "nonce" in body:
                body["nonce"] = self.nonces[body["nonce"]]
            if _PLACEHOLDER_RE.fullmatch(str(body.get("mac", ""))):
                body["mac"] = synapse_mac(self.fake.shared_secret, body["nonce"], body["username"],
                                          body["password"], bool(body["admin"]))
        token = self.tokens[record["auth"]] if record["auth"] else None
        return record["method"], self._sub_ids(record["path"]), query, body, token

    def _learn_id(self, recorded: object, real: object) -> None:
        rec_body, real_body = str(recorded)[1:], str(real)[1:]
        if rec_body in self.ids and self.ids[rec_body] != real_body:
            raise AssertionError(f"recorded ID {recorded} was answered as two different fake IDs")
        if rec_body not in self.ids and real_body in self.ids.values():
            raise AssertionError(f"two recorded IDs, one fake ID: {real}")
        self.ids[rec_body] = real_body

    def learn(self, recorded: dict, real: dict) -> None:
        for key in ("room_id", "event_id"):
            if key in recorded and key in real:
                self._learn_id(recorded[key], real[key])
        if "access_token" in recorded and "access_token" in real:
            self.tokens[recorded["access_token"]] = real["access_token"]
        if "nonce" in recorded and "nonce" in real:
            self.nonces[recorded["nonce"]] = real["nonce"]
        for key in BATCH_KEYS:
            if key in recorded and key in real:
                self._learn_batch(recorded[key], real[key])
        for room_id, room in recorded.get("rooms", {}).get("join", {}).items():
            real_room = real["rooms"]["join"]["!" + self.ids[room_id[1:]]]
            self._learn_batch(room["timeline"]["prev_batch"], real_room["timeline"]["prev_batch"])

    def _learn_batch(self, recorded: str, real: str) -> None:
        if recorded in self.batches and self.batches[recorded] != real:
            raise AssertionError(f"recorded batch {recorded} answered as {self.batches[recorded]} and {real}")
        self.batches[recorded] = real


class ReplayTest(unittest.TestCase):
    def test_replays_every_recorded_flow(self) -> None:
        fake = fake_admin_api.FakeAdminHomeserver(server_name=PROBE_SN)
        replay = Replay(fake)
        fixtures = load_fixtures()
        self.assertEqual(len(fixtures), 73)
        for name, record in fixtures:
            with self.subTest(fixture=name):
                status, response = fake.request(*replay.request(record))
                self.assertEqual(status, record["status"], f"{name}: {response}")
                expected = drop_divergence(name, record["response"])
                self.assertEqual(normalise(response), normalise(expected))
                replay.learn(record["response"], response)

    def test_every_fixture_is_scrubbed(self) -> None:
        names = json.loads((FIXTURES / "index.json").read_text(encoding="utf-8"))
        self.assertEqual(sorted(p.name for p in FIXTURES.iterdir()), sorted(names + ["index.json"]))
        for name, record in load_fixtures():
            with self.subTest(fixture=name):
                if record["auth"] is not None:
                    self.assertRegex(record["auth"], _PLACEHOLDER_RE)
                body = record["body"] if isinstance(record["body"], dict) else {}
                for key in ("password", "new_password", "mac"):
                    if key in body and name != "005-register-wrong-mac.json":
                        self.assertRegex(body[key], _PLACEHOLDER_RE)
                if "access_token" in record["response"]:
                    self.assertRegex(record["response"]["access_token"], _PLACEHOLDER_RE)
                text = (FIXTURES / name).read_text(encoding="utf-8")
                for match in _ID_BODY_RE.finditer(text):
                    self.assertTrue(protocol.is_room_id("!" + match.group(1)))
                self.assertNotIn("Bearer", text)

    def test_the_wrong_mac_fixture_carries_no_secret(self) -> None:
        record = dict(load_fixtures())["005-register-wrong-mac.json"]
        self.assertEqual(set(record["body"]["mac"]), {"0"})
        self.assertEqual(set(record["body"]["password"]), {"x"})


class TeamRoom(unittest.TestCase):
    """A fresh fake with the team room of agent-bus-protocol.md §8: admin, one human
    (power 50), two agents (power 0), all joined."""

    def setUp(self) -> None:
        self.fake = fake_admin_api.FakeAdminHomeserver(server_name=SN)
        self.admin = self.fake.add_user("admin", admin=True)
        self.human = self.fake.add_user("alice")
        self.agent = self.fake.add_user("repo.1+ws.podman")
        self.other = self.fake.add_user("repo.2+ws.podman")
        self.outsider = self.fake.add_user("repo.3+ws.podman")
        self.tok = {uid: self.fake.mint_token(uid) for uid in
                    (self.admin, self.human, self.agent, self.other, self.outsider)}
        record = {"v": 1, "team": "team-a", "humans": [self.human],
                  "roles": {self.agent: protocol.ROLE_WORKER, self.other: protocol.ROLE_ORCHESTRATOR},
                  "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
                  "path_prefixes": ["CLAUDE/Plan/"], "forge_api": "https://api.github.com"}
        status, body = self.call(self.admin, "POST", "/_matrix/client/v3/createRoom", body={
            "preset": "private_chat", "room_version": "12", "name": "team-a", "topic": "t",
            "visibility": "private",
            "power_level_content_override": protocol.expected_power_levels([self.human]),
            "initial_state": [{"type": protocol.EVENT_TEAM, "state_key": "", "content": record}]})
        self.assertEqual(status, 200, body)
        self.room = body["room_id"]
        for uid in (self.human, self.agent, self.other):
            self.assertEqual(self.invite(self.admin, uid)[0], 200)
            self.assertEqual(self.call(uid, "POST", f"/_matrix/client/v3/rooms/{q(self.room)}/join",
                                       body={})[0], 200)

    def call(self, uid: str | None, method: str, path: str, query: dict | None = None,
             body: object = None) -> tuple[int, dict]:
        return self.fake.request(method, path, query or {}, body, self.tok[uid] if uid else None)

    def invite(self, uid: str, target: str) -> tuple[int, dict]:
        return self.call(uid, "POST", f"/_matrix/client/v3/rooms/{q(self.room)}/invite",
                         body={"user_id": target})

    def put_state(self, uid: str, event_type: str, key: str, content: dict) -> tuple[int, dict]:
        return self.call(uid, "PUT", f"/_matrix/client/v3/rooms/{q(self.room)}/state/{event_type}/{q(key)}",
                         body=content)

    def send(self, uid: str, event_type: str, content: dict, txn: str) -> tuple[int, dict]:
        return self.call(uid, "PUT", f"/_matrix/client/v3/rooms/{q(self.room)}/send/{event_type}/{txn}",
                         body=content)

    def sync(self, uid: str, since: str | None = None, timeout: int = 0) -> dict:
        query = {"timeout": str(timeout)}
        if since is not None:
            query["since"] = since
        status, body = self.call(uid, "GET", "/_matrix/client/v3/sync", query)
        self.assertEqual(status, 200, body)
        return body


def q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


STATUS = {"v": 1, "state": protocol.STATUS_LISTENING, "until": 1}


class StateKeyAndPowerTest(TeamRoom):
    def test_power_levels_per_event_type(self) -> None:
        a, h, adm = "agent", "human", "admin"
        cases = [
            # (who, kind, event type, state key, expected status)
            (a, "send", "m.room.message", None, 200),
            (a, "send", "m.reaction", None, 403),
            (a, "send", "m.sticker", None, 403),
            (a, "state", protocol.EVENT_TEAM, "", 403),
            (a, "state", "m.room.power_levels", "", 403),
            (a, "state", "m.room.name", "", 403),
            (h, "send", "m.room.message", None, 200),
            (h, "send", "m.reaction", None, 200),
            (h, "send", "m.sticker", None, 403),
            (h, "state", protocol.EVENT_TEAM, "", 403),
            (h, "state", "m.room.topic", "", 403),
            (adm, "state", protocol.EVENT_TEAM, "", 200),
            (adm, "state", "m.room.name", "", 200),
            (adm, "send", "m.sticker", None, 200),
        ]
        users = {a: self.agent, h: self.human, adm: self.admin}
        for n, (who, kind, event_type, key, expected) in enumerate(cases):
            with self.subTest(who=who, type=event_type):
                if kind == "send":
                    status, body = self.send(users[who], event_type, {"body": "x"}, f"t{n}")
                else:
                    status, body = self.put_state(users[who], event_type, key, {"x": n})
                self.assertEqual(status, expected, body)
                if expected == 403:
                    self.assertEqual(body["errcode"], "M_FORBIDDEN")

    def test_refusal_names_the_power_tuwunel_names(self) -> None:
        status, body = self.put_state(self.human, "m.room.topic", "", {"topic": "x"})
        self.assertEqual((status, body["error"]), (403, "Auth check failed: sender does not have "
                         "enough power (Int(50)) for `m.room.topic` event type (100)"))

    def test_status_under_own_user_id(self) -> None:
        self.assertEqual(self.put_state(self.agent, protocol.EVENT_STATUS, self.agent, STATUS)[0], 200)

    def test_status_under_another_users_id_is_refused_for_everyone(self) -> None:
        for uid in (self.agent, self.human, self.admin):
            with self.subTest(sender=uid):
                status, body = self.put_state(uid, protocol.EVENT_STATUS, self.other, STATUS)
                self.assertEqual(status, 403)
                self.assertIn("another user's ID", body["error"])

    def test_power_zero_member_writes_status_under_a_non_at_key(self) -> None:
        """The server forces only `@` keys to equal the sender (DESIGN.md section 4), so
        receivers must ignore a status whose key is not its sender's user ID."""
        status, body = self.put_state(self.agent, protocol.EVENT_STATUS, "someone-else", STATUS)
        self.assertEqual(status, 200, body)
        status, got = self.call(self.human, "GET", f"/_matrix/client/v3/rooms/{q(self.room)}/state/"
                                f"{protocol.EVENT_STATUS}/someone-else", {"format": "event"})
        self.assertEqual((status, got["sender"], got["state_key"]), (200, self.agent, "someone-else"))

    def test_a_non_member_can_neither_send_nor_read(self) -> None:
        self.assertEqual(self.send(self.outsider, "m.room.message", {"body": "x"}, "o1")[0], 403)
        status, _ = self.call(self.outsider, "GET", f"/_matrix/client/v3/rooms/{q(self.room)}/state/"
                              "m.room.power_levels/")
        self.assertEqual(status, 403)

    def test_only_admin_invites(self) -> None:
        self.assertEqual(self.invite(self.human, self.outsider)[0], 403)
        self.assertEqual(self.invite(self.agent, self.outsider)[0], 403)
        self.assertEqual(self.invite(self.admin, self.outsider)[0], 200)

    def test_only_admin_redacts(self) -> None:
        status, sent = self.send(self.agent, "m.room.message", {"msgtype": "m.text", "body": "x"}, "r0")
        self.assertEqual(status, 200)
        path = f"/_matrix/client/v3/rooms/{q(self.room)}/redact/{q(sent['event_id'])}"
        self.assertEqual(self.call(self.human, "PUT", path + "/r1", body={})[0], 403)
        self.assertEqual(self.call(self.agent, "PUT", path + "/r2", body={})[0], 403)
        self.assertEqual(self.call(self.admin, "PUT", path + "/r3", body={})[0], 200)
        status, event = self.call(self.human, "GET", f"/_matrix/client/v3/rooms/{q(self.room)}/event/"
                                  f"{q(sent['event_id'])}")
        self.assertEqual((status, event["content"]), (200, {}))

    def test_create_event_id_is_the_room_id_in_v12(self) -> None:
        status, create = self.call(self.agent, "GET", f"/_matrix/client/v3/rooms/{q(self.room)}/state/"
                                   "m.room.create/", {"format": "event"})
        self.assertEqual(status, 200)
        self.assertEqual(create["event_id"][1:], self.room[1:])
        self.assertTrue(protocol.is_room_id(self.room))
        self.assertTrue(protocol.is_event_id(create["event_id"]))
        self.assertEqual((create["sender"], create["content"]), (self.admin, {"room_version": "12"}))

    def test_power_levels_read_back_exactly(self) -> None:
        status, levels = self.call(self.agent, "GET", f"/_matrix/client/v3/rooms/{q(self.room)}/state/"
                                   "m.room.power_levels/")
        self.assertEqual(status, 200)
        protocol.check_power_levels(levels, [self.human])

    def test_txn_reuse_returns_the_same_event(self) -> None:
        first = self.send(self.agent, "m.room.message", {"body": "a"}, "same")
        second = self.send(self.agent, "m.room.message", {"body": "a"}, "same")
        self.assertEqual(first, second)
        third = self.call(self.other, "PUT", f"/_matrix/client/v3/rooms/{q(self.room)}/send/"
                          "m.room.message/same", body={"body": "a"})
        self.assertNotEqual(first[1]["event_id"], third[1]["event_id"])


class SyncTest(TeamRoom):
    def test_held_sync_returns_when_an_event_arrives(self) -> None:
        since = self.sync(self.other)["next_batch"]
        result: dict = {}

        def hold() -> None:
            result["body"] = self.sync(self.other, since, timeout=5000)

        thread = threading.Thread(target=hold)
        started = time.monotonic()
        thread.start()
        time.sleep(0.1)
        self.send(self.agent, "m.room.message", {"msgtype": "m.notice", "body": "x"}, "h1")
        thread.join(5)
        self.assertFalse(thread.is_alive())
        self.assertLess(time.monotonic() - started, 4)
        events = result["body"]["rooms"]["join"][self.room]["timeline"]["events"]
        self.assertEqual([(e["sender"], e["content"]["body"]) for e in events], [(self.agent, "x")])

    def test_held_sync_times_out_empty(self) -> None:
        since = self.sync(self.other)["next_batch"]
        started = time.monotonic()
        body = self.sync(self.other, since, timeout=200)
        self.assertGreaterEqual(time.monotonic() - started, 0.15)
        self.assertEqual((body["next_batch"], "rooms" in body), (since, False))

    def test_limited_timeline_and_gap_fill(self) -> None:
        since = self.sync(self.other)["next_batch"]
        for n in range(7):
            self.send(self.agent, "m.room.message", {"body": str(n)}, f"g{n}")
        flt = json.dumps({"room": {"timeline": {"limit": 3}}})
        status, body = self.call(self.other, "GET", "/_matrix/client/v3/sync",
                                 {"since": since, "timeout": "0", "filter": flt})
        self.assertEqual(status, 200)
        timeline = body["rooms"]["join"][self.room]["timeline"]
        self.assertTrue(timeline["limited"])
        self.assertEqual([e["content"]["body"] for e in timeline["events"]], ["4", "5", "6"])
        status, gap = self.call(self.other, "GET", f"/_matrix/client/v3/rooms/{q(self.room)}/messages",
                                {"dir": "f", "from": since, "to": timeline["prev_batch"], "limit": "100",
                                 "filter": json.dumps({"types": ["m.room.message"]})})
        self.assertEqual(status, 200)
        self.assertEqual([e["content"]["body"] for e in gap["chunk"]], ["0", "1", "2", "3"])

    def test_invite_appears_with_stripped_state(self) -> None:
        self.assertEqual(self.invite(self.admin, self.outsider)[0], 200)
        body = self.sync(self.outsider)
        stripped = body["rooms"]["invite"][self.room]["invite_state"]["events"]
        create = [e for e in stripped if e["type"] == "m.room.create"]
        self.assertEqual([e["sender"] for e in create], [self.admin])
        self.assertNotIn("join", body["rooms"])


class AccountTest(unittest.TestCase):
    def setUp(self) -> None:
        self.fake = fake_admin_api.FakeAdminHomeserver(server_name=SN)
        self.admin = self.fake.add_user("admin", admin=True)
        self.admin_token = self.fake.mint_token(self.admin)

    def admin_call(self, method: str, path: str, body: object = None, token: str | None = "") -> tuple[int, dict]:
        return self.fake.request(method, path, {}, body, self.admin_token if token == "" else token)

    def whoami(self, token: str) -> int:
        return self.fake.request("GET", "/_matrix/client/v3/account/whoami", {}, None, token)[0]

    def test_put_user_creates_without_admin_key_and_refuses_admin_false(self) -> None:
        uid = f"@alice:{SN}"
        status, user = self.admin_call("PUT", f"/_synapse/admin/v2/users/{q(uid)}", {"password": "p"})
        self.assertEqual((status, user["admin"], user["name"]), (200, False, uid))
        status, body = self.admin_call("PUT", f"/_synapse/admin/v2/users/{q(f'@bob:{SN}')}",
                                       {"password": "p", "admin": False})
        self.assertEqual((status, body["errcode"]), (500, "M_UNKNOWN"))

    def test_password_reset_logs_the_old_token_out(self) -> None:
        uid = self.fake.add_user("alice", password="old")
        token = self.fake.mint_token(uid)
        self.assertEqual(self.whoami(token), 200)
        self.admin_call("PUT", f"/_synapse/admin/v2/users/{q(uid)}", {"password": "new", "logout_devices": True})
        self.assertEqual(self.whoami(token), 401)
        status, login = self.fake.request("POST", "/_matrix/client/v3/login", {}, {
            "type": "m.login.password", "identifier": {"type": "m.id.user", "user": "alice"},
            "password": "new"}, None)
        self.assertEqual(status, 200, login)
        self.admin_call("POST", f"/_synapse/admin/v1/reset_password/{q(uid)}", {"new_password": "newer"})
        self.assertEqual(self.whoami(login["access_token"]), 401)

    def test_admin_api_needs_an_admin_token(self) -> None:
        member = self.fake.mint_token(self.fake.add_user("repo.1+ws.podman"))
        self.assertEqual(self.admin_call("GET", "/_synapse/admin/v2/users", token=member)[0], 403)
        status, body = self.admin_call("GET", "/_synapse/admin/v2/users", token=None)
        self.assertEqual((status, body["errcode"]), (401, "M_MISSING_TOKEN"))

    def test_single_admin_query(self) -> None:
        self.fake.add_user("repo.1+ws.podman")
        status, body = self.fake.request("GET", "/_synapse/admin/v2/users", {"admins": "true"}, None,
                                         self.admin_token)
        self.assertEqual((status, [u["name"] for u in body["users"]], body["total"]), (200, [self.admin], 1))

    def test_registration_nonce_is_single_use_and_mac_checked(self) -> None:
        nonce = self.admin_call("GET", "/_synapse/admin/v1/register", token=None)[1]["nonce"]
        body = {"nonce": nonce, "username": "repo.1+ws.podman", "password": "p", "admin": False}
        status, refused = self.admin_call("POST", "/_synapse/admin/v1/register",
                                          {**body, "mac": "0" * 40}, token=None)
        self.assertEqual((status, refused["error"]), (403, "M_FORBIDDEN: HMAC check failed"))
        nonce = self.admin_call("GET", "/_synapse/admin/v1/register", token=None)[1]["nonce"]
        mac = synapse_mac(self.fake.shared_secret, nonce, body["username"], "p", False)
        body = {**body, "nonce": nonce, "mac": mac}
        status, made = self.admin_call("POST", "/_synapse/admin/v1/register", body, token=None)
        self.assertEqual((status, made["user_id"]), (200, f"@repo.1+ws.podman:{SN}"))
        self.assertEqual(self.admin_call("POST", "/_synapse/admin/v1/register", body, token=None)[0], 400)

    def test_minted_tokens_are_distinct_devices(self) -> None:
        uid = self.fake.add_user("repo.1+ws.podman")
        path = f"/_synapse/admin/v1/users/{q(uid)}/login"
        first, second = self.admin_call("POST", path, {}), self.admin_call("POST", path, {})
        self.assertEqual((first[0], second[0]), (200, 200))
        self.assertNotEqual(first[1]["access_token"], second[1]["access_token"])
        self.assertEqual(self.whoami(first[1]["access_token"]), 200)

    def test_login_token_minting_is_refused(self) -> None:
        token = self.fake.mint_token(self.fake.add_user("alice"))
        status, body = self.fake.request("POST", "/_matrix/client/v1/login/get_token", {}, {}, token)
        self.assertEqual((status, body["errcode"]), (403, "M_FORBIDDEN"))

    def test_an_unmodelled_admin_field_fails_loudly(self) -> None:
        uid = self.fake.add_user("alice")
        with self.assertRaises(fake_client_api.Unmodelled):
            self.admin_call("PUT", f"/_synapse/admin/v2/users/{q(uid)}", {"locked": True})


class HttpTest(unittest.TestCase):
    def setUp(self) -> None:
        self.fake = fake_client_api.FakeHomeserver(server_name=SN)
        self.uid = self.fake.add_user("repo.1+ws.podman")
        self.token = self.fake.mint_token(self.uid)
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def get(self, base: str, path: str, token: str | None) -> tuple[int, dict]:
        request = urllib.request.Request(base + path)
        if token:
            request.add_unredirected_header("Authorization", f"Bearer {token}")
        try:
            with self.opener.open(request, timeout=5) as response:
                return response.status, json.loads(response.read())
        except urllib.error.HTTPError as error:
            with error:
                return error.code, json.loads(error.read())

    def test_served_over_loopback_http(self) -> None:
        with fake_client_api.serve(self.fake) as base:
            self.assertTrue(base.startswith("http://127.0.0.1:"))
            status, body = self.get(base, "/_matrix/client/v3/account/whoami", self.token)
            self.assertEqual((status, body["user_id"]), (200, self.uid))
            status, body = self.get(base, "/_matrix/client/v3/account/whoami", None)
            self.assertEqual((status, body["errcode"]), (401, "M_MISSING_TOKEN"))
        seen = self.fake.http_log[0]
        self.assertEqual((seen.method, seen.path, seen.headers.get("Authorization")),
                         ("GET", "/_matrix/client/v3/account/whoami", f"Bearer {self.token}"))

    def test_injected_fault_is_served_once(self) -> None:
        self.fake.inject("GET", r"/account/whoami$", 429,
                         {"errcode": "M_LIMIT_EXCEEDED", "error": "Too many requests", "retry_after_ms": 50})
        with fake_client_api.serve(self.fake) as base:
            status, body = self.get(base, "/_matrix/client/v3/account/whoami", self.token)
            self.assertEqual((status, body["retry_after_ms"]), (429, 50))
            self.assertEqual(self.get(base, "/_matrix/client/v3/account/whoami", self.token)[0], 200)

    def test_unknown_route_is_unrecognized(self) -> None:
        status, body = self.fake.request("GET", "/_matrix/client/v3/nonexistent", {}, None, self.token)
        self.assertEqual((status, body["errcode"]), (404, "M_UNRECOGNIZED"))

    def test_client_server_refuses_admin_routes(self) -> None:
        status, _ = self.fake.request("GET", "/_synapse/admin/v1/register", {}, None, None)
        self.assertEqual(status, 404)


if __name__ == "__main__":
    unittest.main()
