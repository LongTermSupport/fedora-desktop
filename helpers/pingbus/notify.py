"""The client of Claude Code's per-session inbox socket, and when the watcher writes to it.

Spec: docs/agent-bus-protocol.md §15 (the notification is a fixed template of counts and
the watcher's notice number). Plan 00161's DESIGN.md section 6 (wake by the inbox socket;
`S` rises with every notice because the socket drops a body identical to the sender's
previous one; notify only when a pending item appears that was not pending at the last
look, so a reply that lands just after a `recv` emptied the inbox still notifies).

The wire format is what the U01 probe measured (plan journal, 26-10-06 16:12): one
connection per notice, carrying newline-delimited JSON, an `auth` line with the session's
token and then one `user` line; the client half-closes, the socket answers nothing and
closes. The probe saw the socket drop a body identical to the previous one from the same
sender for 30 s by default, and refill its rate bucket at 0.5 messages a second, which is
why notices are at least `NOTICE_MIN_INTERVAL_S` apart.
"""

from __future__ import annotations

import errno
import json
import os
import socket
import stat
from collections.abc import Callable, Iterable, Mapping

SOCKET_ENV = "CLAUDE_CODE_MESSAGING_SOCKET"
TOKEN_ENV = "CLAUDE_CODE_MESSAGING_TOKEN"

#: Spec §15, word for word (`test_notify` holds the document to it).
NOTICE_TEMPLATE = (
    "agent-bus: {total} pending ({humans} from humans, {pings} pings), notice {number}. "
    "Run `pingbus recv`."
)
#: How long one connection may take, from connect to the socket's close.
SEND_TIMEOUT_S = 5.0
#: The socket refills its bucket at 0.5 messages a second (U01).
NOTICE_MIN_INTERVAL_S = 2.0
_GONE_ERRNOS = (errno.ENOENT, errno.ECONNREFUSED, errno.ENOTSOCK)


class SocketGone(Exception):
    """The session's socket is gone: the session ended, so the watcher exits."""


def _count(value: object) -> int:
    if type(value) is not int or value < 0:
        raise ValueError("a count is a non-negative integer")
    return value


def notice_text(total: int, humans: int, pings: int, number: int) -> str:
    """The one notification text: counts and the notice number, nothing else."""
    if _count(humans) + _count(pings) != _count(total) or total == 0:
        raise ValueError("total must be humans plus pings, and more than none")
    if _count(number) < 1:
        raise ValueError("a notice number starts at 1")
    return NOTICE_TEMPLATE.format(total=total, humans=humans, pings=pings, number=number)


def session_socket(environ: Mapping[str, str]) -> tuple[str, str] | None:
    """The session's socket path and token, or None when this session has no socket. A
    socket without its token is refused: every notice authenticates."""
    path = environ.get(SOCKET_ENV, "")
    if not path:
        return None
    token = environ.get(TOKEN_ENV, "")
    if not token:
        raise ValueError(f"{SOCKET_ENV} is set but {TOKEN_ENV} is not")
    return path, token


def socket_present(path: str) -> bool:
    """Whether the session's socket file still exists (it goes when the session ends)."""
    try:
        return stat.S_ISSOCK(os.stat(path).st_mode)
    except FileNotFoundError:
        return False


def _line(obj: Mapping[str, object]) -> bytes:
    return (json.dumps(obj, ensure_ascii=True, separators=(",", ":")) + "\n").encode("ascii")


def frames(token: str, text: str) -> bytes:
    """What one connection carries: the auth line, then one user message."""
    if not token or not text:
        raise ValueError("a notice needs the session token and a text")
    return _line({"type": "auth", "token": token}) + _line(
        {"type": "user", "message": {"role": "user", "content": text}})


def send_notice(path: str, token: str, text: str, *, timeout_s: float = SEND_TIMEOUT_S) -> None:
    """Deliver one notice and wait for the socket to close the connection. `SocketGone`
    when nothing listens at `path`; any other socket error propagates."""
    payload = frames(token, text)
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
        sock.settimeout(timeout_s)
        try:
            sock.connect(path)
        except OSError as exc:
            if exc.errno in _GONE_ERRNOS:
                raise SocketGone(f"the session socket is gone ({exc.strerror})") from None
            raise
        sock.sendall(payload)
        sock.shutdown(socket.SHUT_WR)
        while sock.recv(4096):
            pass


class Notifier:
    """Decides when to notify: whenever a pending event ID appears that was not pending at
    the last look (a total that merely holds steady can hide a consumed item and a new
    one), with the counts current at the send, at most once per `min_interval_s`. A new
    item inside the interval is owed and sent by a later `observe` or `flush`; it is
    forgotten if nothing is pending by then. Every notice takes the next number, starting
    at `first_number`, so no two notices of one watcher are identical."""

    def __init__(self, send: Callable[[str], object], *, first_number: int,
                 clock: Callable[[], float], min_interval_s: float = NOTICE_MIN_INTERVAL_S) -> None:
        if type(first_number) is not int or first_number < 1:
            raise ValueError("the first notice number is a positive integer")
        self._send = send
        self._clock = clock
        self._interval = min_interval_s
        self.number = first_number
        self._last_ids: frozenset[str] = frozenset()
        self._last_sent: float | None = None
        self._owed: tuple[int, int, int] | None = None

    def observe(self, counts: tuple[int, int, int], ids: Iterable[str]) -> bool:
        """Take the current (total, humans, pings) and the pending event IDs; True when a
        notice was sent."""
        current = frozenset(ids)
        total = counts[0]
        if total > 0 and not current <= self._last_ids:
            self._owed = counts
        elif self._owed is not None:
            self._owed = counts if total > 0 else None
        self._last_ids = current
        return self.flush()

    def flush(self) -> bool:
        """Send an owed notice once the interval allows; True when one was sent."""
        if self._owed is None:
            return False
        now = self._clock()
        if self._last_sent is not None and now - self._last_sent < self._interval:
            return False
        total, humans, pings = self._owed
        self._send(notice_text(total, humans, pings, self.number))
        self.number += 1
        self._owed = None
        self._last_sent = now
        return True
