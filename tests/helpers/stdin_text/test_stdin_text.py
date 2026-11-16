"""Tests for helpers.stdin_text: a read of stdin that never waits in silence (issue #260).

A read of an open pipe that nothing writes to or closes never returns. The reader says on stderr
that it is waiting once nothing has arrived for a moment, and a caller that must not wait for
ever gives it a bound. The waits are injected, so no test reads the clock; the real wait is
proven on real pipes with a zero timeout, which needs no clock either.
"""

from __future__ import annotations

import io
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.stdin_text import stdin_text


class _Waits:
    """A wait that answers from a script and records each timeout it was asked for."""

    def __init__(self, *answers: bool) -> None:
        self.answers = list(answers)
        self.asked: list[float] = []

    def __call__(self, _fd: int, timeout_s: float) -> bool:
        self.asked.append(timeout_s)
        return self.answers.pop(0)


class _Pipe:
    """A real pipe's read end as a binary stream, closed when the test ends."""

    def __init__(self, case: unittest.TestCase, data: bytes = b"", *, close_writer: bool) -> None:
        read_fd, write_fd = os.pipe()
        if data:
            os.write(write_fd, data)
        if close_writer:
            os.close(write_fd)
        else:
            case.addCleanup(os.close, write_fd)
        self.stream = os.fdopen(read_fd, "rb")
        case.addCleanup(self.stream.close)


class _Terminal(io.BytesIO):
    def isatty(self) -> bool:
        return True


class TestRead(unittest.TestCase):
    def setUp(self) -> None:
        self.err = io.StringIO()

    def test_ready_input_is_read_with_no_notice(self) -> None:
        waits = _Waits(True)
        pipe = _Pipe(self, b"hello\n", close_writer=True)
        got = stdin_text.read(pipe.stream, prog="tool", err=self.err, wait=waits)
        self.assertEqual(got, b"hello\n")
        self.assertEqual(self.err.getvalue(), "")
        self.assertEqual(waits.asked, [stdin_text.NOTICE_AFTER_S])

    def test_a_slow_start_says_it_is_waiting_then_reads(self) -> None:
        waits = _Waits(False)
        pipe = _Pipe(self, b"late", close_writer=True)
        got = stdin_text.read(pipe.stream, prog="tool", err=self.err, wait=waits)
        self.assertEqual(got, b"late")
        self.assertIn("tool: waiting for input on stdin", self.err.getvalue())
        self.assertEqual(waits.asked, [stdin_text.NOTICE_AFTER_S])

    def test_a_bound_refuses_when_nothing_arrives(self) -> None:
        waits = _Waits(False, False)
        pipe = _Pipe(self, close_writer=False)
        with self.assertRaises(stdin_text.NoInput) as caught:
            stdin_text.read(pipe.stream, prog="tool", err=self.err, give_up_s=60.0, wait=waits)
        self.assertIn("60", str(caught.exception))
        self.assertIn("waiting for input", self.err.getvalue())
        self.assertEqual(waits.asked, [stdin_text.NOTICE_AFTER_S, 60.0 - stdin_text.NOTICE_AFTER_S])

    def test_a_bound_reads_what_arrives_after_the_notice(self) -> None:
        waits = _Waits(False, True)
        pipe = _Pipe(self, b"x", close_writer=True)
        got = stdin_text.read(pipe.stream, prog="tool", err=self.err, give_up_s=60.0, wait=waits)
        self.assertEqual(got, b"x")

    def test_the_limit_caps_the_read(self) -> None:
        pipe = _Pipe(self, b"abcdef", close_writer=True)
        got = stdin_text.read(pipe.stream, prog="tool", err=self.err, limit=3, wait=_Waits(True))
        self.assertEqual(got, b"abc")

    def test_a_line_read_stops_at_the_newline(self) -> None:
        pipe = _Pipe(self, b"secret\nmore\n", close_writer=True)
        got = stdin_text.read_line(pipe.stream, prog="tool", err=self.err, wait=_Waits(True))
        self.assertEqual(got, b"secret\n")

    def test_a_terminal_is_refused_only_when_asked(self) -> None:
        with self.assertRaises(stdin_text.NoInput) as caught:
            stdin_text.read(_Terminal(b"x"), prog="tool", err=self.err, refuse_terminal=True)
        self.assertIn("terminal", str(caught.exception))
        self.assertEqual(stdin_text.read(_Terminal(b"x"), prog="tool", err=self.err), b"x")

    def test_a_stream_with_no_descriptor_is_read_directly(self) -> None:
        got = stdin_text.read(io.BytesIO(b"in memory"), prog="tool", err=self.err, wait=_Waits())
        self.assertEqual(got, b"in memory")

    def test_a_text_stream_reads_as_text(self) -> None:
        pipe = _Pipe(self, b"line one\nline two\n", close_writer=True)
        with os.fdopen(os.dup(pipe.stream.fileno()), "r", encoding="utf-8") as text:
            self.assertEqual(stdin_text.read_line(text, prog="tool", err=self.err, wait=_Waits(True)),
                             "line one\n")

    def test_text_is_utf8_with_the_reads_bounds(self) -> None:
        pipe = _Pipe(self, "café\n".encode(), close_writer=True)
        self.assertEqual(stdin_text.read_text(pipe.stream, prog="tool", err=self.err, wait=_Waits(True)),
                         "café\n")


class TestReadable(unittest.TestCase):
    """The real wait, on real pipes, with a zero timeout: no clock is read."""

    def test_data_is_readable(self) -> None:
        pipe = _Pipe(self, b"x", close_writer=False)
        self.assertTrue(stdin_text.readable(pipe.stream.fileno(), 0))

    def test_end_of_input_is_readable(self) -> None:
        pipe = _Pipe(self, close_writer=True)
        self.assertTrue(stdin_text.readable(pipe.stream.fileno(), 0))

    def test_an_open_empty_pipe_is_not(self) -> None:
        pipe = _Pipe(self, close_writer=False)
        self.assertFalse(stdin_text.readable(pipe.stream.fileno(), 0))


if __name__ == "__main__":
    unittest.main()
