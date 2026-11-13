"""A secret shown so that the terminal keeps no copy of it (rule secret-on-normal-screen).

A terminal keeps what its normal screen showed in the scrollback, and tmux in a pane's history,
so a password printed there "once" stays readable long after. `show_secret` writes a secret to a
binary stream: when the stream is not a terminal (a capture, a pipe, a file) the secret is the
payload and is written as given; on a terminal it is shown on the alternate screen, which keeps
no scrollback, and the screen is left once Enter is read from the terminal, however that ends.
The Go twin is term.SecretScreen; the shell's is scripts/secret-screen.bash.
"""

from __future__ import annotations

from collections.abc import Callable
from typing import BinaryIO, TextIO

ALT_SCREEN_ON = "\x1b[?1049h\x1b[H\x1b[2J"
ALT_SCREEN_OFF = "\x1b[?1049l"
PROMPT = "\nCopy it, then press Enter to clear it from the screen.\n"
TTY_PATH = "/dev/tty"


class NoEnterError(Exception):
    """No Enter could be read from the terminal; the screen was cleared all the same."""


def _open_tty() -> TextIO:
    return open(TTY_PATH, encoding="utf-8")


def show_secret(text: str, stream: BinaryIO, *, open_tty: Callable[[], TextIO] = _open_tty) -> None:
    """Write text, which holds a secret, to stream: plainly, or on a terminal's alternate screen."""
    if not stream.isatty():
        stream.write(text.encode())
        stream.flush()
        return
    stream.write((ALT_SCREEN_ON + text + PROMPT).encode())
    stream.flush()
    try:
        try:
            tty = open_tty()
        except OSError as exc:
            raise NoEnterError(f"could not open the terminal to read the Enter: {exc}") from exc
        with tty:
            if not tty.readline():
                raise NoEnterError("no Enter was read; the screen is cleared")
    finally:
        stream.write(ALT_SCREEN_OFF.encode())
        stream.flush()
