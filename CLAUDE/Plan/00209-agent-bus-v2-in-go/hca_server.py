"""Plan 00209 Task 1.4: a throwaway Tuwunel and a team on it, for the host checks.

The binary is the pinned one the agent-bus play installed (`tuwunel-<version>` beside its
`.asset-sha256` record, checked against this checkout's pin file); it runs as the invoking
user, on a free loopback port, with its config, database and keys under a work directory the
caller removes. The config is a team's (v1's key set, which v2's C 3.2.4 keeps), so each fact
is about the server a team will run. Nothing else on the host is read or changed.

The rooms are built from A 4.1-4.2 as data here, because `teamchat/proto` (Task 1.3) is not
written yet: these checks decide its rules, so they cannot import them.
"""

from __future__ import annotations

import hashlib
import hmac
import json
import pathlib
import secrets
import signal
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

LIB_DIR = pathlib.Path("/usr/local/lib/agent-bus")
PIN_FILE = "files/usr/local/share/agent-bus/tuwunel.pin"
INSTALL_PLAY = "playbooks/imports/play-agent-bus.yml"
SERVER_NAME = "hca.teamchat.internal"
CLIENT = "/_matrix/client/v3"
READY_S = 120
STOP_S = 60
ROOM_VERSION = "12"

#: The team config's keys, in C 3.2.4's order (v1's set; `cache_capacity_modifier` waits for H-C1).
TEAM_KEYS = (
    "server_name", "address", "port", "database_path", "database_backup_path",
    "database_backups_to_keep", "allow_registration", "allow_guest_registration",
    "registration_shared_secret_file", "grant_admin_to_first_user", "login_via_token",
    "login_via_existing_session", "allow_federation", "trusted_servers", "federate_admin_room",
    "admin_escape_commands", "allow_encryption", "allow_room_creation", "auto_accept_invites",
    "new_user_displayname_suffix", "client_sync_timeout_min", "default_room_version", "sentry",
    "log", "admin_signal_execute", "error_on_unknown_config_opts", "rocksdb_allow_fallocate",
    "max_request_size", "enable_set_displayname", "rate_limiting",
)

#: A 6.5's trust types and the message types a member reads (6.10).
FILTER_TYPES = ["m.room.message", "m.room.redaction", "m.room.create", "m.room.power_levels",
                "m.room.join_rules", "teamchat.roster", "teamchat.room", "m.room.member"]


class MissingDependency(Exception):
    """Something this run needs is not installed: the message names the play (exit 1)."""


class ProbeError(Exception):
    """A request did not get the answer setup needs: what it rests on is not judged."""


def parse_pin(text: str) -> dict[str, str]:
    pin: dict[str, str] = {}
    for line in text.splitlines():
        line = line.strip()
        if line and not line.startswith("#"):
            key, _, value = line.partition(":")
            pin[key.strip()] = value.strip()
    return pin


def pinned_binary(pin: dict[str, str], machine: str, lib_dir: pathlib.Path) -> tuple[pathlib.Path, str, str]:
    """The installed binary for the pinned version, the version without its `v`, and its asset sum."""
    version, sha = pin.get("tuwunel_version", ""), pin.get(f"tuwunel_sha256_{machine}", "")
    if not version.startswith("v") or not sha:
        raise MissingDependency(f"{PIN_FILE} pins no Tuwunel build for {machine}")
    return lib_dir / f"tuwunel-{version[1:]}", version[1:], sha


def config_toml(port: int, root: pathlib.Path, extra: dict[str, object]) -> str:
    keys: dict[str, object] = {
        "server_name": SERVER_NAME, "address": ["127.0.0.1"], "port": port,
        "database_path": f"{root}/db", "database_backup_path": f"{root}/backups",
        "database_backups_to_keep": 7, "allow_registration": False, "allow_guest_registration": False,
        "registration_shared_secret_file": f"{root}/keys/registration", "grant_admin_to_first_user": False,
        "login_via_token": False, "login_via_existing_session": False, "allow_federation": False,
        "trusted_servers": [], "federate_admin_room": False, "admin_escape_commands": False,
        "allow_encryption": False, "allow_room_creation": False, "auto_accept_invites": False,
        "new_user_displayname_suffix": "", "client_sync_timeout_min": 0,
        "default_room_version": ROOM_VERSION, "sentry": False, "log": "warn",
        "admin_signal_execute": ["server backup-database"], "error_on_unknown_config_opts": True,
        "rocksdb_allow_fallocate": False, "max_request_size": 10_000_000, "enable_set_displayname": False,
    }
    lines = [f"{key} = {json.dumps(value)}" for key, value in (keys | extra).items()]
    # v1's login limits (helpers/agent_bus/render.py LOGIN_LIMITS), the last key of TEAM_KEYS.
    lines += ["[rate_limiting.login.failed]", "per_second = 0.0033", "burst_count = 3",
              "[rate_limiting.login.account]", "per_second = 0.003", "burst_count = 5"]
    return "\n".join(lines) + "\n"


def _levels(humans: list[str], human_level: int, users_default: int, room_level: int, message: int) -> dict:
    return {"users": dict.fromkeys(humans, human_level), "users_default": users_default,
            "events_default": 100, "state_default": 100, "invite": 100, "kick": 100, "ban": 100,
            "redact": 100, "notifications": {"room": room_level}, "events": {"m.room.message": message}}


def levels_main(humans: list[str]) -> dict:
    """A 4.2: the main room, and the questions room while open."""
    return _levels(humans, 50, 0, 50, 0)


def levels_reports(humans: list[str]) -> dict:
    return _levels(humans, 0, 10, 100, 10)


def levels_questions_closed(humans: list[str]) -> dict:
    return _levels(humans, 50, 0, 50, 100)


def registration_mac(shared: str, nonce: str, user: str, password: str, admin: bool) -> str:
    """Synapse's shared-secret registration MAC, which Tuwunel implements."""
    mac = hmac.new(shared.encode(), digestmod=hashlib.sha1)
    mac.update(b"\0".join([nonce.encode(), user.encode(), password.encode(), b"admin" if admin else b"notadmin"]))
    return mac.hexdigest()


def member_filter(rooms: list[str], presence_senders: list[str]) -> dict:
    """A 6.10's worker filter."""
    return {"room": {"rooms": rooms, "timeline": {"types": FILTER_TYPES, "limit": 100},
                     "state": {"types": FILTER_TYPES, "lazy_load_members": False},
                     "ephemeral": {"types": []}, "account_data": {"types": []}},
            "presence": {"senders": presence_senders}, "account_data": {"types": []}, "event_format": "client"}


def q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


class Server:
    """One Tuwunel process over one work directory; restartable on the same port and database."""

    def __init__(self, work: pathlib.Path, binary: pathlib.Path, extra: dict[str, object]) -> None:
        # Absolute: the process runs with the work directory as its cwd.
        self.work, self.binary, self.port = work.resolve(), binary, free_port()
        work = self.work
        (work / "keys").mkdir(parents=True, mode=0o700)
        (work / "backups").mkdir(mode=0o700)
        self.shared = secrets.token_hex(32)
        key = work / "keys" / "registration"
        key.write_text(self.shared, encoding="ascii")
        key.chmod(0o600)
        (work / "tuwunel.toml").write_text(config_toml(self.port, work, extra), encoding="ascii")
        self.base = f"http://127.0.0.1:{self.port}"
        self.proc: subprocess.Popen[bytes] | None = None

    def start(self) -> None:
        with (self.work / "tuwunel.log").open("ab") as log:
            self.proc = subprocess.Popen(
                [str(self.binary)], env={"PATH": "/usr/bin:/bin", "TUWUNEL_CONFIG": str(self.work / "tuwunel.toml")},
                stdout=log, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL, cwd=self.work)
        (self.work / "tuwunel.pid").write_text(str(self.proc.pid), encoding="ascii")
        deadline = time.monotonic() + READY_S
        while time.monotonic() < deadline:
            if self.proc.poll() is not None:
                raise ProbeError(f"Tuwunel exited {self.proc.returncode} before it answered: {self.log_tail()}")
            if self._answers():
                return
            time.sleep(0.25)
        raise ProbeError(f"Tuwunel did not answer within {READY_S} s: {self.log_tail()}")

    def _answers(self) -> bool:
        """Listening yet? Refused is the expected answer until it is; the wait is bounded."""
        try:
            with urllib.request.urlopen(f"{self.base}/_matrix/client/versions", timeout=5) as resp:
                return resp.status == 200
        except (urllib.error.URLError, ConnectionError):
            return False

    def stop(self) -> None:
        if self.proc is None or self.proc.poll() is not None:
            return
        self.proc.send_signal(signal.SIGTERM)
        try:
            self.proc.wait(timeout=STOP_S)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=STOP_S)
            raise ProbeError(f"Tuwunel did not stop within {STOP_S} s of SIGTERM; killed") from None

    def log_tail(self, lines: int = 8) -> str:
        path = self.work / "tuwunel.log"
        text = path.read_text(encoding="utf-8", errors="replace") if path.exists() else ""
        return " | ".join(text.strip().splitlines()[-lines:]) or "(empty log)"


class Team:
    """Accounts, rooms and requests on one server. `ok` raises ProbeError; `call` returns the status."""

    def __init__(self, server: Server) -> None:
        self.server = server
        self.tokens: dict[str, str] = {}
        self.passwords: dict[str, str] = {}
        self.admin = self.uid("admin")
        self.txn = 0
        self.rooms: dict[str, str] = {}

    @staticmethod
    def uid(localpart: str) -> str:
        return f"@{localpart}:{SERVER_NAME}"

    def call(self, method: str, path: str, body: object = None, token: str | None = None,
             query: dict | None = None, timeout: float = 30) -> tuple[int, dict]:
        url = self.server.base + path + (f"?{urllib.parse.urlencode(query)}" if query else "")
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        if token is not None:
            req.add_header("Authorization", f"Bearer {token}")
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                status, raw = resp.status, resp.read()
        except urllib.error.HTTPError as err:
            status, raw = err.code, err.read()
        except (urllib.error.URLError, ConnectionError, TimeoutError) as err:
            raise ProbeError(f"{method} {path}: {err}") from err
        try:
            obj = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            obj = {"raw": raw[:200].decode(errors="replace")}
        return status, obj if isinstance(obj, dict) else {"value": obj}

    def ok(self, what: str, method: str, path: str, body: object = None, user: str | None = None,
           query: dict | None = None, timeout: float = 30) -> dict:
        token = None if user is None else self.tokens[user]
        status, obj = self.call(method, path, body, token, query, timeout)
        if status != 200:
            raise ProbeError(f"{what}: HTTP {status} {obj.get('errcode', '')} {obj.get('error', '')}".rstrip())
        return obj

    def register_admin(self) -> None:
        nonce = self.ok("registration nonce", "GET", "/_synapse/admin/v1/register")["nonce"]
        password = secrets.token_hex(16)
        mac = registration_mac(self.server.shared, nonce, "admin", password, True)
        got = self.ok("register the admin", "POST", "/_synapse/admin/v1/register",
                      {"nonce": nonce, "username": "admin", "password": password, "admin": True, "mac": mac})
        self.tokens[self.admin], self.passwords[self.admin] = got["access_token"], password

    def account(self, localpart: str) -> str:
        """An account with a password, and a token minted by the admin (no password login spent)."""
        uid, password = self.uid(localpart), secrets.token_hex(16)
        self.ok(f"create {localpart}", "PUT", f"/_synapse/admin/v2/users/{q(uid)}",
                {"password": password, "displayname": localpart}, self.admin)
        self.tokens[uid] = self.ok(f"mint a token for {localpart}", "POST",
                                   f"/_synapse/admin/v1/users/{q(uid)}/login", {}, self.admin)["access_token"]
        self.passwords[uid] = password
        return uid

    def room(self, levels: dict, invite: list[str], state: tuple[dict, ...] = ()) -> str:
        initial = [{"type": "m.room.history_visibility", "state_key": "", "content": {"history_visibility": "shared"}},
                   {"type": "m.room.guest_access", "state_key": "", "content": {"guest_access": "forbidden"}},
                   *state]
        return self.ok("create a room", "POST", f"{CLIENT}/createRoom", {
            "preset": "private_chat", "room_version": ROOM_VERSION, "power_level_content_override": levels,
            "invite": invite, "initial_state": initial}, self.admin)["room_id"]

    def join(self, room: str, user: str) -> tuple[int, str]:
        status, obj = self.call("POST", f"{CLIENT}/join/{q(room)}", {}, self.tokens[user])
        return status, str(obj.get("errcode", "-"))

    def send(self, room: str, user: str, event_type: str, content: dict) -> tuple[int, str, str]:
        """(status, errcode, event ID or '')."""
        self.txn += 1
        status, obj = self.call("PUT", f"{CLIENT}/rooms/{q(room)}/send/{q(event_type)}/hca{self.txn}",
                                content, self.tokens[user])
        return status, str(obj.get("errcode", "-")), str(obj.get("event_id", ""))

    def put_state(self, room: str, user: str, event_type: str, key: str, content: dict) -> tuple[int, str]:
        status, obj = self.call("PUT", f"{CLIENT}/rooms/{q(room)}/state/{q(event_type)}/{q(key)}",
                                content, self.tokens[user])
        return status, str(obj.get("errcode", "-"))

    def sync(self, user: str, filt: dict, since: str | None, timeout_ms: int = 0,
             set_presence: str | None = None) -> dict:
        query = {"filter": json.dumps(filt), "timeout": str(timeout_ms)}
        if since is not None:
            query["since"] = since
        if set_presence is not None:
            query["set_presence"] = set_presence
        return self.ok(f"sync as {user}", "GET", f"{CLIENT}/sync", user=user, query=query,
                       timeout=timeout_ms / 1000 + 30)

    def build(self, humans: list[str], agents: list[str]) -> tuple[list[str], list[str]]:
        """A 4.1's three rooms (and a closed questions room), every member joined to main, the humans
        to the side rooms. Returns the human and agent user IDs."""
        self.register_admin()
        human_ids = [self.account(h) for h in humans]
        agent_ids = [self.account(a) for a in agents]
        main = self.room(levels_main(human_ids), human_ids + agent_ids)
        for user in human_ids + agent_ids:
            self.ok(f"{user} joins main", "POST", f"{CLIENT}/join/{q(main)}", {}, user)
        reports = self.room(levels_reports(human_ids), human_ids, ({
            "type": "m.room.join_rules", "state_key": "",
            "content": {"join_rule": "restricted", "allow": [{"type": "m.room_membership", "room_id": main}]}},))
        questions = self.room(levels_main(human_ids), human_ids)
        closed = self.room(levels_questions_closed(human_ids), human_ids)
        for room in (reports, questions, closed):
            for user in human_ids:
                self.ok(f"{user} joins a side room", "POST", f"{CLIENT}/join/{q(room)}", {}, user)
        self.rooms = {"main": main, "reports": reports, "questions": questions, "closed": closed}
        return human_ids, agent_ids
