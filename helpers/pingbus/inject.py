"""Socket delivery's per-session state, and the check that a delivery reached the conversation.

Spec: Plan 00194 SPEC-inject.md E15, E16 and E18; docs/agent-bus-protocol.md §12.

The watcher and the session's hooks share `PINGBUS_HOME`, which on the host is one directory
per user shared by every team session, so each file here is named by the session's own inbox
socket path (`CLAUDE_CODE_MESSAGING_SOCKET`), which the watcher and the hooks both have:

- `<key>.stalled`: deliveries have failed for `relay.STALL_AFTER_S`;
- `<key>.missing`: a delivery was written to the socket and is not in the transcript;
- `<key>.transcript`: the session's transcript path, written by the hooks from their payload;
- `<key>.watch`: the session's watch lock, held by its one supervisor or watcher (E26);
- `<key>.spawned`: when the hooks last started the watcher, in ms (E26).

A delivery is found when its first line's prefix, `agent-bus delivery <nonce>:` (ASCII, with no
character JSON escapes), appears in what the transcript gained after the send. Claude Code
writes a socket message there as a user entry, or as a `queued_command` attachment when it
arrives during a turn. While the transcript grows the session is working and the message may be
queued; once it has been quiet for `MISSING_QUIET_S`, or after `MISSING_GIVE_UP_S` in any case,
a delivery not found is missing: Claude Code is holding or has dropped it.
"""

from __future__ import annotations

import dataclasses
import hashlib
import os
import pathlib
import secrets
from collections.abc import Callable

INJECT_DIR = "inject"
KEY_HEX = 16
DIR_MODE = 0o700
FILE_MODE = 0o600
TRANSCRIPT_MAX_PATH = 4096
MISSING_QUIET_S = 180.0
MISSING_GIVE_UP_S = 3600.0
#: At most this much of the transcript is read in one poll; the rest waits for the next.
SCAN_MAX_BYTES = 16 << 20
NEEDLE = "agent-bus delivery {nonce}:"


def session_key(socket_path: str) -> str:
    return hashlib.sha256(socket_path.encode("utf-8", "surrogatepass")).hexdigest()[:KEY_HEX]


def _transcript_path(value: object) -> str:
    if (not isinstance(value, str) or not value.startswith("/") or len(value) > TRANSCRIPT_MAX_PATH
            or any(c in value for c in "\x00\n\r")):
        raise ValueError("a transcript path is an absolute path on one line")
    return value


class Marks:
    """The files of one session under `PINGBUS_HOME/inject/`. Reads create nothing."""

    def __init__(self, home: pathlib.Path, socket_path: str) -> None:
        self.dir = pathlib.Path(home) / INJECT_DIR
        self.key = session_key(socket_path)

    def _path(self, name: str) -> pathlib.Path:
        return self.dir / f"{self.key}.{name}"

    def ensure_dir(self) -> None:
        os.makedirs(self.dir, mode=DIR_MODE, exist_ok=True)

    def watch_lock_path(self) -> pathlib.Path:
        """The session's watch lock (E26): held by its one supervisor or watcher. It is never
        removed, so a holder and a newcomer always lock the same file."""
        return self._path("watch")

    def spawned_ms(self) -> int | None:
        """When the hooks last started this session's watcher, or None."""
        try:
            text = self._path("spawned").read_text(encoding="ascii")
        except (OSError, UnicodeDecodeError):
            return None
        return int(text) if text.isdigit() and len(text) <= 16 else None

    def set_spawned(self, now_ms: int) -> None:
        self.ensure_dir()
        path = self._path("spawned")
        tmp = self.dir / f".{path.name}.{secrets.token_hex(8)}.tmp"
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, FILE_MODE)
        with os.fdopen(fd, "w", encoding="ascii") as handle:
            handle.write(str(now_ms))
        os.replace(tmp, path)

    def _set(self, name: str, value: bool) -> None:
        path = self._path(name)
        if not value:
            path.unlink(missing_ok=True)
            return
        os.makedirs(self.dir, mode=DIR_MODE, exist_ok=True)
        os.close(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, FILE_MODE))

    def stalled(self) -> bool:
        return self._path("stalled").is_file()

    def set_stalled(self, value: bool) -> None:
        self._set("stalled", value)

    def missing(self) -> bool:
        return self._path("missing").is_file()

    def set_missing(self, value: bool) -> None:
        self._set("missing", value)

    def forget(self) -> None:
        """Remove this session's files: it ended, and its socket path may be reused."""
        for name in ("stalled", "missing", "transcript", "spawned"):
            self._path(name).unlink(missing_ok=True)

    def record_transcript(self, path: object) -> None:
        """Write the session's transcript path atomically. ValueError for a bad path."""
        text = _transcript_path(path)
        os.makedirs(self.dir, mode=DIR_MODE, exist_ok=True)
        target = self._path("transcript")
        tmp = self.dir / f".{target.name}.{secrets.token_hex(8)}.tmp"
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, FILE_MODE)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                handle.write(text)
            os.replace(tmp, target)
        except BaseException:
            tmp.unlink(missing_ok=True)
            raise

    def transcript(self) -> str | None:
        """The recorded transcript path, or None when there is none or it fails its grammar."""
        try:
            fd = os.open(self._path("transcript"), os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        except OSError:
            return None
        with os.fdopen(fd, "rb") as handle:
            raw = handle.read(TRANSCRIPT_MAX_PATH + 1)
        try:
            return _transcript_path(raw.decode("utf-8"))
        except (UnicodeDecodeError, ValueError):
            return None


@dataclasses.dataclass
class _Outstanding:
    nonce: str
    path: str | None
    scanned: int
    size_seen: int
    sent_at: float
    quiet_since: float


def _size(path: str | None) -> int | None:
    if path is None:
        return None
    try:
        return os.stat(path).st_size
    except OSError:
        return None


def _contains(path: str, start: int, end: int, needle: bytes) -> bool:
    try:
        with open(path, "rb") as handle:
            handle.seek(start)
            data = handle.read(end - start)
    except OSError:
        return False
    return needle in data


class DeliveryCheck:
    """Each delivery written to the socket, until it is found in the transcript or declared
    missing; the session's missing mark follows."""

    def __init__(self, marks: Marks, clock: Callable[[], float], *, quiet_s: float | None = None,
                 give_up_s: float | None = None) -> None:
        self._marks = marks
        self._clock = clock
        self._quiet_s = MISSING_QUIET_S if quiet_s is None else quiet_s
        self._give_up_s = MISSING_GIVE_UP_S if give_up_s is None else give_up_s
        self._waiting: list[_Outstanding] = []

    def prepare(self) -> tuple[str | None, int]:
        """The transcript and its size just before a send: Claude Code may write the message
        into it before `sent` runs."""
        path = self._marks.transcript()
        return path, _size(path) or 0

    def sent(self, nonce: str, before: tuple[str | None, int]) -> None:
        path, size = before
        now = self._clock()
        self._waiting.append(_Outstanding(nonce, path, size, size, now, now))

    def outstanding(self) -> int:
        return len(self._waiting)

    def poll(self) -> tuple[list[str], list[str]]:
        """(nonces found, nonces declared missing) since the last poll."""
        found: list[str] = []
        missing: list[str] = []
        path = self._marks.transcript()
        size = _size(path)
        for item in list(self._waiting):
            now = self._clock()
            if path is not None and item.path != path:
                item.path, item.scanned, item.size_seen = path, 0, 0
            if size is not None and size < item.scanned:
                item.scanned = item.size_seen = 0
            if path is not None and size is not None and size > item.size_seen:
                needle = NEEDLE.format(nonce=item.nonce).encode("ascii")
                start = max(0, item.scanned - len(needle))
                end = min(size, start + SCAN_MAX_BYTES)
                if _contains(path, start, end, needle):
                    found.append(item.nonce)
                    self._waiting.remove(item)
                    continue
                item.scanned = item.size_seen = end
                item.quiet_since = now
            if now - item.quiet_since >= self._quiet_s or now - item.sent_at >= self._give_up_s:
                missing.append(item.nonce)
                self._waiting.remove(item)
        if found and not missing:
            self._marks.set_missing(False)
        if missing:
            self._marks.set_missing(True)
        return found, missing
