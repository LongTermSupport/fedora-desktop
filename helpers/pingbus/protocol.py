"""Agent team bus protocol v1: the identifier grammars config.py needs.

Interface stand-in: unit U02 owns this module (the full validator) and replaces this file
when it merges; it must keep `is_team_name`, `parse_handle` and `is_room_id` with these
signatures. Grammars: PROTOCOL.md section 3.
"""

from __future__ import annotations

import dataclasses
import re

TEAM_NAME_PATTERN = r"[a-z][a-z0-9-]{0,23}"
HANDLE_SEP = "+"
HANDLE_PATTERN = (
    r"(?P<repo>[a-z0-9][a-z0-9_-]{0,47})\.(?P<n>[1-9][0-9]{0,5})"
    + re.escape(HANDLE_SEP)
    + r"(?P<host>[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)"
    r"\.(?P<type>podman|lxc|docker|vm|host)"
)
ROOM_ID_PATTERN = r"![A-Za-z0-9_-]{43}"

_TEAM_RE = re.compile(TEAM_NAME_PATTERN)
_HANDLE_RE = re.compile(HANDLE_PATTERN)
_ROOM_ID_RE = re.compile(ROOM_ID_PATTERN)


@dataclasses.dataclass(frozen=True)
class Handle:
    repo: str
    n: int
    host: str
    type: str


def _full(regex: re.Pattern[str], value: object) -> re.Match[str] | None:
    return regex.fullmatch(value) if isinstance(value, str) else None


def is_team_name(value: object) -> bool:
    return _full(_TEAM_RE, value) is not None


def parse_handle(value: object) -> Handle | None:
    m = _full(_HANDLE_RE, value)
    if m is None:
        return None
    return Handle(m["repo"], int(m["n"]), m["host"], m["type"])


def is_room_id(value: object) -> bool:
    return _full(_ROOM_ID_RE, value) is not None
