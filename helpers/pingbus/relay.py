"""Relay delivery: the spool a supervisor plugin types from, and its stall clock.

Spec: Plan 00194 DESIGN.md sections 2.1, 2.2, 2.7 and "Spool line format";
docs/agent-bus-protocol.md §12 (`PINGBUS_HOME/relay/`) and §15 (the spool line).

`PINGBUS_DELIVERY=relay` is set by the launch (never by a checkout's config) and switches
`pingbus watch` from waking the session by its inbox socket to filling this spool. The
spool holds only lines `watch` built from events it re-fetched and re-validated, so what
sits in it is as trustworthy as what `recv` prints. It lives under `PINGBUS_HOME`, which a
ccy session builds private to its container.

Layout, per team, below `PINGBUS_HOME/relay/`:

- `spool/<team>/<event id>.line`: one §15 line plus a final `cut` field, written
  atomically by `watch`;
- `relayed/<team>/<event id>`: an empty marker the plugin writes once the supervisor has
  typed the line; `watch` then consumes the item and removes both files;
- `stalled`: present while items wait and no relay was confirmed for `STALL_AFTER_S`.
"""

from __future__ import annotations

import contextlib
import fcntl
import os
import pathlib
import re
import secrets
from collections.abc import Callable, Iterator, Mapping

from helpers.pingbus import config, protocol

ENV = "PINGBUS_DELIVERY"
MODE_RELAY = "relay"

RELAY_DIR = "relay"
SPOOL_DIR = "spool"
RELAYED_DIR = "relayed"
STALLED_FILE = "stalled"
LINE_SUFFIX = ".line"

#: Decision F: a body longer than this many UTF-8 bytes is cut, on a character boundary,
#: before it is escaped.
BODY_MAX_BYTES = 2048
CUT_NONE = "-"
#: No relay confirmed for this long while items wait: `relay=stalled` (DESIGN.md section 2.7).
STALL_AFTER_S = 120

DIR_MODE = 0o700
FILE_MODE = 0o600


def enabled(environ: Mapping[str, str]) -> bool:
    """Whether this process delivers by relay. Unset or empty is off; anything but `relay`
    is a configuration error, so a typo never silently falls back to the socket."""
    value = environ.get(ENV, "")
    if value == "":
        return False
    if value != MODE_RELAY:
        raise config.ConfigError(f"{ENV} must be unset or '{MODE_RELAY}'")
    return True


def cap_text(text: str) -> tuple[str, str]:
    """(the text to show, its `cut` field): the whole text and `-` when it fits
    `BODY_MAX_BYTES`, else its first bytes up to a character boundary and `shown/total`
    in bytes."""
    raw = text.encode("utf-8", "surrogatepass")
    if len(raw) <= BODY_MAX_BYTES:
        return text, CUT_NONE
    shown = raw[:BODY_MAX_BYTES].decode("utf-8", "ignore")
    return shown, f"{len(shown.encode('utf-8', 'surrogatepass'))}/{len(raw)}"


#: Between the event ID and the silent target in a TIMEOUT's key. One ping with several
#: silent targets owes one TIMEOUT line each, so the event ID alone cannot name them.
KEY_SEP = "~"
_TARGET_RE = re.compile(r"[a-z0-9][a-z0-9._+-]{0,127}")
LOCK_FILE = "lock"


def key(event_id: str, target: str) -> str:
    """The spool and marker name of the TIMEOUT for `target` (a localpart) of ping `event_id`."""
    return f"{event_id}{KEY_SEP}{target}"


def split_key(name: str) -> tuple[str, str | None]:
    """(event ID, target localpart or None) of a spool key; ValueError if it is neither an
    event ID nor an event ID, `KEY_SEP` and a localpart."""
    event_id, sep, target = name.partition(KEY_SEP)
    if not protocol.is_event_id(event_id):
        raise ValueError("not an event ID")
    if not sep:
        return event_id, None
    if _TARGET_RE.fullmatch(target) is None:
        raise ValueError("not a target")
    return event_id, target


def _valid_key(name: str) -> bool:
    try:
        split_key(name)
    except ValueError:
        return False
    return True


def _check(team: str, name: str) -> None:
    if not protocol.is_team_name(team):
        raise ValueError("not a team name")
    split_key(name)


class Spool:
    """The relay directory of one `PINGBUS_HOME`. Reads create nothing."""

    def __init__(self, home: pathlib.Path) -> None:
        self.root = pathlib.Path(home) / RELAY_DIR

    def _spool(self, team: str) -> pathlib.Path:
        return self.root / SPOOL_DIR / team

    def _relayed(self, team: str) -> pathlib.Path:
        return self.root / RELAYED_DIR / team

    @contextlib.contextmanager
    def exclusive(self, *, block: bool = True) -> Iterator[None]:
        """The one lock that makes the watcher's check-and-spool and `recv`'s
        print-and-consume mutually exclusive, so an item is typed or printed, never both.
        Hold it only around local file work: no network call belongs inside."""
        os.makedirs(self.root, mode=DIR_MODE, exist_ok=True)
        fd = os.open(self.root / LOCK_FILE, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, FILE_MODE)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | (0 if block else fcntl.LOCK_NB))
            yield
        finally:
            os.close(fd)

    def has(self, team: str, event_id: str) -> bool:
        _check(team, event_id)
        return (self._spool(team) / f"{event_id}{LINE_SUFFIX}").is_file()

    def ids(self, team: str) -> frozenset[str]:
        return frozenset(self._ids(self._spool(team), LINE_SUFFIX))

    def confirmed(self, team: str) -> tuple[str, ...]:
        return tuple(sorted(self._ids(self._relayed(team), "")))

    @staticmethod
    def _ids(directory: pathlib.Path, suffix: str) -> list[str]:
        try:
            names = os.listdir(directory)
        except FileNotFoundError:
            return []
        found = []
        for name in names:
            stem = name[: len(name) - len(suffix)] if suffix else name
            if name.endswith(suffix) and _valid_key(stem):
                found.append(stem)
        return found

    def waiting(self) -> int:
        """Lines spooled and not yet finished, over every team."""
        try:
            teams = os.listdir(self.root / SPOOL_DIR)
        except FileNotFoundError:
            return 0
        return sum(len(self._ids(self._spool(team), LINE_SUFFIX)) for team in teams
                   if protocol.is_team_name(team))

    def write(self, team: str, event_id: str, line: str) -> None:
        """Spool `line` atomically: a reader sees the whole line or no file."""
        _check(team, event_id)
        if "\n" in line or "\r" in line:
            raise ValueError("a spool line is one line")
        directory = self._spool(team)
        os.makedirs(directory, mode=DIR_MODE, exist_ok=True)
        path = directory / f"{event_id}{LINE_SUFFIX}"
        tmp = directory / f".{event_id}{LINE_SUFFIX}.{secrets.token_hex(8)}.tmp"
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, FILE_MODE)
        try:
            with os.fdopen(fd, "wb") as handle:
                handle.write(line.encode("utf-8") + b"\n")
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(tmp, path)
        except BaseException:
            tmp.unlink(missing_ok=True)
            raise

    def withdraw(self, team: str, event_id: str) -> None:
        """An item `recv` consumed first: it must not be typed. A line already typed has
        its marker; that is cleaned by `finish`."""
        _check(team, event_id)
        (self._spool(team) / f"{event_id}{LINE_SUFFIX}").unlink(missing_ok=True)

    def finish(self, team: str, event_id: str) -> None:
        """Delivered (or withdrawn): neither the line nor the marker is wanted any more."""
        self.withdraw(team, event_id)
        (self._relayed(team) / event_id).unlink(missing_ok=True)

    def stalled(self) -> bool:
        return (self.root / STALLED_FILE).is_file()

    def set_stalled(self, value: bool) -> None:
        path = self.root / STALLED_FILE
        if not value:
            path.unlink(missing_ok=True)
            return
        os.makedirs(self.root, mode=DIR_MODE, exist_ok=True)
        os.close(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, FILE_MODE))


class Stall:
    """Whether relay has stalled: items wait and no relay was confirmed for `after_s`.
    The window starts when items first wait, and restarts on every confirmation."""

    def __init__(self, clock: Callable[[], float], after_s: float = STALL_AFTER_S) -> None:
        self._clock = clock
        self._after_s = after_s
        self._since: float | None = None

    def observe(self, *, waiting: int, confirmed: int) -> bool:
        now = self._clock()
        if waiting == 0:
            self._since = None
            return False
        if confirmed > 0 or self._since is None:
            self._since = now
            return False
        return now - self._since >= self._after_s
