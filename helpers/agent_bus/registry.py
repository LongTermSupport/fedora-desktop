"""Member handles and the per-team registry, `/var/lib/agent-bus/<team>/registry.json`.

A handle is `<repo>.<n>+<host>.<type>` (PROTOCOL.md section 3). The registry keeps each
current member's handle with its role, and one counter per seat `<repo>+<host>.<type>`;
`<n>` is one more than the seat's counter and the counter never goes down, so a removed
member's handle is never handed out again.

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
from collections.abc import Callable, Mapping
from dataclasses import dataclass, field, replace
from types import MappingProxyType
from typing import TypeVar

from helpers.agent_bus.teamfile import TEAM_PATTERN, decode_strict_json

REGISTRY_VERSION = 1
HANDLE_SEP = "+"
TYPES = ("podman", "lxc", "docker", "vm", "host")
ROLES = ("orchestrator", "worker")
REPO_MAX = 48
N_MAX = 999_999
REPO_PART_PATTERN = r"[a-z0-9][a-z0-9_-]{0,47}"
HOST_PART_PATTERN = r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
TYPE_PATTERN = "|".join(TYPES)
HANDLE_PATTERN = (
    rf"(?P<repo>{REPO_PART_PATTERN})\.(?P<n>[1-9][0-9]{{0,5}})"
    rf"{re.escape(HANDLE_SEP)}(?P<host>{HOST_PART_PATTERN})\.(?P<type>{TYPE_PATTERN})"
)
SEAT_PATTERN = (
    rf"(?P<repo>{REPO_PART_PATTERN}){re.escape(HANDLE_SEP)}"
    rf"(?P<host>{HOST_PART_PATTERN})\.(?P<type>{TYPE_PATTERN})"
)
REGISTRY_KEYS = frozenset({"v", "team", "counters", "members"})

_HANDLE_RE = re.compile(HANDLE_PATTERN)
_SEAT_RE = re.compile(SEAT_PATTERN)
_TEAM_RE = re.compile(TEAM_PATTERN)
_HOST_PART_RE = re.compile(HOST_PART_PATTERN)
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
        raise HandleError(f"host {raw!r} does not fit {HOST_PART_PATTERN} once normalised")
    return host


def _seat(repo: str, host: str, type_: str) -> str:
    seat = f"{repo}{HANDLE_SEP}{host}.{type_}"
    if not _SEAT_RE.fullmatch(seat):
        raise HandleError(f"seat {seat!r}: repo, host or type does not fit the handle grammar")
    return seat


def build_handle(repo: str, n: int, host: str, type_: str) -> str:
    if type(n) is not int or not 1 <= n <= N_MAX:
        raise HandleError(f"handle number must be an integer from 1 to {N_MAX}")
    handle = f"{repo}.{n}{HANDLE_SEP}{host}.{type_}"
    if not _HANDLE_RE.fullmatch(handle):
        raise HandleError(f"handle {handle!r} does not fit the handle grammar")
    return handle


def seat_of(handle: str) -> str:
    match = _HANDLE_RE.fullmatch(handle)
    if match is None:
        raise HandleError(f"{handle!r} is not an agent handle")
    return f"{match['repo']}{HANDLE_SEP}{match['host']}.{match['type']}"


def _check_role(role: object) -> str:
    if role not in ROLES:
        raise RegistryError(f"role {role!r} must be one of {', '.join(ROLES)}")
    return role


@dataclass(frozen=True)
class Registry:
    team: str
    counters: Mapping[str, int] = field(default_factory=dict)
    members: Mapping[str, str] = field(default_factory=dict)

    def __post_init__(self) -> None:
        object.__setattr__(self, "counters", MappingProxyType(dict(self.counters)))
        object.__setattr__(self, "members", MappingProxyType(dict(self.members)))

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, Registry):
            return NotImplemented
        return (self.team, dict(self.counters), dict(self.members)) == (
            other.team,
            dict(other.counters),
            dict(other.members),
        )

    __hash__ = None

    @classmethod
    def empty(cls, team: str) -> Registry:
        if not _TEAM_RE.fullmatch(team):
            raise RegistryError(f"team {team!r} must match {TEAM_PATTERN}")
        return cls(team)

    def add_member(self, repo: str, host: str, type_: str, role: str) -> tuple[Registry, str]:
        """Allocate the seat's next `<n>`; return the new registry and the new handle."""
        _check_role(role)
        seat = _seat(repo, host, type_)
        n = self.counters.get(seat, 0) + 1
        if n > N_MAX:
            raise RegistryError(f"seat {seat!r} has used every handle number")
        handle = build_handle(repo, n, host, type_)
        counters = {**self.counters, seat: n}
        members = {**self.members, handle: role}
        return replace(self, counters=counters, members=members), handle

    def remove_member(self, handle: str) -> Registry:
        """Drop a member; its seat's counter stays, so its `<n>` is never reused."""
        if handle not in self.members:
            raise RegistryError(f"{handle!r} is not a member of team {self.team}")
        members = {h: r for h, r in self.members.items() if h != handle}
        return replace(self, members=members)

    def set_role(self, handle: str, role: str) -> Registry:
        _check_role(role)
        if handle not in self.members:
            raise RegistryError(f"{handle!r} is not a member of team {self.team}")
        return replace(self, members={**self.members, handle: role})

    def as_dict(self) -> dict:
        return {
            "v": REGISTRY_VERSION,
            "team": self.team,
            "counters": dict(self.counters),
            "members": dict(self.members),
        }


def parse_registry(data: object) -> Registry:
    if not isinstance(data, dict) or set(data) != REGISTRY_KEYS:
        raise RegistryError(f"registry must be an object with exactly the keys {sorted(REGISTRY_KEYS)}")
    if type(data["v"]) is not int or data["v"] != REGISTRY_VERSION:
        raise RegistryError(f"registry version must be {REGISTRY_VERSION}")
    team = data["team"]
    if not isinstance(team, str) or not _TEAM_RE.fullmatch(team):
        raise RegistryError(f"registry team must match {TEAM_PATTERN}")
    counters = data["counters"]
    if not isinstance(counters, dict):
        raise RegistryError("registry counters must be an object")
    for seat, n in counters.items():
        if not _SEAT_RE.fullmatch(seat):
            raise RegistryError(f"registry counter key {seat!r} is not <repo>{HANDLE_SEP}<host>.<type>")
        if type(n) is not int or not 1 <= n <= N_MAX:
            raise RegistryError(f"registry counter {seat!r} must be an integer from 1 to {N_MAX}")
    members = data["members"]
    if not isinstance(members, dict):
        raise RegistryError("registry members must be an object")
    for handle, role in members.items():
        match = _HANDLE_RE.fullmatch(handle)
        if match is None:
            raise RegistryError(f"registry member {handle!r} is not an agent handle")
        _check_role(role)
        if int(match["n"]) > counters.get(seat_of(handle), 0):
            raise RegistryError(f"registry member {handle!r} is beyond its seat's counter")
    return Registry(team, counters, members)


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
    runs cannot both read the same counter and hand out the same `<n>`. If `change`
    raises, nothing is saved. Returns the second element of `change`'s result.
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
