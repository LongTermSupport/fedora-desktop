"""Member configuration for pingbus: the environment, the bundles, the refusals.

Spec: docs/agent-bus-protocol.md §12 (bundle layout, `member.json`, `PINGBUS_HOME`,
`PINGBUS_TEAMS`, `--team`) and Plan 00161's DESIGN.md sections 5.2 (the Python gate) and 8
(no `host` member beside an Element profile). The `limits` object is parsed by
`limits.parse_limits`, which owns its keys and bounds (§10); this module only turns its
refusal into a `ConfigError`.

Every refusal is a `ConfigError` (exit 78) or, for a bad `--team`, a `UsageError`
(exit 64); the CLI maps them. Messages name the team, the file and the key at fault and
never quote a value read from a bundle, so a token cannot reach a message even when it
has been pasted into the wrong key. The token is read only by `read_token`, which
re-checks the file each time, and is never kept on a `Member`.
"""

from __future__ import annotations

import dataclasses
import ipaddress
import json
import os
import pathlib
import pwd
import re
import stat
import sys
import urllib.parse
from collections.abc import Callable, Mapping, Sequence

from helpers.pingbus import limits, protocol

MIN_PYTHON = (3, 11)
ADMIN_LOCALPART = "admin"
MEMBER_FILE = "member.json"
TOKEN_FILE = "token"
STATE_DIR = "state"
MEMBER_FILE_MAX_BYTES = 65536
TOKEN_MAX_BYTES = 4096
TOKEN_MODE_ALLOWED = 0o600

REQUIRED_KEYS = (
    "protocol",
    "team",
    "user_id",
    "server_name",
    "base_url",
    "plain_http_hosts",
    "token_file",
    "admin",
    "room",
)
OPTIONAL_KEYS = ("human_text", "limits")

#: Commands that run with no active team (agent-bus-protocol.md §12).
COMMANDS_WITHOUT_CONFIG = frozenset({"version", "validate", "suggest-handle"})

#: Where Element keeps a human's Matrix session, relative to the user's home: the
#: Flatpak's whole tree, and the native client's `Element` / `Element-<profile>`.
ELEMENT_FLATPAK_DIR = ".var/app/im.riot.Riot"
ELEMENT_NATIVE_PARENT = ".config"
ELEMENT_NATIVE_NAME = "Element"

_DNS_LABEL = r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
_DNS_NAME_RE = re.compile(rf"{_DNS_LABEL}(?:\.{_DNS_LABEL})*")
_DNS_NAME_MAX = 253
_PRINTABLE_RE = re.compile(r"[\x21-\x7e]+")
_KEY_NAME_RE = re.compile(r"[a-z_]{1,32}")

Refuse = Callable[[str, str], "ConfigError"]


class ConfigError(Exception):
    """Configuration refused: agent-bus-protocol.md §14, exit 78."""

    EXIT_CODE = 78


class UsageError(Exception):
    """A bad `--team` for this command: agent-bus-protocol.md §14, exit 64."""

    EXIT_CODE = 64


@dataclasses.dataclass(frozen=True)
class Member:
    """One validated bundle. Holds no token: call `read_token` when one is needed."""

    team: str
    user_id: str
    handle: str
    member_type: str
    server_name: str
    base_url: str
    plain_http_hosts: tuple[str, ...]
    admin: str
    room: str
    human_text: bool
    limits: limits.Limits
    bundle_dir: pathlib.Path
    token_path: pathlib.Path

    @property
    def state_dir(self) -> pathlib.Path:
        return self.bundle_dir / STATE_DIR


def check_python(version: Sequence[int]) -> None:
    """Refuse an interpreter older than MIN_PYTHON (pass `sys.version_info`)."""
    if tuple(version[:2]) < MIN_PYTHON:
        found = ".".join(str(part) for part in version[:3])
        need = ".".join(str(part) for part in MIN_PYTHON)
        raise ConfigError(f"pingbus needs Python {need} or later; this is {found}")


def resolve_home(environ: Mapping[str, str]) -> pathlib.Path:
    """`PINGBUS_HOME`, else `${XDG_CONFIG_HOME:-$HOME/.config}/pingbus`.

    A relative `XDG_CONFIG_HOME` is ignored, as the XDG base directory spec says; a
    relative `PINGBUS_HOME` or `HOME` is refused.
    """
    explicit = environ.get("PINGBUS_HOME", "")
    if explicit:
        if not os.path.isabs(explicit):
            raise ConfigError("PINGBUS_HOME must be an absolute path")
        return pathlib.Path(explicit)
    xdg = environ.get("XDG_CONFIG_HOME", "")
    if xdg and os.path.isabs(xdg):
        return pathlib.Path(xdg) / "pingbus"
    home = environ.get("HOME", "")
    if not home or not os.path.isabs(home):
        raise ConfigError("PINGBUS_HOME is unset and HOME is not an absolute path")
    return pathlib.Path(home) / ".config" / "pingbus"


def active_teams(environ: Mapping[str, str]) -> tuple[str, ...]:
    """The comma-separated team names in `PINGBUS_TEAMS`, in order."""
    raw = environ.get("PINGBUS_TEAMS", "")
    if not raw:
        raise ConfigError("PINGBUS_TEAMS is not set: no team is active")
    teams = tuple(raw.split(","))
    for position, name in enumerate(teams, start=1):
        if not protocol.is_team_name(name):
            raise ConfigError(f"PINGBUS_TEAMS entry {position} is not a team name")
    if len(set(teams)) != len(teams):
        raise ConfigError("PINGBUS_TEAMS lists a team more than once")
    return teams


def select_teams(active: Sequence[str], team: str | None, single: bool = False) -> tuple[str, ...]:
    """The teams a command covers: `--team` alone, else every active team.

    `single` is for a command that acts on exactly one team (`send`), which needs
    `--team` when more than one team is active.
    """
    if team is not None:
        if not protocol.is_team_name(team):
            raise UsageError("--team is not a team name")
        if team not in active:
            raise UsageError(f"--team {team} is not in PINGBUS_TEAMS")
        return (team,)
    if single and len(active) > 1:
        raise UsageError("several teams are active: name one with --team")
    return tuple(active)


def load_bundle(
    home: pathlib.Path,
    team: str,
    *,
    uid: int | None = None,
    user_home: pathlib.Path | None = None,
) -> Member:
    """Validate `home/<team>/` and return its member.

    `uid` must own the token (default: the running user); `user_home` is where a `host`
    member's Element profile is looked for (default: that user's home from passwd).
    """
    if not protocol.is_team_name(team):
        raise ConfigError("not a team name")
    owner = os.getuid() if uid is None else uid
    bundle_dir = home / team
    data = _read_member_file(team, bundle_dir / MEMBER_FILE)
    member = _parse_member(team, bundle_dir, data)
    read_token(member, uid=owner)
    if member.member_type == "host":
        _refuse_beside_element(team, _home_of(owner) if user_home is None else user_home)
    return member


def read_token(member: Member, *, uid: int | None = None) -> str:
    """The member's token, after re-checking the file: a regular file (never followed
    through a symlink), owned by `uid`, mode 0600 or stricter, one printable line with no
    trailing newline."""
    owner = os.getuid() if uid is None else uid
    where = f"team {member.team}: {member.token_path}"
    try:
        fd = os.open(member.token_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    except FileNotFoundError:
        raise ConfigError(f"{where}: the token file is missing") from None
    except OSError as exc:
        raise ConfigError(f"{where}: the token is not a regular file ({exc.strerror})") from None
    with os.fdopen(fd, "rb") as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode):
            raise ConfigError(f"{where}: the token is not a regular file")
        if info.st_uid != owner:
            raise ConfigError(f"{where}: the token is owned by uid {info.st_uid}, not {owner}")
        mode = stat.S_IMODE(info.st_mode)
        if mode & ~TOKEN_MODE_ALLOWED:
            raise ConfigError(f"{where}: the token's mode {mode:04o} is looser than 0600")
        raw = handle.read(TOKEN_MAX_BYTES + 1)
    if len(raw) > TOKEN_MAX_BYTES:
        raise ConfigError(f"{where}: the token is longer than {TOKEN_MAX_BYTES} bytes")
    text = raw.decode("ascii", errors="replace")
    if _PRINTABLE_RE.fullmatch(text) is None:
        raise ConfigError(
            f"{where}: the token must be one line of printable ASCII, no spaces, no newline"
        )
    return text


def load_active(
    environ: Mapping[str, str],
    *,
    team: str | None = None,
    single: bool = False,
    uid: int | None = None,
    user_home: pathlib.Path | None = None,
) -> tuple[Member, ...]:
    """Every bundle the command covers, validated, in `PINGBUS_TEAMS` order.

    Also refuses two bundles that share an account or a token: accounts on different
    teams never share a token (DESIGN.md section 2).
    """
    check_python(sys.version_info)
    home = resolve_home(environ)
    selected = select_teams(active_teams(environ), team, single)
    members = tuple(load_bundle(home, name, uid=uid, user_home=user_home) for name in selected)
    seen_users: dict[str, str] = {}
    seen_tokens: dict[str, str] = {}
    for member in members:
        first = seen_users.setdefault(member.user_id, member.team)
        if first != member.team:
            raise ConfigError(f"teams {first} and {member.team}: the bundles share one user_id")
        first = seen_tokens.setdefault(read_token(member, uid=uid), member.team)
        if first != member.team:
            raise ConfigError(f"teams {first} and {member.team}: the bundles share one token")
    return members


def _home_of(uid: int) -> pathlib.Path:
    return pathlib.Path(pwd.getpwuid(uid).pw_dir)


def _refuse_beside_element(team: str, user_home: pathlib.Path) -> None:
    """DESIGN.md section 8: a `host` member never runs as a user holding a human's
    Matrix session. Dangling symlinks count: something put them there."""
    found: list[pathlib.Path] = []
    flatpak = user_home / ELEMENT_FLATPAK_DIR
    if os.path.lexists(flatpak):
        found.append(flatpak)
    native_parent = user_home / ELEMENT_NATIVE_PARENT
    if native_parent.is_dir():
        try:
            entries = sorted(native_parent.iterdir())
        except OSError as exc:
            raise ConfigError(
                f"team {team}: {native_parent} cannot be listed to look for an Element "
                f"profile ({exc.strerror})"
            ) from None
        found.extend(
            entry
            for entry in entries
            if entry.name == ELEMENT_NATIVE_NAME
            or entry.name.startswith(f"{ELEMENT_NATIVE_NAME}-")
        )
    if found:
        raise ConfigError(
            f"team {team}: a host member must not run as a user with an Element profile "
            f"({found[0]}); run it as a dedicated agent user"
        )


def _read_member_file(team: str, path: pathlib.Path) -> dict[str, object]:
    """Non-blocking open so a FIFO cannot hang the read; at most one byte past the cap."""
    where = f"team {team}: {path}"
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC)
    except FileNotFoundError:
        raise ConfigError(f"{where}: no bundle (member.json is missing)") from None
    except OSError as exc:
        raise ConfigError(f"{where}: member.json cannot be read ({exc.strerror})") from None
    try:
        with os.fdopen(fd, "rb") as handle:
            if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
                raise ConfigError(f"{where}: member.json is not a regular file")
            raw = handle.read(MEMBER_FILE_MAX_BYTES + 1)
    except OSError as exc:
        raise ConfigError(f"{where}: member.json cannot be read ({exc.strerror})") from None
    if len(raw) > MEMBER_FILE_MAX_BYTES:
        raise ConfigError(f"{where}: member.json is larger than {MEMBER_FILE_MAX_BYTES} bytes")
    try:
        data = json.loads(raw.decode("utf-8"), object_pairs_hook=_no_duplicate_keys)
    except (UnicodeDecodeError, ValueError) as exc:
        raise ConfigError(
            f"{where}: member.json is not valid JSON ({type(exc).__name__})"
        ) from None
    if not isinstance(data, dict):
        raise ConfigError(f"{where}: member.json is not a JSON object")
    return data


def _no_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result


def _parse_member(team: str, bundle_dir: pathlib.Path, data: dict[str, object]) -> Member:
    def refuse(key: str, why: str) -> ConfigError:
        return ConfigError(f"team {team}: {bundle_dir / MEMBER_FILE}: {key} {why}")

    unknown = sorted(set(data) - set(REQUIRED_KEYS) - set(OPTIONAL_KEYS))
    if unknown:
        names = ", ".join(_key_label(key) for key in unknown[:5])
        raise refuse("member.json", f"has unknown keys: {names}")
    for key in REQUIRED_KEYS:
        if key not in data:
            raise refuse(key, "is missing")

    version = data["protocol"]
    if type(version) is not int or version != protocol.PROTOCOL_VERSION:
        raise refuse("protocol", f"must be the integer {protocol.PROTOCOL_VERSION}")
    if data["team"] != team:
        raise refuse("team", f"does not equal the bundle directory's name ({team})")

    server_name = data["server_name"]
    if not isinstance(server_name, str) or not _is_dns_name(server_name):
        raise refuse("server_name", "is not a lowercase DNS name")
    user_id = data["user_id"]
    handle = protocol.parse_user_id(user_id, server_name)
    parsed = protocol.parse_handle(handle)
    if not isinstance(user_id, str) or handle is None or parsed is None:
        raise refuse("user_id", "is not @<handle>:<server_name> for this server_name")
    admin = f"@{ADMIN_LOCALPART}:{server_name}"
    if data["admin"] != admin:
        raise refuse("admin", f"is not @{ADMIN_LOCALPART}:<server_name>")
    room = data["room"]
    if not isinstance(room, str) or not protocol.is_room_id(room):
        raise refuse("room", "is not a room ID")
    if data["token_file"] != TOKEN_FILE:
        raise refuse("token_file", f'must be "{TOKEN_FILE}", the file inside the bundle')

    plain_hosts = _plain_http_hosts(data["plain_http_hosts"], refuse)
    base_url = _base_url(data["base_url"], plain_hosts, refuse)
    human_text = data.get("human_text", True)
    if not isinstance(human_text, bool):
        raise refuse("human_text", "must be true or false")
    member_limits = _limits(data, f"team {team}: {bundle_dir / MEMBER_FILE}")

    return Member(
        team=team,
        user_id=user_id,
        handle=handle,
        member_type=parsed.type,
        server_name=server_name,
        base_url=base_url,
        plain_http_hosts=plain_hosts,
        admin=admin,
        room=room,
        human_text=human_text,
        limits=member_limits,
        bundle_dir=bundle_dir,
        token_path=bundle_dir / TOKEN_FILE,
    )


def _is_dns_name(value: str) -> bool:
    return len(value) <= _DNS_NAME_MAX and _DNS_NAME_RE.fullmatch(value) is not None


def _key_label(key: str) -> str:
    """A key is echoed only when it looks like a key name, never like a pasted value."""
    return repr(key) if _KEY_NAME_RE.fullmatch(key) else "a key that is not a name"


def _plain_http_hosts(value: object, refuse: Refuse) -> tuple[str, ...]:
    if not isinstance(value, list):
        raise refuse("plain_http_hosts", "must be an array of IP literals")
    hosts: list[str] = []
    for entry in value:
        if not isinstance(entry, str) or "%" in entry:
            raise refuse("plain_http_hosts", "must hold only IP literals")
        try:
            canonical = str(ipaddress.ip_address(entry))
        except ValueError:
            raise refuse("plain_http_hosts", "must hold only IP literals") from None
        if canonical != entry:
            raise refuse("plain_http_hosts", "must hold IP literals in canonical form")
        if entry in hosts:
            raise refuse("plain_http_hosts", "lists an address twice")
        hosts.append(entry)
    return tuple(hosts)


def _base_url(value: object, plain_hosts: tuple[str, ...], refuse: Refuse) -> str:
    """`https://<host>[:port]`, or `http://<ip>[:port]` with `<ip>` in `plain_hosts`:
    no credentials, path, query or fragment."""
    if not isinstance(value, str) or _PRINTABLE_RE.fullmatch(value) is None:
        raise refuse("base_url", "must be a URL of printable ASCII")
    if "?" in value or "#" in value:
        raise refuse("base_url", "must have no query or fragment")
    parts = urllib.parse.urlsplit(value)
    if parts.scheme not in ("http", "https"):
        raise refuse("base_url", "must be http:// or https://")
    if "@" in parts.netloc:
        raise refuse("base_url", "must carry no credentials")
    if parts.path:
        raise refuse("base_url", "must have no path")
    try:
        parts.port
    except ValueError:
        raise refuse("base_url", "has an invalid port") from None
    host = parts.hostname
    if not host:
        raise refuse("base_url", "has no host")
    ip = _ip_literal(host)
    if parts.scheme == "http":
        if ip is None or ip not in plain_hosts:
            raise refuse(
                "base_url", "may use http:// only to an IP literal listed in plain_http_hosts"
            )
    elif ip is None and not _is_dns_name(host):
        raise refuse("base_url", "host is neither an IP literal nor a DNS name")
    return value


def _ip_literal(host: str) -> str | None:
    if "%" in host:
        return None
    try:
        return str(ipaddress.ip_address(host))
    except ValueError:
        return None


def _limits(data: Mapping[str, object], where: str) -> limits.Limits:
    """`limits` absent gives the defaults; present, it must be an object `limits.py`
    accepts (an explicit `null` is refused, not read as absent)."""
    if "limits" not in data:
        return limits.Limits()
    value = data["limits"]
    if value is None:
        raise ConfigError(f"{where}: limits must be an object")
    try:
        return limits.parse_limits(value)
    except limits.LimitsError as exc:
        raise ConfigError(f"{where}: {exc}") from None
