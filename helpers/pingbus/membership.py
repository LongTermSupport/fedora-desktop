"""A running session's membership: the desired file the host writes, the applied file the
seat keeper writes, and which teams are active.

Spec: Plan 00203 SPEC-hot-control.md section 3 and H4, H8.

- The desired membership (3.1), `membership.json` in the directory `PINGBUS_MEMBERSHIP`
  names, is written by the host only, atomically, and is read-only in a ccy container. It
  is the only authority on which seats a session may hold.
- The applied membership (3.2), `applied.json` in the session home, is what the seat keeper
  holds. The session can write it, so it is never trusted alone: a team is active only when
  its seat is held in the applied file and named, with the same seat, in the desired file.

This module imports nothing of pingbus's but `protocol`, so `config` can use it. Every
refusal is a `MembershipError` (exit 78); `config` turns it into a `ConfigError`.
"""

from __future__ import annotations

import contextlib
import dataclasses
import fcntl
import json
import os
import pathlib
import re
import secrets
import stat
from collections.abc import Iterator, Mapping, Sequence

from helpers.pingbus import protocol

MEMBERSHIP_VAR = "PINGBUS_MEMBERSHIP"
#: The launcher's value for a session started without the bus (`ccy --no-bus`).
MEMBERSHIP_OFF = "off"
#: Where a ccy launch mounts the session's membership directory, read-only.
CCY_MOUNT = "/run/ccy/membership"
DESIRED_FILE = "membership.json"
APPLIED_FILE = "applied.json"
#: Present in the session home if and only if the keeper holds at least one seat (H33).
FLAG_FILE = "active"
VERSION = 1
MAX_FILE_BYTES = 65536
MAX_SEATS = 64
MAX_FAREWELLS = 256

KINDS = ("ccy", "cc")
#: A farewell's reason, and the words a removal is told with.
FAREWELL_REASONS = {"removed": "removed by the owner"}
DEFAULT_REASON = FAREWELL_REASONS["removed"]
SEAT_STATES = ("held",)
#: Why the keeper does not hold a desired seat (3.2): a closed set.
WHY = ("held-elsewhere", "no-bundle", "bundle-invalid", "not-desired")
NO_HOLDER = "-"
ROLES = ("worker", "coordinator")

DESIRED_KEYS = frozenset({"v", "session", "kind", "checkout", "generation", "written_ms", "seats",
                          "farewell"})
APPLIED_KEYS = frozenset({"v", "generation", "seats", "refused"})
APPLIED_OPTIONAL = frozenset({"told", "inbound"})
#: What the session was last told about its teams (H12, H13), in the session home, and the
#: lock its two tellers (the watcher, the prompt hook) take in turn.
TOLD_FILE = "told.json"
TOLD_LOCK = "told.lock"
TOLD_KEYS = frozenset({"v", "generation", "teams", "left"})
#: A told team whose role the session has not been told, its record not read yet.
UNKNOWN_ROLE = "-"

#: The registry name grammar the host's writer uses (src-go/internal/teams/membership); never
#: `.` or `..`, and no `/`, since it names the report's directory (3.3).
_SESSION_RE = re.compile(r"[A-Za-z0-9_][A-Za-z0-9_.@+-]{0,254}")
_CHECKOUT_RE = re.compile(r"/[^\x00-\x1f\x7f]{0,4095}")
_HOLDER_RE = re.compile(r"[a-z-]{1,32}")


class MembershipError(Exception):
    """A membership file that is not what the host or the keeper writes (exit 78)."""

    EXIT_CODE = 78


@dataclasses.dataclass(frozen=True)
class SeatEntry:
    team: str
    seat: str


@dataclasses.dataclass(frozen=True)
class Farewell:
    team: str
    seat: str
    reason: str


@dataclasses.dataclass(frozen=True)
class Desired:
    session: str
    kind: str
    checkout: str
    generation: int
    written_ms: int
    seats: tuple[SeatEntry, ...]
    farewell: tuple[Farewell, ...]

    def identity(self, entry: SeatEntry) -> tuple[str, str, str]:
        """A seat's identity across checkouts: `(checkout, team, seat)`."""
        return (self.checkout, entry.team, entry.seat)

    def reason_for(self, team: str, seat: str) -> str:
        """The words a removal of this seat is told with; the default when no farewell says."""
        for entry in self.farewell:
            if (entry.team, entry.seat) == (team, seat):
                return FAREWELL_REASONS[entry.reason]
        return DEFAULT_REASON


@dataclasses.dataclass(frozen=True)
class AppliedSeat:
    team: str
    seat: str
    handle: str
    state: str = "held"


@dataclasses.dataclass(frozen=True)
class Refused:
    team: str
    seat: str
    why: str
    holder: str = NO_HOLDER


@dataclasses.dataclass(frozen=True)
class Told:
    generation: int
    roles: tuple[tuple[str, str, str], ...]


@dataclasses.dataclass(frozen=True)
class Applied:
    generation: int
    seats: tuple[AppliedSeat, ...]
    refused: tuple[Refused, ...]
    told: Told | None = None
    #: Whether the session was launched accepting team messages (`crossSessionInbound`): a
    #: session launched on no team was not, and its first join relaunches it (H22, H34).
    inbound: bool | None = None

    def to_json(self) -> dict[str, object]:
        data: dict[str, object] = {
            "v": VERSION, "generation": self.generation,
            "seats": [dataclasses.asdict(seat) for seat in self.seats],
            "refused": [dataclasses.asdict(entry) for entry in self.refused],
        }
        if self.inbound is not None:
            data["inbound"] = self.inbound
        if self.told is not None:
            data["told"] = {"generation": self.told.generation,
                            "roles": [{"team": team, "handle": handle, "role": role}
                                      for team, handle, role in self.told.roles]}
        return data


# ── parsing ──────────────────────────────────────────────────────────────────────────────


def _is_int(value: object, low: int) -> bool:
    return type(value) is int and value >= low


def _object(data: object, keys: frozenset[str], where: str,
            optional: frozenset[str] = frozenset()) -> Mapping[str, object]:
    if not isinstance(data, dict):
        raise MembershipError(f"{where}: not an object")
    missing = sorted(keys - set(data))
    unknown = sorted(set(data) - keys - optional)
    if missing:
        raise MembershipError(f"{where}: missing {', '.join(missing)}")
    if unknown:
        raise MembershipError(f"{where}: unknown key {', '.join(unknown)}")
    return data


def _list(data: Mapping[str, object], key: str, where: str, most: int) -> list[object]:
    value = data[key]
    if not isinstance(value, list) or len(value) > most:
        raise MembershipError(f"{where}: {key} is not a list of at most {most}")
    return value


def _names(item: Mapping[str, object], key: str, where: str) -> tuple[str, str]:
    team, seat = item["team"], item["seat"]
    if not protocol.is_team_name(team) or not protocol.is_seat_name(seat):
        raise MembershipError(f"{where}: {key}: an entry's team or seat is not a name")
    return team, seat


def _version(data: Mapping[str, object], where: str) -> None:
    if type(data["v"]) is not int or data["v"] != VERSION:
        raise MembershipError(f"{where}: v is not {VERSION}")


def parse_desired(data: object, where: str = DESIRED_FILE) -> Desired:
    """Section 3.1, every key checked; anything else is refused."""
    obj = _object(data, DESIRED_KEYS, where)
    _version(obj, where)
    if not isinstance(obj["session"], str) or not _SESSION_RE.fullmatch(obj["session"]):
        raise MembershipError(f"{where}: session is not a session name")
    if obj["kind"] not in KINDS:
        raise MembershipError(f"{where}: kind is not one of {', '.join(KINDS)}")
    if not isinstance(obj["checkout"], str) or not _CHECKOUT_RE.fullmatch(obj["checkout"]):
        raise MembershipError(f"{where}: checkout is not an absolute path")
    if not _is_int(obj["generation"], 1):
        raise MembershipError(f"{where}: generation is not a positive integer")
    if not _is_int(obj["written_ms"], 0):
        raise MembershipError(f"{where}: written_ms is not a non-negative integer")
    seats: list[SeatEntry] = []
    for item in _list(obj, "seats", where, MAX_SEATS):
        team, seat = _names(_object(item, frozenset({"team", "seat"}), f"{where}: seats"), "seats", where)
        if any(entry.team == team for entry in seats):
            raise MembershipError(f"{where}: seats: one seat per team, and team {team} has two")
        seats.append(SeatEntry(team, seat))
    farewell: list[Farewell] = []
    for item in _list(obj, "farewell", where, MAX_FAREWELLS):
        entry = _object(item, frozenset({"team", "seat", "reason"}), f"{where}: farewell")
        team, seat = _names(entry, "farewell", where)
        if entry["reason"] not in FAREWELL_REASONS:
            raise MembershipError(f"{where}: farewell: the reason is not one of {', '.join(FAREWELL_REASONS)}")
        farewell.append(Farewell(team, seat, entry["reason"]))
    return Desired(obj["session"], obj["kind"], obj["checkout"], obj["generation"], obj["written_ms"],
                   tuple(seats), tuple(farewell))


def _told(data: object, where: str) -> Told:
    obj = _object(data, frozenset({"generation", "roles"}), f"{where}: told")
    if not _is_int(obj["generation"], 0):
        raise MembershipError(f"{where}: told: generation is not a non-negative integer")
    roles = []
    for item in _list(obj, "roles", f"{where}: told", MAX_SEATS):
        entry = _object(item, frozenset({"team", "handle", "role"}), f"{where}: told: roles")
        if (not protocol.is_team_name(entry["team"]) or protocol.parse_handle(entry["handle"]) is None
                or entry["role"] not in ROLES):
            raise MembershipError(f"{where}: told: a role entry is not a team, a handle and a role")
        roles.append((entry["team"], entry["handle"], entry["role"]))
    return Told(obj["generation"], tuple(roles))


def parse_applied(data: object, where: str = APPLIED_FILE) -> Applied:
    """Section 3.2, every key checked. `generation` is 0 or more (a desired file's is 1 or more)."""
    obj = _object(data, APPLIED_KEYS, where, APPLIED_OPTIONAL)
    _version(obj, where)
    if not _is_int(obj["generation"], 0):
        raise MembershipError(f"{where}: generation is not a non-negative integer")
    seats = []
    for item in _list(obj, "seats", where, MAX_SEATS):
        entry = _object(item, frozenset({"team", "seat", "handle", "state"}), f"{where}: seats")
        team, seat = _names(entry, "seats", where)
        if protocol.parse_handle(entry["handle"]) is None or entry["state"] not in SEAT_STATES:
            raise MembershipError(f"{where}: seats: an entry's handle or state is refused")
        seats.append(AppliedSeat(team, seat, entry["handle"], entry["state"]))
    refused = []
    for item in _list(obj, "refused", where, MAX_SEATS):
        entry = _object(item, frozenset({"team", "seat", "why", "holder"}), f"{where}: refused")
        team, seat = _names(entry, "refused", where)
        holder = entry["holder"]
        if entry["why"] not in WHY or not isinstance(holder, str) or not (
                holder == NO_HOLDER or _HOLDER_RE.fullmatch(holder)):
            raise MembershipError(f"{where}: refused: an entry's why or holder is refused")
        refused.append(Refused(team, seat, entry["why"], holder))
    told = _told(obj["told"], where) if "told" in obj else None
    inbound = obj.get("inbound")
    if "inbound" in obj and type(inbound) is not bool:
        raise MembershipError(f"{where}: inbound is not true or false")
    return Applied(obj["generation"], tuple(seats), tuple(refused), told, inbound)


@dataclasses.dataclass(frozen=True)
class ToldTeam:
    team: str
    seat: str
    handle: str
    role: str


@dataclasses.dataclass(frozen=True)
class ToldRecord:
    """What the session was last told (H12, H13): the desired generation, each team it was
    told it is on, with its seat, handle and role (`-` until its record is read), and each
    team it was told it left, with the farewell reason."""

    generation: int
    teams: tuple[ToldTeam, ...]
    left: tuple[tuple[str, str], ...] = ()

    def said(self) -> Told:
        """The roles told, as the keeper copies them into the applied file (3.2)."""
        return Told(self.generation, tuple((t.team, t.handle, t.role) for t in self.teams
                                           if t.role in ROLES))

    def to_json(self) -> dict[str, object]:
        return {"v": VERSION, "generation": self.generation,
                "teams": [dataclasses.asdict(team) for team in self.teams],
                "left": [{"team": team, "reason": reason} for team, reason in self.left]}


def parse_told(data: object, where: str = TOLD_FILE) -> ToldRecord:
    obj = _object(data, TOLD_KEYS, where)
    _version(obj, where)
    if not _is_int(obj["generation"], 0):
        raise MembershipError(f"{where}: generation is not a non-negative integer")
    teams = []
    for item in _list(obj, "teams", where, MAX_SEATS):
        entry = _object(item, frozenset({"team", "seat", "handle", "role"}), f"{where}: teams")
        team, seat = _names(entry, "teams", where)
        if protocol.parse_handle(entry["handle"]) is None or entry["role"] not in (*ROLES, UNKNOWN_ROLE):
            raise MembershipError(f"{where}: teams: an entry's handle or role is refused")
        teams.append(ToldTeam(team, seat, entry["handle"], entry["role"]))
    left = []
    for item in _list(obj, "left", where, MAX_FAREWELLS):
        entry = _object(item, frozenset({"team", "reason"}), f"{where}: left")
        if not protocol.is_team_name(entry["team"]) or entry["reason"] not in FAREWELL_REASONS:
            raise MembershipError(f"{where}: left: an entry's team or reason is refused")
        left.append((entry["team"], entry["reason"]))
    return ToldRecord(obj["generation"], tuple(teams), tuple(left))


# ── the files ────────────────────────────────────────────────────────────────────────────


def _no_duplicate_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("a key is given twice")
        result[key] = value
    return result


def _read_json(path: pathlib.Path) -> object | None:
    """The JSON in `path`, a regular file never followed through a symlink; None when absent."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except FileNotFoundError:
        return None
    except OSError as exc:
        raise MembershipError(f"{path}: cannot be read ({exc.strerror})") from None
    with os.fdopen(fd, "rb") as handle:
        if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
            raise MembershipError(f"{path}: not a regular file")
        raw = handle.read(MAX_FILE_BYTES + 1)
    if len(raw) > MAX_FILE_BYTES:
        raise MembershipError(f"{path}: larger than {MAX_FILE_BYTES} bytes")
    try:
        return json.loads(raw.decode("utf-8"), object_pairs_hook=_no_duplicate_keys)
    except (UnicodeDecodeError, ValueError):
        raise MembershipError(f"{path}: not one JSON object") from None


def mount_dir(marker: str) -> pathlib.Path:
    """The membership directory a `PINGBUS_MEMBERSHIP` value names; `off` and a relative
    path are refused."""
    if marker == MEMBERSHIP_OFF:
        raise MembershipError(f"{MEMBERSHIP_VAR} is off: this session was launched without the "
                              "bus (--no-bus)")
    if not os.path.isabs(marker):
        raise MembershipError(f"{MEMBERSHIP_VAR} must be an absolute path")
    return pathlib.Path(marker)


def desired_path(directory: pathlib.Path) -> pathlib.Path:
    return pathlib.Path(directory) / DESIRED_FILE


def read_desired(directory: pathlib.Path) -> Desired:
    """The desired membership in `directory`; its absence is refused."""
    path = desired_path(directory)
    data = _read_json(path)
    if data is None:
        raise MembershipError(f"{MEMBERSHIP_VAR}: {path}: no such file: the host writes it at launch")
    return parse_desired(data, str(path))


def read_applied(home: pathlib.Path) -> Applied | None:
    """The applied membership in the session home; None before the keeper writes it."""
    path = pathlib.Path(home) / APPLIED_FILE
    data = _read_json(path)
    return None if data is None else parse_applied(data, str(path))


def write_applied(path: pathlib.Path, applied: Applied) -> None:
    """Write `applied` to `path` atomically, mode 0600: a temporary file beside it, then a rename."""
    _write_json(pathlib.Path(path), applied.to_json())


def read_told(home: pathlib.Path) -> ToldRecord | None:
    """What the session was last told about its teams; None before anything was."""
    path = pathlib.Path(home) / TOLD_FILE
    data = _read_json(path)
    return None if data is None else parse_told(data, str(path))


def write_told(home: pathlib.Path, record: ToldRecord) -> None:
    _write_json(pathlib.Path(home) / TOLD_FILE, record.to_json())


@contextlib.contextmanager
def telling(home: pathlib.Path) -> Iterator[bool]:
    """Hold the session's told lock while the session is told: True when it was had, False
    (without waiting) when another teller holds it, which is then telling the same change."""
    fd = os.open(pathlib.Path(home) / TOLD_LOCK,
                 os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            yield False
            return
        try:
            yield True
        finally:
            fcntl.flock(fd, fcntl.LOCK_UN)
    finally:
        os.close(fd)


def _write_json(path: pathlib.Path, obj: Mapping[str, object]) -> None:
    temp = path.with_name(f".{path.name}.{secrets.token_hex(8)}.tmp")
    data = (json.dumps(obj, ensure_ascii=True, sort_keys=True) + "\n").encode("ascii")
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        with os.fdopen(fd, "wb") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temp, path)
    except BaseException:
        if os.path.lexists(temp):
            os.unlink(temp)
        raise


# ── what is active ───────────────────────────────────────────────────────────────────────


def active(desired: Desired, applied: Applied | None) -> tuple[str, ...]:
    """H8: the teams whose seat is held in `applied` and named, with the same seat, in
    `desired`; in desired order. Nothing in `applied` alone ever makes a team active (H4)."""
    if applied is None:
        return ()
    held = {(seat.team, seat.seat) for seat in applied.seats if seat.state == "held"}
    return tuple(entry.team for entry in desired.seats if (entry.team, entry.seat) in held)


def entries(pairs: Sequence[tuple[str, str]]) -> tuple[SeatEntry, ...]:
    """`(team, seat)` pairs as seat entries."""
    return tuple(SeatEntry(team, seat) for team, seat in pairs)
