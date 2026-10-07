"""Member handles and the per-team registry, `/var/lib/agent-bus/<team>/registry.json`.

A handle is `<repo>.<seat>+<host>.<type>` (protocol spec, docs/agent-bus-protocol.md §3;
its grammar is imported from `helpers/pingbus/protocol.py`). The registry keeps each
member's handle with its role, the handles that are parked, and one counter per prefix
`<repo>+<host>.<type>`. A member added without a seat is numbered one more than its
prefix's counter; one added with a seat (`add-member --seat`, Plan 00161's DESIGN.md
section 5.5) takes that seat, and a numbered seat above the counter moves the counter up
to it. The counter never goes down, so the counter never issues a number twice.

A parked member (`park-member`) keeps its role and its entry in `members`; returning it
(`add-member --seat` on its handle) only unparks it. The file is version 2; a version 1
file, which had no `parked`, loads as version 2 with nothing parked.

`<host>` is the install's role or an explicit `--host`; never a hostname, because handles
reach public forge text. Callers pass the role in: this module reads no environment.

Pure logic plus `load_registry`/`save_registry`/`update_registry`, the only file I/O.
Every change goes through `update_registry`, which holds the team's lock across the
load and the save.
"""

from __future__ import annotations

import fcntl
import json
import os
import pathlib
import re
import tempfile
from collections.abc import Callable, Iterable, Mapping
from dataclasses import dataclass, field, replace
from types import MappingProxyType
from typing import TypeVar

from helpers.agent_bus.teamfile import decode_strict_json
from helpers.pingbus import protocol

REGISTRY_VERSION = 2
#: Version 1 had no `parked`; it loads as version 2 with nothing parked.
REGISTRY_VERSIONS = {1: frozenset({"v", "team", "counters", "members"}),
                     2: frozenset({"v", "team", "counters", "members", "parked"})}
#: The handle grammar is the protocol's (one home, so probe H4 can change the separator).
HANDLE_SEP = protocol.HANDLE_SEP
TYPES = protocol.HANDLE_TYPES
ROLES = protocol.ROLES
REPO_MAX = 48
N_MAX = 999_999
PREFIX_PATTERN = (
    rf"(?P<repo>{protocol.HANDLE_REPO_PATTERN}){re.escape(HANDLE_SEP)}"
    rf"(?P<host>{protocol.HANDLE_HOST_PATTERN})\.(?P<type>{'|'.join(TYPES)})"
)

_PREFIX_RE = re.compile(PREFIX_PATTERN)
_HOST_PART_RE = re.compile(protocol.HANDLE_HOST_PATTERN)
_REPO_UNSAFE_RE = re.compile(r"[^a-z0-9_-]")
_HOST_UNSAFE_RE = re.compile(r"[^a-z0-9-]")

T = TypeVar("T")


class HandleError(ValueError):
    """A handle, or one of its parts, cannot be built from the input."""


class RegistryError(ValueError):
    """The registry file or a registry change breaks a rule."""


def normalise_repo(raw: str) -> str:
    """Lowercase; outside `[a-z0-9_-]` becomes `-`; leading `-`/`_` stripped; at most 48."""
    repo = _REPO_UNSAFE_RE.sub("-", raw.lower()).lstrip("-_")[:REPO_MAX]
    if not repo:
        raise HandleError(f"repository name {raw!r} is empty once normalised")
    return repo


def repo_from_remote(remote: str | None, checkout_dir: str) -> str:
    """`<repo>` from the forge remote URL's last component (`.git` removed), else the directory."""
    source = remote if remote else checkout_dir
    last = re.split(r"[/:]", source.rstrip("/"))[-1]
    if remote and last.endswith(".git"):
        last = last[: -len(".git")]
    return normalise_repo(last)


def resolve_host(explicit: str | None, role: str | None) -> str:
    """`<host>`: an explicit `--host` first, else the install's role; refuse with neither."""
    raw = explicit or role
    if not raw:
        raise HandleError("no role is set for this install: pass --host")
    host = _HOST_UNSAFE_RE.sub("-", raw.lower())
    if not _HOST_PART_RE.fullmatch(host):
        raise HandleError(f"host {raw!r} does not fit {protocol.HANDLE_HOST_PATTERN} once normalised")
    return host


def _prefix(repo: str, host: str, type_: str) -> str:
    prefix = f"{repo}{HANDLE_SEP}{host}.{type_}"
    if not _PREFIX_RE.fullmatch(prefix):
        raise HandleError(f"prefix {prefix!r}: repo, host or type does not fit the handle grammar")
    return prefix


def build_handle(repo: str, seat: int | str, host: str, type_: str) -> str:
    """A counter number (an int from 1 to `N_MAX`) or a seat (a string in the seat grammar)."""
    if type(seat) is int:
        if not 1 <= seat <= N_MAX:
            raise HandleError(f"handle number must be an integer from 1 to {N_MAX}")
    elif not protocol.is_seat_name(seat):
        raise HandleError(f"seat {seat!r} must match {protocol.HANDLE_SEAT_PATTERN}")
    handle = protocol.format_handle(repo, seat, host, type_)
    if protocol.parse_handle(handle) is None:
        raise HandleError(f"handle {handle!r} does not fit the handle grammar")
    return handle


def prefix_of(handle: str) -> str:
    """The counter key `<repo>+<host>.<type>` of a handle."""
    parsed = protocol.parse_handle(handle)
    if parsed is None:
        raise HandleError(f"{handle!r} is not an agent handle")
    return f"{parsed.repo}{HANDLE_SEP}{parsed.host}.{parsed.type}"


def _check_role(role: object) -> str:
    if role not in ROLES:
        raise RegistryError(f"role {role!r} must be one of {', '.join(ROLES)}")
    return role


@dataclass(frozen=True)
class Registry:
    team: str
    counters: Mapping[str, int] = field(default_factory=dict)
    members: Mapping[str, str] = field(default_factory=dict)
    parked: frozenset[str] = frozenset()

    def __post_init__(self) -> None:
        object.__setattr__(self, "counters", MappingProxyType(dict(self.counters)))
        object.__setattr__(self, "members", MappingProxyType(dict(self.members)))
        object.__setattr__(self, "parked", frozenset(self.parked))

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, Registry):
            return NotImplemented
        return (self.team, dict(self.counters), dict(self.members), self.parked) == (
            other.team,
            dict(other.counters),
            dict(other.members),
            other.parked,
        )

    __hash__ = None

    @classmethod
    def empty(cls, team: str) -> Registry:
        if not protocol.is_team_name(team):
            raise RegistryError(f"team {team!r} must match {protocol.TEAM_NAME_PATTERN}")
        return cls(team)

    def is_parked(self, handle: str) -> bool:
        return handle in self.parked

    def add_member(self, repo: str, host: str, type_: str, role: str,
                   seat: str | None = None) -> tuple[Registry, str]:
        """A new member: in `seat` when given, else the prefix's next number. Returns the
        new registry and the handle. A handle already in the registry is refused, parked
        or not: a parked one returns through `unpark`."""
        _check_role(role)
        prefix = _prefix(repo, host, type_)
        counter = self.counters.get(prefix, 0)
        if seat is None:
            n = counter + 1
            if n > N_MAX:
                raise RegistryError(f"prefix {prefix!r} has used every handle number")
            handle = build_handle(repo, n, host, type_)
        else:
            handle = build_handle(repo, seat, host, type_)
            n = max(counter, protocol.parse_handle(handle).n or 0)
        if handle in self.parked:
            raise RegistryError(f"{handle} is parked in team {self.team}: it returns, it is not added")
        if handle in self.members:
            raise RegistryError(f"{handle} is a current member of team {self.team}")
        counters = {**self.counters, prefix: n} if n else dict(self.counters)
        members = {**self.members, handle: role}
        return replace(self, counters=counters, members=members), handle

    def park(self, handle: str) -> Registry:
        """Mark a member parked; its role stays. Parking a parked member changes nothing."""
        self._member(handle)
        return replace(self, parked=self.parked | {handle})

    def unpark(self, handle: str) -> Registry:
        """Return a parked member, with the role it had."""
        if handle not in self.parked:
            raise RegistryError(f"{handle} is not a parked member of team {self.team}")
        return replace(self, parked=self.parked - {handle})

    def remove_member(self, handle: str) -> Registry:
        """Drop a member, parked or not; its prefix's counter stays."""
        self._member(handle)
        members = {h: r for h, r in self.members.items() if h != handle}
        return replace(self, members=members, parked=self.parked - {handle})

    def set_role(self, handle: str, role: str) -> Registry:
        _check_role(role)
        self._member(handle)
        return replace(self, members={**self.members, handle: role})

    def _member(self, handle: str) -> None:
        if handle not in self.members:
            raise RegistryError(f"{handle!r} is not a member of team {self.team}")

    def as_dict(self) -> dict:
        return {
            "v": REGISTRY_VERSION,
            "team": self.team,
            "counters": dict(self.counters),
            "members": dict(self.members),
            "parked": sorted(self.parked),
        }


def _parse_parked(parked: object, members: Iterable[str]) -> frozenset[str]:
    if not isinstance(parked, list):
        raise RegistryError("registry parked must be a list of handles")
    for handle in parked:
        if not isinstance(handle, str) or handle not in members:
            raise RegistryError(f"registry parked {handle!r} is not a member")
    if len(set(parked)) != len(parked):
        raise RegistryError("registry parked lists a handle twice")
    return frozenset(parked)


def parse_registry(data: object) -> Registry:
    if not isinstance(data, dict) or type(data.get("v")) is not int or data["v"] not in REGISTRY_VERSIONS:
        raise RegistryError(f"registry must be an object with v {' or '.join(map(str, REGISTRY_VERSIONS))}")
    keys = REGISTRY_VERSIONS[data["v"]]
    if set(data) != keys:
        raise RegistryError(f"registry version {data['v']} must have exactly the keys {sorted(keys)}")
    team = data["team"]
    if not protocol.is_team_name(team):
        raise RegistryError(f"registry team must match {protocol.TEAM_NAME_PATTERN}")
    counters = data["counters"]
    if not isinstance(counters, dict):
        raise RegistryError("registry counters must be an object")
    for prefix, n in counters.items():
        if not _PREFIX_RE.fullmatch(prefix):
            raise RegistryError(f"registry counter key {prefix!r} is not <repo>{HANDLE_SEP}<host>.<type>")
        if type(n) is not int or not 1 <= n <= N_MAX:
            raise RegistryError(f"registry counter {prefix!r} must be an integer from 1 to {N_MAX}")
    members = data["members"]
    if not isinstance(members, dict):
        raise RegistryError("registry members must be an object")
    for handle, role in members.items():
        parsed = protocol.parse_handle(handle)
        if parsed is None:
            raise RegistryError(f"registry member {handle!r} is not an agent handle")
        _check_role(role)
        if parsed.n is not None and parsed.n > counters.get(prefix_of(handle), 0):
            raise RegistryError(f"registry member {handle!r} is beyond its prefix's counter")
    parked = _parse_parked(data.get("parked", []), members)
    return Registry(team, counters, members, parked)


def dump_registry(registry: Registry) -> str:
    return json.dumps(registry.as_dict(), indent=2, sort_keys=True) + "\n"


def load_registry(path: pathlib.Path, team: str) -> Registry:
    """Read the team's registry; a missing file is an empty registry for `team`."""
    path = pathlib.Path(path)
    try:
        text = path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return Registry.empty(team)
    try:
        data = decode_strict_json(text)
    except ValueError as exc:
        raise RegistryError(f"{path.name}: not valid JSON: {exc}") from exc
    registry = parse_registry(data)
    if registry.team != team:
        raise RegistryError(f"{path.name} belongs to team {registry.team!r}, not {team!r}")
    return registry


def save_registry(path: pathlib.Path, registry: Registry) -> None:
    """Write atomically (temporary file, fsync, rename, directory fsync), mode 0600."""
    path = pathlib.Path(path)
    fd, tmp_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(dump_registry(registry))
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(tmp_name, 0o600)
        os.replace(tmp_name, path)
    except BaseException:
        pathlib.Path(tmp_name).unlink(missing_ok=True)
        raise
    dir_fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(dir_fd)
    finally:
        os.close(dir_fd)


def update_registry(path: pathlib.Path, team: str, change: Callable[[Registry], tuple[Registry, T]]) -> T:
    """Load, apply `change`, save if it changed anything; all under an exclusive lock.

    The lock is `flock` on the sibling `registry.lock`, so two concurrent `add-member`
    runs cannot both read the same counter and hand out the same number, nor both return
    one parked seat. If `change` raises, nothing is saved. Returns the second element of
    `change`'s result.
    """
    path = pathlib.Path(path)
    lock_fd = os.open(path.with_suffix(".lock"), os.O_RDWR | os.O_CREAT | os.O_CLOEXEC, 0o600)
    try:
        fcntl.flock(lock_fd, fcntl.LOCK_EX)
        current = load_registry(path, team)
        updated, result = change(current)
        if updated != current:
            save_registry(path, updated)
        return result
    finally:
        os.close(lock_fd)
