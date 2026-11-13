"""Unit tests for helpers/agent_bus/screen.py: a secret shown so the terminal keeps no copy.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_screen

Rule secret-on-normal-screen (CLAUDE/QA.md): not a terminal, the secret is the payload, written
as given; on a terminal it is shown on the alternate screen, which keeps no scrollback, and the
screen is left once Enter is read from the terminal, however that ends.
"""

from __future__ import annotations

import io
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import screen


class _Stream(io.BytesIO):
    def __init__(self, terminal: bool) -> None:
        super().__init__()
        self.terminal = terminal

    def isatty(self) -> bool:
        return self.terminal


def _tty(answer: str | None):
    def open_tty():
        if answer is None:
            raise OSError("no terminal")
        return io.StringIO(answer)

    return open_tty


class ShowTest(unittest.TestCase):
    def test_not_a_terminal_is_the_payload_as_given(self) -> None:
        for text in ("value\n", "two\nlines\n\n", "no newline", ""):
            out = _Stream(terminal=False)
            screen.show_secret(text, out, open_tty=_tty(None))
            self.assertEqual(out.getvalue(), text.encode())

    def test_a_terminal_shows_it_on_the_alternate_screen_until_enter(self) -> None:
        out = _Stream(terminal=True)
        screen.show_secret("value\n", out, open_tty=_tty("\n"))
        self.assertEqual(out.getvalue(),
                         (screen.ALT_SCREEN_ON + "value\n" + screen.PROMPT + screen.ALT_SCREEN_OFF).encode())

    def test_no_enter_still_leaves_the_screen_and_raises(self) -> None:
        out = _Stream(terminal=True)
        with self.assertRaises(screen.NoEnterError):
            screen.show_secret("value\n", out, open_tty=_tty(""))
        self.assertTrue(out.getvalue().endswith(screen.ALT_SCREEN_OFF.encode()))

    def test_no_terminal_to_read_enter_from_still_leaves_the_screen_and_raises(self) -> None:
        out = _Stream(terminal=True)
        with self.assertRaises(screen.NoEnterError):
            screen.show_secret("value\n", out, open_tty=_tty(None))
        self.assertTrue(out.getvalue().endswith(screen.ALT_SCREEN_OFF.encode()))

    def test_the_default_terminal_is_dev_tty(self) -> None:
        self.assertEqual(screen.TTY_PATH, "/dev/tty")


if __name__ == "__main__":
    unittest.main()
