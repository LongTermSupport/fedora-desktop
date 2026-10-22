"""A fake of Tuwunel 1.9.x's Synapse-compatible admin API, sharing fake_client_api's state.

Models the calls DESIGN.md section 4 makes, as probe H4 recorded them: shared-secret
registration (`GET`/`POST /_synapse/admin/v1/register`, single-use nonce, HMAC-SHA1 MAC),
`GET`/`PUT /_synapse/admin/v2/users/{id}`, the admin list (`?admins=true`), the token mint
(`POST /_synapse/admin/v1/users/{id}/login`) and `POST /_synapse/admin/v1/reset_password`.
A `PUT v2/users` carrying `"admin": false` for a non-admin answers Tuwunel's 500; whether
Tuwunel keeps the account it was creating was not recorded, so the fake creates none.
A registration with `"admin": true` joins the account to Tuwunel's server admin room, a
room version 11 room with an `!opaque:server` ID, as Tuwunel 1.9.4 does.
Fields H4 did not exercise (`deactivated`, `locked`, `valid_until_ms`, ...) raise
`Unmodelled`, so a unit that needs one records Tuwunel's answer before modelling it.
"""

from __future__ import annotations

import hashlib
import hmac
import secrets
import time
from collections.abc import Callable

from tests.helpers.pingbus.fake_client_api import (
    DEFAULT_SERVER_NAME,
    MEMBER,
    FakeHomeserver,
    Handler,
    MatrixError,
    Req,
    Room,
    Unmodelled,
    User,
    forbidden,
    not_found,
)

_ADMIN = r"/_synapse/admin"
REGISTER_KEYS = frozenset({"nonce", "username", "password", "admin", "mac", "displayname"})
PUT_USER_KEYS = frozenset({"password", "displayname", "admin", "logout_devices"})
RESET_KEYS = frozenset({"new_password", "logout_devices"})
# The list endpoint's entries lack two keys of the single-user object (H4, 022 vs 023).
LIST_OMITS = frozenset({"consent_ts", "suspended"})


def registration_mac(secret: str, nonce: str, username: str, password: str, admin: bool) -> str:
    message = b"\x00".join([nonce.encode(), username.encode(), password.encode(),
                            b"admin" if admin else b"notadmin"])
    return hmac.new(secret.encode(), message, hashlib.sha1).hexdigest()


def _only(body: dict, allowed: frozenset[str], what: str) -> dict:
    extra = set(body) - allowed
    if extra:
        raise Unmodelled(f"{what} with {sorted(extra)}")
    return body


class FakeAdminHomeserver(FakeHomeserver):
    """The client-server fake plus the admin API, gated on a server-admin token."""

    def __init__(self, server_name: str = DEFAULT_SERVER_NAME,
                 clock: Callable[[], float] = time.time, shared_secret: str | None = None) -> None:
        self.shared_secret = shared_secret or secrets.token_hex(32)
        self._nonces: set[str] = set()
        #: Tuwunel's server admin room, made when the first server admin registers.
        self.server_admin_room: str | None = None
        super().__init__(server_name, clock)

    def _route_table(self) -> list[tuple[str, str, Handler, str]]:
        return super()._route_table() + [
            ("GET", _ADMIN + r"/v1/register", self._register_nonce, "none"),
            ("POST", _ADMIN + r"/v1/register", self._register, "none"),
            ("GET", _ADMIN + r"/v2/users", self._list_users, "admin"),
            ("GET", _ADMIN + r"/v2/users/([^/]+)", self._get_user, "admin"),
            ("PUT", _ADMIN + r"/v2/users/([^/]+)", self._put_user, "admin"),
            ("POST", _ADMIN + r"/v1/users/([^/]+)/login", self._mint, "admin"),
            ("POST", _ADMIN + r"/v1/reset_password/([^/]+)", self._reset_password, "admin"),
        ]

    def _user_object(self, user: User) -> dict:
        return {"admin": user.admin, "consent_ts": None, "creation_ts": 0, "deactivated": False,
                "displayname": user.displayname, "erased": False, "is_guest": False,
                "last_seen_ts": user.last_seen_ts, "locked": False, "name": user.user_id,
                "shadow_banned": False, "suspended": False}

    def _existing(self, user_id: str) -> User:
        if user_id not in self.users:
            raise not_found("User not found.")
        return self.users[user_id]

    def _local_part(self, user_id: str) -> str:
        suffix = ":" + self.server_name
        if not (user_id.startswith("@") and user_id.endswith(suffix)):
            raise MatrixError(400, "M_INVALID_PARAM", "User ID is not local to this server.")
        return user_id[1:-len(suffix)]

    def _register_nonce(self, req: Req) -> dict:
        nonce = secrets.token_hex(16)
        self._nonces.add(nonce)
        return {"nonce": nonce}

    def _register(self, req: Req) -> dict:
        body = _only(req.obj(), REGISTER_KEYS, "shared-secret registration")
        nonce = body.get("nonce")
        if nonce not in self._nonces:
            raise MatrixError(400, "M_UNKNOWN", "Unrecognised nonce.")
        self._nonces.discard(nonce)
        expected = registration_mac(self.shared_secret, nonce, body["username"], body["password"],
                                    bool(body.get("admin", False)))
        if not hmac.compare_digest(expected, str(body.get("mac", ""))):
            raise forbidden("M_FORBIDDEN: HMAC check failed")
        user = self._create_user(body["username"], body["password"], bool(body.get("admin", False)),
                                 body.get("displayname"))
        if user.admin:
            self._join_server_admin_room(user.user_id)
        token, device = self._new_token(user.user_id)
        return {"access_token": token, "device_id": device, "home_server": self.server_name,
                "user_id": user.user_id}

    def _join_server_admin_room(self, user_id: str) -> None:
        """Tuwunel's own server admin room, which it joins a new server admin to. Tuwunel
        1.9.4 creates it as room version 11 (src/service/admin/create.rs: `RoomId::new_v1`,
        `RoomVersionId::V11`), so its ID has the `!opaque:server` shape; the host probe of
        Plan 00211 found it, named `<server> Admin Room` with the alias `#admins:<server>`,
        in every team admin's `joined_rooms`."""
        if self.server_admin_room is None:
            room = Room(f"!{secrets.token_hex(9)[:18]}:{self.server_name}", f"@conduit:{self.server_name}")
            self.rooms[room.room_id] = room
            self._append(room, "m.room.create", room.creator, {"room_version": "11", "creator": room.creator}, "")
            self._append(room, MEMBER, room.creator, {"membership": "join"}, room.creator)
            self._append(room, "m.room.name", room.creator, {"name": f"{self.server_name} Admin Room"}, "")
            self._append(room, "m.room.canonical_alias", room.creator, {"alias": f"#admins:{self.server_name}"}, "")
            self.server_admin_room = room.room_id
        room = self.rooms[self.server_admin_room]
        self._append(room, MEMBER, user_id, self._member_content(user_id, "join"), user_id)

    def _list_users(self, req: Req) -> dict:
        if set(req.query) - {"admins"}:
            raise Unmodelled(f"user list with {sorted(req.query)}")
        only_admins = req.query.get("admins") == "true"
        users = [u for _, u in sorted(self.users.items()) if u.admin or not only_admins]
        entries = [{k: v for k, v in self._user_object(u).items() if k not in LIST_OMITS} for u in users]
        return {"next_token": None, "total": len(entries), "users": entries}

    def _get_user(self, req: Req, user_id: str) -> dict:
        return self._user_object(self._existing(user_id))

    def _put_user(self, req: Req, user_id: str) -> dict:
        body = _only(req.obj(), PUT_USER_KEYS, "PUT v2/users")
        user = self.users.get(user_id)
        if body.get("admin") is False and not (user and user.admin):
            raise MatrixError(500, "M_UNKNOWN", f"{user_id} was never an admin.")
        if user is None:
            user = self._create_user(self._local_part(user_id), body.get("password"),
                                     bool(body.get("admin", False)), body.get("displayname"))
        else:
            if "displayname" in body:
                user.displayname = body["displayname"]
            if "admin" in body:
                user.admin = bool(body["admin"])
            if "password" in body:
                user.password = body["password"]
                if body.get("logout_devices", True):
                    self.logout_all(user_id)
        return self._user_object(user)

    def _mint(self, req: Req, user_id: str) -> dict:
        _only(req.obj(), frozenset(), "token mint")
        return {"access_token": self._new_token(self._existing(user_id).user_id)[0]}

    def _reset_password(self, req: Req, user_id: str) -> dict:
        body = _only(req.obj(), RESET_KEYS, "reset_password")
        user = self._existing(user_id)
        user.password = body["new_password"]
        if body.get("logout_devices", True):
            self.logout_all(user_id)
        return {}
