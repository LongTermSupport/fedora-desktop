"""A team's watcher as its supervisor and the watcher last left it: `state/watcher.json`.

Spec: docs/agent-bus-protocol.md §12 and §13 (`watch --supervise`, `status`); issue #116.

A supervised watcher that keeps exiting must not be visible only in `watch.log`. Its supervisor
records each exit it restarts the watcher after (`failing`, counted on until a sync succeeds),
and a team a restart cannot help is recorded `stopped`: by the watcher, for a team it dropped
for a permanent failure, and by the supervisor, for a final exit. The watcher's first sync that
succeeds removes the record. `pingbus status` prints it as a `WATCHER` line, and the
SessionStart hook names it. The file is replaced atomically and is mode 0600, like every state
file; its cause is one printable ASCII line, so it can be printed as one report field.
"""

from __future__ import annotations

import dataclasses
import os
import pathlib

from helpers.pingbus import inbox

FILE = "watcher.json"
VERSION = 1
FAILING = "failing"
STOPPED = "stopped"
STATES = (FAILING, STOPPED)
CAUSE_MAX = 200
_KEYS = {"v", "state", "cause", "restarts", "since_ms", "at_ms"}


@dataclasses.dataclass(frozen=True)
class Record:
    state: str
    cause: str
    #: Restarts since the last successful sync.
    restarts: int
    #: When the first of them happened, and when this record was written (ms).
    since_ms: int
    at_ms: int


def path(state_dir: pathlib.Path) -> pathlib.Path:
    return pathlib.Path(state_dir) / FILE


def cause_text(text: str) -> str:
    """`text` as one printable ASCII line of at most `CAUSE_MAX` characters; `-` when empty."""
    line = "".join(c if " " <= c <= "~" else "?" for c in text.strip())
    if len(line) > CAUSE_MAX:
        line = line[:CAUSE_MAX - 3] + "..."
    return line or "-"


def _count(value: object) -> bool:
    return type(value) is int and value >= 0


def _valid(data: object) -> bool:
    return (isinstance(data, dict) and set(data) == _KEYS and data["v"] == VERSION
            and data["state"] in STATES and isinstance(data["cause"], str)
            and 0 < len(data["cause"]) <= CAUSE_MAX and data["cause"] == cause_text(data["cause"])
            and all(_count(data[key]) for key in ("restarts", "since_ms", "at_ms")))


def read(state_dir: pathlib.Path) -> Record | None:
    """The record, or None when there is none. `inbox.StateError` when it is malformed."""
    target = path(state_dir)
    data = inbox.read_json_file(target)
    if data is None:
        return None
    if not _valid(data):
        raise inbox.StateError(f"{target} is malformed")
    return Record(data["state"], data["cause"], data["restarts"], data["since_ms"], data["at_ms"])


def _write(state_dir: pathlib.Path, record: Record) -> Record:
    os.makedirs(state_dir, mode=inbox.DIR_MODE, exist_ok=True)
    inbox.write_json_file(path(state_dir), {"v": VERSION, **dataclasses.asdict(record)})
    return record


def _previous(state_dir: pathlib.Path) -> Record | None:
    """The record this one follows; a malformed one is the writer's own and is replaced."""
    try:
        return read(state_dir)
    except inbox.StateError:
        return None


def record_exit(state_dir: pathlib.Path, cause: str, now_ms: int) -> Record:
    """The supervisor restarts the watcher after an exit: counted on from a `failing` record."""
    previous = _previous(state_dir)
    if previous is not None and previous.state == FAILING:
        return _write(state_dir, Record(FAILING, cause_text(cause), previous.restarts + 1,
                                        previous.since_ms, now_ms))
    return _write(state_dir, Record(FAILING, cause_text(cause), 1, now_ms, now_ms))


def record_stop(state_dir: pathlib.Path, cause: str, now_ms: int) -> Record:
    """Nothing will restart this team's watcher: a restart cannot change what it met."""
    previous = _previous(state_dir)
    restarts, since = (previous.restarts, previous.since_ms) if previous is not None else (0, now_ms)
    return _write(state_dir, Record(STOPPED, cause_text(cause), restarts, since, now_ms))


def clear(state_dir: pathlib.Path) -> None:
    """The team synced: its watcher works."""
    path(state_dir).unlink(missing_ok=True)
