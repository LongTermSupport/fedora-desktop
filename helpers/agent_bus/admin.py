"""The team admin tool's operations: accounts, the team room and member bundles.

Plan 00161's DESIGN.md section 3.4 step 6 (`bootstrap`), 3.7 (secrets) and 4 (accounts,
the room, the commands); the team record and power levels are docs/agent-bus-protocol.md
§8 and the bundle §12. Runs as the `agent-bus` user (the wrapper drops to it) against the
team's homeserver on loopback only.

Where things live, under `/var/lib/agent-bus/<team>/` (0700, the running user's):
`team.json` (the validated team file), `registry.json` (helpers/agent_bus/registry.py;
every change goes through `registry.update_registry`), `room_id` (the team room, written
when `bootstrap` creates it), and `secrets/` holding `registration_shared_secret` (created
by the installer), `admin.token` and `admin.password` (created here). Human passwords
and member tokens are never stored: a member's token leaves only inside its bundle, and a
human's password only as `human password`'s output.

Accounts are created with `PUT /_synapse/admin/v2/users/<id>` carrying a random password
that is discarded at once and **no `admin` key**: with `"admin": false` Tuwunel 1.9.3
answers 500 (probe H4, DESIGN.md section 4).

The homeserver is reached through a `Transport`, `(method, path, query, body, token) ->
(status, JSON object)`, `path` percent-encoded as on the wire. `http_transport` is the
real one (stdlib urllib, loopback only, no proxy, no redirect, the token only in an
unredirected `Authorization` header); tests pass the fake homeserver's `request`. It is
the narrowest interface the admin tool needs, kept apart from the member-side Matrix
client (U09, `helpers/pingbus/matrix.py`), which serves a member's token and room.

Errors are `AdminError` (and its `Unreachable` and `ConfigError`); no message ever holds
a token, password or secret.
"""

from __future__ import annotations

import hashlib
import hmac
import io
import ipaddress
import json
import os
import pathlib
import re
import secrets
import stat
import string
import tarfile
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable, Iterable, Mapping
from dataclasses import dataclass

from helpers.agent_bus import registry, teamfile
from helpers.pingbus import config, protocol

Transport = Callable[[str, str, "Mapping[str, str] | None", object, "str | None"], "tuple[int, dict]"]

LOOPBACK = "127.0.0.1"
HTTP_TIMEOUT_S = 30
PASSWORD_LENGTH = 32
PASSWORD_ALPHABET = string.ascii_letters + string.digits
ROOM_TOPIC = "Agent team bus: agents' pings and the team's humans' addressed messages"
REMOVED_REASON = "removed from the team"

TEAM_FILE = "team.json"
REGISTRY_FILE = "registry.json"
ROOM_ID_FILE = "room_id"
SECRETS_DIR = "secrets"
SHARED_SECRET_FILE = "registration_shared_secret"
ADMIN_TOKEN_FILE = "admin.token"
ADMIN_PASSWORD_FILE = "admin.password"
README_FILE = "README"
BUNDLE_FILES = (config.MEMBER_FILE, config.TOKEN_FILE, README_FILE)
JOINED = ("join", "invite")

_ADMIN = "/_synapse/admin"
_CLIENT = "/_matrix/client/v3"
_ERRCODE_RE = re.compile(r"[A-Z][A-Z0-9_]{0,63}")
_DEVICE_ID_RE = re.compile(r"[A-Za-z0-9_.=+/-]{1,128}")
_ERROR_TEXT_MAX = 200

#: What a member does with its bundle, per type (DESIGN.md section 5.2).
NEXT_STEPS = {
    "podman": "A ccy (podman) member: copy this directory to <checkout>/.claude/ccy/pingbus/{team}/,\n"
              "which ccy's .gitignore ignores, and add `export PINGBUS_TEAMS={team}` to the\n"
              "checkout's untracked .claude/ccy/ccy.env.local (placed by that install's IaC).",
    "host": "A bare-host member: copy this directory to ~/.config/pingbus/{team}/ of the dedicated\n"
            "agent user (never a user that holds a human's Matrix session), list {team} in\n"
            "PINGBUS_TEAMS in ~/.config/pingbus/env, and start sessions with agent-bus-claude.",
    "lxc": "An LXC member: inside the container, copy this directory to the agent user's\n"
           "~/.config/pingbus/{team}/ and list {team} in PINGBUS_TEAMS in ~/.config/pingbus/env.",
    "vm": "A VM member: inside the guest, copy this directory to the agent user's\n"
          "~/.config/pingbus/{team}/ and list {team} in PINGBUS_TEAMS in ~/.config/pingbus/env.",
    "docker": "A docker member: bind-mount this directory read-only, or copy it, at\n"
              "$PINGBUS_HOME/{team}/ in the container, and set PINGBUS_TEAMS={team} in its environment.",
}


class AdminError(Exception):
    """The operation failed; the message says why and never holds a secret."""


class Unreachable(AdminError):
    """The homeserver did not answer."""


class ConfigError(AdminError):
    """The team's files on this host are missing or refused."""


@dataclass(frozen=True)
class Bundle:
    handle: str
    tar: bytes


@dataclass(frozen=True)
class Team:
    name: str
    dir: pathlib.Path
    file: teamfile.TeamFile

    @property
    def server_name(self) -> str:
        return self.file.server_name

    @property
    def admin_id(self) -> str:
        return self.user_id(config.ADMIN_LOCALPART)

    @property
    def secrets(self) -> pathlib.Path:
        return self.dir / SECRETS_DIR

    @property
    def registry_path(self) -> pathlib.Path:
        return self.dir / REGISTRY_FILE

    def user_id(self, localpart: str) -> str:
        return f"@{localpart}:{self.server_name}"

    def humans(self) -> list[str]:
        return sorted(self.user_id(h) for h in self.file.humans)

    def base_url(self) -> str:
        return loopback_url(self.file.port)


# ---------------------------------------------------------------- files


def load_team(root: pathlib.Path, name: str) -> Team:
    """The team's directory, which this user must own with mode 0700, and its team file."""
    if not protocol.is_team_name(name):
        raise ConfigError(f"{name!r} is not a team name")
    team_dir = pathlib.Path(root) / name
    try:
        info = os.lstat(team_dir)
    except FileNotFoundError:
        raise ConfigError(f"team {name}: {team_dir} does not exist; run agent-bus-install team first") from None
    if not stat.S_ISDIR(info.st_mode):
        raise ConfigError(f"team {name}: {team_dir} is not a directory")
    if info.st_uid != os.geteuid():
        raise ConfigError(f"team {name}: {team_dir} is owned by uid {info.st_uid}, not this user "
                          f"(uid {os.geteuid()}): run it as sudo agent-bus")
    if stat.S_IMODE(info.st_mode) & 0o077:
        raise ConfigError(f"team {name}: {team_dir} must be mode 0700")
    try:
        tf = teamfile.load_team_file(team_dir / TEAM_FILE)
    except FileNotFoundError:
        raise ConfigError(f"team {name}: {team_dir / TEAM_FILE} does not exist") from None
    except teamfile.TeamFileError as exc:
        raise ConfigError(f"team {name}: {team_dir / TEAM_FILE}: {exc}") from None
    if tf.team != name:
        raise ConfigError(f"team {name}: {team_dir / TEAM_FILE} is for team {tf.team}")
    return Team(name, team_dir, tf)


def write_private(path: pathlib.Path, text: str, *, replace: bool = False) -> None:
    """Write `text` atomically, mode 0600. Without `replace` an existing file is never
    overwritten (FileExistsError), so two runs cannot both create one secret."""
    path = pathlib.Path(path)
    fd, tmp_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(text)
            handle.flush()
            os.fsync(handle.fileno())
        if replace:
            os.replace(tmp_name, path)
        else:
            os.link(tmp_name, path)
    finally:
        pathlib.Path(tmp_name).unlink(missing_ok=True)
    dir_fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(dir_fd)
    finally:
        os.close(dir_fd)


def _read_private(path: pathlib.Path, what: str) -> str:
    """One line of printable ASCII from a file this user owns, mode 0600 or stricter."""
    try:
        return config.read_token_file(path, f"{what} ({path})")
    except config.ConfigError as exc:
        raise ConfigError(str(exc)) from None


def _room_id(team: Team) -> str:
    path = team.dir / ROOM_ID_FILE
    if not os.path.lexists(path):
        raise ConfigError(f"team {team.name}: no team room yet: run agent-bus bootstrap {team.name}")
    room = _read_private(path, "the team room ID")
    if not protocol.is_room_id(room):
        raise ConfigError(f"team {team.name}: {path} does not hold a room ID")
    return room


def generate_password() -> str:
    return "".join(secrets.choice(PASSWORD_ALPHABET) for _ in range(PASSWORD_LENGTH))


# ---------------------------------------------------------------- transport


def loopback_url(port: int) -> str:
    return f"http://{LOOPBACK}:{port}"


def check_base_url(url: str) -> str:
    """Only `http://127.0.0.1:<port>`: the admin tool talks to its own host's loopback."""
    parts = urllib.parse.urlsplit(url)
    try:
        port = parts.port
    except ValueError:
        port = None
    if (parts.scheme != "http" or parts.hostname != LOOPBACK or parts.netloc != f"{LOOPBACK}:{port}"
            or port is None or parts.path or parts.query or parts.fragment):
        raise AdminError(f"refusing base URL {url!r}: the admin tool talks to http://{LOOPBACK}:<port> only")
    return url


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args: object, **kwargs: object) -> None:
        return None


def http_transport(base_url: str, timeout: float = HTTP_TIMEOUT_S) -> Transport:
    check_base_url(base_url)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), _NoRedirect())

    def call(method: str, path: str, query: Mapping[str, str] | None = None, body: object = None,
             token: str | None = None) -> tuple[int, dict]:
        if not path.startswith("/"):
            raise AdminError(f"not a request path: {path!r}")
        url = base_url + path + ("?" + urllib.parse.urlencode(query) if query else "")
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(url, data=data, method=method)
        if data is not None:
            request.add_header("Content-Type", "application/json")
        if token is not None:
            request.add_unredirected_header("Authorization", f"Bearer {token}")
        try:
            with opener.open(request, timeout=timeout) as response:
                status, raw = response.status, response.read()
        except urllib.error.HTTPError as exc:
            status, raw = exc.code, exc.read()
        except (urllib.error.URLError, OSError) as exc:
            reason = getattr(exc, "reason", exc)
            raise Unreachable(f"the homeserver at {base_url} is unreachable: {reason}") from None
        try:
            obj = json.loads(raw)
        except (UnicodeDecodeError, ValueError):
            raise AdminError(f"{method} {path}: the homeserver answered {status} with no JSON") from None
        if not isinstance(obj, dict):
            raise AdminError(f"{method} {path}: the homeserver answered {status} with no JSON object")
        return status, obj

    return call


# ---------------------------------------------------------------- requests


def _q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


def _user_path(user_id: str) -> str:
    return f"{_ADMIN}/v2/users/{_q(user_id)}"


def _state_path(room: str, event_type: str, key: str = "") -> str:
    return f"{_CLIENT}/rooms/{_q(room)}/state/{_q(event_type)}/{_q(key)}"


def _refused(what: str, status: int, obj: Mapping) -> AdminError:
    errcode = obj.get("errcode")
    errcode = errcode if isinstance(errcode, str) and _ERRCODE_RE.fullmatch(errcode) else "-"
    error = obj.get("error")
    text = error if isinstance(error, str) and error.isascii() and error.isprintable() else ""
    return AdminError(f"{what}: the homeserver answered {status} {errcode} {text[:_ERROR_TEXT_MAX]}".rstrip())


@dataclass
class _Api:
    team: Team
    transport: Transport
    token: str | None = None

    def call(self, what: str, method: str, path: str, *, query: Mapping[str, str] | None = None,
             body: object = None, allow: Iterable[int] = (), token: str | None = None) -> tuple[int, dict]:
        status, obj = self.transport(method, path, query, body, token or self.token)
        if status != 200 and status not in allow:
            raise _refused(what, status, obj)
        return status, obj

    def whoami(self, token: str) -> str:
        status, obj = self.transport("GET", f"{_CLIENT}/account/whoami", None, None, token)
        if status in (401, 403):
            raise AdminError(f"team {self.team.name}: the admin token was rejected ({status}); "
                             "rotating it is deferred (DESIGN.md section 3.7)")
        if status != 200:
            raise _refused("whoami", status, obj)
        return obj.get("user_id")

    def account(self, user_id: str) -> dict | None:
        status, obj = self.call(f"look up {user_id}", "GET", _user_path(user_id), allow=(404,))
        return None if status == 404 else obj

    def create_account(self, user_id: str, displayname: str) -> None:
        """A non-admin account with a discarded random password and no `admin` key."""
        _, obj = self.call(f"create {user_id}", "PUT", _user_path(user_id),
                           body={"password": generate_password(), "displayname": displayname})
        if obj.get("name") != user_id or obj.get("admin") is not False:
            raise AdminError(f"create {user_id}: the homeserver did not create a non-admin account")

    def reset_password(self, user_id: str, password: str) -> None:
        """A new password; every device of the account is logged out (probe H4)."""
        self.call(f"set the password of {user_id}", "PUT", _user_path(user_id),
                  body={"password": password, "logout_devices": True})

    def mint(self, user_id: str) -> str:
        _, obj = self.call(f"mint a token for {user_id}", "POST",
                           f"{_ADMIN}/v1/users/{_q(user_id)}/login", body={})
        token = obj.get("access_token")
        if not config.is_printable_token(token):
            raise AdminError(f"mint a token for {user_id}: no usable access token in the answer")
        return token

    def state(self, room: str, event_type: str, key: str = "", *, event: bool = False) -> dict | None:
        status, obj = self.call(f"read {event_type}", "GET", _state_path(room, event_type, key),
                                query={"format": "event"} if event else None, allow=(404,))
        return None if status == 404 else obj

    def put_state(self, room: str, event_type: str, content: dict) -> None:
        self.call(f"set {event_type}", "PUT", _state_path(room, event_type), body=content)

    def membership(self, room: str, user_id: str) -> str | None:
        content = self.state(room, "m.room.member", user_id)
        membership = content.get("membership") if content else None
        return membership if isinstance(membership, str) else None

    def invite(self, room: str, user_id: str) -> None:
        self.call(f"invite {user_id}", "POST", f"{_CLIENT}/rooms/{_q(room)}/invite",
                  body={"user_id": user_id})

    def kick(self, room: str, user_id: str) -> None:
        self.call(f"remove {user_id} from the team room", "POST", f"{_CLIENT}/rooms/{_q(room)}/kick",
                  body={"user_id": user_id, "reason": REMOVED_REASON})

    def set_user_flag(self, user_id: str, flag: str, value: bool) -> None:
        self.call(f"set {flag} on {user_id}", "PUT", _user_path(user_id), body={flag: value})


def _session(team: Team, transport: Transport) -> _Api:
    """An admin session from the stored token, checked to be this team's `admin`."""
    path = team.secrets / ADMIN_TOKEN_FILE
    if not os.path.lexists(path):
        raise ConfigError(f"team {team.name}: no admin token: run agent-bus bootstrap {team.name}")
    api = _Api(team, transport)
    api.token = _read_private(path, "the admin token")
    _check_admin(api, api.whoami(api.token))
    return api


def _check_admin(api: _Api, user_id: object) -> None:
    if user_id != api.team.admin_id:
        raise ConfigError(f"team {api.team.name}: the homeserver's admin is {user_id}, not "
                          f"{api.team.admin_id}: team.json's server_name does not match the homeserver")


# ---------------------------------------------------------------- bootstrap


def registration_mac(secret: str, nonce: str, username: str, password: str, admin: bool) -> str:
    """Synapse's shared-secret registration MAC (HMAC-SHA1), which Tuwunel implements."""
    message = b"\x00".join([nonce.encode(), username.encode(), password.encode(),
                            b"admin" if admin else b"notadmin"])
    return hmac.new(secret.encode(), message, hashlib.sha1).hexdigest()


def _register_admin(api: _Api, password: str) -> tuple[int, dict]:
    team = api.team
    shared = _read_private(team.secrets / SHARED_SECRET_FILE, "the registration shared secret")
    _, nonce_obj = api.call("get a registration nonce", "GET", f"{_ADMIN}/v1/register")
    nonce = nonce_obj.get("nonce")
    if not config.is_printable_token(nonce):
        raise AdminError("get a registration nonce: no nonce in the answer")
    mac = registration_mac(shared, nonce, config.ADMIN_LOCALPART, password, True)
    body = {"nonce": nonce, "username": config.ADMIN_LOCALPART, "password": password,
            "admin": True, "mac": mac}
    return api.transport("POST", f"{_ADMIN}/v1/register", None, body, None)


def _login_admin(api: _Api, password: str) -> str:
    _, obj = api.call("log the admin in", "POST", f"{_CLIENT}/login", body={
        "type": "m.login.password", "identifier": {"type": "m.id.user", "user": config.ADMIN_LOCALPART},
        "password": password, "initial_device_display_name": "agent-bus"})
    return obj.get("access_token")


def _admin_api(team: Team, transport: Transport, changes: list[str]) -> _Api:
    """The admin session; registers `admin` with the shared secret the first time.

    The password is stored before registering, so a run that dies after registering
    and before storing the token logs in with it next time instead of failing."""
    if os.path.lexists(team.secrets / ADMIN_TOKEN_FILE):
        return _session(team, transport)
    api = _Api(team, transport)
    password_path = team.secrets / ADMIN_PASSWORD_FILE
    if os.path.lexists(password_path):
        password = _read_private(password_path, "the admin password")
    else:
        password = generate_password()
        write_private(password_path, password)
        changes.append("admin password")
    status, obj = _register_admin(api, password)
    if status == 200:
        _check_admin(api, obj.get("user_id"))
        token = obj.get("access_token")
    elif status == 400 and obj.get("errcode") == "M_USER_IN_USE":
        token = _login_admin(api, password)
    else:
        raise _refused("register the team admin", status, obj)
    if not config.is_printable_token(token):
        raise AdminError("register the team admin: no usable access token in the answer")
    _check_admin(api, api.whoami(token))
    write_private(team.secrets / ADMIN_TOKEN_FILE, token)
    changes.append("admin token")
    api.token = token
    return api


def _check_single_admin(api: _Api) -> None:
    _, obj = api.call("list the server admins", "GET", f"{_ADMIN}/v2/users", query={"admins": "true"})
    users = obj.get("users")
    if not isinstance(users, list) or obj.get("next_token") is not None:
        raise AdminError("list the server admins: unexpected answer")
    names = sorted(str(u.get("name")) for u in users if isinstance(u, dict))
    if names != [api.team.admin_id]:
        raise AdminError(f"team {api.team.name}: the homeserver has {len(names)} server admin(s); "
                         f"only {api.team.admin_id} may be one")


def _ensure_humans(api: _Api, changes: list[str]) -> None:
    for name in api.team.file.humans:
        user_id = api.team.user_id(name)
        account = api.account(user_id)
        if account is None:
            api.create_account(user_id, name)
            changes.append(f"human account {name}")
        elif account.get("deactivated") is not False:
            raise AdminError(f"human {name}'s account is deactivated (removed from the team earlier); "
                             "a deactivated account cannot return: choose another name")
        elif account.get("admin") is not False:
            raise AdminError(f"human {name}'s account is a server admin; a human never is")


def _team_record(team: Team, reg: registry.Registry) -> dict:
    tf = team.file
    record = {
        "v": protocol.PROTOCOL_VERSION,
        "team": tf.team,
        "humans": team.humans(),
        "roles": {team.user_id(handle): role for handle, role in sorted(reg.members.items())},
        "repos": [{"repo": r.repo, "branches": list(r.branches)} for r in tf.repos],
        "path_prefixes": list(tf.path_prefixes),
        "forge_api": tf.forge_api,
    }
    try:
        protocol.parse_team_record(record, team.server_name, team.name)
    except protocol.Untrusted as exc:
        raise AdminError(f"team {team.name}: the team record would be invalid ({exc})") from None
    return record


def _canonical(value: object) -> str:
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def _ensure_room(api: _Api, changes: list[str]) -> str:
    team = api.team
    if os.path.lexists(team.dir / ROOM_ID_FILE):
        room = _room_id(team)
        create = api.state(room, "m.room.create", event=True)
        content = create.get("content") if create else None
        if (not isinstance(content, dict) or create.get("sender") != team.admin_id
                or content.get("room_version") != protocol.ROOM_VERSION or "additional_creators" in content):
            raise AdminError(f"team {team.name}: room {room} was not created by {team.admin_id} as version {protocol.ROOM_VERSION}")
        return room
    record = _team_record(team, registry.load_registry(team.registry_path, team.name))
    _, obj = api.call("create the team room", "POST", f"{_CLIENT}/createRoom", body={
        "preset": "private_chat", "room_version": protocol.ROOM_VERSION, "visibility": "private",
        "name": team.name, "topic": ROOM_TOPIC,
        "power_level_content_override": protocol.expected_power_levels(team.humans()),
        "initial_state": [{"type": protocol.EVENT_TEAM, "state_key": "", "content": record}],
    })
    room = obj.get("room_id")
    if not protocol.is_room_id(room):
        raise AdminError("create the team room: no room ID in the answer")
    write_private(team.dir / ROOM_ID_FILE, room)
    changes.append("team room")
    return room


def _sync_team(api: _Api, room: str, changes: list[str]) -> None:
    """Power levels for the current humans, then the team record from the team file and the
    registry (protocol §8); each written only when it differs."""
    team = api.team
    levels = protocol.expected_power_levels(team.humans())
    if _canonical(api.state(room, "m.room.power_levels")) != _canonical(levels):
        api.put_state(room, "m.room.power_levels", levels)
        changes.append("power levels")
    record = _team_record(team, registry.load_registry(team.registry_path, team.name))
    if _canonical(api.state(room, protocol.EVENT_TEAM)) != _canonical(record):
        api.put_state(room, protocol.EVENT_TEAM, record)
        changes.append("team record")


def _remove_account(api: _Api, room: str, user_id: str, changes: list[str]) -> None:
    """Out of the room and deactivated, each only if not already done."""
    if api.membership(room, user_id) in JOINED:
        api.kick(room, user_id)
        changes.append(f"kicked {user_id}")
    account = api.account(user_id)
    if account is not None and account.get("deactivated") is not True:
        api.set_user_flag(user_id, "deactivated", True)
        changes.append(f"deactivated {user_id}")


def bootstrap(team: Team, transport: Transport) -> list[str]:
    """Idempotent (DESIGN.md section 3.4 step 6): the admin account, a single server admin,
    the human accounts, the team room, its power levels and record, the humans' invites,
    and the removal of humans no longer in the team file. Returns what changed."""
    changes: list[str] = []
    api = _admin_api(team, transport, changes)
    _check_single_admin(api)
    _ensure_humans(api, changes)
    room = _ensure_room(api, changes)
    # Removals run before the record is rewritten: the old record is what names a removed
    # human, so a failed kick or deactivation leaves it in place for the next run to retry.
    previous = api.state(room, protocol.EVENT_TEAM) or {}
    listed = previous.get("humans") if isinstance(previous.get("humans"), list) else []
    for user_id in sorted(set(map(str, listed)) - set(team.humans())):
        if protocol.is_human_user_id(user_id, team.server_name):
            _remove_account(api, room, user_id, changes)
    _sync_team(api, room, changes)
    for user_id in team.humans():
        if api.membership(room, user_id) not in JOINED:
            api.invite(room, user_id)
            changes.append(f"invited {user_id}")
    return changes


# ---------------------------------------------------------------- members


def _member_address(team: Team, address: str, type_: str) -> tuple[str, list[str]]:
    """The bundle's `base_url` and `plain_http_hosts`: an address the homeserver listens on,
    or, for a podman member, the link-local address `host.containers.internal` gives it
    (probe H1: rootless podman reaches its own host only that way)."""
    try:
        ip = ipaddress.ip_address(address)
    except ValueError:
        raise AdminError(f"--address {address!r} is not an IP literal") from None
    if str(ip) != address:
        raise AdminError(f"--address {address!r} must be written canonically as {ip}")
    bound = team.file.listen_addresses()
    if address not in bound and not (type_ == "podman" and ip.is_link_local):
        raise AdminError(f"--address {address} is not an address team {team.name} listens on "
                         f"({', '.join(bound)}); a podman member may also use the link-local "
                         "address host.containers.internal resolves to")
    host = f"[{address}]" if ip.version == 6 else address
    return f"http://{host}:{team.file.port}", [address]


def _tar(files: Mapping[str, bytes]) -> bytes:
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w", format=tarfile.PAX_FORMAT) as tar:
        for name, data in files.items():
            info = tarfile.TarInfo(name)
            info.size, info.mode, info.mtime = len(data), 0o600, 0
            tar.addfile(info, io.BytesIO(data))
    return buffer.getvalue()


def _bundle(team: Team, room: str, handle: str, type_: str, token: str, address: str,
            human_text: bool) -> bytes:
    base_url, plain_hosts = _member_address(team, address, type_)
    member = {
        "protocol": protocol.PROTOCOL_VERSION,
        "team": team.name,
        "user_id": team.user_id(handle),
        "server_name": team.server_name,
        "base_url": base_url,
        "plain_http_hosts": plain_hosts,
        "token_file": config.TOKEN_FILE,
        "admin": team.admin_id,
        "room": room,
        "human_text": human_text,
    }
    readme = (f"Agent team bus bundle: member {handle} of team {team.name}.\n\n"
              f"{NEXT_STEPS[type_].format(team=team.name)}\n\n"
              "Then run `pingbus config check`. `token` is this member's credential: keep it\n"
              "mode 0600, never commit it, and ask the team's admin for `rotate-token` if it leaks.\n")
    return _tar({config.MEMBER_FILE: (json.dumps(member, indent=2) + "\n").encode(),
                 config.TOKEN_FILE: token.encode(), README_FILE: readme.encode()})


def _registry_change(team: Team, change: Callable[[registry.Registry], tuple[registry.Registry, object]]) -> object:
    try:
        return registry.update_registry(team.registry_path, team.name, change)
    except registry.RegistryError as exc:
        raise AdminError(f"team {team.name}: {exc}") from None


def add_member(team: Team, transport: Transport, *, repo: str, host: str, type_: str, role: str,
               address: str, human_text: bool) -> Bundle:
    """A new handle, its account (discarded password, no `admin` key), its token, its
    invite, the team record with its role, and its bundle as a tar (protocol §12)."""
    if type_ not in registry.TYPES:
        raise AdminError(f"--type must be one of {', '.join(registry.TYPES)}")
    _member_address(team, address, type_)
    try:
        repo = registry.normalise_repo(repo)
    except registry.HandleError as exc:
        raise AdminError(str(exc)) from None
    api = _session(team, transport)
    room = _room_id(team)
    handle = _registry_change(team, lambda reg: reg.add_member(repo, host, type_, role))
    user_id = team.user_id(handle)
    try:
        if api.account(user_id) is not None:
            raise AdminError(f"{handle} already has an account on the homeserver")
        api.create_account(user_id, handle)
        token = api.mint(user_id)
    except BaseException:
        _registry_change(team, lambda reg: (reg.remove_member(handle), None))
        raise
    api.invite(room, user_id)
    _sync_team(api, room, [])
    return Bundle(handle, _bundle(team, room, handle, type_, token, address, human_text))


def _issued(reg: registry.Registry, handle: str) -> None:
    parsed = protocol.parse_handle(handle)
    if parsed is None:
        raise AdminError(f"{handle!r} is not an agent handle")
    if parsed.n > reg.counters.get(registry.seat_of(handle), 0):
        raise AdminError(f"{handle} was never issued in team {reg.team}")


def remove_member(team: Team, transport: Transport, handle: str) -> list[str]:
    """Role dropped from the record, then kicked and deactivated; its `<n>` is never reused."""
    api = _session(team, transport)
    room = _room_id(team)

    def change(reg: registry.Registry) -> tuple[registry.Registry, bool]:
        _issued(reg, handle)
        if handle not in reg.members:
            return reg, False
        return reg.remove_member(handle), True

    changes = [f"removed {handle} from the registry"] if _registry_change(team, change) else []
    _sync_team(api, room, changes)
    _remove_account(api, room, team.user_id(handle), changes)
    return changes


def set_role(team: Team, transport: Transport, handle: str, role: str) -> list[str]:
    api = _session(team, transport)
    room = _room_id(team)

    def change(reg: registry.Registry) -> tuple[registry.Registry, bool]:
        if handle not in reg.members:
            raise AdminError(f"{handle} is not a member of team {team.name}")
        return reg.set_role(handle, role), reg.members[handle] != role

    changes = [f"{handle} is now {role}"] if _registry_change(team, change) else []
    _sync_team(api, room, changes)
    return changes


def _current_member(api: _Api, handle: str) -> str:
    team = api.team
    if handle not in registry.load_registry(team.registry_path, team.name).members:
        raise AdminError(f"{handle} is not a member of team {team.name}")
    user_id = team.user_id(handle)
    account = api.account(user_id)
    if account is None or account.get("deactivated") is not False:
        raise AdminError(f"{handle} has no active account on the homeserver")
    return user_id


def rotate_token(team: Team, transport: Transport, handle: str) -> bytes:
    """Every device of the member logged out, a new token minted; a tar holding `token`."""
    api = _session(team, transport)
    user_id = _current_member(api, handle)
    api.reset_password(user_id, generate_password())
    return _tar({config.TOKEN_FILE: api.mint(user_id).encode()})


def list_members(team: Team, transport: Transport) -> list[str]:
    """`HUMAN <name> <membership>` and `MEMBER <handle> <role> <membership>`; no tokens."""
    api = _session(team, transport)
    room = _room_id(team)
    lines = [f"HUMAN\t{name}\t{api.membership(room, team.user_id(name)) or '-'}"
             for name in sorted(team.file.humans)]
    reg = registry.load_registry(team.registry_path, team.name)
    lines += [f"MEMBER\t{handle}\t{role}\t{api.membership(room, team.user_id(handle)) or '-'}"
              for handle, role in sorted(reg.members.items())]
    return lines


# ---------------------------------------------------------------- humans


def _human(team: Team, transport: Transport, name: str) -> tuple[_Api, str]:
    if not protocol.is_human_localpart(name) or name not in team.file.humans:
        raise AdminError(f"{name!r} is not a human of team {team.name}")
    api = _session(team, transport)
    user_id = team.user_id(name)
    account = api.account(user_id)
    if account is None or account.get("deactivated") is not False:
        raise AdminError(f"human {name} has no active account: run agent-bus bootstrap {team.name}")
    return api, user_id


def human_password(team: Team, transport: Transport, name: str) -> str:
    """A new 32-character password, every other session logged out; returned, never stored."""
    api, user_id = _human(team, transport, name)
    password = generate_password()
    api.reset_password(user_id, password)
    return password


def human_logout_all(team: Team, transport: Transport, name: str) -> None:
    """Every session ended. Tuwunel's recorded way (probe H4) is a password change with
    `logout_devices`, so the password is replaced by a discarded one too."""
    api, user_id = _human(team, transport, name)
    api.reset_password(user_id, generate_password())


def human_lock(team: Team, transport: Transport, name: str, locked: bool) -> None:
    api, user_id = _human(team, transport, name)
    api.set_user_flag(user_id, "locked", locked)


def human_devices(team: Team, transport: Transport, name: str) -> list[str]:
    """`DEVICE <id> <last seen, Unix ms, or ->` per session; nothing the client named itself."""
    api, user_id = _human(team, transport, name)
    _, obj = api.call(f"list the devices of {user_id}", "GET", f"{_user_path(user_id)}/devices")
    devices = obj.get("devices")
    if not isinstance(devices, list):
        raise AdminError(f"list the devices of {user_id}: unexpected answer")
    lines = []
    for device in devices:
        device_id = device.get("device_id") if isinstance(device, dict) else None
        seen = device.get("last_seen_ts") if isinstance(device, dict) else None
        if not isinstance(device_id, str) or not _DEVICE_ID_RE.fullmatch(device_id):
            raise AdminError(f"list the devices of {user_id}: a device ID is not printable")
        if seen is not None and (type(seen) is not int or seen < 0):
            raise AdminError(f"list the devices of {user_id}: a last-seen time is not a number")
        lines.append(f"DEVICE\t{device_id}\t{'-' if seen is None else seen}")
    return lines
