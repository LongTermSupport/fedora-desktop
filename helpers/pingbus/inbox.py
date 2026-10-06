"""One team's local pingbus state: the inbox, the sync token, the outbox with the send
gate, and the sync lock.

Spec: docs/agent-bus-protocol.md §9 (the inbox is a cache: every stored file is re-run
through the offline validator on read and ignored when it fails; the sync token is saved
only after the batch's inbox writes are durable), §10 (TIMEOUT lines; the send bucket and
duplicate window hold across `send` processes), §12 (the layout of
`PINGBUS_HOME/<team>/state/` and the `lock`), §14 (exit 75). DESIGN.md section 6 (liveness
is a lock, never a PID).

Layout under the state directory (created 0700, files 0600, never through a symlink):

- `inbox/<event ID>.json`, `consumed/<event ID>.json`: one accepted event each, stored as
  a projection of the event (`STORED_KEYS`). An event ID is checked against §3 before it
  names a file.
- `sync.json`: `{"v": 1, "next_batch": <token>}`.
- `outbox.json`: `{"v": 1, "pings": [...], "gate": <limits.SendGate state or null>}`. Every
  read-modify-write holds `flock` on the state directory itself, so concurrent `send`,
  `recv` and the watcher serialise without a further file.
- `lock`: held with `flock` by the one process syncing this team, holding its kind.
- `team.json`: the last verified team record, removed when the room stops being trusted.
- `room.json`: `{"v": 1, "members": {<user ID>: "join"|"invite"}, "statuses": {<user ID>:
  <until ms>}}`, the room as the syncer last saw it, for an offline `status`.
- `dropped.log`: one tab-separated line per drop (team, event ID, sender, reason, ms).

Writes go to a temporary file in the same directory, are fsynced, renamed over the target,
and the directory is fsynced. Errors propagate; the only things skipped are inbox files
that fail re-validation, which the spec requires, and they are counted.
"""

from __future__ import annotations

import contextlib
import dataclasses
import errno
import fcntl
import json
import os
import pathlib
import re
import secrets
import stat
import time
from collections.abc import Callable, Iterable, Iterator, Mapping, Sequence

from helpers.pingbus import config, limits, protocol

STATE_VERSION = 1
INBOX_DIR = "inbox"
CONSUMED_DIR = "consumed"
OUTBOX_FILE = "outbox.json"
SYNC_FILE = "sync.json"
LOCK_FILE = "lock"
#: The last verified team record (§12): the `agent_bus.team` content the syncer verified.
TEAM_FILE = "team.json"
#: Why the room is not trusted (§8, §12), for an offline `status`; gone while it is trusted.
UNTRUSTED_FILE = "untrusted.json"
UNTRUSTED_REASON_MAX = 200
#: One line per drop (§9 "Drop"): team, event ID, sender user ID, reason code, time (ms).
DROPPED_LOG = "dropped.log"
#: The room as the syncer last saw it (§12), for an offline `status`: joined and invited
#: members by user ID, and the `listening` statuses §8 lets it read.
ROOM_FILE = "room.json"
ROOM_MEMBERSHIPS = ("join", "invite")
ROOM_MEMBERS_MAX = 4096
TEAM_RECORD_MAX_BYTES = 65536
ABSENT = "-"

#: What a lock holder writes into `lock`. `recv` holds it only while it syncs once.
LOCK_KINDS = ("watch", "wait", "recv")
#: The holders that wake a session (spec §12); a `recv` holder is not one.
WAKER_KINDS = ("watch", "wait")
#: A held lock whose file does not (yet) name a kind.
KIND_UNKNOWN = "unknown"
#: A probe holds a shared lock for an instant; a few tries keep it from making a starting
#: watcher report the seat taken.
LOCK_ATTEMPTS = 3
LOCK_RETRY_S = 0.05

DIR_MODE = 0o700
FILE_MODE = 0o600
STORED_KEYS = ("type", "event_id", "sender", "origin_server_ts", "content")
#: Stored JSON is ASCII-escaped, which can grow a 64 KiB Matrix event up to six times.
MAX_INBOX_FILE_BYTES = 6 * 65536
MAX_STATE_FILE_BYTES = 1 << 20
SYNC_TOKEN_MAX = 4096

_TOKEN_RE = re.compile(r"[\x21-\x7e]+")
#: A sender written to `dropped.log`: printable ASCII, no space or tab, Matrix's 255 limit.
_LOG_USER_RE = re.compile(r"@[\x21-\x7e]{1,254}")
_INBOX_NAME_RE = re.compile(rf"({protocol.EVENT_ID_PATTERN})\.json")
_TMP_NAME_RE = re.compile(r"\..+\.[0-9a-f]{16}\.tmp")
_PING_ENTRY_KEYS = frozenset({"event_id", "verb", "ref", "to", "sent_ms", "answered", "reported"})


class StateError(Exception):
    """A state file or directory is not what this module writes."""


class Busy(Exception):
    """Another process holds this team's sync lock (spec §14, exit 75)."""

    EXIT_CODE = 75

    def __init__(self, holder: str) -> None:
        super().__init__(f"the sync lock is held by a {holder} process")
        self.holder = holder


class _Unusable(Exception):
    """An inbox file that is a symlink, not a regular file, or too large."""


@dataclasses.dataclass(frozen=True)
class Item:
    event_id: str
    origin_server_ts: int
    outcome: protocol.Outcome


@dataclasses.dataclass(frozen=True)
class Pending:
    """Valid inbox items, oldest first, and how many stored files failed re-validation."""

    items: tuple[Item, ...]
    rejected: int

    def counts(self) -> tuple[int, int, int]:
        """(total, from humans, pings), for the hooks' and the watcher's templates."""
        humans = sum(1 for item in self.items if item.outcome.human is not None)
        return len(self.items), humans, len(self.items) - humans


@dataclasses.dataclass(frozen=True)
class Timeout:
    """One `TIMEOUT` line owed: a silent target of an ack-expected ping (spec §10, §15)."""

    event_id: str
    target: str
    verb: str
    ref: str | None


def _is_int(value: object) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def _check_event_id(value: object) -> str:
    if not protocol.is_event_id(value):
        raise ValueError("not an event ID")
    return value


def _check_sync_token(value: object) -> str:
    if not isinstance(value, str) or len(value) > SYNC_TOKEN_MAX or not _TOKEN_RE.fullmatch(value):
        raise ValueError("a sync token is 1 to 4096 printable ASCII characters, no spaces")
    return value


def _is_reason(value: object) -> bool:
    return (isinstance(value, str) and 0 < len(value) <= UNTRUSTED_REASON_MAX
            and value.isascii() and value.isprintable())


def _write_atomic(path: pathlib.Path, data: bytes) -> None:
    """Write `data` to `path` by a fsynced temporary file renamed over it. The caller
    fsyncs the directory."""
    tmp = path.with_name(f".{path.name}.{secrets.token_hex(8)}.tmp")
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, FILE_MODE)
    try:
        with os.fdopen(fd, "wb") as handle:
            os.fchmod(handle.fileno(), FILE_MODE)
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(tmp, path)
    except BaseException:
        tmp.unlink(missing_ok=True)
        raise


def _fsync_dir(path: pathlib.Path) -> None:
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _read_regular(path: pathlib.Path, max_bytes: int) -> bytes:
    """A regular file's bytes, never through a symlink and never blocking on a FIFO.
    `FileNotFoundError` propagates; anything else unusable raises `_Unusable`."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except OSError as exc:
        if exc.errno == errno.ELOOP:
            raise _Unusable("a symlink") from None
        raise
    with os.fdopen(fd, "rb") as handle:
        if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
            raise _Unusable("not a regular file")
        data = handle.read(max_bytes + 1)
    if len(data) > max_bytes:
        raise _Unusable("too large")
    return data


def _dump(value: object) -> bytes:
    return json.dumps(value, ensure_ascii=True, separators=(",", ":")).encode("ascii")


def is_user_id_text(value: object) -> bool:
    """A user ID that can be printed as one field: printable ASCII with no space or tab,
    `@`, a localpart, `:`, a server name, at most Matrix's 255 characters."""
    return isinstance(value, str) and _LOG_USER_RE.fullmatch(value) is not None and ":" in value[2:]


def write_json_file(path: pathlib.Path, value: object) -> None:
    """Replace `path` with `value` as JSON: fsynced temporary file, rename, directory fsync."""
    _write_atomic(path, _dump(value))
    _fsync_dir(path.parent)


def read_json_file(path: pathlib.Path) -> object | None:
    """A state file's JSON, None when absent; `StateError` when it is not a regular file
    (never followed through a symlink), too large, or not JSON."""
    try:
        raw = _read_regular(path, MAX_STATE_FILE_BYTES)
    except FileNotFoundError:
        return None
    except _Unusable as exc:
        raise StateError(f"{path} is {exc}") from None
    try:
        return json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        raise StateError(f"{path} is not valid JSON") from None


def load_cached_record(member: config.Member) -> protocol.TeamRecord:
    """The team record last verified for `member` (§12 `team.json`), re-checked; `Untrusted`
    (exit 10) when there is none yet, it fails §8, or it gives this member no role."""
    path = member.state_dir / TEAM_FILE
    where = f"team {member.team}: {path}"
    try:
        raw = _read_regular(path, TEAM_RECORD_MAX_BYTES)
    except FileNotFoundError:
        raise protocol.Untrusted(f"{where}: no verified team record yet (not joined)") from None
    except _Unusable as exc:
        raise protocol.Untrusted(f"{where}: {exc}") from None
    except OSError as exc:
        raise protocol.Untrusted(f"{where}: cannot be read ({exc.strerror})") from None
    try:
        content = json.loads(raw)
    except (UnicodeDecodeError, ValueError):
        raise protocol.Untrusted(f"{where}: not JSON") from None
    try:
        record = protocol.parse_team_record(content, member.server_name, member.team)
    except protocol.Untrusted as exc:
        raise protocol.Untrusted(f"{where}: {exc}") from None
    if record.roles.get(member.user_id) not in protocol.ROLES:
        raise protocol.Untrusted(f"{where}: the team record gives this member no role")
    return record


class TeamState:
    """`PINGBUS_HOME/<team>/state/`. Reads create nothing; the first write creates the
    state directories inside an existing bundle directory."""

    def __init__(self, path: pathlib.Path) -> None:
        self.path = pathlib.Path(path)

    @classmethod
    def for_member(cls, member: config.Member) -> TeamState:
        """The state of a `config.Member`'s bundle."""
        return cls(member.state_dir)

    @property
    def inbox_dir(self) -> pathlib.Path:
        return self.path / INBOX_DIR

    @property
    def consumed_dir(self) -> pathlib.Path:
        return self.path / CONSUMED_DIR

    def ensure_dirs(self) -> None:
        for path in (self.path, self.inbox_dir, self.consumed_dir):
            if not os.path.lexists(path):
                os.mkdir(path, DIR_MODE)
                os.chmod(path, DIR_MODE)
            if not stat.S_ISDIR(os.lstat(path).st_mode):
                raise StateError(f"{path} is not a directory (a symlink is refused)")

    # The inbox and the sync token.

    def known_event_ids(self) -> frozenset[str]:
        """Event IDs in the inbox or already consumed: the validator's `seen`."""
        found: set[str] = set()
        for directory in (self.inbox_dir, self.consumed_dir):
            if not directory.is_dir():
                continue
            for name in os.listdir(directory):
                m = _INBOX_NAME_RE.fullmatch(name)
                if m is not None:
                    found.add(m[1])
        return frozenset(found)

    def commit_batch(self, events: Sequence[Mapping[str, object]], next_batch: str) -> int:
        """Store a sync batch's accepted events, make them durable, then save `next_batch`
        (spec §9). Returns how many were new. Every event ID is checked before any file is
        written, so a bad one writes nothing and the token stays where it was."""
        token = _check_sync_token(next_batch)
        prepared: list[tuple[str, bytes]] = []
        for event in events:
            if not isinstance(event, Mapping):
                raise ValueError("an event is not an object")
            event_id = _check_event_id(event.get("event_id"))
            data = _dump({key: event[key] for key in STORED_KEYS if key in event})
            if len(data) > MAX_INBOX_FILE_BYTES:
                raise ValueError(f"event {event_id} is larger than an inbox file may be")
            prepared.append((event_id, data))
        self.ensure_dirs()
        known = set(self.known_event_ids())
        written = 0
        for event_id, data in prepared:
            if event_id in known:
                continue
            _write_atomic(self.inbox_dir / f"{event_id}.json", data)
            known.add(event_id)
            written += 1
        _fsync_dir(self.inbox_dir)
        _write_atomic(self.path / SYNC_FILE, _dump({"v": STATE_VERSION, "next_batch": token}))
        _fsync_dir(self.path)
        return written

    def sync_token(self) -> str | None:
        """The saved `next_batch`, or None before the first sync."""
        data = self._load_json(self.path / SYNC_FILE)
        if data is None:
            return None
        if not isinstance(data, dict) or set(data) != {"v", "next_batch"} or data["v"] != STATE_VERSION:
            raise StateError(f"{self.path / SYNC_FILE} is not a sync token file")
        try:
            return _check_sync_token(data["next_batch"])
        except ValueError:
            raise StateError(f"{self.path / SYNC_FILE} holds a malformed sync token") from None

    def pending(self, ctx: protocol.Context, self_user_id: str, *, human_text: bool) -> Pending:
        """Every inbox item that still passes the offline validator, oldest first."""
        if not self.inbox_dir.is_dir():
            return Pending((), 0)
        items: list[Item] = []
        rejected = 0
        for name in sorted(os.listdir(self.inbox_dir)):
            if _TMP_NAME_RE.fullmatch(name):
                continue
            outcome = self._revalidate(name, ctx, self_user_id, human_text)
            if outcome is None:
                continue
            if outcome.kind != protocol.ACCEPT:
                rejected += 1
                continue
            item_ts = (outcome.ping or outcome.human).origin_server_ts
            items.append(Item(name[: -len(".json")], item_ts, outcome))
        items.sort(key=lambda item: (item.origin_server_ts, item.event_id))
        return Pending(tuple(items), rejected)

    def _revalidate(
        self, name: str, ctx: protocol.Context, self_user_id: str, human_text: bool
    ) -> protocol.Outcome | None:
        """The stored file's outcome; a DROP outcome for anything unusable; None when the
        file went away (consumed meanwhile)."""
        rejected = protocol.Outcome(protocol.DROP, "schema")
        m = _INBOX_NAME_RE.fullmatch(name)
        if m is None:
            return rejected
        try:
            raw = _read_regular(self.inbox_dir / name, MAX_INBOX_FILE_BYTES)
        except FileNotFoundError:
            return None
        except _Unusable:
            return rejected
        try:
            event = json.loads(raw.decode("utf-8"))
        except (UnicodeDecodeError, ValueError):
            return rejected
        if not isinstance(event, dict) or event.get("event_id") != m[1]:
            return rejected
        return protocol.validate_event(event, ctx, self_user_id, human_text=human_text)

    def consume(self, event_id: str) -> bool:
        """Move an item from the inbox to `consumed/`; False when it was not pending."""
        name = f"{_check_event_id(event_id)}.json"
        self.ensure_dirs()
        try:
            os.rename(self.inbox_dir / name, self.consumed_dir / name)
        except FileNotFoundError:
            return False
        _fsync_dir(self.consumed_dir)
        _fsync_dir(self.inbox_dir)
        return True

    # The verified team record and the drop log.

    def save_team_record(self, content: Mapping[str, object]) -> None:
        """Replace `team.json` with the team record content the syncer just verified; the
        room is trusted again, so any `untrusted.json` goes."""
        self.ensure_dirs()
        _write_atomic(self.path / TEAM_FILE, _dump(content))
        (self.path / UNTRUSTED_FILE).unlink(missing_ok=True)
        _fsync_dir(self.path)

    def mark_untrusted(self, reason: str) -> None:
        """Record why the room stopped being trusted, and remove `team.json`."""
        if not _is_reason(reason):
            raise ValueError(f"a reason is one line of 1 to {UNTRUSTED_REASON_MAX} printable ASCII characters")
        self.ensure_dirs()
        _write_atomic(self.path / UNTRUSTED_FILE, _dump({"v": STATE_VERSION, "reason": reason}))
        self.forget_team_record()
        _fsync_dir(self.path)

    def untrusted_reason(self) -> str | None:
        """Why the room is not trusted, or None when no loss of trust is recorded."""
        data = self._load_json(self.path / UNTRUSTED_FILE)
        if data is None:
            return None
        if (not isinstance(data, dict) or set(data) != {"v", "reason"} or data["v"] != STATE_VERSION
                or not _is_reason(data["reason"])):
            raise StateError(f"{self.path / UNTRUSTED_FILE} is not an untrusted-reason file")
        return data["reason"]

    def save_room_view(self, members: Mapping[str, str], statuses: Mapping[str, int]) -> None:
        """Replace `room.json`: each joined or invited member's membership, and the
        `listening` statuses (`until`, Unix ms) the syncer accepted under §8."""
        view = {"v": STATE_VERSION, "members": dict(members), "statuses": dict(statuses)}
        _check_room_view(view, self.path / ROOM_FILE)
        self.ensure_dirs()
        write_json_file(self.path / ROOM_FILE, view)

    def room_view(self) -> tuple[dict[str, str], dict[str, int]]:
        """(members, statuses) from `room.json`, both empty before the first save;
        `StateError` when the file is not what `save_room_view` writes."""
        data = read_json_file(self.path / ROOM_FILE)
        if data is None:
            return {}, {}
        _check_room_view(data, self.path / ROOM_FILE)
        return dict(data["members"]), dict(data["statuses"])

    def forget_team_record(self) -> bool:
        """Remove `team.json` when the room stops being trusted; False when there was none."""
        try:
            os.unlink(self.path / TEAM_FILE)
        except FileNotFoundError:
            return False
        _fsync_dir(self.path)
        return True

    def log_drop(self, team: str, event_id: object, sender: object, reason: str, now_ms: int) -> None:
        """Append one `dropped.log` line. An event ID or sender that fails its grammar is
        written as `-`, so nothing an event chose can add a field or a line."""
        if not protocol.is_team_name(team):
            raise ValueError("not a team name")
        if reason not in protocol.DROP_REASONS:
            raise ValueError(f"not a drop reason code: {reason!r}")
        if not _is_int(now_ms):
            raise ValueError("now_ms must be an integer of milliseconds")
        fields = (
            team,
            event_id if protocol.is_event_id(event_id) else ABSENT,
            sender if isinstance(sender, str) and _LOG_USER_RE.fullmatch(sender) else ABSENT,
            reason,
            str(now_ms),
        )
        self.ensure_dirs()
        path = self.path / DROPPED_LOG
        try:
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW | os.O_CLOEXEC, FILE_MODE)
        except OSError as exc:
            if exc.errno == errno.ELOOP:
                raise StateError(f"{path} is a symlink") from None
            raise
        with os.fdopen(fd, "ab") as handle:
            if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
                raise StateError(f"{path} is not a regular file")
            handle.write(("\t".join(fields) + "\n").encode("ascii"))

    def drop_count(self) -> int:
        """How many lines `dropped.log` holds (0 before the first drop)."""
        path = self.path / DROPPED_LOG
        try:
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
        except FileNotFoundError:
            return 0
        except OSError as exc:
            raise StateError(f"{path} cannot be read ({exc.strerror})") from None
        count = 0
        with os.fdopen(fd, "rb") as handle:
            if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
                raise StateError(f"{path} is not a regular file")
            while chunk := handle.read(1 << 16):
                count += chunk.count(b"\n")
        return count

    # The outbox and the send gate.

    @contextlib.contextmanager
    def outbox(self) -> Iterator[Outbox]:
        """A read-modify-write of `outbox.json` under `flock` on the state directory.
        Saved when the block ends normally and something changed; never on an exception.
        Do no network work inside it: the lock is held throughout, and a later failure
        would undo the send gate's charge, which spec §10 keeps. Use `admit_send`."""
        self.ensure_dirs()
        fd = os.open(self.path, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX)
            box = Outbox.from_state(self._load_json(self.path / OUTBOX_FILE), self.path / OUTBOX_FILE)
            yield box
            if box.changed:
                _write_atomic(self.path / OUTBOX_FILE, _dump(box.to_state()))
                _fsync_dir(self.path)
        finally:
            os.close(fd)

    def admit_send(
        self, member_limits: limits.Limits, verb: str, ref: str | None, re_: str | None,
        to: Iterable[str], now_ms: int,
    ) -> None:
        """§9 send step 5 in its own transaction, saved before returning, so a send refused
        or failing later has still spent its token and entered the duplicate window (§10).
        `RateLimited` propagates having recorded nothing."""
        with self.outbox() as box:
            box.send_gate(member_limits, now_ms).admit(verb, ref, re_, to, now_ms)

    def _load_json(self, path: pathlib.Path) -> object | None:
        return read_json_file(path)


def _check_room_view(data: object, where: pathlib.Path) -> None:
    if not isinstance(data, dict) or set(data) != {"v", "members", "statuses"} or data["v"] != STATE_VERSION:
        raise StateError(f"{where} is not a room view file")
    members, statuses = data["members"], data["statuses"]
    if (not isinstance(members, dict) or len(members) > ROOM_MEMBERS_MAX
            or not all(is_user_id_text(uid) and m in ROOM_MEMBERSHIPS for uid, m in members.items())):
        raise StateError(f"{where} holds a malformed member")
    if (not isinstance(statuses, dict) or len(statuses) > ROOM_MEMBERS_MAX
            or not all(is_user_id_text(uid) and _is_int(until) and until >= 0
                       for uid, until in statuses.items())):
        raise StateError(f"{where} holds a malformed status")


class Outbox:
    """Ack-expected pings this member sent, who has answered, which TIMEOUTs were
    reported; and the saved send gate. Use only inside `TeamState.outbox()`."""

    def __init__(self, pings: list[dict], gate_state: object, where: pathlib.Path) -> None:
        self._pings = pings
        self._gate_state = gate_state
        self._gate: limits.SendGate | None = None
        self._where = where
        self.changed = False

    @classmethod
    def from_state(cls, data: object, where: pathlib.Path) -> Outbox:
        if data is None:
            return cls([], None, where)
        if not isinstance(data, dict) or set(data) != {"v", "pings", "gate"} or data["v"] != STATE_VERSION:
            raise StateError(f"{where} is not an outbox file")
        pings = data["pings"]
        if not isinstance(pings, list) or not all(_valid_entry(entry) for entry in pings):
            raise StateError(f"{where} holds a malformed outbox entry")
        if data["gate"] is not None and not isinstance(data["gate"], dict):
            raise StateError(f"{where} holds a malformed send gate")
        return cls(pings, data["gate"], where)

    def to_state(self) -> dict[str, object]:
        pings = [entry for entry in self._pings if not _settled(entry)]
        gate = self._gate.to_dict() if self._gate is not None else self._gate_state
        return {"v": STATE_VERSION, "pings": pings, "gate": gate}

    def tracked(self) -> tuple[str, ...]:
        """Event IDs of pings still awaiting an answer or a TIMEOUT."""
        return tuple(entry["event_id"] for entry in self._pings if not _settled(entry))

    def send_gate(self, member_limits: limits.Limits, now_ms: int) -> limits.SendGate:
        """The send gate saved by earlier `send`/`say` runs (fresh on the first); what
        `admit` records on it is saved when the transaction ends normally. `send` and `say`
        go through `TeamState.admit_send`; no network work belongs inside `outbox()`."""
        if self._gate is None:
            if self._gate_state is None:
                self._gate = limits.SendGate.fresh(member_limits, now_ms)
            else:
                try:
                    self._gate = limits.SendGate.from_dict(self._gate_state, member_limits)
                except ValueError:
                    raise StateError(f"{self._where} holds a malformed send gate") from None
        self.changed = True
        return self._gate

    def record_sent(self, event_id: str, verb: str, ref: str | None, to: Sequence[str], sent_ms: int) -> None:
        """Track a sent ping for acks and TIMEOUTs; pings that expect no ack are not kept."""
        _check_event_id(event_id)
        rule = protocol.VERBS.get(verb)
        if rule is None:
            raise ValueError("not a verb")
        if ref is not None and not isinstance(ref, str):
            raise ValueError("ref must be a string or None")
        targets = list(to)
        if not targets or not all(isinstance(t, str) for t in targets) or len(set(targets)) != len(targets):
            raise ValueError("to must be distinct user IDs, at least one")
        if not _is_int(sent_ms):
            raise ValueError("sent_ms must be an integer of milliseconds")
        if not rule.ack_expected:
            return
        if any(entry["event_id"] == event_id for entry in self._pings):
            raise ValueError("this event ID is already tracked")
        self._pings.append({
            "event_id": event_id, "verb": verb, "ref": ref, "to": sorted(targets),
            "sent_ms": sent_ms, "answered": [], "reported": [],
        })
        self.changed = True

    def record_answer(self, re_: str, sender: str) -> bool:
        """An `ack` or `nack` from `sender` naming `re_`: True when `re_` is a tracked ping
        that addressed `sender`."""
        for entry in self._pings:
            if entry["event_id"] == re_ and sender in entry["to"]:
                if sender not in entry["answered"]:
                    entry["answered"].append(sender)
                    self.changed = True
                return True
        return False

    def due_timeouts(self, now_ms: int, member_limits: limits.Limits) -> tuple[Timeout, ...]:
        """TIMEOUTs owed now and not yet reported; `mark_reported` once they are printed."""
        due: list[Timeout] = []
        for entry in self._pings:
            if not limits.ack_overdue(entry["sent_ms"], entry["verb"], now_ms, member_limits):
                continue
            done = set(entry["answered"]) | set(entry["reported"])
            due.extend(
                Timeout(entry["event_id"], target, entry["verb"], entry["ref"])
                for target in entry["to"]
                if target not in done
            )
        return tuple(due)

    def mark_reported(self, timeouts: Iterable[Timeout]) -> None:
        for timeout in timeouts:
            for entry in self._pings:
                if entry["event_id"] == timeout.event_id and timeout.target in entry["to"]:
                    if timeout.target not in entry["reported"]:
                        entry["reported"].append(timeout.target)
                        self.changed = True


def _valid_entry(entry: object) -> bool:
    if not isinstance(entry, dict) or set(entry) != _PING_ENTRY_KEYS:
        return False
    rule = protocol.VERBS.get(entry["verb"]) if isinstance(entry["verb"], str) else None
    to = entry["to"]
    return (
        protocol.is_event_id(entry["event_id"])
        and rule is not None
        and rule.ack_expected
        and (entry["ref"] is None or isinstance(entry["ref"], str))
        and isinstance(to, list)
        and bool(to)
        and all(isinstance(t, str) for t in to)
        and len(set(to)) == len(to)
        and _is_int(entry["sent_ms"])
        and all(
            isinstance(entry[key], list) and set(entry[key]) <= set(to) for key in ("answered", "reported")
        )
    )


def _settled(entry: Mapping[str, object]) -> bool:
    return set(entry["to"]) <= set(entry["answered"]) | set(entry["reported"])


# The sync lock.


class Lock:
    """A held sync lock. Released by `release`, by the context manager, or by the kernel
    when the process dies."""

    def __init__(self, fd: int, kind: str) -> None:
        self._fd: int | None = fd
        self.kind = kind

    def release(self) -> None:
        if self._fd is None:
            return
        fd, self._fd = self._fd, None
        try:
            os.ftruncate(fd, 0)
        finally:
            os.close(fd)

    def __enter__(self) -> Lock:
        return self

    def __exit__(self, *exc: object) -> None:
        self.release()


def _open_lock(state: TeamState, flags: int) -> int:
    path = state.path / LOCK_FILE
    try:
        fd = os.open(path, flags | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, FILE_MODE)
    except OSError as exc:
        if exc.errno == errno.ELOOP:
            raise StateError(f"{path} is a symlink") from None
        raise
    if not stat.S_ISREG(os.fstat(fd).st_mode):
        os.close(fd)
        raise StateError(f"{path} is not a regular file")
    return fd


def _read_kind(fd: int) -> str:
    kind = os.pread(fd, 16, 0).decode("ascii", errors="replace")
    return kind if kind in LOCK_KINDS else KIND_UNKNOWN


def acquire_lock(state: TeamState, kind: str, *, sleep: Callable[[float], None] = time.sleep) -> Lock:
    """Take this team's sync lock without waiting on its holder, and write `kind` into
    it; raise `Busy` naming the holder's kind when another process has it."""
    if kind not in LOCK_KINDS:
        raise ValueError(f"a lock kind is one of {', '.join(LOCK_KINDS)}")
    state.ensure_dirs()
    fd = _open_lock(state, os.O_RDWR | os.O_CREAT)
    for attempt in range(1, LOCK_ATTEMPTS + 1):
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            break
        except BlockingIOError:
            if attempt == LOCK_ATTEMPTS:
                holder = _read_kind(fd)
                os.close(fd)
                raise Busy(holder) from None
            sleep(LOCK_RETRY_S)
    os.ftruncate(fd, 0)
    os.pwrite(fd, kind.encode("ascii"), 0)
    return Lock(fd, kind)


def is_waker(kind: str | None) -> bool:
    """Whether a `probe_lock` result is a watcher or a waiter (the `status` wake path and
    the Stop guard's "no waker" test)."""
    return kind in WAKER_KINDS


def probe_lock(state: TeamState) -> str | None:
    """The kind of the process holding this team's sync lock, or None when no process
    does. A held lock is not always a waker: pass the result to `is_waker`. Never
    creates the file."""
    try:
        fd = _open_lock(state, os.O_RDONLY)
    except FileNotFoundError:
        return None
    try:
        try:
            fcntl.flock(fd, fcntl.LOCK_SH | fcntl.LOCK_NB)
        except BlockingIOError:
            return _read_kind(fd)
        fcntl.flock(fd, fcntl.LOCK_UN)
        return None
    finally:
        os.close(fd)
