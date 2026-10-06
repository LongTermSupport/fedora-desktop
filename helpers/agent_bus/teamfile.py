"""The team file: the JSON a human writes and `agent-bus-install team` applies.

DESIGN.md section 3.4 is the schema; PROTOCOL.md sections 3 and 11 give the grammars
shared with the team record. Everything here is offline and pure: whether a listen
address is really on `lo`, `agentbus0` or a WireGuard interface, and whether an
`allow_from` subnet is WireGuard-routed or a port-less bridge, is decided by the
installer against the live host (DESIGN.md section 3.3). This module only enforces the
shape rules that hold on every host.

The grammars below are PROTOCOL.md's. `helpers/pingbus/protocol.py` owns them for
pingbus; when both exist, these names should become imports from there.
"""

from __future__ import annotations

import ipaddress
import json
import pathlib
import re
import urllib.parse
from collections.abc import Mapping
from dataclasses import dataclass

TEAM_PATTERN = r"[a-z][a-z0-9-]{0,23}"
HUMAN_LOCALPART_PATTERN = r"[a-z][a-z0-9_-]{0,31}"
RESERVED_LOCALPARTS = ("admin", "conduit")
OWNER_PATTERN = r"[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?"
REPO_PATTERN = r"[a-z0-9._-]{1,100}"
BRANCH_PATTERN = r"[A-Za-z0-9._/-]{1,100}"
SEG_PATTERN = r"[A-Za-z0-9._-]{1,64}"
PATH_MAX_SEGMENTS = 8
DNS_LABEL_PATTERN = r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"

SERVER_NAME_SUFFIX = ".agent-bus.internal"
SERVER_NAME_TLD = "internal"
STATES = ("present", "absent")
DEFAULT_STATE = "present"
LOOPBACK_LISTEN = "127.0.0.1"
PORT_MIN = 1024
PORT_MAX = 65535
LISTEN_MAX = 16
ALLOW_FROM_MAX = 32
HUMANS_MAX = 16
REPOS_MAX = 32
BRANCHES_MAX = 8
PATH_PREFIXES_MAX = 32

REQUIRED_KEYS = frozenset(
    {"team", "port", "listen", "allow_from", "humans", "repos", "path_prefixes", "forge_api"}
)
OPTIONAL_KEYS = frozenset({"state", "server_name"})
REPO_ENTRY_KEYS = frozenset({"repo", "branches"})

_TEAM_RE = re.compile(TEAM_PATTERN)
_HUMAN_RE = re.compile(HUMAN_LOCALPART_PATTERN)
_OWNER_REPO_RE = re.compile(rf"({OWNER_PATTERN})/({REPO_PATTERN})")
_BRANCH_RE = re.compile(BRANCH_PATTERN)
_SEG_RE = re.compile(SEG_PATTERN)
_DNS_LABEL_RE = re.compile(DNS_LABEL_PATTERN)


class TeamFileError(ValueError):
    """The team file breaks a rule; the message names the key and the rule."""


@dataclass(frozen=True)
class RepoEntry:
    repo: str
    branches: tuple[str, ...]


@dataclass(frozen=True)
class TeamFile:
    team: str
    state: str
    server_name: str
    port: int
    listen: tuple[str, ...]
    allow_from: tuple[str, ...]
    humans: tuple[str, ...]
    repos: tuple[RepoEntry, ...]
    path_prefixes: tuple[str, ...]
    forge_api: str

    def listen_addresses(self) -> tuple[str, ...]:
        """Every address the homeserver binds: loopback always, then `listen`."""
        return (LOOPBACK_LISTEN, *self.listen)

    def as_dict(self) -> dict:
        return {
            "team": self.team,
            "state": self.state,
            "server_name": self.server_name,
            "port": self.port,
            "listen": list(self.listen),
            "allow_from": list(self.allow_from),
            "humans": list(self.humans),
            "repos": [{"repo": r.repo, "branches": list(r.branches)} for r in self.repos],
            "path_prefixes": list(self.path_prefixes),
            "forge_api": self.forge_api,
        }


def _fail(key: str, rule: str) -> TeamFileError:
    return TeamFileError(f"team file: {key}: {rule}")


def _full(regex: re.Pattern[str], value: object) -> bool:
    return isinstance(value, str) and regex.fullmatch(value) is not None


def _string_list(data: Mapping, key: str, low: int, high: int) -> list[str]:
    value = data[key]
    if not isinstance(value, list) or not all(isinstance(v, str) for v in value):
        raise _fail(key, "must be a list of strings")
    if not low <= len(value) <= high:
        raise _fail(key, f"must hold {low} to {high} entries, has {len(value)}")
    if len(set(value)) != len(value):
        raise _fail(key, "entries must be distinct")
    return value


def _team(value: object) -> str:
    if not _full(_TEAM_RE, value):
        raise _fail("team", f"must match {TEAM_PATTERN}")
    return value


def _state(value: object) -> str:
    if value not in STATES:
        raise _fail("state", f"must be one of {', '.join(STATES)}")
    return value


def _server_name(value: object) -> str:
    if not isinstance(value, str) or len(value) > 253:
        raise _fail("server_name", "must be a DNS name of at most 253 characters")
    labels = value.split(".")
    if len(labels) < 2 or not all(_DNS_LABEL_RE.fullmatch(label) for label in labels):
        raise _fail("server_name", "must be a lower-case DNS name with at least two labels")
    if labels[-1] != SERVER_NAME_TLD:
        raise _fail("server_name", f"must end in .{SERVER_NAME_TLD}, which is never delegated")
    return value


def _port(value: object) -> int:
    if type(value) is not int or not PORT_MIN <= value <= PORT_MAX:
        raise _fail("port", f"must be an integer from {PORT_MIN} to {PORT_MAX}")
    return value


def _listen(data: Mapping) -> tuple[str, ...]:
    out = []
    for value in _string_list(data, "listen", 0, LISTEN_MAX):
        if "%" in value:
            raise _fail("listen", f"{value!r}: a scoped address is not allowed")
        try:
            address = ipaddress.ip_address(value)
        except ValueError as exc:
            raise _fail("listen", f"{value!r} is not an IP address literal") from exc
        if str(address) != value:
            raise _fail("listen", f"{value!r} must be written canonically as {address}")
        if address.is_unspecified or address.is_multicast:
            raise _fail("listen", f"{value!r}: wildcard and multicast addresses are refused")
        if value == LOOPBACK_LISTEN:
            raise _fail("listen", f"{LOOPBACK_LISTEN} is always bound; do not list it")
        out.append(value)
    return tuple(out)


def _allow_from(data: Mapping) -> tuple[str, ...]:
    out = []
    for value in _string_list(data, "allow_from", 0, ALLOW_FROM_MAX):
        if "/" not in value:
            raise _fail("allow_from", f"{value!r} must be a CIDR with an explicit prefix length")
        try:
            network = ipaddress.ip_network(value, strict=True)
        except ValueError as exc:
            raise _fail("allow_from", f"{value!r} is not a CIDR without host bits") from exc
        if str(network) != value:
            raise _fail("allow_from", f"{value!r} must be written canonically as {network}")
        if network.prefixlen == 0 or network.is_multicast:
            raise _fail("allow_from", f"{value!r}: a default route or multicast range is refused")
        out.append(value)
    return tuple(out)


def _humans(data: Mapping) -> tuple[str, ...]:
    values = _string_list(data, "humans", 1, HUMANS_MAX)
    for value in values:
        if not _HUMAN_RE.fullmatch(value) or value in RESERVED_LOCALPARTS:
            raise _fail("humans", f"{value!r} must match {HUMAN_LOCALPART_PATTERN} and not be reserved")
    return tuple(values)


def _owner_repo(value: object) -> str:
    match = _OWNER_REPO_RE.fullmatch(value) if isinstance(value, str) else None
    if match is None:
        raise _fail("repos", f"{value!r} must be lower-case OWNER/REPO")
    name = match.group(2)
    if name in (".", "..") or name.endswith(".git"):
        raise _fail("repos", f"{value!r}: REPO may not be '.', '..' or end in .git")
    return value


def _repos(data: Mapping) -> tuple[RepoEntry, ...]:
    value = data["repos"]
    if not isinstance(value, list) or not 1 <= len(value) <= REPOS_MAX:
        raise _fail("repos", f"must be a list of 1 to {REPOS_MAX} objects")
    out: list[RepoEntry] = []
    for entry in value:
        if not isinstance(entry, dict) or set(entry) != REPO_ENTRY_KEYS:
            raise _fail("repos", 'each entry must be exactly {"repo": ..., "branches": [...]}')
        repo = _owner_repo(entry["repo"])
        branches = entry["branches"]
        if (
            not isinstance(branches, list)
            or not 1 <= len(branches) <= BRANCHES_MAX
            or not all(_full(_BRANCH_RE, b) for b in branches)
            or len(set(branches)) != len(branches)
        ):
            raise _fail("repos", f"{repo}: branches must be 1 to {BRANCHES_MAX} distinct names matching {BRANCH_PATTERN}")
        out.append(RepoEntry(repo, tuple(branches)))
    if len({r.repo for r in out}) != len(out):
        raise _fail("repos", "each repository may be listed once")
    return tuple(out)


def _is_path_prefix(value: str) -> bool:
    """A PATH ending in `/`, or an exact file PATH (PROTOCOL.md section 11)."""
    body = value[:-1] if value.endswith("/") else value
    segments = body.split("/")
    return (
        1 <= len(segments) <= PATH_MAX_SEGMENTS
        and all(_SEG_RE.fullmatch(s) and s not in (".", "..") for s in segments)
    )


def _path_prefixes(data: Mapping) -> tuple[str, ...]:
    values = _string_list(data, "path_prefixes", 1, PATH_PREFIXES_MAX)
    for value in values:
        if not _is_path_prefix(value):
            raise _fail("path_prefixes", f"{value!r} must be a relative PATH of 1 to {PATH_MAX_SEGMENTS} segments")
    return tuple(values)


def _forge_api(value: object) -> str:
    if not isinstance(value, str) or any(c.isspace() for c in value):
        raise _fail("forge_api", "must be an https:// URL")
    parts = urllib.parse.urlsplit(value)
    if (
        parts.scheme != "https"
        or not parts.hostname
        or parts.username is not None
        or parts.password is not None
        or parts.query
        or parts.fragment
        or parts.path.endswith("/")
    ):
        raise _fail("forge_api", "must be an https:// URL with no credentials, query, fragment or trailing /")
    return value


def parse_team_file(data: object) -> TeamFile:
    """Validate a decoded team file; raise TeamFileError at the first broken rule."""
    if not isinstance(data, dict):
        raise _fail("team file", "must be a JSON object")
    unknown = sorted(set(data) - REQUIRED_KEYS - OPTIONAL_KEYS)
    if unknown:
        raise _fail(unknown[0], "unknown key")
    missing = sorted(REQUIRED_KEYS - set(data))
    if missing:
        raise _fail(missing[0], "required key missing")
    team = _team(data["team"])
    return TeamFile(
        team=team,
        state=_state(data.get("state", DEFAULT_STATE)),
        server_name=_server_name(data.get("server_name", team + SERVER_NAME_SUFFIX)),
        port=_port(data["port"]),
        listen=_listen(data),
        allow_from=_allow_from(data),
        humans=_humans(data),
        repos=_repos(data),
        path_prefixes=_path_prefixes(data),
        forge_api=_forge_api(data["forge_api"]),
    )


def _refuse_duplicates(pairs: list[tuple[str, object]]) -> dict:
    out: dict = {}
    for key, value in pairs:
        if key in out:
            raise ValueError(f"duplicate key {key!r}")
        out[key] = value
    return out


def _refuse_constant(name: str) -> object:
    raise ValueError(f"{name} is not JSON")


def decode_strict_json(text: str) -> object:
    """json.loads that refuses duplicate keys and NaN/Infinity (raises ValueError)."""
    return json.loads(text, object_pairs_hook=_refuse_duplicates, parse_constant=_refuse_constant)


def load_team_file(path: pathlib.Path) -> TeamFile:
    """Read and validate a team file. A missing file raises FileNotFoundError."""
    text = pathlib.Path(path).read_text(encoding="utf-8")
    try:
        data = decode_strict_json(text)
    except ValueError as exc:
        raise TeamFileError(f"team file: not valid JSON: {exc}") from exc
    return parse_team_file(data)


def dump_team_file(team_file: TeamFile) -> str:
    """Canonical JSON for `team.json`, defaults filled in; parses back to the same value."""
    return json.dumps(team_file.as_dict(), indent=2, sort_keys=True) + "\n"
