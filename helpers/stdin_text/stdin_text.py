"""A read of stdin that never waits in silence (Defence Before Fix, issue #260).

A read of stdin returns at end of input. When stdin is an open pipe that nothing writes to and
nothing closes, which is what an agent's shell gives a command run with no input, the read never
returns and nothing says why. This is the one place a raw read of stdin is made
(.semgrep/stdin-wait.yml, rule stdin-read-unbounded): it says on stderr that it is waiting once
nothing has arrived for NOTICE_AFTER_S, then waits on, so a slow producer still works. A caller
that must not wait for ever gives `give_up_s`, and one whose input is never typed at a terminal
gives `refuse_terminal`; both raise NoInput, which the caller reports as its own usage error.
"""

from __future__ import annotations

import select
import sys
from collections.abc import Callable
from typing import IO, AnyStr, BinaryIO, TextIO

#: How long nothing may arrive before the reader says it is waiting.
NOTICE_AFTER_S = 1.0

#: A wait for the descriptor to be readable (data, or end of input) within a timeout.
Wait = Callable[[int, float], bool]


class NoInput(Exception):
    """Stdin gave no input the caller can use: a terminal it refused, or nothing in time."""


def readable(fd: int, timeout_s: float) -> bool:
    """Whether `fd` has data or has reached end of input within `timeout_s` seconds."""
    ready, _, _ = select.select([fd], [], [], timeout_s)
    return bool(ready)


def _descriptor(stream: IO[AnyStr]) -> int | None:
    """The stream's descriptor, or None for one with none (an in-memory stream in a test)."""
    try:
        return stream.fileno()
    except (AttributeError, OSError, ValueError):
        return None


def _await(stream: IO[AnyStr], *, prog: str, err: TextIO, give_up_s: float | None,
           refuse_terminal: bool, wait: Wait) -> None:
    if refuse_terminal and stream.isatty():
        raise NoInput("stdin is a terminal; give the input with a pipe or a file (< FILE)")
    fd = _descriptor(stream)
    if fd is None or wait(fd, NOTICE_AFTER_S):
        return
    err.write(f"{prog}: waiting for input on stdin, which has not ended; pipe it in, "
              "or end it (ctrl-D, or < /dev/null for none)\n")
    err.flush()
    if give_up_s is not None and not wait(fd, max(give_up_s - NOTICE_AFTER_S, 0.0)):
        raise NoInput(f"no input arrived on stdin within {give_up_s:g} s")


def read(stream: IO[AnyStr], *, prog: str, err: TextIO = sys.stderr, limit: int = -1,
         give_up_s: float | None = None, refuse_terminal: bool = False,
         wait: Wait = readable) -> AnyStr:
    """Up to `limit` bytes (characters, of a text stream) of `stream`, all of it for -1."""
    _await(stream, prog=prog, err=err, give_up_s=give_up_s, refuse_terminal=refuse_terminal, wait=wait)
    return stream.read(limit)


def read_line(stream: IO[AnyStr], *, prog: str, err: TextIO = sys.stderr, limit: int = -1,
              give_up_s: float | None = None, refuse_terminal: bool = False,
              wait: Wait = readable) -> AnyStr:
    """One line of `stream`, newline included, as a password prompt's input is read."""
    _await(stream, prog=prog, err=err, give_up_s=give_up_s, refuse_terminal=refuse_terminal, wait=wait)
    return stream.readline(limit)


def read_text(stream: BinaryIO, *, prog: str, err: TextIO = sys.stderr, limit: int = -1,
              give_up_s: float | None = None, refuse_terminal: bool = False,
              wait: Wait = readable) -> str:
    """`read`, decoded as UTF-8; a decode error is the caller's to report."""
    return read(stream, prog=prog, err=err, limit=limit, give_up_s=give_up_s,
                refuse_terminal=refuse_terminal, wait=wait).decode("utf-8")
