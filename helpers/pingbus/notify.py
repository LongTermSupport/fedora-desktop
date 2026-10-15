"""The client of Claude Code's per-session inbox socket.

Spec: docs/agent-bus-protocol.md §15 (what the watcher writes: a delivery envelope, Plan
00194 SPEC-inject.md); Plan 00161's DESIGN.md section 6 (wake by the inbox socket).

The wire format is what the U01 probe measured (plan journal, 26-10-06 16:12): one
connection per notice, carrying newline-delimited JSON, an `auth` line with the session's
token and then one `user` line; the client half-closes, the socket answers nothing and
closes. The probe saw the socket drop a body identical to the previous one from the same
sender for 30 s by default, and refill its rate bucket at 0.5 messages a second, which is
why deliveries are at least `NOTICE_MIN_INTERVAL_S` apart (and each carries its own nonce).
"""

from __future__ import annotations

import errno
import json
import os
import socket
import stat
from collections.abc import Mapping

SOCKET_ENV = "CLAUDE_CODE_MESSAGING_SOCKET"
TOKEN_ENV = "CLAUDE_CODE_MESSAGING_TOKEN"

#: How long one connection may take, from connect to the socket's close.
SEND_TIMEOUT_S = 5.0
#: The socket refills its bucket at 0.5 messages a second (U01).
NOTICE_MIN_INTERVAL_S = 2.0
_GONE_ERRNOS = (errno.ENOENT, errno.ECONNREFUSED, errno.ENOTSOCK)


class SocketGone(Exception):
    """The session's socket is gone: the session ended, so the watcher exits."""


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


def send_notice(path: str, token: str, text: str, *, timeout_s: float = SEND_TIMEOUT_S) -> bool:
    """Write one message and wait up to `timeout_s` for the socket to close the connection.
    `SocketGone` when nothing listens at `path`; any other error before the payload was
    written in full propagates, and the message is not sent. Once it was written in full it
    is sent: True when the close came in time, False when it did not or the close path
    failed (SPEC-inject.md E11)."""
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
        try:
            sock.shutdown(socket.SHUT_WR)
            while sock.recv(4096):
                pass
        except OSError:
            # Written in full is sent (SPEC-inject.md E11): a timeout, a reset or a broken pipe
            # on the close path only means the close was not seen.
            return False
    return True
