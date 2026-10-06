"""A fake Tuwunel 1.9.3 client-server API, in process, for Plan 00161's container tests.

It models what pingbus and the admin tool use, and nothing else: password login, whoami,
`createRoom` (room version 12, `private_chat`), invite, join, send, state get/put with
`format=event`, redact, `GET /event`, `/messages`, and `/sync` with an inline filter,
`timeout` long-polling and lazy-loaded members. Its answers are checked against Tuwunel's
recorded ones (fixtures/tuwunel/, probe H4) by test_fakes.py; where it says what Tuwunel
says, the text is the recorded text. A request for anything outside that raises
`Unmodelled` rather than guess: extend the fake, with a recorded answer to check it by.

Use `FakeHomeserver.request()` directly, or `serve()` for real HTTP on loopback (it logs
each request's headers in `http_log`). `inject()` queues a canned answer (a 429, say).
The admin API is in fake_admin_api.py, a subclass sharing this state.
"""

from __future__ import annotations

import base64
import contextlib
import copy
import dataclasses
import hashlib
import http.server
import json
import math
import pathlib
import re
import secrets
import threading
import time
import urllib.parse
from collections.abc import Callable, Iterator

FIXTURES = pathlib.Path(__file__).resolve().parent / "fixtures" / "tuwunel"
DEFAULT_SERVER_NAME = "team-a.agent-bus.internal"
ROOM_VERSION = "12"
DEFAULT_TIMELINE_LIMIT = 10
MEMBER = "m.room.member"
POWER_LEVELS = "m.room.power_levels"
_LOCALPART_RE = re.compile(r"[a-z0-9._=/+-]+")
# Stripped state an invitee sees (Tuwunel's `room_invite_state` set, as recorded in H4).
INVITE_STATE_TYPES = ("m.room.create", "m.room.join_rules", "m.room.name", "m.room.topic")
CREATE_ROOM_KEYS = frozenset({"preset", "room_version", "name", "topic", "initial_state",
                              "power_level_content_override", "visibility", "invite"})


class MatrixError(Exception):
    """An error the server answers with: an HTTP status, an errcode and its text."""

    def __init__(self, status: int, errcode: str, error: str) -> None:
        super().__init__(f"{status} {errcode}: {error}")
        self.status, self.errcode, self.error = status, errcode, error


class Unmodelled(AssertionError):
    """The request needs behaviour H4 did not record and the fake does not model."""


def forbidden(error: str) -> MatrixError:
    return MatrixError(403, "M_FORBIDDEN", error)


def not_found(error: str) -> MatrixError:
    return MatrixError(404, "M_NOT_FOUND", error)


def recorded_response(name: str) -> dict:
    """A static answer Tuwunel gave in H4, served as is (versions, login flows)."""
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))["response"]


@dataclasses.dataclass
class User:
    user_id: str
    localpart: str
    password: str | None
    admin: bool
    displayname: str
    last_seen_ts: int | None = None


@dataclasses.dataclass
class Event:
    pos: int
    event_id: str
    room_id: str
    type: str
    sender: str
    content: dict
    ts: int
    state_key: str | None = None
    txn: tuple[str, str, str] | None = None

    def client(self, *, with_room: bool, unsigned: dict | None) -> dict:
        out = {"content": copy.deepcopy(self.content), "event_id": self.event_id,
               "origin_server_ts": self.ts, "sender": self.sender, "type": self.type}
        if self.state_key is not None:
            out["state_key"] = self.state_key
        if with_room:
            out["room_id"] = self.room_id
        if unsigned is not None:
            out["unsigned"] = unsigned
        return out

    def stripped(self) -> dict:
        out = {"content": copy.deepcopy(self.content), "sender": self.sender,
               "state_key": self.state_key, "type": self.type}
        if self.type == "m.room.create":
            out["origin_server_ts"] = self.ts
        return out


@dataclasses.dataclass
class Room:
    room_id: str
    creator: str
    events: list[Event] = dataclasses.field(default_factory=list)
    state: dict[tuple[str, str], Event] = dataclasses.field(default_factory=dict)

    def membership(self, user_id: str) -> str | None:
        event = self.state.get((MEMBER, user_id))
        return event.content.get("membership") if event else None

    def levels(self) -> dict:
        event = self.state.get((POWER_LEVELS, ""))
        return event.content if event else {}

    def power(self, user_id: str) -> float:
        """A v12 creator's power is infinite and is not in `users`."""
        if user_id == self.creator:
            return math.inf
        levels = self.levels()
        return levels.get("users", {}).get(user_id, levels.get("users_default", 0))

    def state_before(self, pos: int) -> dict[tuple[str, str], Event]:
        found: dict[tuple[str, str], Event] = {}
        for event in self.events:
            if event.pos < pos and event.state_key is not None:
                found[(event.type, event.state_key)] = event
        return found


@dataclasses.dataclass
class SyncFilter:
    rooms: frozenset[str] | None = None
    timeline_types: tuple[str, ...] | None = None
    timeline_limit: int = DEFAULT_TIMELINE_LIMIT
    state_types: tuple[str, ...] | None = None
    lazy_members: bool = False

    @staticmethod
    def parse(raw: str | None) -> SyncFilter:
        if raw is None:
            return SyncFilter()
        if not raw.startswith("{"):
            raise Unmodelled("stored filter IDs; the fake takes inline JSON filters only")
        room = json.loads(raw).get("room", {})
        timeline, state = room.get("timeline", {}), room.get("state", {})
        listed = room.get("rooms")
        return SyncFilter(
            rooms=frozenset(listed) if listed is not None else None,
            timeline_types=tuple(timeline["types"]) if "types" in timeline else None,
            timeline_limit=int(timeline.get("limit", DEFAULT_TIMELINE_LIMIT)),
            state_types=tuple(state["types"]) if "types" in state else None,
            lazy_members=bool(state.get("lazy_load_members", False)))


def _type_ok(event_type: str, types: tuple[str, ...] | None) -> bool:
    return types is None or event_type in types


@dataclasses.dataclass(frozen=True)
class HttpRecord:
    method: str
    path: str
    query: dict[str, str]
    headers: dict[str, str]


@dataclasses.dataclass
class _Fault:
    method: str
    pattern: re.Pattern[str]
    status: int
    body: dict
    remaining: int


@dataclasses.dataclass
class Req:
    user_id: str | None
    device_id: str | None
    query: dict[str, str]
    body: object

    def obj(self) -> dict:
        if not isinstance(self.body, dict):
            raise MatrixError(400, "M_NOT_JSON", "Content not JSON.")
        return self.body


Handler = Callable[..., dict]
_ROOM = r"/_matrix/client/v3/rooms/([^/]+)"


class FakeHomeserver:
    """One homeserver's accounts, tokens and rooms, behind the client-server API."""

    def __init__(self, server_name: str = DEFAULT_SERVER_NAME,
                 clock: Callable[[], float] = time.time) -> None:
        self.server_name = server_name
        self.users: dict[str, User] = {}
        self.rooms: dict[str, Room] = {}
        self.http_log: list[HttpRecord] = []
        self.unmodelled: list[Unmodelled] = []
        self._clock = clock
        self._cond = threading.Condition()
        self._tokens: dict[str, tuple[str, str]] = {}
        self._txns: dict[tuple[str, str, str, str], str] = {}
        self._faults: list[_Fault] = []
        self._pos = 0
        self._ids = 0
        self._salt = secrets.token_bytes(16)
        self._routes = [(method, re.compile(pattern), handler, auth)
                        for method, pattern, handler, auth in self._route_table()]

    # ── seeding, for tests that need an account rather than the admin flow ──────────────

    def add_user(self, localpart: str, password: str | None = None, admin: bool = False,
                 displayname: str | None = None) -> str:
        with self._cond:
            return self._create_user(localpart, password, admin, displayname).user_id

    def mint_token(self, user_id: str) -> str:
        with self._cond:
            if user_id not in self.users:
                raise not_found("User not found.")
            return self._new_token(user_id)[0]

    def inject(self, method: str, path_pattern: str, status: int, body: dict, times: int = 1) -> None:
        """Answer the next `times` matching requests with `status` and `body`."""
        with self._cond:
            self._faults.append(_Fault(method, re.compile(path_pattern), status, body, times))

    # ── the API ──────────────────────────────────────────────────────────────────────────

    def request(self, method: str, path: str, query: dict[str, str] | None = None,
                body: object = None, token: str | None = None) -> tuple[int, dict]:
        """One request: `path` as on the wire (percent-encoded). Returns (status, JSON)."""
        with self._cond:
            for fault in self._faults:
                if fault.remaining and fault.method == method and fault.pattern.search(path):
                    fault.remaining -= 1
                    return fault.status, copy.deepcopy(fault.body)
            try:
                return 200, self._dispatch(method, path, query or {}, body, token)
            except MatrixError as error:
                return error.status, {"errcode": error.errcode, "error": error.error}

    def _dispatch(self, method: str, path: str, query: dict[str, str], body: object,
                  token: str | None) -> dict:
        for route_method, pattern, handler, auth in self._routes:
            match = pattern.fullmatch(path)
            if route_method != method or not match:
                continue
            user_id = device_id = None
            if auth != "none":
                user_id, device_id = self._authenticate(token)
                if auth == "admin" and not self.users[user_id].admin:
                    raise forbidden("M_FORBIDDEN: Only server administrators can use this endpoint")
            args = [urllib.parse.unquote(group) if group is not None else None for group in match.groups()]
            return handler(Req(user_id, device_id, query, body), *args)
        raise MatrixError(404, "M_UNRECOGNIZED", "Unrecognized request")

    def _route_table(self) -> list[tuple[str, str, Handler, str]]:
        return [
            ("GET", r"/_matrix/client/versions", self._static("001-client-versions.json"), "none"),
            ("GET", r"/_tuwunel/server_version", self._static("002-server-version.json"), "none"),
            ("GET", r"/_matrix/client/v3/login", self._static("003-login-flows.json"), "none"),
            ("POST", r"/_matrix/client/v3/login", self._login, "none"),
            ("GET", r"/_matrix/client/v3/account/whoami", self._whoami, "user"),
            ("POST", r"/_matrix/client/v1/login/get_token", self._get_token, "user"),
            ("POST", r"/_matrix/client/v3/createRoom", self._create_room, "user"),
            ("POST", _ROOM + r"/invite", self._invite, "user"),
            ("POST", _ROOM + r"/join", self._join, "user"),
            ("PUT", _ROOM + r"/send/([^/]+)/([^/]+)", self._send, "user"),
            ("PUT", _ROOM + r"/state/([^/]+)(?:/([^/]*))?", self._put_state, "user"),
            ("GET", _ROOM + r"/state/([^/]+)(?:/([^/]*))?", self._get_state, "user"),
            ("PUT", _ROOM + r"/redact/([^/]+)/([^/]+)", self._redact, "user"),
            ("GET", _ROOM + r"/event/([^/]+)", self._get_event, "user"),
            ("GET", _ROOM + r"/messages", self._messages, "user"),
            ("GET", r"/_matrix/client/v3/sync", self._sync, "user"),
        ]

    # ── accounts and tokens ──────────────────────────────────────────────────────────────

    def now_ms(self) -> int:
        return int(self._clock() * 1000)

    def user_id_of(self, localpart: str) -> str:
        return f"@{localpart}:{self.server_name}"

    def _create_user(self, localpart: str, password: str | None, admin: bool,
                     displayname: str | None) -> User:
        if not _LOCALPART_RE.fullmatch(localpart):
            raise MatrixError(400, "M_INVALID_USERNAME", "User ID is not valid.")
        user_id = self.user_id_of(localpart)
        if user_id in self.users:
            raise MatrixError(400, "M_USER_IN_USE", "User ID already taken.")
        user = User(user_id, localpart, password, admin, displayname or localpart)
        self.users[user_id] = user
        return user

    def _new_token(self, user_id: str) -> tuple[str, str]:
        token, device = secrets.token_urlsafe(24), secrets.token_hex(5).upper()
        self._tokens[token] = (user_id, device)
        return token, device

    def logout_all(self, user_id: str) -> None:
        for token in [t for t, (uid, _) in self._tokens.items() if uid == user_id]:
            del self._tokens[token]

    def _authenticate(self, token: str | None) -> tuple[str, str]:
        if token is None:
            raise MatrixError(401, "M_MISSING_TOKEN", "Missing access token.")
        if token not in self._tokens:
            raise MatrixError(401, "M_UNKNOWN_TOKEN", "M_UNKNOWN_TOKEN: Unknown access token.")
        user_id, device_id = self._tokens[token]
        self.users[user_id].last_seen_ts = self.now_ms()
        return user_id, device_id

    def _static(self, name: str) -> Handler:
        return lambda req: recorded_response(name)

    def _login(self, req: Req) -> dict:
        body = req.obj()
        if body.get("type") != "m.login.password":
            raise Unmodelled(f"login type {body.get('type')!r}")
        name = body.get("identifier", {}).get("user", body.get("user", ""))
        user = self.users.get(name if name.startswith("@") else self.user_id_of(name))
        if user is None or user.password is None or user.password != body.get("password"):
            raise forbidden("Wrong username or password.")
        token, device = self._new_token(user.user_id)
        return {"access_token": token, "device_id": device, "home_server": self.server_name,
                "user_id": user.user_id}

    def _whoami(self, req: Req) -> dict:
        return {"device_id": req.device_id, "user_id": req.user_id}

    def _get_token(self, req: Req) -> dict:
        raise forbidden("M_FORBIDDEN: Login via an existing session is not enabled")

    # ── rooms and events ─────────────────────────────────────────────────────────────────

    def _new_id_body(self) -> str:
        self._ids += 1
        digest = hashlib.sha256(self._salt + self._ids.to_bytes(8, "big")).digest()
        return base64.urlsafe_b64encode(digest).decode().rstrip("=")

    def _append(self, room: Room, event_type: str, sender: str, content: dict,
                state_key: str | None = None, event_id: str | None = None,
                txn: tuple[str, str, str] | None = None) -> Event:
        self._pos += 1
        event = Event(self._pos, event_id or "$" + self._new_id_body(), room.room_id, event_type,
                      sender, copy.deepcopy(content), self.now_ms(), state_key, txn)
        room.events.append(event)
        if state_key is not None:
            room.state[(event_type, state_key)] = event
        self._cond.notify_all()
        return event

    def _room(self, room_id: str) -> Room:
        if room_id not in self.rooms:
            raise not_found("Room not found.")
        return self.rooms[room_id]

    def _joined_room(self, room_id: str, user_id: str) -> Room:
        room = self._room(room_id)
        if room.membership(user_id) != "join":
            raise forbidden("You don't have permission to view this room.")
        return room

    def _member_content(self, user_id: str, membership: str) -> dict:
        return {"displayname": self.users[user_id].displayname, "membership": membership}

    def _create_room(self, req: Req) -> dict:
        body = req.obj()
        unknown = set(body) - CREATE_ROOM_KEYS
        if unknown or body.get("room_version", ROOM_VERSION) != ROOM_VERSION \
                or body.get("preset", "private_chat") != "private_chat":
            raise Unmodelled(f"createRoom beyond a private v12 room: {sorted(body)}")
        id_body, creator = self._new_id_body(), req.user_id
        room = Room("!" + id_body, creator)
        self.rooms[room.room_id] = room
        self._append(room, "m.room.create", creator, {"room_version": ROOM_VERSION}, "", "$" + id_body)
        self._append(room, MEMBER, creator, self._member_content(creator, "join"), creator)
        levels = {"users": {}, "users_default": 0, "events_default": 0, "state_default": 50,
                  "ban": 50, "kick": 50, "redact": 50, "invite": 0, "notifications": {"room": 50},
                  "events": {"m.room.name": 50, POWER_LEVELS: 100, "m.room.history_visibility": 100,
                             "m.room.canonical_alias": 50, "m.room.avatar": 50,
                             "m.room.tombstone": 150, "m.room.server_acl": 100,
                             "m.room.encryption": 100}}
        levels.update(body.get("power_level_content_override", {}))
        self._append(room, POWER_LEVELS, creator, levels, "")
        self._append(room, "m.room.join_rules", creator, {"join_rule": "invite"}, "")
        self._append(room, "m.room.history_visibility", creator, {"history_visibility": "shared"}, "")
        self._append(room, "m.room.guest_access", creator, {"guest_access": "can_join"}, "")
        for event in body.get("initial_state", []):
            self._append(room, event["type"], creator, event["content"], event.get("state_key", ""))
        if "name" in body:
            self._append(room, "m.room.name", creator, {"name": body["name"]}, "")
        if "topic" in body:
            topic = body["topic"]
            self._append(room, "m.room.topic", creator,
                         {"m.topic": {"m.text": [{"body": topic}]}, "topic": topic}, "")
        for target in body.get("invite", []):
            self._do_invite(room, creator, target)
        return {"room_id": room.room_id}

    def _authorise(self, room: Room, sender: str, event_type: str, state_key: str | None) -> None:
        """The room-version-12 auth rules pingbus relies on, in the spec's order."""
        if room.membership(sender) != "join":
            raise forbidden("Auth check failed: sender is not joined to the room")
        levels = room.levels()
        default = levels.get("state_default", 50) if state_key is not None else levels.get("events_default", 0)
        required = levels.get("events", {}).get(event_type, default)
        have = room.power(sender)
        if have < required:
            raise forbidden(f"Auth check failed: sender does not have enough power (Int({have})) "
                            f"for `{event_type}` event type ({required})")
        if state_key is not None and state_key.startswith("@") and state_key != sender:
            raise forbidden("Auth check failed: sender cannot send event with `state_key` "
                            "matching another user's ID")

    def _do_invite(self, room: Room, sender: str, target: str) -> None:
        if room.membership(sender) != "join":
            raise forbidden("Auth check failed: sender is not joined to the room")
        if room.membership(target) in ("join", "ban"):
            raise forbidden("Auth check failed: cannot invite user that is joined or banned")
        if room.power(sender) < room.levels().get("invite", 0):
            raise forbidden("Auth check failed: sender does not have enough power to invite")
        if target not in self.users:
            raise not_found("User not found.")
        self._append(room, MEMBER, sender, self._member_content(target, "invite"), target)

    def _invite(self, req: Req, room_id: str) -> dict:
        self._do_invite(self._room(room_id), req.user_id, req.obj()["user_id"])
        return {}

    def _join(self, req: Req, room_id: str) -> dict:
        room = self._room(room_id)
        membership = room.membership(req.user_id)
        if membership not in ("invite", "join"):
            raise forbidden("You are not invited to this room.")
        if membership == "invite":
            self._append(room, MEMBER, req.user_id, self._member_content(req.user_id, "join"), req.user_id)
        return {"room_id": room.room_id}

    def _txn_event(self, req: Req, room: Room, txn: str,
                   send: Callable[[tuple[str, str, str]], Event]) -> dict:
        """A transaction ID reused by the same device returns the first event's ID."""
        key = (req.user_id, req.device_id, room.room_id, txn)
        if key not in self._txns:
            self._txns[key] = send((req.user_id, req.device_id, txn)).event_id
        return {"event_id": self._txns[key]}

    def _send(self, req: Req, room_id: str, event_type: str, txn: str) -> dict:
        room, content = self._room(room_id), req.obj()

        def send(txn_key: tuple[str, str, str]) -> Event:
            self._authorise(room, req.user_id, event_type, None)
            return self._append(room, event_type, req.user_id, content, txn=txn_key)

        return self._txn_event(req, room, txn, send)

    def _put_state(self, req: Req, room_id: str, event_type: str, state_key: str | None) -> dict:
        room, content, key = self._room(room_id), req.obj(), state_key or ""
        self._authorise(room, req.user_id, event_type, key)
        return {"event_id": self._append(room, event_type, req.user_id, content, key).event_id}

    def _get_state(self, req: Req, room_id: str, event_type: str, state_key: str | None) -> dict:
        room = self._joined_room(room_id, req.user_id)
        event = room.state.get((event_type, state_key or ""))
        if event is None:
            raise not_found("Event not found.")
        if req.query.get("format") == "event":
            return event.client(with_room=True, unsigned={})
        return copy.deepcopy(event.content)

    def _find_event(self, room: Room, event_id: str) -> Event:
        for event in room.events:
            if event.event_id == event_id:
                return event
        raise not_found("Event not found.")

    def _redact(self, req: Req, room_id: str, event_id: str, txn: str) -> dict:
        room = self._room(room_id)

        def send(txn_key: tuple[str, str, str]) -> Event:
            self._authorise(room, req.user_id, "m.room.redaction", None)
            target = self._find_event(room, event_id)
            if target.sender != req.user_id and room.power(req.user_id) < room.levels().get("redact", 50):
                raise forbidden("Auth check failed: sender does not have enough power to redact")
            if target.state_key is not None:
                raise Unmodelled("redacting a state event: the spec keeps per-type content keys")
            target.content = {}
            return self._append(room, "m.room.redaction", req.user_id, {"redacts": event_id}, txn=txn_key)

        return self._txn_event(req, room, txn, send)

    def _unsigned(self, event: Event, req: Req) -> dict:
        unsigned: dict[str, object] = {"age": max(0, self.now_ms() - event.ts)}
        if event.txn and event.txn[:2] == (req.user_id, req.device_id):
            unsigned["transaction_id"] = event.txn[2]
        return unsigned

    def _get_event(self, req: Req, room_id: str, event_id: str) -> dict:
        event = self._find_event(self._joined_room(room_id, req.user_id), event_id)
        return event.client(with_room=True, unsigned=self._unsigned(event, req))

    def _batch(self, value: str | None, default: int) -> int:
        if value is None:
            return default
        if not value.isdigit():
            raise MatrixError(400, "M_INVALID_PARAM", "Invalid pagination token.")
        return int(value)

    def _messages(self, req: Req, room_id: str) -> dict:
        room, query = self._joined_room(room_id, req.user_id), req.query
        direction = query.get("dir")
        listed = json.loads(query["filter"]).get("types") if "filter" in query else None
        types = tuple(listed) if listed is not None else None
        limit = int(query.get("limit", "10"))
        if direction == "f":
            low, high = self._batch(query.get("from"), 0), self._batch(query.get("to"), self._pos)
        elif direction == "b":
            high, low = self._batch(query.get("from"), self._pos), self._batch(query.get("to"), 0)
        else:
            raise MatrixError(400, "M_INVALID_PARAM", "dir must be f or b")
        events = [e for e in room.events if low < e.pos <= high and _type_ok(e.type, types)]
        chunk = (events if direction == "f" else events[::-1])[:limit]
        out: dict[str, object] = {
            "chunk": [e.client(with_room=True, unsigned=self._unsigned(e, req)) for e in chunk],
            "start": query.get("from", str(self._pos))}
        if chunk:
            out["end"] = str(chunk[-1].pos if direction == "f" else chunk[-1].pos - 1)
        return out

    # ── sync ─────────────────────────────────────────────────────────────────────────────

    def _sync(self, req: Req) -> dict:
        since = self._batch(req.query.get("since"), -1)
        since_pos = None if since < 0 else since
        flt = SyncFilter.parse(req.query.get("filter"))
        deadline = time.monotonic() + int(req.query.get("timeout", "0")) / 1000
        while True:
            response = self._sync_once(req, since_pos, flt)
            remaining = deadline - time.monotonic()
            if "rooms" in response or since_pos is None or remaining <= 0:
                return response
            self._cond.wait(remaining)

    def _sync_once(self, req: Req, since_pos: int | None, flt: SyncFilter) -> dict:
        joined: dict[str, dict] = {}
        invited: dict[str, dict] = {}
        for room in self.rooms.values():
            member = room.state.get((MEMBER, req.user_id))
            if member is None or (flt.rooms is not None and room.room_id not in flt.rooms):
                continue
            membership = member.content.get("membership")
            if membership == "invite" and (since_pos is None or member.pos > since_pos):
                invited[room.room_id] = {"invite_state": {"events": self._invite_state(room, member)}}
            elif membership == "join":
                entry = self._joined_entry(room, req, member, since_pos, flt)
                if entry is not None:
                    joined[room.room_id] = entry
        response: dict[str, object] = {"device_one_time_keys_count": {"signed_curve25519": 0},
                                       "device_unused_fallback_key_types": [],
                                       "next_batch": str(self._pos)}
        rooms = {kind: found for kind, found in (("join", joined), ("invite", invited)) if found}
        if rooms:
            response["rooms"] = rooms
        return response

    def _invite_state(self, room: Room, invite: Event) -> list[dict]:
        events = [room.state[(t, "")] for t in INVITE_STATE_TYPES if (t, "") in room.state]
        inviter = room.state.get((MEMBER, invite.sender))
        return [e.stripped() for e in events + ([inviter] if inviter else []) + [invite]]

    def _joined_entry(self, room: Room, req: Req, member: Event, since_pos: int | None,
                      flt: SyncFilter) -> dict | None:
        newly = since_pos is None or member.pos > since_pos
        in_range = [e for e in room.events if since_pos is None or e.pos > since_pos]
        candidates = [e for e in in_range if _type_ok(e.type, flt.timeline_types)]
        timeline = candidates[-flt.timeline_limit:] if flt.timeline_limit > 0 else []
        start = timeline[0].pos if timeline else self._pos + 1
        limited = len(candidates) > len(timeline) or (
            newly and since_pos is not None and room.events[0].pos <= since_pos)
        at_start = room.state_before(start)
        state = {k: e for k, e in at_start.items()
                 if (newly or e.pos > since_pos) and _type_ok(k[0], flt.state_types)}
        if flt.lazy_members:
            senders = {e.sender for e in timeline}
            state = {k: e for k, e in state.items() if k[0] != MEMBER or k[1] in senders}
            for sender in senders:
                if (MEMBER, sender) in at_start and _type_ok(MEMBER, flt.state_types):
                    state[(MEMBER, sender)] = at_start[(MEMBER, sender)]
        if not newly and not timeline and not state:
            return None
        members = {k[1]: e.content.get("membership") for k, e in room.state.items() if k[0] == MEMBER}
        heroes = sorted(u for u, m in members.items() if m in ("join", "invite") and u != req.user_id)
        out_timeline: dict[str, object] = {
            "events": [e.client(with_room=False, unsigned=self._unsigned(e, req)) for e in timeline],
            "prev_batch": str(start - 1)}
        if limited:
            out_timeline["limited"] = True
        return {
            "account_data": {"events": []}, "ephemeral": {"events": []},
            "state": {"events": [e.client(with_room=False, unsigned=None) for e in state.values()]},
            "summary": {"m.heroes": heroes[:5],
                        "m.invited_member_count": sum(m == "invite" for m in members.values()),
                        "m.joined_member_count": sum(m == "join" for m in members.values())},
            "timeline": out_timeline,
            "unread_notifications": {"highlight_count": 0, "notification_count": 0}}


# ── real HTTP on loopback ────────────────────────────────────────────────────────────────


def _handler_for(fake: FakeHomeserver) -> type[http.server.BaseHTTPRequestHandler]:
    class Handler(http.server.BaseHTTPRequestHandler):
        def _handle(self) -> None:
            parsed = urllib.parse.urlsplit(self.path)
            query = dict(urllib.parse.parse_qsl(parsed.query, keep_blank_values=True))
            fake.http_log.append(HttpRecord(self.command, parsed.path, query, dict(self.headers.items())))
            length = int(self.headers.get("Content-Length") or 0)
            raw = self.rfile.read(length) if length else b""
            auth = self.headers.get("Authorization", "")
            token = auth[len("Bearer "):] if auth.startswith("Bearer ") else None
            try:
                body = json.loads(raw) if raw else None
            except ValueError:
                status, payload = 400, {"errcode": "M_NOT_JSON", "error": "Content not JSON."}
            else:
                try:
                    status, payload = fake.request(self.command, parsed.path, query, body, token)
                except Unmodelled as gap:
                    fake.unmodelled.append(gap)
                    status, payload = 500, {"errcode": "M_UNKNOWN", "error": f"fake: unmodelled: {gap}"}
            data = json.dumps(payload).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        do_GET = do_PUT = do_POST = do_DELETE = _handle

        def log_message(self, format: str, *args: object) -> None:
            """Requests are kept in `fake.http_log`; nothing is printed per request."""

    return Handler


@contextlib.contextmanager
def serve(fake: FakeHomeserver) -> Iterator[str]:
    """Serve `fake` on 127.0.0.1 at a free port; yields the base URL.

    An unmodelled request is answered 500 and recorded; on exit the first one is raised."""
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _handler_for(fake))
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_address[1]}"
    finally:
        server.shutdown()
        server.server_close()
        thread.join(5)
    if fake.unmodelled:
        raise fake.unmodelled[0]
